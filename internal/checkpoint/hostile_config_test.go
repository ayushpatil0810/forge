package checkpoint_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ayushpatil0810/forge/internal/checkpoint"
)

func TestCheckpoint_HostileGitConfig(t *testing.T) {
	// Create a hostile global git configuration file
	hostileDir := t.TempDir()
	hostileConfigFile := filepath.Join(hostileDir, ".gitconfig")
	failingHookDir := filepath.Join(hostileDir, "hooks")
	_ = os.MkdirAll(failingHookDir, 0755)

	// Create a failing hook script
	hookPath := filepath.Join(failingHookDir, "pre-commit")
	_ = os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0755)

	hostileContent := `[commit]
	gpgsign = true
	template = /nonexistent/path/template
[core]
	hooksPath = ` + filepath.ToSlash(failingHookDir) + `
	autocrlf = true
[user]
	useConfigOnly = true
`
	if err := os.WriteFile(hostileConfigFile, []byte(hostileContent), 0644); err != nil {
		t.Fatalf("failed to write hostile gitconfig: %v", err)
	}

	// Set environment variables that normally point to the user's global config
	t.Setenv("GIT_CONFIG_GLOBAL", hostileConfigFile)
	t.Setenv("HOME", hostileDir)
	t.Setenv("USERPROFILE", hostileDir)
	t.Setenv("GIT_AUTHOR_NAME", "")
	t.Setenv("GIT_AUTHOR_EMAIL", "")

	// Create test workspace
	ws := t.TempDir()
	testFile := filepath.Join(ws, "main.go")
	if err := os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	mgr, err := checkpoint.New(ws)
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	// Snapshot creation must succeed despite hostile global config
	cp1, err := mgr.Create(context.Background(), "hostile config test snapshot")
	if err != nil {
		t.Fatalf("checkpoint.Create failed under hostile git config: %v", err)
	}
	if cp1.ID == "" {
		t.Fatal("expected valid checkpoint ID")
	}

	// Mutate file
	if err := os.WriteFile(testFile, []byte("package main\n// mutated\n"), 0644); err != nil {
		t.Fatalf("failed to mutate file: %v", err)
	}

	// Restore must succeed despite hostile global config
	cpRestored, err := mgr.Restore(context.Background(), cp1.ID)
	if err != nil {
		t.Fatalf("checkpoint.Restore failed under hostile git config: %v", err)
	}
	if cpRestored == nil {
		t.Fatal("expected non-nil restored checkpoint")
	}

	// Verify restored content matches
	restoredData, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(restoredData) != "package main\n\nfunc main() {}\n" {
		t.Fatalf("content was not restored correctly: got %q", string(restoredData))
	}
}
