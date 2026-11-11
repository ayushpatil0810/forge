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
	Model          string `json:"model,omitempty"`
	MaxTurns       int    `json:"max_turns,omitempty"`
	MaxTokens      int    `json:"max_tokens,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	WorkspaceRoot  string `json:"workspace_root,omitempty"`
	Theme          string `json:"theme,omitempty"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
}

// LoadOptions represents caller/CLI overrides and path overrides.
type LoadOptions struct {
	UserConfigFile    string
	ProjectConfigFile string
	WorkspaceRoot     string
	Model             string
	PermissionMode    string
	MaxTurns          int
	MaxTokens         int
	Theme             string
	SystemPrompt      string
}

// DefaultConfig returns sensible hardcoded defaults.
func DefaultConfig() Config {
	return Config{
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
		if err := mergeFile(&cfg, projectPath); err != nil && !os.IsNotExist(err) {
			return cfg, fmt.Errorf("reading project config %q: %w", projectPath, err)
		}
	}

	// 3. Environment variables
	applyEnv(&cfg)

	// 4. Flag/Options overrides
	applyOptions(&cfg, opts)

	return cfg, nil
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
}

func applyEnv(dst *Config) {
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
}

func applyOptions(dst *Config, opts LoadOptions) {
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
}

