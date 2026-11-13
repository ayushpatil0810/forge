package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProviderProfile represents a named provider configuration in user config.
type ProviderProfile struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	BaseURL      string `json:"base_url,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
}

// SessionsConfig controls session persistence and retention settings.
type SessionsConfig struct {
	Enabled       *bool  `json:"enabled,omitempty"`
	RetentionDays int    `json:"retention_days,omitempty"`
	DatabasePath  string `json:"database_path,omitempty"`
}

// AuditConfig controls audit log settings (SPEC §3.3 P-7).
type AuditConfig struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	Path     string `json:"path,omitempty"`
	MaxMB    int    `json:"max_mb,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// RedactionConfig controls secret redaction in tool output (SEC-5).
type RedactionConfig struct {
	ToolOutput *bool `json:"tool_output,omitempty"`
}

// Config holds agent configuration parameters across layers.
type Config struct {
	Provider           string            `json:"provider,omitempty"`
	Model              string            `json:"model,omitempty"`
	BaseURL            string            `json:"base_url,omitempty"`
	MaxTurns           int               `json:"max_turns,omitempty"`
	MaxTokens          int               `json:"max_tokens,omitempty"`
	PermissionMode     string            `json:"permission_mode,omitempty"`
	WorkspaceRoot      string            `json:"workspace_root,omitempty"`
	Theme              string            `json:"theme,omitempty"`
	SystemPrompt       string            `json:"system_prompt,omitempty"`
	Shell              string            `json:"shell,omitempty"`
	Sessions           *SessionsConfig   `json:"sessions,omitempty"`
	Audit              *AuditConfig      `json:"audit,omitempty"`
	Redaction          *RedactionConfig  `json:"redaction,omitempty"`
	Providers          []ProviderProfile `json:"providers,omitempty"`
	IgnoredProjectKeys map[string]string `json:"ignored_project_keys,omitempty"`
	Warnings           []string          `json:"-"`
}

// Redacted returns a copy of Config suitable for logging or display with warnings detached.
func (c Config) Redacted() Config {
	copyCfg := c
	copyCfg.Warnings = nil
	return copyCfg
}

// Validate checks that the configuration values are valid.
func (c Config) Validate() error {
	if c.Provider != "" {
		p := strings.ToLower(c.Provider)
		if p != "anthropic" && p != "openai" && p != "gemini" && p != "mock" && p != "ollama" && p != "lmstudio" {
			return fmt.Errorf("unknown provider %q (expected anthropic, openai, gemini, ollama, lmstudio, or mock)", c.Provider)
		}
	}
	if c.PermissionMode != "" {
		m := strings.ToLower(c.PermissionMode)
		if m != "ask" && m != "accept-edits" && m != "plan" && m != "yolo" {
			return fmt.Errorf("unknown permission mode %q (expected ask, accept-edits, plan, or yolo)", c.PermissionMode)
		}
	}
	if c.MaxTurns < 0 {
		return fmt.Errorf("max_turns cannot be negative, got %d", c.MaxTurns)
	}
	if c.MaxTokens < 0 {
		return fmt.Errorf("max_tokens cannot be negative, got %d", c.MaxTokens)
	}
	return nil
}

// CheckInsecureBaseURL inspects custom base URLs and returns a visible warning if
// API keys are being transmitted in cleartext over insecure HTTP to non-loopback hosts.
func CheckInsecureBaseURL(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if strings.HasPrefix(strings.ToLower(raw), "http://") {
		host := strings.TrimPrefix(strings.ToLower(raw), "http://")
		if slashIdx := strings.Index(host, "/"); slashIdx != -1 {
			host = host[:slashIdx]
		}
		if colonIdx := strings.Index(host, ":"); colonIdx != -1 {
			host = host[:colonIdx]
		}
		if host != "localhost" && host != "127.0.0.1" && host != "::1" && host != "[::1]" {
			return fmt.Sprintf("SEC-6: Custom base_url %q transmits API keys in cleartext over insecure HTTP. Use HTTPS or tunnel over a secure connection.", baseURL)
		}
	}
	return ""
}

// LoadOptions represents caller/CLI overrides and path overrides.
type LoadOptions struct {
	UserConfigFile    string
	ProjectConfigFile string
	WorkspaceRoot     string
	Provider          string
	Model             string
	BaseURL           string
	PermissionMode    string
	MaxTurns          int
	MaxTokens         int
	Theme             string
	SystemPrompt      string
	Shell             string
}

// DefaultModelForProvider returns the sensible default model ID for a given provider.
func DefaultModelForProvider(p string) string {
	switch strings.ToLower(p) {
	case "openai":
		return "gpt-4o"
	case "gemini":
		return "gemini-2.5-pro"
	case "ollama":
		return "llama3.3"
	case "lmstudio":
		return "model"
	case "mock":
		return "mock-model"
	default:
		return "claude-sonnet-5-5"
	}
}

// DefaultConfig returns sensible hardcoded defaults.
func DefaultConfig() Config {
	return Config{
		Provider:       "anthropic",
		Model:          "claude-sonnet-5-5",
		MaxTurns:       40,
		MaxTokens:      4096,
		PermissionMode: "ask",
		WorkspaceRoot:  ".",
		Theme:          "dark",
		// SystemPrompt stays empty. The CLI always builds the security prompt
		// and appends a user-set system_prompt after it (F-006). A non-empty
		// default here used to replace that built prompt entirely.
	}
}

// Load reads and merges configuration from all layers in order:
// 1. Defaults
// 2. User config (~/.forge/config.json)
// 3. Project config (<root>/.forge/config.json)
// 4. Environment variables (FORGE_*)
// 5. Explicit options / flags
func Load(opts LoadOptions) (Config, error) {
	cfg := DefaultConfig()

	// 1. User config
	userPath := opts.UserConfigFile
	if userPath == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			userPath = filepath.Join(home, ".forge", "config.json")
		}
	}
	if userPath != "" {
		if err := mergeFile(&cfg, userPath); err != nil && !os.IsNotExist(err) {
			return cfg, fmt.Errorf("reading user config %q: %w", userPath, err)
		}
	}

	// Determine workspace root before loading project config
	wsRoot := cfg.WorkspaceRoot
	if envRoot := os.Getenv("FORGE_WORKSPACE_ROOT"); envRoot != "" {
		wsRoot = envRoot
	}
	if opts.WorkspaceRoot != "" {
		wsRoot = opts.WorkspaceRoot
	}
	cfg.WorkspaceRoot = wsRoot

	// 2. Project config (untrusted repository configuration - strict allowlist enforced)
	projectPath := opts.ProjectConfigFile
	if projectPath == "" && wsRoot != "" {
		projectPath = filepath.Join(wsRoot, ".forge", "config.json")
	}
	if projectPath != "" {
		if err := mergeProjectFile(&cfg, projectPath); err != nil && !os.IsNotExist(err) {
			return cfg, fmt.Errorf("reading project config %q: %w", projectPath, err)
		}
	}

	// 3. Environment variables
	applyEnv(&cfg)

	// 4. Flag/Options overrides
	applyOptions(&cfg, opts)

	// Preset auto-fill for local providers when base_url is not set
	if strings.ToLower(cfg.Provider) == "ollama" && cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:11434"
	}
	if strings.ToLower(cfg.Provider) == "lmstudio" && cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:1234/v1"
	}

	// Match named provider profile if provider matches a profile name
	for _, p := range cfg.Providers {
		if strings.EqualFold(p.Name, cfg.Provider) {
			cfg.Provider = p.Kind
			if cfg.BaseURL == "" && p.BaseURL != "" {
				cfg.BaseURL = p.BaseURL
			}
			if opts.Model == "" && p.DefaultModel != "" {
				cfg.Model = p.DefaultModel
			}
			break
		}
	}

	// Check for cleartext API key transmission over non-loopback HTTP
	if warn := CheckInsecureBaseURL(cfg.BaseURL); warn != "" {
		cfg.Warnings = append(cfg.Warnings, warn)
	}

	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func mergeProjectFile(dst *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing json: %w", err)
	}

	if dst.IgnoredProjectKeys == nil {
		dst.IgnoredProjectKeys = make(map[string]string)
	}

	for k, v := range raw {
		kLower := strings.ToLower(k)
		switch kLower {
		case "model":
			if s, ok := v.(string); ok && s != "" {
				dst.Model = s
			}
		case "theme":
			if s, ok := v.(string); ok && s != "" {
				dst.Theme = s
			}
		case "max_turns":
			if f, ok := v.(float64); ok && int(f) > 0 {
				dst.MaxTurns = int(f)
			}
		case "max_tokens":
			if f, ok := v.(float64); ok && int(f) > 0 {
				dst.MaxTokens = int(f)
			}
		case "checkpoints":
			// Allowed project configuration
		case "system_prompt":
			reason := "a cloned repository must not set the system prompt"
			dst.IgnoredProjectKeys["system_prompt"] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "permission_mode":
			reason := "a cloned repository must not grant a permission mode"
			dst.IgnoredProjectKeys["permission_mode"] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "shell":
			reason := "arbitrary code execution risk; shell can only be configured in user config or CLI flags"
			dst.IgnoredProjectKeys["shell"] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "base_url":
			reason := "credential exfiltration and SSRF risk; base_url can only be configured in user config or CLI flags"
			dst.IgnoredProjectKeys["base_url"] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "yolo":
			reason := "cannot disable safety prompts via repository files"
			dst.IgnoredProjectKeys["yolo"] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "sessions", "session", "sessions.enabled", "sessions.retention_days", "sessions.database_path":
			reason := "session database path and persistence settings can only be configured in user config or CLI flags"
			dst.IgnoredProjectKeys[k] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "audit", "audit.path", "audit.enabled", "audit.max_mb", "audit.required", "audit_path":
			reason := "audit log configuration can only be configured in user config or CLI flags"
			dst.IgnoredProjectKeys[k] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "redaction", "redaction.tool_output":
			reason := "redaction settings can only be configured in user config or CLI flags"
			dst.IgnoredProjectKeys[k] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "database_path", "db_path":
			reason := "database path can only be configured in user config or CLI flags"
			dst.IgnoredProjectKeys[k] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "endpoint", "url", "proxy":
			reason := "endpoint URLs can only be configured in user config or CLI flags"
			dst.IgnoredProjectKeys[k] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		default:
			kUpper := strings.ToUpper(k)
			if strings.Contains(kUpper, "API_KEY") || strings.Contains(kUpper, "TOKEN") || strings.Contains(kUpper, "SECRET") {
				reason := "credentials in repository configuration are prohibited"
				dst.IgnoredProjectKeys[k] = reason
				dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Potential credential key %q in project config %q ignored (%s)", k, path, reason))
			} else {
				reason := "key not permitted in project configuration allowlist"
				dst.IgnoredProjectKeys[k] = reason
				dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Disallowed key %q in project config %q ignored (%s)", k, path, reason))
			}
		}
	}
	return nil
}

func mergeFile(dst *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var src Config
	if err := json.Unmarshal(data, &src); err != nil {
		return fmt.Errorf("parsing json: %w", err)
	}
	merge(dst, src)
	return nil
}

func merge(dst *Config, src Config) {
	if src.Provider != "" {
		oldProvider := dst.Provider
		dst.Provider = src.Provider
		if src.Model == "" && oldProvider != src.Provider {
			dst.Model = DefaultModelForProvider(src.Provider)
		}
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.BaseURL != "" {
		dst.BaseURL = src.BaseURL
	}
	if src.MaxTurns > 0 {
		dst.MaxTurns = src.MaxTurns
	}
	if src.MaxTokens > 0 {
		dst.MaxTokens = src.MaxTokens
	}
	if src.PermissionMode != "" {
		dst.PermissionMode = src.PermissionMode
	}
	if src.WorkspaceRoot != "" {
		dst.WorkspaceRoot = src.WorkspaceRoot
	}
	if src.Theme != "" {
		dst.Theme = src.Theme
	}
	if src.SystemPrompt != "" {
		dst.SystemPrompt = src.SystemPrompt
	}
	if src.Shell != "" {
		dst.Shell = src.Shell
	}
	if len(src.Providers) > 0 {
		dst.Providers = append(dst.Providers, src.Providers...)
	}
}

func applyEnv(dst *Config) {
	if v := os.Getenv("FORGE_PROVIDER"); v != "" {
		oldProvider := dst.Provider
		dst.Provider = v
		if os.Getenv("FORGE_MODEL") == "" && oldProvider != v {
			dst.Model = DefaultModelForProvider(v)
		}
	}
	if v := os.Getenv("FORGE_MODEL"); v != "" {
		dst.Model = v
	}
	if v := os.Getenv("FORGE_MAX_TURNS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			dst.MaxTurns = n
		}
	}
	if v := os.Getenv("FORGE_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			dst.MaxTokens = n
		}
	}
	if v := os.Getenv("FORGE_PERMISSION_MODE"); v != "" {
		dst.PermissionMode = v
	}
	if v := os.Getenv("FORGE_WORKSPACE_ROOT"); v != "" {
		dst.WorkspaceRoot = v
	}
	if v := os.Getenv("FORGE_THEME"); v != "" {
		dst.Theme = v
	}
	if v := os.Getenv("FORGE_SYSTEM_PROMPT"); v != "" {
		dst.SystemPrompt = v
	}
	if v := os.Getenv("FORGE_SHELL"); v != "" {
		dst.Shell = v
	}
	if v := os.Getenv("FORGE_BASE_URL"); v != "" {
		dst.BaseURL = v
	} else if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		dst.BaseURL = v
	}
}

func applyOptions(dst *Config, opts LoadOptions) {
	if opts.Provider != "" {
		oldProvider := dst.Provider
		dst.Provider = opts.Provider
		if opts.Model == "" && oldProvider != opts.Provider {
			dst.Model = DefaultModelForProvider(opts.Provider)
		}
	}
	if opts.Model != "" {
		dst.Model = opts.Model
	}
	if opts.BaseURL != "" {
		dst.BaseURL = opts.BaseURL
	}
	if opts.MaxTurns > 0 {
		dst.MaxTurns = opts.MaxTurns
	}
	if opts.MaxTokens > 0 {
		dst.MaxTokens = opts.MaxTokens
	}
	if opts.PermissionMode != "" {
		dst.PermissionMode = opts.PermissionMode
	}
	if opts.WorkspaceRoot != "" {
		dst.WorkspaceRoot = opts.WorkspaceRoot
	}
	if opts.Theme != "" {
		dst.Theme = opts.Theme
	}
	if opts.SystemPrompt != "" {
		dst.SystemPrompt = opts.SystemPrompt
	}
	if opts.Shell != "" {
		dst.Shell = opts.Shell
	}
}
