package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/zalando/go-keyring"
)

const ServiceName = "forge"

type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type osKeyring struct{}

func (k osKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (k osKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (k osKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// MockKeyring is an in-memory Keyring implementation for unit tests.
type MockKeyring struct {
	mu    sync.Mutex
	items map[string]string
}

func NewMockKeyring() *MockKeyring {
	return &MockKeyring{items: make(map[string]string)}
}

func (m *MockKeyring) Get(service, user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := service + ":" + user
	if val, ok := m.items[key]; ok {
		return val, nil
	}
	return "", errors.New("secret not found in keyring")
}

func (m *MockKeyring) Set(service, user, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := service + ":" + user
	m.items[key] = password
	return nil
}

func (m *MockKeyring) Delete(service, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := service + ":" + user
	delete(m.items, key)
	return nil
}

type Source string

const (
	SourceEnv      Source = "env"
	SourceKeychain Source = "keychain"
	SourceFile     Source = "file"
	SourceNone     Source = "none"
)

// IsFallbackFileSupported returns true if the platform permits unencrypted 0600 file storage with consent.
// Per specification, file fallback is strictly Linux-only (for headless/container environments without D-Bus)
// and never supported or created on Windows or macOS.
func IsFallbackFileSupported() bool {
	return runtime.GOOS == "linux"
}

type ResolvedCredential struct {
	Key    string
	Source Source
}

var (
	defaultKeyringMu sync.RWMutex
	defaultKeyring   Keyring = osKeyring{}
)

// SetDefaultKeyring overrides the default Keyring implementation (used in unit tests).
func SetDefaultKeyring(kr Keyring) {
	defaultKeyringMu.Lock()
	defer defaultKeyringMu.Unlock()
	if kr == nil {
		defaultKeyring = osKeyring{}
	} else {
		defaultKeyring = kr
	}
}

func getDefaultKeyring() Keyring {
	defaultKeyringMu.RLock()
	defer defaultKeyringMu.RUnlock()
	return defaultKeyring
}

type Manager struct {
	Keyring      Keyring
	FallbackFile string
}

func NewManager(kr Keyring) *Manager {
	if kr == nil {
		if testing.Testing() && !testKeyringReady() {
			panic("credentials: refusing to open the OS keychain under go test; call credentials.ArmForTest in TestMain")
		}
		kr = getDefaultKeyring()
	}
	fbPath, _ := getFallbackPath()
	return &Manager{
		Keyring:      kr,
		FallbackFile: fbPath,
	}
}

func getFallbackPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("user home directory not found")
	}
	dir := filepath.Join(home, ".forge")
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "credentials.json"), nil
}

// EnvVarForProvider returns the primary environment variable name for a provider.
func EnvVarForProvider(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	default:
		return fmt.Sprintf("FORGE_%s_API_KEY", strings.ToUpper(provider))
	}
}

// Resolve looks up credentials following the resolution order:
// Environment Variable > OS Keychain > (Consented) Fallback File (Linux only) > None.
func (m *Manager) Resolve(provider string) ResolvedCredential {
	// 1. Environment variable
	envName := EnvVarForProvider(provider)
	if val := os.Getenv(envName); val != "" {
		agent.RegisterSecret(val)
		return ResolvedCredential{Key: val, Source: SourceEnv}
	}
	if strings.ToLower(provider) == "gemini" {
		if val := os.Getenv("GOOGLE_API_KEY"); val != "" {
			agent.RegisterSecret(val)
			return ResolvedCredential{Key: val, Source: SourceEnv}
		}
	}

	// 2. Keychain
	if key, err := m.Keyring.Get(ServiceName, strings.ToLower(provider)); err == nil && key != "" {
		agent.RegisterSecret(key)
		return ResolvedCredential{Key: key, Source: SourceKeychain}
	}

	// 3. Fallback File (Linux only, 0600)
	if IsFallbackFileSupported() {
		if key, err := m.getFallback(strings.ToLower(provider)); err == nil && key != "" {
			agent.RegisterSecret(key)
			return ResolvedCredential{Key: key, Source: SourceFile}
		}
	}

	return ResolvedCredential{Source: SourceNone}
}

// Store saves a credential to the OS keychain. If the keychain is unavailable:
// - On Windows and macOS: fails immediately (unencrypted file fallback is not supported).
// - On Linux: fails unless allowPlaintextFile is true (explicit user consent).
func (m *Manager) Store(provider, key string, allowPlaintextFile bool) (Source, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return SourceNone, errors.New("credential cannot be empty")
	}
	p := strings.ToLower(provider)

	// Always register key with redactor immediately
	agent.RegisterSecret(key)

	// Attempt keychain storage
	err := m.Keyring.Set(ServiceName, p, key)
	if err == nil {
		return SourceKeychain, nil
	}

	// Unencrypted file fallback is strictly Linux-only (for headless/container setups)
	if !IsFallbackFileSupported() {
		return SourceNone, fmt.Errorf("keychain storage failed: %w (unencrypted file fallback is not supported on %s; use environment variables or configure OS credential store)", err, runtime.GOOS)
	}

	// On Linux, file fallback requires explicit consent
	if !allowPlaintextFile {
		return SourceNone, fmt.Errorf("keychain storage failed (%w) and unencrypted file fallback was not consented (use --allow-plaintext-key-file to consent)", err)
	}

	if fbErr := m.setFallback(p, key); fbErr != nil {
		return SourceNone, fmt.Errorf("keychain error (%v) and fallback file error: %w", err, fbErr)
	}
	return SourceFile, nil
}

// Delete removes a stored credential from keychain and fallback file.
func (m *Manager) Delete(provider string) error {
	p := strings.ToLower(provider)
	_ = m.Keyring.Delete(ServiceName, p)
	_ = m.deleteFallback(p)
	return nil
}

func (m *Manager) getFallback(provider string) (string, error) {
	if m.FallbackFile == "" {
		return "", errors.New("no fallback file configured")
	}
	data, err := os.ReadFile(m.FallbackFile)
	if err != nil {
		return "", err
	}
	var stored map[string]string
	if err := json.Unmarshal(data, &stored); err != nil {
		return "", err
	}
	if val, ok := stored[provider]; ok && val != "" {
		return val, nil
	}
	return "", errors.New("key not found in fallback file")
}

func (m *Manager) setFallback(provider, key string) error {
	if m.FallbackFile == "" {
		return errors.New("no fallback file configured")
	}
	stored := make(map[string]string)
	if data, err := os.ReadFile(m.FallbackFile); err == nil {
		_ = json.Unmarshal(data, &stored)
	}
	stored[provider] = key

	b, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.FallbackFile, b, 0600); err != nil {
		return err
	}

	// Verify mode after writing (SEC-6 / 0.1)
	fi, err := os.Stat(m.FallbackFile)
	if err != nil {
		return fmt.Errorf("verifying fallback file mode: %w", err)
	}
	if fi.Mode().Perm() != 0600 {
		_ = os.Chmod(m.FallbackFile, 0600)
		if fi2, err := os.Stat(m.FallbackFile); err == nil && fi2.Mode().Perm() != 0600 {
			return fmt.Errorf("fallback file mode is %04o; expected 0600", fi2.Mode().Perm())
		}
	}
	return nil
}

func (m *Manager) deleteFallback(provider string) error {
	if m.FallbackFile == "" {
		return nil
	}
	data, err := os.ReadFile(m.FallbackFile)
	if err != nil {
		return nil
	}
	var stored map[string]string
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil
	}
	delete(stored, provider)
	if len(stored) == 0 {
		_ = os.Remove(m.FallbackFile)
		return nil
	}
	b, _ := json.MarshalIndent(stored, "", "  ")
	if err := os.WriteFile(m.FallbackFile, b, 0600); err != nil {
		return err
	}
	return nil
}

// MaskKey returns a masked version of an API key revealing at most the trailing 4 characters.
func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 4 {
		return "****"
	}
	tail := key[len(key)-4:]
	if strings.HasPrefix(key, "sk-") {
		return "sk-..." + tail
	}
	return "..." + tail
}

// TestConnection verifies an API key against a provider's endpoint using only read-only model-list requests.
func (m *Manager) TestConnection(ctx context.Context, provider, key, baseURL string) error {
	p := strings.ToLower(provider)
	reqURL := ""
	headers := make(http.Header)

	switch p {
	case "anthropic":
		// Anthropic Models API (cheapest read-only authenticated request, zero generation cost):
		// Doc: https://docs.anthropic.com/en/api/models-list
		reqURL = "https://api.anthropic.com/v1/models"
		if baseURL != "" {
			reqURL = strings.TrimRight(baseURL, "/") + "/models"
		}
		headers.Set("x-api-key", key)
		headers.Set("anthropic-version", "2023-06-01")
	case "openai":
		// OpenAI Models API (cheapest read-only authenticated model list call):
		// Doc: https://platform.openai.com/docs/api-reference/models/list
		reqURL = "https://api.openai.com/v1/models"
		if baseURL != "" {
			reqURL = strings.TrimRight(baseURL, "/") + "/models"
		}
		headers.Set("Authorization", "Bearer "+key)
	case "gemini":
		// Google Gemini Developer API Models List (header authentication only; key is never placed in query string):
		// Doc: https://ai.google.dev/gemini-api/docs/models/gemini
		reqURL = "https://generativelanguage.googleapis.com/v1beta/models"
		if baseURL != "" {
			reqURL = strings.TrimRight(baseURL, "/") + "/models"
		}
		headers.Set("x-goog-api-key", key)
	case "ollama":
		// Ollama Tags API:
		// Doc: https://github.com/ollama/ollama/blob/main/docs/api.md#list-local-models
		reqURL = "http://127.0.0.1:11434/api/tags"
		if baseURL != "" {
			reqURL = strings.TrimRight(baseURL, "/") + "/api/tags"
		}
	case "lmstudio":
		// LM Studio Models API:
		// Doc: https://lmstudio.ai/docs/api/endpoints
		reqURL = "http://127.0.0.1:1234/v1/models"
		if baseURL != "" {
			reqURL = strings.TrimRight(baseURL, "/") + "/v1/models"
		}
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("creating connection test request: %w", err)
	}
	for k, vv := range headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed (network unreachable or timeout): %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("authentication failed: invalid API key")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return errors.New("rate limited or quota exhausted")
	}
	return fmt.Errorf("provider returned unexpected status %d", resp.StatusCode)
}

// LocalServerProbe probes local loopback endpoints (Ollama on 11434, LM Studio on 1234)
// with a strict short timeout (300ms) without probing external networks.
func ProbeLocalServers(ctx context.Context) (ollamaFound, lmstudioFound bool) {
	client := &http.Client{Timeout: 300 * time.Millisecond}

	// 1. Probe Ollama (127.0.0.1:11434)
	reqOllama, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:11434/api/tags", nil)
	if err == nil {
		if resp, err := client.Do(reqOllama); err == nil {
			_ = resp.Body.Close()
			ollamaFound = resp.StatusCode == http.StatusOK
		}
	}

	// 2. Probe LM Studio (127.0.0.1:1234)
	reqLM, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:1234/v1/models", nil)
	if err == nil {
		if resp, err := client.Do(reqLM); err == nil {
			_ = resp.Body.Close()
			lmstudioFound = resp.StatusCode == http.StatusOK
		}
	}

	return ollamaFound, lmstudioFound
}
