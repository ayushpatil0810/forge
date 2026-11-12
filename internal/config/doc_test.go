package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigurationDocumentationCompleteness(t *testing.T) {
	// Locate docs/configuration.md relative to repo root
	// In tests, current working directory is the package directory (internal/config)
	docPath := filepath.Join("..", "..", "docs", "configuration.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", docPath, err)
	}
	docText := string(data)

	// Verify all exported fields of Config (except internal ones like Warnings)
	// have their json key documented in docs/configuration.md
	val := reflect.TypeOf(Config{})
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		key := strings.Split(jsonTag, ",")[0]
		if !strings.Contains(docText, "`"+key+"`") {
			t.Errorf("field %s (json key %q) is missing from %s", field.Name, key, docPath)
		}
	}

	// Verify precedence table is present in docs
	precedenceTokens := []string{
		"CLI Flags",
		"Environment Variables",
		"Project Configuration",
		"User Configuration",
		"Built-in Defaults",
	}
	for _, tok := range precedenceTokens {
		if !strings.Contains(docText, tok) {
			t.Errorf("precedence table in %s is missing %q", docPath, tok)
		}
	}
}

func TestConfigSEC6ProjectWarning(t *testing.T) {
	tempDir := t.TempDir()
	forgeDir := filepath.Join(tempDir, ".forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		t.Fatalf("failed to create .forge dir: %v", err)
	}

	configFile := filepath.Join(forgeDir, "config.json")
	insecureConfig := `{
		"provider": "openai",
		"openai_api_key": "sk-insecure-test-key-should-not-be-here"
	}`
	if err := os.WriteFile(configFile, []byte(insecureConfig), 0644); err != nil {
		t.Fatalf("failed to write insecure config: %v", err)
	}

	cfg, err := Load(LoadOptions{
		WorkspaceRoot:     tempDir,
		ProjectConfigFile: configFile,
		UserConfigFile:    filepath.Join(tempDir, "nonexistent.json"),
	})
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if len(cfg.Warnings) == 0 {
		t.Fatalf("expected SEC-6 warning for API key in project config, got none")
	}

	foundSEC6 := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "SEC-6") && strings.Contains(w, "openai_api_key") {
			foundSEC6 = true
			break
		}
	}
	if !foundSEC6 {
		t.Fatalf("expected warning mentioning SEC-6 and openai_api_key, got: %v", cfg.Warnings)
	}
}

func TestConfigValidation(t *testing.T) {
	// Invalid provider
	cfg := DefaultConfig()
	cfg.Provider = "invalid-provider-xyz"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for invalid provider")
	}

	// Invalid permission mode
	cfg = DefaultConfig()
	cfg.PermissionMode = "super-admin"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for invalid permission mode")
	}

	// Negative max turns
	cfg = DefaultConfig()
	cfg.MaxTurns = -1
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for negative max_turns")
	}

	// Negative max tokens
	cfg = DefaultConfig()
	cfg.MaxTokens = -5
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for negative max_tokens")
	}
}
