package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
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

// BashConfig holds user-only shell settings. Project config cannot set these.
type BashConfig struct {
	EnvAllow []string `json:"env_allow,omitempty"`
}

// ContextConfig controls automatic context compaction and window limits (SPEC A-7).
type ContextConfig struct {
	AutoCompact        *bool   `json:"auto_compact,omitempty"`
	Window             int     `json:"window,omitempty"`
	Tier1Threshold     float64 `json:"tier1_threshold,omitempty"`
	Tier2Threshold     float64 `json:"tier2_threshold,omitempty"`
	EmergencyThreshold float64 `json:"emergency_threshold,omitempty"`
	KeepRecentTurns    int     `json:"keep_recent_turns,omitempty"`
	SummaryMaxTokens   int     `json:"summary_max_tokens,omitempty"`
}

// UserModelEntry holds user-configured metadata for a model under a provider (Amendment A).
type UserModelEntry struct {
	ContextWindow int     `json:"context_window,omitempty"`
	PriceIn       float64 `json:"price_in,omitempty"`
	PriceOut      float64 `json:"price_out,omitempty"`
}

// ModelKey returns the canonical lookup key for provider + model id ("provider/model").
func ModelKey(provider, model string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "/" + strings.ToLower(strings.TrimSpace(model))
}

// BudgetConfig controls session token and cost budgets (SPEC A-6).
type BudgetConfig struct {
	MaxCost   float64 `json:"max_cost,omitempty"`
	MaxTokens int     `json:"max_tokens,omitempty"`
}

// Config holds agent configuration parameters across layers.
type Config struct {
	Provider           string                    `json:"provider,omitempty"`
	Model              string                    `json:"model,omitempty"`
	BaseURL            string                    `json:"base_url,omitempty"`
	MaxTurns           int                       `json:"max_turns,omitempty"`
	MaxTokens          int                       `json:"max_tokens,omitempty"`
	PermissionMode     string                    `json:"permission_mode,omitempty"`
	WorkspaceRoot      string                    `json:"workspace_root,omitempty"`
	Theme              string                    `json:"theme,omitempty"`
	SystemPrompt       string                    `json:"system_prompt,omitempty"`
	Shell              string                    `json:"shell,omitempty"`
	Sessions           *SessionsConfig           `json:"sessions,omitempty"`
	Audit              *AuditConfig              `json:"audit,omitempty"`
	Redaction          *RedactionConfig          `json:"redaction,omitempty"`
	Bash               *BashConfig               `json:"bash,omitempty"`
	Context            *ContextConfig            `json:"context,omitempty"`
	Budget             *BudgetConfig             `json:"budget,omitempty"`
	Providers          []ProviderProfile         `json:"providers,omitempty"`
	Models             map[string]UserModelEntry `json:"models,omitempty"`
	IgnoredProjectKeys map[string]string         `json:"ignored_project_keys,omitempty"`
	Warnings           []string                  `json:"-"`
	// baseURLRank records which layer set BaseURL so OPENAI_BASE_URL cannot
	// override FORGE_BASE_URL or a flag, and is applied only for compatible providers.
	baseURLRank int
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

const (
	rankBaseDefault = iota
	rankBaseUser
	rankBaseEnvOpenAI
	rankBaseEnvForge
	rankBaseFlag
)

// CheckInsecureBaseURL inspects custom base URLs and returns a message when
// API keys would be sent in cleartext over HTTP to a non-loopback host.
// Loopback http (localhost, 127.0.0.0/8, ::1) is allowed. Empty and https are allowed.
func CheckInsecureBaseURL(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if raw == "" || !strings.HasPrefix(strings.ToLower(raw), "http://") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return fmt.Sprintf("SEC-6: Custom base_url %q is not a valid URL.", baseURL)
	}
	if isLoopbackHost(u.Hostname()) {
		return ""
	}
	return fmt.Sprintf("SEC-6: Custom base_url %q transmits API keys in cleartext over insecure HTTP. Use HTTPS or a loopback address.", baseURL)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isOpenAICompatible(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "ollama", "lmstudio":
		return true
	default:
		return false
	}
}

// LoadOptions represents caller/CLI overrides and path overrides.
type LoadOptions struct {
	UserConfigFile       string
	ProjectConfigFile    string
	WorkspaceRoot        string
	Provider             string
	Model                string
	BaseURL              string
	PermissionMode       string
	MaxTurns             int
	MaxTokens            int
	Theme                string
	SystemPrompt         string
	Shell                string
	AllowInsecureBaseURL bool
	// Flag layer. Nil leaves the value from lower layers.
	AuditEnabled        *bool
	AuditRequired       *bool
	SessionsEnabled     *bool
	RedactionToolOutput *bool
	NoAutoCompact       *bool
	ContextWindow       int
	MaxCost             float64
	MaxTokensBudget     int
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
	autoCompact := true
	return Config{
		Provider:       "anthropic",
		Model:          "claude-sonnet-5-5",
		MaxTurns:       40,
		MaxTokens:      4096,
		PermissionMode: "ask",
		WorkspaceRoot:  ".",
		Theme:          "dark",
		Context: &ContextConfig{
			AutoCompact:        &autoCompact,
			Window:             0,
			Tier1Threshold:     0.60,
			Tier2Threshold:     0.80,
			EmergencyThreshold: 0.92,
			KeepRecentTurns:    6,
			SummaryMaxTokens:   1500,
		},
		Budget: &BudgetConfig{
			MaxCost:   0,
			MaxTokens: 0,
		},
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

	// OPENAI_BASE_URL retargets only OpenAI and OpenAI-compatible presets.
	// It loses to FORGE_BASE_URL and to an explicit base URL flag (F-010).
	if v := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); v != "" && isOpenAICompatible(cfg.Provider) && cfg.baseURLRank < rankBaseEnvForge {
		cfg.BaseURL = v
		cfg.baseURLRank = rankBaseEnvOpenAI
	}

	if msg := CheckInsecureBaseURL(cfg.BaseURL); msg != "" {
		if !opts.AllowInsecureBaseURL {
			return cfg, fmt.Errorf("%s Pass --allow-insecure-base-url to use it anyway", msg)
		}
		cfg.Warnings = append(cfg.Warnings, msg)
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
		case "bash", "bash.env_allow":
			reason := "bash.env_allow can only be configured in user config"
			dst.IgnoredProjectKeys[k] = reason
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
		case "context", "context.auto_compact", "context.window", "context.tier1_threshold",
			"context.tier2_threshold", "context.emergency_threshold", "context.keep_recent_turns",
			"context.summary_max_tokens", "context.input_price", "context.output_price":
			reason := "context and compaction settings can only be configured in user config or CLI flags"
			dst.IgnoredProjectKeys[k] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "budget", "budget.max_cost", "budget.max_tokens", "max_cost":
			reason := "budget and spend limits can only be configured in user config, environment variables, or CLI flags"
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
		case "models", "price_in", "price_out", "context_window":
			reason := "model definitions and pricing can only be configured in user config"
			dst.IgnoredProjectKeys[k] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		case "endpoint", "url", "proxy":
			reason := "endpoint URLs can only be configured in user config or CLI flags"
			dst.IgnoredProjectKeys[k] = reason
			dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
		default:
			if strings.HasPrefix(kLower, "models.") || strings.HasPrefix(kLower, "models/") {
				reason := "model definitions and pricing can only be configured in user config"
				dst.IgnoredProjectKeys[k] = reason
				dst.Warnings = append(dst.Warnings, fmt.Sprintf("SEC-6: Forbidden key %q in project config %q ignored (%s)", k, path, reason))
				continue
			}
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
		dst.baseURLRank = rankBaseUser
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
	if len(src.Models) > 0 {
		if dst.Models == nil {
			dst.Models = make(map[string]UserModelEntry)
		}
		for k, v := range src.Models {
			dst.Models[strings.ToLower(strings.TrimSpace(k))] = v
		}
	}
	if src.Sessions != nil {
		c := *src.Sessions
		c.Enabled = cloneBool(src.Sessions.Enabled)
		dst.Sessions = &c
	}
	if src.Audit != nil {
		c := *src.Audit
		c.Enabled = cloneBool(src.Audit.Enabled)
		dst.Audit = &c
	}
	if src.Redaction != nil {
		c := *src.Redaction
		c.ToolOutput = cloneBool(src.Redaction.ToolOutput)
		dst.Redaction = &c
	}
	if src.Bash != nil {
		c := *src.Bash
		if src.Bash.EnvAllow != nil {
			c.EnvAllow = append([]string(nil), src.Bash.EnvAllow...)
		}
		dst.Bash = &c
	}
	if src.Context != nil {
		if dst.Context == nil {
			dst.Context = &ContextConfig{}
		}
		if src.Context.AutoCompact != nil {
			dst.Context.AutoCompact = cloneBool(src.Context.AutoCompact)
		}
		if src.Context.Window > 0 {
			dst.Context.Window = src.Context.Window
		}
		if src.Context.Tier1Threshold > 0 {
			dst.Context.Tier1Threshold = src.Context.Tier1Threshold
		}
		if src.Context.Tier2Threshold > 0 {
			dst.Context.Tier2Threshold = src.Context.Tier2Threshold
		}
		if src.Context.EmergencyThreshold > 0 {
			dst.Context.EmergencyThreshold = src.Context.EmergencyThreshold
		}
		if src.Context.KeepRecentTurns > 0 {
			dst.Context.KeepRecentTurns = src.Context.KeepRecentTurns
		}
		if src.Context.SummaryMaxTokens > 0 {
			dst.Context.SummaryMaxTokens = src.Context.SummaryMaxTokens
		}
	}
	if src.Budget != nil {
		if dst.Budget == nil {
			dst.Budget = &BudgetConfig{}
		}
		if src.Budget.MaxCost > 0 {
			dst.Budget.MaxCost = src.Budget.MaxCost
		}
		if src.Budget.MaxTokens > 0 {
			dst.Budget.MaxTokens = src.Budget.MaxTokens
		}
	}
}

func cloneBool(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func envBool(key string) (bool, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, false
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
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
		dst.baseURLRank = rankBaseEnvForge
	}
	if v, ok := envBool("FORGE_AUDIT_ENABLED"); ok {
		if dst.Audit == nil {
			dst.Audit = &AuditConfig{}
		}
		dst.Audit.Enabled = cloneBool(&v)
	}
	if v, ok := envBool("FORGE_AUDIT_REQUIRED"); ok {
		if dst.Audit == nil {
			dst.Audit = &AuditConfig{}
		}
		dst.Audit.Required = v
	}
	if v, ok := envBool("FORGE_SESSIONS_ENABLED"); ok {
		if dst.Sessions == nil {
			dst.Sessions = &SessionsConfig{}
		}
		dst.Sessions.Enabled = cloneBool(&v)
	}
	if v := os.Getenv("FORGE_MAX_COST"); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f >= 0 {
			if dst.Budget == nil {
				dst.Budget = &BudgetConfig{}
			}
			dst.Budget.MaxCost = f
		}
	}
	if v := os.Getenv("FORGE_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			if dst.Budget == nil {
				dst.Budget = &BudgetConfig{}
			}
			dst.Budget.MaxTokens = n
		}
	}
	if v := os.Getenv("FORGE_CONTEXT_WINDOW"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			if dst.Context == nil {
				dst.Context = &ContextConfig{}
			}
			dst.Context.Window = n
		}
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
		dst.baseURLRank = rankBaseFlag
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
	if opts.AuditEnabled != nil {
		if dst.Audit == nil {
			dst.Audit = &AuditConfig{}
		}
		dst.Audit.Enabled = cloneBool(opts.AuditEnabled)
	}
	if opts.AuditRequired != nil {
		if dst.Audit == nil {
			dst.Audit = &AuditConfig{}
		}
		dst.Audit.Required = *opts.AuditRequired
	}
	if opts.SessionsEnabled != nil {
		if dst.Sessions == nil {
			dst.Sessions = &SessionsConfig{}
		}
		dst.Sessions.Enabled = cloneBool(opts.SessionsEnabled)
	}
	if opts.RedactionToolOutput != nil {
		if dst.Redaction == nil {
			dst.Redaction = &RedactionConfig{}
		}
		dst.Redaction.ToolOutput = cloneBool(opts.RedactionToolOutput)
	}
	if opts.NoAutoCompact != nil {
		if dst.Context == nil {
			dst.Context = &ContextConfig{}
		}
		val := !*opts.NoAutoCompact
		dst.Context.AutoCompact = &val
	}
	if opts.ContextWindow > 0 {
		if dst.Context == nil {
			dst.Context = &ContextConfig{}
		}
		dst.Context.Window = opts.ContextWindow
	}
	if opts.MaxCost > 0 {
		if dst.Budget == nil {
			dst.Budget = &BudgetConfig{}
		}
		dst.Budget.MaxCost = opts.MaxCost
	}
	if opts.MaxTokensBudget > 0 {
		if dst.Budget == nil {
			dst.Budget = &BudgetConfig{}
		}
		dst.Budget.MaxTokens = opts.MaxTokensBudget
	}
}

// SaveUserModel writes the chosen provider, model, optional base URL, and optional model metadata
// (context_window, price_in, price_out) into the user config (~/.forge/config.json).
// Keyed by provider + model id together (Amendment A).
func SaveUserModel(provider, model, baseURL string, contextWindow int, priceIn, priceOut float64) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	baseURL = strings.TrimSpace(baseURL)
	if provider == "" || model == "" {
		return fmt.Errorf("provider and model are required")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("home directory is not available")
	}
	dir := filepath.Join(home, ".forge")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	raw := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("reading user config: %w", err)
		}
	}
	raw["provider"] = provider
	raw["model"] = model
	if baseURL == "" {
		delete(raw, "base_url")
	} else {
		raw["base_url"] = baseURL
	}

	key := ModelKey(provider, model)
	var modelsMap map[string]any
	if m, ok := raw["models"].(map[string]any); ok && m != nil {
		modelsMap = m
	} else {
		modelsMap = make(map[string]any)
	}

	modelEntry := map[string]any{}
	if existing, ok := modelsMap[key].(map[string]any); ok && existing != nil {
		for ek, ev := range existing {
			modelEntry[ek] = ev
		}
	}
	if contextWindow > 0 {
		modelEntry["context_window"] = contextWindow
	}
	if priceIn > 0 {
		modelEntry["price_in"] = priceIn
	}
	if priceOut > 0 {
		modelEntry["price_out"] = priceOut
	}
	if len(modelEntry) > 0 {
		modelsMap[key] = modelEntry
		raw["models"] = modelsMap
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o600)
}

// SaveUserSelection writes the chosen provider and model into the user config
// (~/.forge/config.json). Other keys already in that file are kept. The API
// key is never written here; it stays in the OS keychain.
func SaveUserSelection(provider, model, baseURL string) error {
	return SaveUserModel(provider, model, baseURL, 0, 0, 0)
}
