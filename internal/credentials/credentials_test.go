package credentials_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/credentials"
)

type failingKeyring struct{}

func (failingKeyring) Get(s, u string) (string, error) {
	return "", errors.New("keychain unavailable")
}
func (failingKeyring) Set(s, u, p string) error {
	return errors.New("keychain unavailable")
}
func (failingKeyring) Delete(s, u string) error {
	return nil
}

func TestResolutionPrecedence(t *testing.T) {
	kr := credentials.NewMockKeyring()
	fbDir := t.TempDir()
	fbFile := filepath.Join(fbDir, "creds.json")

	mgr := &credentials.Manager{
		Keyring:      kr,
		FallbackFile: fbFile,
	}

	// 1. None configured
	res := mgr.Resolve("openai")
	if res.Source != credentials.SourceNone {
		t.Fatalf("expected SourceNone, got %v", res.Source)
	}

	// 2. Fallback file (on Linux only): keyring fails, stores in fallback file if consented
	if runtime.GOOS == "linux" {
		failingMgr := &credentials.Manager{Keyring: failingKeyring{}, FallbackFile: fbFile}
		source, err := failingMgr.Store("openai", "file-key-12345", true)
		if err != nil || source != credentials.SourceFile {
			t.Fatalf("expected store to fallback file, got source %v, err %v", source, err)
		}
		res = failingMgr.Resolve("openai")
		if res.Source != credentials.SourceFile || res.Key != "file-key-12345" {
			t.Fatalf("expected SourceFile, got %+v", res)
		}
	}

	// 3. Keychain overrides fallback file
	_ = kr.Set(credentials.ServiceName, "openai", "keychain-key-6789")
	res = mgr.Resolve("openai")
	if res.Source != credentials.SourceKeychain || res.Key != "keychain-key-6789" {
		t.Fatalf("expected SourceKeychain, got %+v", res)
	}

	// 4. Environment variable overrides keychain
	t.Setenv("OPENAI_API_KEY", "env-key-9999")
	res = mgr.Resolve("openai")
	if res.Source != credentials.SourceEnv || res.Key != "env-key-9999" {
		t.Fatalf("expected SourceEnv, got %+v", res)
	}
}

func TestFallbackConsentRequirement(t *testing.T) {
	fbDir := t.TempDir()
	fbFile := filepath.Join(fbDir, "creds.json")
	failingMgr := &credentials.Manager{Keyring: failingKeyring{}, FallbackFile: fbFile}

	// Attempt Store without consent
	source, err := failingMgr.Store("anthropic", "ant-secret-key", false)
	if err == nil {
		t.Fatal("expected error storing without consent, got nil")
	}
	if source != credentials.SourceNone {
		t.Fatalf("expected SourceNone, got %v", source)
	}

	// Assert no file was written
	if _, statErr := os.Stat(fbFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected credentials file not to exist, got stat err: %v", statErr)
	}
}

func TestFallbackPlatformSupport(t *testing.T) {
	isLinux := runtime.GOOS == "linux"
	if credentials.IsFallbackFileSupported() != isLinux {
		t.Fatalf("expected IsFallbackFileSupported() == %v on %s, got %v", isLinux, runtime.GOOS, credentials.IsFallbackFileSupported())
	}

	if !isLinux {
		fbDir := t.TempDir()
		fbFile := filepath.Join(fbDir, "creds.json")
		failingMgr := &credentials.Manager{Keyring: failingKeyring{}, FallbackFile: fbFile}

		// Even with consent, on Windows / macOS file fallback is forbidden
		source, err := failingMgr.Store("openai", "key-123", true)
		if err == nil {
			t.Fatalf("expected error on non-Linux platform, got nil")
		}
		if source != credentials.SourceNone {
			t.Fatalf("expected SourceNone on non-Linux platform, got %v", source)
		}
		if _, statErr := os.Stat(fbFile); !os.IsNotExist(statErr) {
			t.Fatalf("credentials file must not exist on non-Linux platforms")
		}
	}
}

func TestFallbackFileMode0600(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("file fallback mode test is Linux-only")
	}

	fbDir := t.TempDir()
	fbFile := filepath.Join(fbDir, "creds.json")
	failingMgr := &credentials.Manager{Keyring: failingKeyring{}, FallbackFile: fbFile}

	source, err := failingMgr.Store("openai", "mode-check-key", true)
	if err != nil || source != credentials.SourceFile {
		t.Fatalf("failed to store: %v", err)
	}

	fi, err := os.Stat(fbFile)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("expected file permissions 0600, got %04o", fi.Mode().Perm())
	}
}

func TestDeleteRemovesCredential(t *testing.T) {
	kr := credentials.NewMockKeyring()
	fbDir := t.TempDir()
	fbFile := filepath.Join(fbDir, "creds.json")

	mgr := &credentials.Manager{
		Keyring:      kr,
		FallbackFile: fbFile,
	}

	source, err := mgr.Store("anthropic", "ant-key", false)
	if err != nil || source != credentials.SourceKeychain {
		t.Fatalf("failed to store: %v", err)
	}
	if res := mgr.Resolve("anthropic"); res.Source != credentials.SourceKeychain {
		t.Fatal("expected keychain credential")
	}

	if err := mgr.Delete("anthropic"); err != nil {
		t.Fatal(err)
	}

	if res := mgr.Resolve("anthropic"); res.Source != credentials.SourceNone {
		t.Fatalf("expected SourceNone after delete, got %v", res.Source)
	}
}

func TestMaskKey(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", "(not set)"},
		{"123", "****"},
		{"1234", "****"},
		{"abcdef1234", "...1234"},
		{"sk-proj-abcde9876", "sk-...9876"},
	}

	for _, tc := range cases {
		got := credentials.MaskKey(tc.input)
		if got != tc.expected {
			t.Errorf("MaskKey(%q): got %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestRedactorRegistration(t *testing.T) {
	kr := credentials.NewMockKeyring()
	mgr := credentials.NewManager(kr)

	secret := "secret-super-token-9876"
	t.Setenv("OPENAI_API_KEY", secret)
	_ = mgr.Resolve("openai")

	masked := agent.RedactSecrets("Bearer " + secret)
	if masked == "Bearer "+secret {
		t.Fatalf("expected secret to be redacted by RedactSecrets, got %q", masked)
	}
}

func TestConnectionProbes_NoKeyInURL(t *testing.T) {
	testSecret := "super-secret-probe-key-12345"

	// Mock server that verifies NO key is ever in URL query string, only headers
	var capturedURL string
	var capturedAuthHeader string
	var capturedXAPIKey string
	var capturedXGoogAPIKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		capturedAuthHeader = r.Header.Get("Authorization")
		capturedXAPIKey = r.Header.Get("x-api-key")
		capturedXGoogAPIKey = r.Header.Get("x-goog-api-key")

		if strings.Contains(r.URL.RawQuery, testSecret) || strings.Contains(r.URL.Path, testSecret) {
			t.Errorf("CRITICAL SECURITY FLAW: API key found in URL: %s", r.URL.String())
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	mgr := credentials.NewManager(credentials.NewMockKeyring())
	ctx := context.Background()

	// 1. Anthropic: header x-api-key
	if err := mgr.TestConnection(ctx, "anthropic", testSecret, server.URL); err != nil {
		t.Fatalf("anthropic connection test failed: %v", err)
	}
	if capturedXAPIKey != testSecret {
		t.Fatalf("expected x-api-key header %q, got %q", testSecret, capturedXAPIKey)
	}
	if strings.Contains(capturedURL, testSecret) {
		t.Fatalf("key leaked into Anthropic URL: %s", capturedURL)
	}

	// 2. OpenAI: header Authorization: Bearer
	if err := mgr.TestConnection(ctx, "openai", testSecret, server.URL); err != nil {
		t.Fatalf("openai connection test failed: %v", err)
	}
	if capturedAuthHeader != "Bearer "+testSecret {
		t.Fatalf("expected Authorization header Bearer %q, got %q", testSecret, capturedAuthHeader)
	}
	if strings.Contains(capturedURL, testSecret) {
		t.Fatalf("key leaked into OpenAI URL: %s", capturedURL)
	}

	// 3. Gemini: header x-goog-api-key (NOT ?key= query string)
	if err := mgr.TestConnection(ctx, "gemini", testSecret, server.URL); err != nil {
		t.Fatalf("gemini connection test failed: %v", err)
	}
	if capturedXGoogAPIKey != testSecret {
		t.Fatalf("expected x-goog-api-key header %q, got %q", testSecret, capturedXGoogAPIKey)
	}
	if strings.Contains(capturedURL, testSecret) {
		t.Fatalf("key leaked into Gemini URL: %s", capturedURL)
	}
}

func TestRedactorMasksEchoedKeyInErrorBody(t *testing.T) {
	testSecret := "sk-echo-leak-test-key-9999"
	agent.RegisterSecret(testSecret)

	// Simulating a provider error response that echoes the API key back in the error message
	rawErrorBody := `{"error":{"message":"Invalid authentication key: sk-echo-leak-test-key-9999 for organization org-123"}}`

	sanitized := agent.RedactSecrets(rawErrorBody)
	if strings.Contains(sanitized, testSecret) {
		t.Fatalf("expected secret to be masked in error body, but found in: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[REDACTED") {
		t.Fatalf("expected [REDACTED] in sanitized error body, got: %s", sanitized)
	}
}
