package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func TestAuditSessionsRedactionPrecedence(t *testing.T) {
	t.Setenv("FORGE_AUDIT_ENABLED", "")
	t.Setenv("FORGE_AUDIT_REQUIRED", "")
	t.Setenv("FORGE_SESSIONS_ENABLED", "")
	t.Setenv("FORGE_PROVIDER", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("FORGE_BASE_URL", "")

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")

	// 1. Defaults: sections are unset.
	cfg, err := Load(LoadOptions{UserConfigFile: missing, ProjectConfigFile: missing, WorkspaceRoot: dir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audit != nil || cfg.Sessions != nil || cfg.Redaction != nil {
		t.Fatalf("defaults should leave sections unset, got audit=%v sessions=%v redaction=%v", cfg.Audit, cfg.Sessions, cfg.Redaction)
	}

	// 2. User config supplies the sections.
	user := filepath.Join(dir, "user.json")
	userBody := `{
		"provider": "openai",
		"audit": {"enabled": false, "required": true, "path": "C:/user-audit.jsonl", "max_mb": 12},
		"sessions": {"enabled": false, "retention_days": 7, "database_path": "C:/user-sessions.db"},
		"redaction": {"tool_output": false}
	}`
	if err := os.WriteFile(user, []byte(userBody), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(LoadOptions{UserConfigFile: user, ProjectConfigFile: missing, WorkspaceRoot: dir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audit == nil || cfg.Audit.Enabled == nil || *cfg.Audit.Enabled || !cfg.Audit.Required || cfg.Audit.Path != "C:/user-audit.jsonl" || cfg.Audit.MaxMB != 12 {
		t.Fatalf("user audit not applied: %+v", cfg.Audit)
	}
	if cfg.Sessions == nil || cfg.Sessions.Enabled == nil || *cfg.Sessions.Enabled || cfg.Sessions.RetentionDays != 7 || cfg.Sessions.DatabasePath != "C:/user-sessions.db" {
		t.Fatalf("user sessions not applied: %+v", cfg.Sessions)
	}
	if cfg.Redaction == nil || cfg.Redaction.ToolOutput == nil || *cfg.Redaction.ToolOutput {
		t.Fatalf("user redaction not applied: %+v", cfg.Redaction)
	}

	// 3. Project config cannot override those sections.
	projDir := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(projDir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	projBody := `{
		"theme": "light",
		"audit": {"enabled": true, "required": false, "path": "C:/evil.jsonl"},
		"sessions": {"enabled": true, "database_path": "C:/evil.db"},
		"redaction": {"tool_output": true}
	}`
	projFile := filepath.Join(projDir, ".forge", "config.json")
	if err := os.WriteFile(projFile, []byte(projBody), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(LoadOptions{UserConfigFile: user, ProjectConfigFile: projFile, WorkspaceRoot: projDir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "light" {
		t.Fatalf("harmless project theme should apply, got %q", cfg.Theme)
	}
	if cfg.Audit == nil || cfg.Audit.Enabled == nil || *cfg.Audit.Enabled || !cfg.Audit.Required || cfg.Audit.Path != "C:/user-audit.jsonl" {
		t.Fatalf("project audit must be ignored, got %+v", cfg.Audit)
	}
	if cfg.Sessions == nil || cfg.Sessions.Enabled == nil || *cfg.Sessions.Enabled || cfg.Sessions.DatabasePath != "C:/user-sessions.db" {
		t.Fatalf("project sessions must be ignored, got %+v", cfg.Sessions)
	}
	if cfg.Redaction == nil || cfg.Redaction.ToolOutput == nil || *cfg.Redaction.ToolOutput {
		t.Fatalf("project redaction must be ignored, got %+v", cfg.Redaction)
	}
	for _, key := range []string{"audit", "sessions", "redaction"} {
		if _, ok := cfg.IgnoredProjectKeys[key]; !ok {
			t.Errorf("expected %q in IgnoredProjectKeys", key)
		}
		found := false
		for _, w := range cfg.Warnings {
			if strings.Contains(w, "SEC-6") && strings.Contains(w, key) {
				found = true
			}
		}
		if !found {
			t.Errorf("expected SEC-6 warning for %q, got %v", key, cfg.Warnings)
		}
	}

	// 4. Environment overrides user config.
	t.Setenv("FORGE_AUDIT_ENABLED", "true")
	t.Setenv("FORGE_AUDIT_REQUIRED", "false")
	t.Setenv("FORGE_SESSIONS_ENABLED", "true")
	cfg, err = Load(LoadOptions{UserConfigFile: user, ProjectConfigFile: projFile, WorkspaceRoot: projDir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audit == nil || cfg.Audit.Enabled == nil || !*cfg.Audit.Enabled || cfg.Audit.Required {
		t.Fatalf("env audit override failed: %+v", cfg.Audit)
	}
	if cfg.Audit.Path != "C:/user-audit.jsonl" {
		t.Fatalf("env should not clear audit path, got %q", cfg.Audit.Path)
	}
	if cfg.Sessions == nil || cfg.Sessions.Enabled == nil || !*cfg.Sessions.Enabled {
		t.Fatalf("env sessions override failed: %+v", cfg.Sessions)
	}

	// 5. Flags override environment.
	cfg, err = Load(LoadOptions{
		UserConfigFile:      user,
		ProjectConfigFile:   projFile,
		WorkspaceRoot:       projDir,
		AuditEnabled:        boolPtr(false),
		AuditRequired:       boolPtr(true),
		SessionsEnabled:     boolPtr(false),
		RedactionToolOutput: boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audit == nil || cfg.Audit.Enabled == nil || *cfg.Audit.Enabled || !cfg.Audit.Required {
		t.Fatalf("flag audit override failed: %+v", cfg.Audit)
	}
	if cfg.Sessions == nil || cfg.Sessions.Enabled == nil || *cfg.Sessions.Enabled {
		t.Fatalf("flag sessions override failed: %+v", cfg.Sessions)
	}
	if cfg.Redaction == nil || cfg.Redaction.ToolOutput == nil || !*cfg.Redaction.ToolOutput {
		t.Fatalf("flag redaction override failed: %+v", cfg.Redaction)
	}
}
