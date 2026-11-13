package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/audit"
)

func TestPolicyCLI_ListAndCheck(t *testing.T) {
	tmpDir := t.TempDir()
	projForgeDir := filepath.Join(tmpDir, ".forge")
	_ = os.MkdirAll(projForgeDir, 0755)

	projPolicy := `{
		"allow": ["bash(*)"],
		"deny": ["bash(git push:*)"]
	}`
	_ = os.WriteFile(filepath.Join(projForgeDir, "policy.json"), []byte(projPolicy), 0600)

	// 1. Test policy list
	cmdList := newRootCmd()
	var outList bytes.Buffer
	var errList bytes.Buffer
	cmdList.SetOut(&outList)
	cmdList.SetErr(&errList)
	cmdList.SetArgs([]string{"policy", "list", "--root", tmpDir})

	if err := cmdList.Execute(); err != nil {
		t.Fatalf("policy list failed: %v", err)
	}

	errStr := errList.String()
	if !strings.Contains(errStr, "project policy allow rule \"bash(*)\" ignored") {
		t.Fatalf("expected warning about ignored project allow rule, got:\n%s", errStr)
	}

	outStr := outList.String()
	if !strings.Contains(outStr, "bash(git push:*)") {
		t.Fatalf("expected project deny rule in list output, got:\n%s", outStr)
	}

	// 2. Test policy check denied command
	cmdCheckDeny := newRootCmd()
	var outCheckDeny bytes.Buffer
	cmdCheckDeny.SetOut(&outCheckDeny)
	cmdCheckDeny.SetArgs([]string{"policy", "check", "bash", "git push origin main", "--root", tmpDir})

	if err := cmdCheckDeny.Execute(); err != nil {
		t.Fatalf("policy check failed: %v", err)
	}
	if !strings.Contains(outCheckDeny.String(), "Verdict:  DENY") {
		t.Fatalf("expected DENY verdict for git push, got:\n%s", outCheckDeny.String())
	}

	// 3. Test policy check dangerous floor command
	cmdCheckDang := newRootCmd()
	var outCheckDang bytes.Buffer
	cmdCheckDang.SetOut(&outCheckDang)
	cmdCheckDang.SetArgs([]string{"policy", "check", "bash", "go test -exec evil ./...", "--root", tmpDir})

	if err := cmdCheckDang.Execute(); err != nil {
		t.Fatalf("policy check failed: %v", err)
	}
	if !strings.Contains(outCheckDang.String(), "Source:   floor") {
		t.Fatalf("expected floor source for argument hazard, got:\n%s", outCheckDang.String())
	}
}

func TestAuditCLI_ShowAndVerify(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.jsonl")

	logger := audit.New(audit.Config{
		Enabled: true,
		Path:    logPath,
		MaxMB:   10,
	})

	ctx := context.Background()
	exit0 := 0
	dur := int64(42)
	_ = logger.Log(ctx, "sess-123", "tool_result", "bash", "echo hello", "allow", "mode", &exit0, &dur, "hello\n")
	_ = logger.Log(ctx, "sess-123", "tool_result", "edit_file", "edit(main.go)", "allow", "rule:edit(**)", &exit0, &dur, "ok")

	// 1. Test audit show
	cmdShow := newRootCmd()
	var outShow bytes.Buffer
	cmdShow.SetOut(&outShow)
	cmdShow.SetArgs([]string{"audit", "show", "--path", logPath})

	if err := cmdShow.Execute(); err != nil {
		t.Fatalf("audit show failed: %v", err)
	}
	showOutput := outShow.String()
	if !strings.Contains(showOutput, "Session:sess-123") || !strings.Contains(showOutput, "echo hello") {
		t.Fatalf("expected audit records in show output, got:\n%s", showOutput)
	}

	// 2. Test audit verify
	cmdVerify := newRootCmd()
	var outVerify bytes.Buffer
	cmdVerify.SetOut(&outVerify)
	cmdVerify.SetArgs([]string{"audit", "verify", "--path", logPath})

	if err := cmdVerify.Execute(); err != nil {
		t.Fatalf("audit verify failed: %v", err)
	}
	if !strings.Contains(outVerify.String(), "verified successfully") {
		t.Fatalf("expected success message from verify, got:\n%s", outVerify.String())
	}
}
