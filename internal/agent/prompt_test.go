package agent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/agent"
)

func TestBuildSystemPromptBase(t *testing.T) {
	dir := t.TempDir()
	prompt := agent.BuildSystemPrompt(dir)

	if !strings.Contains(prompt, "You are Forge") {
		t.Errorf("expected role in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "SEC-1") || !strings.Contains(prompt, "UNTRUSTED DATA") {
		t.Errorf("expected SEC-1 untrusted data guidance in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "grep") || !strings.Contains(prompt, "glob") {
		t.Errorf("expected tool-use guidance in prompt, got:\n%s", prompt)
	}
}

func TestBuildSystemPromptWithAgentsMD(t *testing.T) {
	dir := t.TempDir()
	content := "Run tests with go test ./..."
	_ = os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(content), 0o644)

	prompt := agent.BuildSystemPrompt(dir)
	if !strings.Contains(prompt, "Project Instructions (from AGENTS.md)") {
		t.Errorf("expected AGENTS.md section, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, content) {
		t.Errorf("expected AGENTS.md content, got:\n%s", prompt)
	}
}

func TestBuildSystemPromptFallbackToClaudeMD(t *testing.T) {
	dir := t.TempDir()
	content := "Claude specific guidelines"
	_ = os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0o644)

	prompt := agent.BuildSystemPrompt(dir)
	if !strings.Contains(prompt, "Project Instructions (from CLAUDE.md)") {
		t.Errorf("expected CLAUDE.md section, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, content) {
		t.Errorf("expected CLAUDE.md content, got:\n%s", prompt)
	}
}

func TestBuildSystemPromptPrefersAgentsOverClaude(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("use agents.md"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("use claude.md"), 0o644)

	prompt := agent.BuildSystemPrompt(dir)
	if !strings.Contains(prompt, "use agents.md") {
		t.Errorf("expected AGENTS.md to be preferred")
	}
	if strings.Contains(prompt, "use claude.md") {
		t.Errorf("did not expect CLAUDE.md when AGENTS.md is present")
	}
}
