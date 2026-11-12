package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds agent configuration parameters across layers.
type Config struct {
	Provider       string   `json:"provider,omitempty"`
	Model          string   `json:"model,omitempty"`
	MaxTurns       int      `json:"max_turns,omitempty"`
	MaxTokens      int      `json:"max_tokens,omitempty"`
	PermissionMode string   `json:"permission_mode,omitempty"`
	WorkspaceRoot  string   `json:"workspace_root,omitempty"`
	Theme          string   `json:"theme,omitempty"`
	SystemPrompt   string   `json:"system_prompt,omitempty"`
	Shell          string   `json:"shell,omitempty"`
	Warnings       []string `json:"-"`
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
		if p != "anthropic" && p != "openai" && p != "gemini" && p != "mock" {
			return fmt.Errorf("unknown provider %q (expected anthropic, openai, gemini, or mock)", c.Provider)
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

// LoadOptions represents caller/CLI overrides and path overrides.
type LoadOptions struct {
	UserConfigFile    string
	ProjectConfigFile string
	WorkspaceRoot     string
	Provider          string
	Model             string
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
		SystemPrompt:   "You are Forge, a careful and capable coding agent.",
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

	// 2. Project config
	projectPath := opts.ProjectConfigFile
	if projectPath == "" && wsRoot != "" {
		projectPath = filepath.Join(wsRoot, ".forge", "config.json")
	}
	if projectPath != "" {
		if secWarnings := checkProjectSecurity(projectPath); len(secWarnings) > 0 {
			cfg.Warnings = append(cfg.Warnings, secWarnings...)
		}
		if err := mergeFile(&cfg, projectPath); err != nil && !os.IsNotExist(err) {
			return cfg, fmt.Errorf("reading project config %q: %w", projectPath, err)
		}
	}

	// 3. Environment variables
	applyEnv(&cfg)

	// 4. Flag/Options overrides
	applyOptions(&cfg, opts)

	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func checkProjectSecurity(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	var warnings []string
	for k, v := range raw {
		kUpper := strings.ToUpper(k)
		if strings.Contains(kUpper, "API_KEY") || strings.Contains(kUpper, "TOKEN") || strings.Contains(kUpper, "SECRET") {
			if strVal, ok := v.(string); ok && strings.TrimSpace(strVal) != "" {
				warnings = append(warnings, fmt.Sprintf("SEC-6: Potential credential %q found in project configuration file %q. Never commit API keys or secrets to project repositories; use environment variables instead.", k, path))
			}
		}
	}
	return warnings
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
