package audit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditTailTruncationFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	logger := New(Config{Enabled: true, Path: path, MaxMB: 10})
	for i := 0; i < 3; i++ {
		if err := logger.Log(context.Background(), "s", "tool_result", "bash", "echo ok", "allow", "mode", nil, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Verify(path, "s"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(data), "\n")
	if err := os.WriteFile(path, []byte(first+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(path, "s"); err == nil {
		t.Fatal("tail truncation was accepted")
	}
}

func TestAuditRotationVerifies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	logger := New(Config{Enabled: true, Path: path, MaxMB: 1})
	logger.maxBytes = 80
	for i := 0; i < 4; i++ {
		if err := logger.Log(context.Background(), "rot", "tool_result", "bash", "echo rotate", "allow", "mode", nil, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := filepath.Glob(path + ".*")
	if err != nil {
		t.Fatal(err)
	}
	rotated := false
	for _, m := range matches {
		if strings.HasSuffix(m, ".state.json") || strings.HasSuffix(m, ".lock") {
			continue
		}
		rotated = true
	}
	if !rotated {
		t.Fatal("expected the log to rotate")
	}
	n, err := Verify(path, "rot")
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("verified %d records, want 4", n)
	}
}

func TestAuditRequiredAndOptional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(path, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	required := New(Config{Enabled: true, Path: path, Required: true})
	err := required.Log(context.Background(), "s", "decision", "bash", "echo", "deny", "floor", nil, nil, "")
	if err == nil {
		t.Fatal("expected required audit write to fail")
	}
	optional := New(Config{Enabled: true, Path: path, Required: false})
	if err := optional.Log(context.Background(), "s", "decision", "bash", "echo", "deny", "floor", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if optional.Warning() == "" {
		t.Fatal("expected a visible audit warning")
	}
	if err := optional.Log(context.Background(), "s", "decision", "bash", "echo", "deny", "floor", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
}

func TestSummarizeOmitsBodiesAndCapsCommands(t *testing.T) {
	body := "FILEBODYSECRET-xyz"
	got := SummarizeAction("write_file", `{"path":"a.txt","content":"FILEBODYSECRET-xyz"}`)
	if strings.Contains(got, body) {
		t.Fatalf("file body leaked into summary: %s", got)
	}
	if !strings.Contains(got, "path=a.txt") || !strings.Contains(got, "after_sha256=") {
		t.Fatalf("summary missing path or hash: %s", got)
	}
	cmd := `{"command":"echo sk-ant-api03-1234567890abcdef1234567890abcdef"}`
	redacted := SummarizeAction("bash", cmd)
	if strings.Contains(redacted, "sk-ant-api03-1234567890abcdef1234567890abcdef") {
		t.Fatalf("command secret was stored: %s", redacted)
	}
	long := strings.Repeat("a", 3000)
	capped := SummarizeAction("bash", `{"command":"`+long+`"}`)
	if len(capped) > 2048+len("…[truncated]") {
		t.Fatalf("command summary was not capped: %d", len(capped))
	}
}

func TestAuditConcurrentProcesses(t *testing.T) {
	if os.Getenv("FORGE_AUDIT_HELPER") == "1" {
		logger := New(Config{Enabled: true, Path: os.Getenv("FORGE_AUDIT_PATH"), MaxMB: 10})
		id := os.Getenv("FORGE_AUDIT_SESSION")
		for i := 0; i < 8; i++ {
			if err := logger.Log(context.Background(), id, "tool_result", "bash", "echo concurrent", "allow", "mode", nil, nil, ""); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	start := func(id string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAuditConcurrentProcesses$")
		cmd.Env = append(os.Environ(),
			"FORGE_AUDIT_HELPER=1",
			"FORGE_AUDIT_PATH="+path,
			"FORGE_AUDIT_SESSION="+id,
		)
		return cmd
	}
	a := start("session-a")
	b := start("session-b")
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	if err := a.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := b.Wait(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"session-a", "session-b"} {
		n, err := Verify(path, id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if n != 8 {
			t.Fatalf("%s verified %d records, want 8", id, n)
		}
	}
}
