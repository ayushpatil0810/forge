package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Provider != "anthropic" {
		t.Fatalf("expected default provider 'anthropic', got %s", cfg.Provider)
	}
	if cfg.Model == "" {
		t.Fatal("expected default model to be set")
	}
	if cfg.MaxTurns <= 0 {
		t.Fatalf("expected positive MaxTurns, got %d", cfg.MaxTurns)
	}
	if cfg.PermissionMode != "ask" {
		t.Fatalf("expected PermissionMode 'ask', got %s", cfg.PermissionMode)
	}
}

func TestDefaultModelForProvider(t *testing.T) {
	cases := []struct {
		provider string
		expected string
	}{
		{"anthropic", "claude-sonnet-5-5"},
		{"openai", "gpt-4o"},
		{"gemini", "gemini-2.5-pro"},
		{"mock", "mock-model"},
	}
	for _, tc := range cases {
		got := DefaultModelForProvider(tc.provider)
		if got != tc.expected {
			t.Errorf("provider %s: expected %s, got %s", tc.provider, tc.expected, got)
		}
	}
}

func TestLayeredConfigPrecedence(t *testing.T) {
	tmpDir := t.TempDir()

	userConfigFile := filepath.Join(tmpDir, "user_config.json")
	userJSON := `{"provider": "openai", "model": "user-model", "max_turns": 10, "theme": "light"}`
	if err := os.WriteFile(userConfigFile, []byte(userJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpDir, "project")
	projectForgeDir := filepath.Join(projectDir, ".forge")
	if err := os.MkdirAll(projectForgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfigFile := filepath.Join(projectForgeDir, "config.json")
	projectJSON := `{"model": "project-model", "max_turns": 20}`
	if err := os.WriteFile(projectConfigFile, []byte(projectJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Defaults overridden by User config
	cfg, err := Load(LoadOptions{
		UserConfigFile:    userConfigFile,
		ProjectConfigFile: filepath.Join(tmpDir, "nonexistent.json"),
		WorkspaceRoot:     tmpDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "openai" || cfg.Model != "user-model" || cfg.MaxTurns != 10 || cfg.Theme != "light" {
		t.Fatalf("user config merge failed: %+v", cfg)
	}

	// 2. Project config overrides User config
	cfg, err = Load(LoadOptions{
		UserConfigFile:    userConfigFile,
		ProjectConfigFile: projectConfigFile,
		WorkspaceRoot:     projectDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "openai" || cfg.Model != "project-model" || cfg.MaxTurns != 20 || cfg.Theme != "light" {
		t.Fatalf("project config merge failed: %+v", cfg)
	}

	// 3. Environment variables override Project config
	t.Setenv("FORGE_PROVIDER", "gemini")
	t.Setenv("FORGE_MODEL", "env-model")
	t.Setenv("FORGE_MAX_TURNS", "30")
	cfg, err = Load(LoadOptions{
		UserConfigFile:    userConfigFile,
		ProjectConfigFile: projectConfigFile,
		WorkspaceRoot:     projectDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "gemini" || cfg.Model != "env-model" || cfg.MaxTurns != 30 {
		t.Fatalf("env override failed: %+v", cfg)
	}

	// 4. Flags / LoadOptions override Environment variables
	cfg, err = Load(LoadOptions{
		UserConfigFile:    userConfigFile,
		ProjectConfigFile: projectConfigFile,
		WorkspaceRoot:     projectDir,
		Provider:          "anthropic",
		Model:             "flag-model",
		MaxTurns:          50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "anthropic" || cfg.Model != "flag-model" || cfg.MaxTurns != 50 {
		t.Fatalf("flag override failed: %+v", cfg)
	}
}
