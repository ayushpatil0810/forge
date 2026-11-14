package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/credentials"
	"github.com/zalando/go-keyring"
)

func TestBudget_UnknownPriceRefusesToStartWithExitCode2(t *testing.T) {
	resetCLIFlags()
	t.Cleanup(resetCLIFlags)

	t.Setenv("FORGE_WORKSPACE_ROOT", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
	_ = keyring.Set(credentials.ServiceName, "anthropic", "sk-ant-test-key")
	t.Cleanup(func() { _ = keyring.Delete(credentials.ServiceName, "anthropic") })

	cmd := newRootCmd()
	// Set model to custom-unknown-model with --max-cost
	cmd.SetArgs([]string{
		"run",
		"--model", "my-custom-unlisted-model",
		"--max-cost", "5.0",
		"hello",
	})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error when cost budget is set for model with unknown pricing")
	}

	exitErr, ok := err.(ExitCodeError)
	if !ok {
		t.Fatalf("expected ExitCodeError, got %T: %v", err, err)
	}
	if exitErr.Code != 2 {
		t.Fatalf("expected exit code 2, got %d (err: %v)", exitErr.Code, exitErr)
	}
	if !strings.Contains(err.Error(), "unknown pricing") && !strings.Contains(err.Error(), "--max-tokens") {
		t.Fatalf("expected error message to mention unknown pricing and --max-tokens, got: %v", err)
	}
}

func TestBudget_HeadlessExitCode5(t *testing.T) {
	resetCLIFlags()
	t.Cleanup(resetCLIFlags)

	tempDir := t.TempDir()
	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)

	// Run demo with a tiny token budget of 1 token so it immediately exceeds
	cmd.SetArgs([]string{
		"run",
		"--demo",
		"--yes",
		"--root", tempDir,
		"--output", "json",
		"--max-tokens", "1",
		"say hello",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := cmd.ExecuteContext(ctx)
	if err == nil {
		t.Fatal("expected error with exit code 5 when budget is exceeded")
	}

	exitErr, ok := err.(ExitCodeError)
	if !ok {
		t.Fatalf("expected ExitCodeError, got %T: %v", err, err)
	}
	if exitErr.Code != 5 {
		t.Fatalf("expected exit code 5, got %d (err: %v)", exitErr.Code, exitErr)
	}

	// Verify JSON output format
	var out headlessJSONOutput
	if jsonErr := json.Unmarshal(stdout.Bytes(), &out); jsonErr == nil {
		if out.Status != "budget_exceeded" {
			t.Fatalf("expected status 'budget_exceeded', got %q", out.Status)
		}
	}
}
