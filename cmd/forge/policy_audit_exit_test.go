package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/audit"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/policy"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestSlice9a_AutomatedExitCheck(t *testing.T) {
	wsDir := t.TempDir()
	userDir := t.TempDir()
	logPath := filepath.Join(userDir, "audit.jsonl")

	userPolicyPath := filepath.Join(userDir, "policy.json")
	projPolicyPath := filepath.Join(wsDir, ".forge", "policy.json")
	_ = os.MkdirAll(filepath.Dir(projPolicyPath), 0755)

	// User policy: allow bash(go test:*) and edit(docs/**), plus edit(**) to prove floor overrides matching allow
	userPolicyJSON := `{
		"allow": ["bash(go test:*)", "edit(docs/**)", "edit(.env*)"]
	}`
	if err := os.WriteFile(userPolicyPath, []byte(userPolicyJSON), 0600); err != nil {
		t.Fatal(err)
	}

	// Project policy: attempting allow bash(*) (must be ignored) and deny bash(git push:*)
	projPolicyJSON := `{
		"allow": ["bash(*)"],
		"deny": ["bash(git push:*)"]
	}`
	if err := os.WriteFile(projPolicyPath, []byte(projPolicyJSON), 0600); err != nil {
		t.Fatal(err)
	}

	// Load policy
	ps, err := policy.Load(userPolicyPath, projPolicyPath)
	if err != nil {
		t.Fatalf("failed to load policy: %v", err)
	}

	// Verify project allow rule was ignored with warning
	if len(ps.Warnings) == 0 {
		t.Fatal("expected warning about ignored project allow rule, got none")
	}
	if !strings.Contains(ps.Warnings[0], "project policy allow rule \"bash(*)\" ignored") {
		t.Fatalf("unexpected warning: %s", ps.Warnings[0])
	}
	for _, r := range ps.ProjectRules {
		if r.Action == policy.ActionAllow {
			t.Fatalf("project allow rule was loaded into ProjectRules: %+v", r)
		}
	}

	auditLogger := audit.New(audit.Config{
		Enabled:  true,
		Path:     logPath,
		MaxMB:    10,
		Required: true,
		Root:     wsDir,
	})

	registry := tools.NewRegistry()
	registry.Register(tools.Bash{Root: wsDir})
	registry.Register(tools.ReadFile{Root: wsDir})
	registry.Register(tools.EditFile{Root: wsDir})
	registry.Register(tools.WriteFile{Root: wsDir})

	// Check 1: In ask mode, 'go test ./...' runs without a prompt (allowed by user rule)
	res1 := policy.Evaluate(policy.EvalRequest{
		ToolName: "bash",
		Command:  "go test ./...",
		Mode:     permission.ModeAsk,
	}, ps)
	if res1.Verdict != permission.Allow || res1.Source != "rule:bash(go test:*)" {
		t.Fatalf("go test ./... should be allowed without prompt: got verdict=%v source=%s reason=%s", res1.Verdict, res1.Source, res1.Reason)
	}

	// Check 2: 'go test -exec ...' prompts (argument hazard floor overrides allow rule)
	res2 := policy.Evaluate(policy.EvalRequest{
		ToolName: "bash",
		Command:  "go test -exec ./evil ./...",
		Mode:     permission.ModeAsk,
	}, ps)
	if res2.Verdict != permission.Ask || res2.Source != "floor" {
		t.Fatalf("go test -exec should prompt via safety floor: got verdict=%v source=%s reason=%s", res2.Verdict, res2.Source, res2.Reason)
	}

	// Check 3: Editing docs/x.md needs no prompt
	res3 := policy.Evaluate(policy.EvalRequest{
		ToolName: "edit_file",
		Path:     "docs/x.md",
		Mode:     permission.ModeAsk,
	}, ps)
	if res3.Verdict != permission.Allow || res3.Source != "rule:edit(docs/**)" {
		t.Fatalf("editing docs/x.md should be allowed without prompt: got verdict=%v source=%s", res3.Verdict, res3.Source)
	}

	// Check 4: Editing main.go prompts (does not match docs/**)
	res4 := policy.Evaluate(policy.EvalRequest{
		ToolName: "edit_file",
		Path:     "main.go",
		Mode:     permission.ModeAsk,
	}, ps)
	if res4.Verdict != permission.Ask || res4.Source != "mode" {
		t.Fatalf("editing main.go should prompt in ask mode: got verdict=%v source=%s", res4.Verdict, res4.Source)
	}

	// Check 5: 'git push' is denied even with --yes and in yolo mode (deny precedence)
	res5Yolo := policy.Evaluate(policy.EvalRequest{
		ToolName:    "bash",
		Command:     "git push origin main",
		Mode:        permission.ModeYOLO,
		AutoApprove: true,
	}, ps)
	if res5Yolo.Verdict != permission.Deny || res5Yolo.Source != "rule:bash(git push:*)" {
		t.Fatalf("git push must be denied in yolo mode: got verdict=%v source=%s", res5Yolo.Verdict, res5Yolo.Source)
	}

	// Check 6: An .env write still asks despite matching allow (safety floor protected path)
	res6 := policy.Evaluate(policy.EvalRequest{
		ToolName:    "edit_file",
		Path:        ".env",
		Mode:        permission.ModeAsk,
		IsProtected: true,
	}, ps)
	if res6.Verdict != permission.Ask || res6.Source != "floor" {
		t.Fatalf(".env write must trigger safety floor Ask despite matching allow: got verdict=%v source=%s", res6.Verdict, res6.Source)
	}

	// Check 7: Headless auto-denies unanswerable approvals
	res7 := policy.Evaluate(policy.EvalRequest{
		ToolName:   "edit_file",
		Path:       "main.go",
		Mode:       permission.ModeAsk,
		IsHeadless: true,
	}, ps)
	if res7.Verdict != permission.Deny || res7.Source != "headless-auto-deny" {
		t.Fatalf("headless run must auto-deny unanswerable approvals: got verdict=%v source=%s", res7.Verdict, res7.Source)
	}

	// Test End-to-End Execution with Mock Provider and Audit Logging
	turn1 := mock.Turn{mock.ToolUse("c1", "write_file", map[string]any{"path": "docs/x.md", "content": "updated doc"})}
	turn2 := mock.Turn{mock.Text("Updated documentation")}
	turn3 := mock.Turn{mock.ToolUse("c2", "bash", map[string]any{"command": "git push origin main"})}
	turn4 := mock.Turn{mock.Text("Cannot push")}

	mockP := mock.New(turn1, turn2, turn3, turn4)
	sess := agent.NewLocal(agent.Config{
		ID:          "exit-session",
		Tools:       registry,
		Provider:    mockP,
		PolicySet:   ps,
		AuditLogger: auditLogger,
		Policy:      permission.Policy{Mode: permission.ModeAsk},
	})
	defer sess.Close()

	ctx := context.Background()

	// 1. Tool execution that runs without prompt
	_ = sess.Send(ctx, "update docs")
	drainSession(sess)

	// 2. Denied tool execution (git push)
	_ = sess.Send(ctx, "push changes")
	drainSession(sess)

	// 3. Audit log verification
	records, err := audit.ReadRecords(logPath, "exit-session", 0)
	if err != nil {
		t.Fatalf("failed reading audit log: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected audit log records, got 0")
	}

	// Verify decisions and decision sources in audit records
	foundAllow := false
	foundDeny := false
	for _, r := range records {
		if r.Event == "decision" && r.Decision == "allow" && r.DecisionSource == "rule:edit(docs/**)" {
			foundAllow = true
		}
		if r.Event == "decision" && r.Decision == "deny" && r.DecisionSource == "rule:bash(git push:*)" {
			foundDeny = true
		}
	}
	if !foundAllow {
		t.Errorf("audit log missing allow decision for edit(docs/**); records: %+v", records)
	}
	if !foundDeny {
		t.Errorf("audit log missing deny decision for bash(git push:*); records: %+v", records)
	}

	// 4. Cryptographic tamper verification passes on valid log
	verifiedCount, err := audit.Verify(logPath, "exit-session")
	if err != nil {
		t.Fatalf("audit.Verify failed on valid log: %v", err)
	}
	if verifiedCount == 0 {
		t.Fatalf("audit.Verify verified 0 records")
	}

	// 5. Tampered copy fails verification
	tamperedPath := filepath.Join(userDir, "tampered_audit.jsonl")
	tamperedRecs := append([]audit.Record{}, records...)
	tamperedRecs[0].ActionSummary = "tampered command"
	writeAuditRecords(t, tamperedPath, tamperedRecs)

	_, err = audit.Verify(tamperedPath, "exit-session")
	if err == nil {
		t.Fatal("expected audit.Verify to fail on tampered log, but it passed!")
	}
	if !errors.Is(err, audit.ErrTampered) {
		t.Fatalf("expected error wrapping ErrTampered, got: %v", err)
	}
}

func drainSession(s agent.Session) {
	for ev := range s.Events() {
		if ev.Terminal() {
			return
		}
	}
}

func writeAuditRecords(t *testing.T, path string, records []audit.Record) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	for _, r := range records {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write(append(data, '\n'))
	}
}
