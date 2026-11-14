package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ayushpatil0810/forge/internal/config"
)

func TestContextAndBudgetConfigPrecedence(t *testing.T) {
	tempDir := t.TempDir()
	userConfigFile := filepath.Join(tempDir, "user_config.json")
	projectConfigFile := filepath.Join(tempDir, "project_config.json")

	// 1. User config sets baseline values
	userCfgData := map[string]any{
		"context": map[string]any{
			"auto_compact": true,
			"window":       50000,
		},
		"budget": map[string]any{
			"max_cost":   10.5,
			"max_tokens": 100000,
		},
	}
	data, _ := json.Marshal(userCfgData)
	if err := os.WriteFile(userConfigFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Project config attempts to maliciously override budget and context settings
	projectCfgData := map[string]any{
		"context": map[string]any{
			"auto_compact": false,
			"window":       999999,
		},
		"budget": map[string]any{
			"max_cost":   9999.0,
			"max_tokens": 9999999,
		},
		"context.window":  888888,
		"budget.max_cost": 8888.0,
	}
	data, _ = json.Marshal(projectCfgData)
	if err := os.WriteFile(projectConfigFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Load with user and project config (no env, no flags)
	cfg, err := config.Load(config.LoadOptions{
		WorkspaceRoot:     tempDir,
		UserConfigFile:    userConfigFile,
		ProjectConfigFile: projectConfigFile,
	})
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	// Verify project config was ignored and SEC-6 warnings were recorded
	if cfg.Context == nil || cfg.Context.Window != 50000 {
		t.Errorf("expected context.window 50000 from user config, got %v", cfg.Context)
	}
	if cfg.Budget == nil || cfg.Budget.MaxCost != 10.5 || cfg.Budget.MaxTokens != 100000 {
		t.Errorf("expected budget max_cost 10.5 and max_tokens 100000 from user config, got %v", cfg.Budget)
	}
	if len(cfg.IgnoredProjectKeys) == 0 {
		t.Errorf("expected hostile keys to be recorded in IgnoredProjectKeys")
	}

	// 3. Env variables override user config
	t.Setenv("FORGE_MAX_COST", "25.0")
	t.Setenv("FORGE_MAX_TOKENS", "250000")
	t.Setenv("FORGE_CONTEXT_WINDOW", "64000")

	cfgEnv, err := config.Load(config.LoadOptions{
		WorkspaceRoot:     tempDir,
		UserConfigFile:    userConfigFile,
		ProjectConfigFile: projectConfigFile,
	})
	if err != nil {
		t.Fatalf("unexpected load error with env: %v", err)
	}
	if cfgEnv.Budget.MaxCost != 25.0 {
		t.Errorf("expected budget max_cost 25.0 from env, got %f", cfgEnv.Budget.MaxCost)
	}
	if cfgEnv.Budget.MaxTokens != 250000 {
		t.Errorf("expected budget max_tokens 250000 from env, got %d", cfgEnv.Budget.MaxTokens)
	}
	if cfgEnv.Context.Window != 64000 {
		t.Errorf("expected context window 64000 from env, got %d", cfgEnv.Context.Window)
	}

	// 4. CLI flags override Env variables
	noCompact := true
	cfgFlag, err := config.Load(config.LoadOptions{
		WorkspaceRoot:     tempDir,
		UserConfigFile:    userConfigFile,
		ProjectConfigFile: projectConfigFile,
		NoAutoCompact:     &noCompact,
		ContextWindow:     128000,
		MaxCost:           50.0,
		MaxTokensBudget:   500000,
	})
	if err != nil {
		t.Fatalf("unexpected load error with flags: %v", err)
	}
	if cfgFlag.Budget.MaxCost != 50.0 {
		t.Errorf("expected budget max_cost 50.0 from flag, got %f", cfgFlag.Budget.MaxCost)
	}
	if cfgFlag.Budget.MaxTokens != 500000 {
		t.Errorf("expected budget max_tokens 500000 from flag, got %d", cfgFlag.Budget.MaxTokens)
	}
	if cfgFlag.Context.Window != 128000 {
		t.Errorf("expected context window 128000 from flag, got %d", cfgFlag.Context.Window)
	}
	if cfgFlag.Context.AutoCompact == nil || *cfgFlag.Context.AutoCompact != false {
		t.Errorf("expected auto_compact false from flag")
	}
}

func TestUserModelEntry_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("USERPROFILE", tempDir)
	t.Setenv("HOME", tempDir)

	err := config.SaveUserModel("anthropic", "claude-custom-1", "", 120000, 3.5, 14.0)
	if err != nil {
		t.Fatalf("unexpected error saving user model: %v", err)
	}

	userCfgFile := filepath.Join(tempDir, ".forge", "config.json")
	cfg, err := config.Load(config.LoadOptions{
		UserConfigFile: userCfgFile,
	})
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	key := config.ModelKey("anthropic", "claude-custom-1")
	entry, ok := cfg.Models[key]
	if !ok {
		t.Fatalf("expected key %q in Models map, got: %+v", key, cfg.Models)
	}
	if entry.ContextWindow != 120000 {
		t.Errorf("expected context window 120000, got %d", entry.ContextWindow)
	}
	if entry.PriceIn != 3.5 {
		t.Errorf("expected price_in 3.5, got %f", entry.PriceIn)
	}
	if entry.PriceOut != 14.0 {
		t.Errorf("expected price_out 14.0, got %f", entry.PriceOut)
	}
}
