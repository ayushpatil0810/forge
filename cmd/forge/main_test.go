package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/credentials"
	"github.com/zalando/go-keyring"
)

func TestRootCmdVersion(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing --version: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "forge 0.1.0-dev") {
		t.Fatalf("expected version output to contain 'forge 0.1.0-dev', got %q", got)
	}
}

func TestRootCmdHelp(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing --help: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Forge is a provider-agnostic terminal AI coding agent") {
		t.Fatalf("expected help output to contain short description, got %q", got)
	}
}

func TestRootCmdMissingAPIKeyFailsFast(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_ = keyring.Delete(credentials.ServiceName, "anthropic")
	cmd := newRootCmd()
	cmd.SetArgs([]string{"hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is missing")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("expected error to mention ANTHROPIC_API_KEY, got: %v", err)
	}
}

func TestStoredKeyDoesNotReachNetwork(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	const secret = "sk-ant-test-not-a-real-key"
	if err := keyring.Set(credentials.ServiceName, "anthropic", secret); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = keyring.Delete(credentials.ServiceName, "anthropic") })

	cmd := newRootCmd()
	cmd.SetArgs([]string{"run", "--yes", "--output", "text", "hello"})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := cmd.ExecuteContext(ctx)
	if err == nil {
		t.Fatal("expected the provider call to fail closed at the test network guard")
	}
	if !strings.Contains(err.Error(), "non-loopback") && !strings.Contains(err.Error(), "test network guard") {
		t.Fatalf("expected the dial to be refused by the test guard, got: %v", err)
	}
}

func TestRootCmdDemoFlagExecutes(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--demo", "--yes", "fix failing test"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error in demo mode: %v", err)
	}
}

func TestConfigSubcommand(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"config"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing 'forge config': %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"provider":`) || !strings.Contains(got, `"model":`) {
		t.Fatalf("expected JSON config output, got: %s", got)
	}
}

func TestConfigSubcommand_ListsIgnoredProjectKeys(t *testing.T) {
	ws := t.TempDir()
	forgeDir := filepath.Join(ws, ".forge")
	_ = os.MkdirAll(forgeDir, 0755)

	hostileCfg := `{
		"provider": "openai",
		"shell": "powershell.exe -NoProfile",
		"sessions.enabled": false,
		"database_path": "/tmp/evil.db",
		"openai_api_key": "sk-proj-test12345"
	}`
	_ = os.WriteFile(filepath.Join(forgeDir, "config.json"), []byte(hostileCfg), 0644)

	cmd := newRootCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"config", "--root", ws})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing config with project config: %v", err)
	}

	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "Ignored project config keys (security policy):") {
		t.Fatalf("expected output to mention 'Ignored project config keys', got:\n%s", errOutput)
	}
	for _, key := range []string{"shell", "sessions.enabled", "database_path", "openai_api_key"} {
		if !strings.Contains(errOutput, key) {
			t.Errorf("expected ignored keys list to contain %q, got:\n%s", key, errOutput)
		}
	}
}

func TestCommandsNeverOpenDatabase(t *testing.T) {
	tempDir := t.TempDir()
	// Set database path to an uncreatable / unwriteable file path inside a non-existent file
	unwriteablePath := filepath.Join(tempDir, "a-file", "nested", "database.db")
	_ = os.WriteFile(filepath.Join(tempDir, "a-file"), []byte("not a directory"), 0644)
	t.Setenv("FORGE_SESSIONS_PATH", unwriteablePath)

	// 1. --version
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected --version to succeed without touching database: %v", err)
	}

	// 2. config
	cmd = newRootCmd()
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"config"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected config to succeed without touching database: %v", err)
	}

	// 3. auth status
	cmd = newRootCmd()
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"auth", "status"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected auth status to succeed without touching database: %v", err)
	}
}

func TestDecideHeadlessApproval(t *testing.T) {
	tests := []struct {
		name        string
		isDangerous bool
		isProtected bool
		yes         bool
		yolo        bool
		want        agent.Decision
		wantReason  string
	}{
		{
			name:        "ordinary command without flags auto-denies",
			isDangerous: false,
			isProtected: false,
			yes:         false,
			yolo:        false,
			want:        agent.DecisionDeny,
			wantReason:  "approval requested in headless mode; use --yes to approve",
		},
		{
			name:        "ordinary command with --yes is approved",
			isDangerous: false,
			isProtected: false,
			yes:         true,
			yolo:        false,
			want:        agent.DecisionAllow,
			wantReason:  "",
		},
		{
			name:        "ordinary command with --yolo is approved",
			isDangerous: false,
			isProtected: false,
			yes:         false,
			yolo:        true,
			want:        agent.DecisionAllow,
			wantReason:  "",
		},
		{
			name:        "dangerous command without flags is denied",
			isDangerous: true,
			isProtected: false,
			yes:         false,
			yolo:        false,
			want:        agent.DecisionDeny,
			wantReason:  "dangerous command cannot be auto-approved in headless mode",
		},
		{
			name:        "dangerous command with --yes only is still denied",
			isDangerous: true,
			isProtected: false,
			yes:         true,
			yolo:        false,
			want:        agent.DecisionDeny,
			wantReason:  "dangerous command cannot be auto-approved in headless mode",
		},
		{
			name:        "dangerous command with --yolo is still denied",
			isDangerous: true,
			isProtected: false,
			yes:         false,
			yolo:        true,
			want:        agent.DecisionDeny,
			wantReason:  "dangerous command cannot be auto-approved in headless mode",
		},
		{
			name:        "protected path modification is always denied in headless",
			isDangerous: false,
			isProtected: true,
			yes:         true,
			yolo:        false,
			want:        agent.DecisionDeny,
			wantReason:  "protected path modification cannot be auto-approved in headless mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDecision, gotReason := decideHeadlessApproval(tt.isDangerous, tt.isProtected, tt.yes, tt.yolo)
			if gotDecision != tt.want {
				t.Fatalf("decideHeadlessApproval(%v, %v, %v, %v) decision = %v, want %v",
					tt.isDangerous, tt.isProtected, tt.yes, tt.yolo, gotDecision, tt.want)
			}
			if gotReason != tt.wantReason {
				t.Fatalf("decideHeadlessApproval(%v, %v, %v, %v) reason = %q, want %q",
					tt.isDangerous, tt.isProtected, tt.yes, tt.yolo, gotReason, tt.wantReason)
			}
		})
	}
}

func TestForgeRunHeadlessJSON(t *testing.T) {
	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"run", "--demo", "--demo-speed", "0", "--yes", "--output", "json", "Fix the failing test"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error in forge run --output json: %v", err)
	}
}

func TestForgeRunInvalidFlagsExitCode(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"run", "--output", "invalid-format", "Fix test"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --output format")
	}

	var exitErr ExitCodeError
	if errors.As(err, &exitErr) {
		if exitErr.Code != 2 {
			t.Fatalf("expected exit code 2 for invalid flag value, got %d", exitErr.Code)
		}
	} else {
		t.Fatalf("expected ExitCodeError, got %T: %v", err, err)
	}
}

func TestForgeRunRequiresPromptArgument(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when prompt argument is missing")
	}
}

func TestDemoWithoutExternalToolchain(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--demo", "--demo-speed", "0", "--yes", "Fix boundary bug"})

	// Empty PATH test: ensure demo doesn't need external go/git/bash on host
	t.Setenv("PATH", "")

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected demo to run cleanly even with empty PATH, got: %v", err)
	}
}

func TestForbiddenKeyFlagsFailWithExitCode2(t *testing.T) {
	flagsToTest := []string{
		"--api-key",
		"--key",
		"--api_key",
		"--anthropic-key",
		"--openai-api-key",
	}

	for _, flag := range flagsToTest {
		t.Run(flag, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs([]string{flag, "sk-secret-key-1234"})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error passing %s, got nil", flag)
			}
			var exitErr ExitCodeError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected ExitCodeError, got %T: %v", err, err)
			}
			if exitErr.Code != 2 {
				t.Fatalf("expected exit code 2, got %d", exitErr.Code)
			}
			if !strings.Contains(exitErr.Error(), "forge auth login") {
				t.Fatalf("expected error to hint 'forge auth login', got: %v", exitErr)
			}
		})
	}

	t.Run("run_subcommand_with_api_key", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"run", "--api-key", "sk-secret-key-1234", "my prompt"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var exitErr ExitCodeError
		if !errors.As(err, &exitErr) || exitErr.Code != 2 {
			t.Fatalf("expected ExitCodeError with code 2, got %v", err)
		}
	})
}
