package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAIBaseURL_OnlyCompatibleProviders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGE_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("FORGE_PROVIDER", "")

	cases := []struct {
		provider string
		want     bool
	}{
		{provider: "openai", want: true},
		{provider: "ollama", want: true},
		{provider: "lmstudio", want: true},
		{provider: "anthropic", want: false},
		{provider: "gemini", want: false},
		{provider: "mock", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			cfg, err := Load(LoadOptions{
				Provider:          tc.provider,
				UserConfigFile:    filepath.Join(dir, "no-user.json"),
				ProjectConfigFile: filepath.Join(dir, "no-project.json"),
				WorkspaceRoot:     dir,
			})
			if err != nil {
				t.Fatalf("Load(%s): %v", tc.provider, err)
			}
			applied := cfg.BaseURL == "https://example.test/v1"
			if applied != tc.want {
				t.Fatalf("provider %s base_url=%q, applied=%v want %v", tc.provider, cfg.BaseURL, applied, tc.want)
			}
		})
	}
}

func TestOpenAIBaseURL_CustomOpenAIProfile(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.json")
	body := `{
		"provider": "custom",
		"providers": [{"name": "custom", "kind": "openai", "base_url": "https://profile.example/v1"}]
	}`
	if err := os.WriteFile(user, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "https://env.example/v1")
	t.Setenv("FORGE_PROVIDER", "")

	cfg, err := Load(LoadOptions{
		UserConfigFile:    user,
		ProjectConfigFile: filepath.Join(dir, "no-project.json"),
		WorkspaceRoot:     dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "openai" {
		t.Fatalf("resolved provider = %q, want openai", cfg.Provider)
	}
	if cfg.BaseURL != "https://env.example/v1" {
		t.Fatalf("OPENAI_BASE_URL should apply to a custom openai-compatible profile, got %q", cfg.BaseURL)
	}
}

func TestOpenAIBaseURL_CustomGeminiProfileIgnored(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.json")
	body := `{"provider":"custom","providers":[{"name":"custom","kind":"gemini"}]}`
	if err := os.WriteFile(user, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "https://env.example/v1")
	t.Setenv("FORGE_PROVIDER", "")

	cfg, err := Load(LoadOptions{
		UserConfigFile:    user,
		ProjectConfigFile: filepath.Join(dir, "no-project.json"),
		WorkspaceRoot:     dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "gemini" {
		t.Fatalf("resolved provider = %q, want gemini", cfg.Provider)
	}
	if cfg.BaseURL != "" {
		t.Fatalf("OPENAI_BASE_URL must not retarget gemini, got %q", cfg.BaseURL)
	}
}

func TestInsecureBaseURL_RefusedUnlessAllowed(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	t.Setenv("FORGE_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("FORGE_PROVIDER", "")

	public := "http://203.0.113.10/v1"
	_, err := Load(LoadOptions{
		Provider:          "openai",
		BaseURL:           public,
		UserConfigFile:    missing,
		ProjectConfigFile: missing,
		WorkspaceRoot:     dir,
	})
	if err == nil || !strings.Contains(err.Error(), "203.0.113.10") {
		t.Fatalf("expected refusal of non-loopback http, got %v", err)
	}

	for _, loopback := range []string{"http://127.0.0.1:11434/v1", "http://localhost:11434", "http://[::1]:8080/v1"} {
		cfg, err := Load(LoadOptions{
			Provider:          "openai",
			BaseURL:           loopback,
			UserConfigFile:    missing,
			ProjectConfigFile: missing,
			WorkspaceRoot:     dir,
		})
		if err != nil {
			t.Fatalf("loopback %s should be allowed: %v", loopback, err)
		}
		if cfg.BaseURL != loopback {
			t.Fatalf("loopback base = %q, want %q", cfg.BaseURL, loopback)
		}
	}

	cfg, err := Load(LoadOptions{
		Provider:             "openai",
		BaseURL:              public,
		AllowInsecureBaseURL: true,
		UserConfigFile:       missing,
		ProjectConfigFile:    missing,
		WorkspaceRoot:        dir,
	})
	if err != nil {
		t.Fatalf("allow flag should permit insecure base URL: %v", err)
	}
	if cfg.BaseURL != public {
		t.Fatalf("allowed base = %q", cfg.BaseURL)
	}

	cfg, err = Load(LoadOptions{
		Provider:          "anthropic",
		BaseURL:           "https://203.0.113.10/v1",
		UserConfigFile:    missing,
		ProjectConfigFile: missing,
		WorkspaceRoot:     dir,
	})
	if err != nil {
		t.Fatalf("https must be allowed: %v", err)
	}
	if cfg.BaseURL != "https://203.0.113.10/v1" {
		t.Fatalf("https base = %q", cfg.BaseURL)
	}
}
