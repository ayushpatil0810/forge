package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/policy"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestReadFile_AliasToCredentialIsProtected(t *testing.T) {
	ws := t.TempDir()
	secret := "DB_PASSWORD=poc-not-a-real-secret\n"
	envPath := filepath.Join(ws, ".env")
	if err := os.WriteFile(envPath, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(envPath, filepath.Join(ws, "notes.txt")); err != nil {
		t.Fatalf("hardlink: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(envPath, filepath.Join(ws, "sub", "notes.txt")); err != nil {
		t.Fatalf("nested hardlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "readme.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	assertAliasProtected(t, ws, "notes.txt", "same-directory hardlink")
	assertAliasProtected(t, ws, "sub/notes.txt", "nested hardlink")

	plain := tools.ReadFile{Root: ws, Tracker: tools.NewReadTracker()}
	plainReq := plain.NeedsApproval(json.RawMessage(`{"path":"readme.txt"}`))
	if plainReq.IsProtected {
		t.Fatal("a normal file with link count 1 was classified protected")
	}

	sym := filepath.Join(ws, "alias.txt")
	if err := os.Symlink(envPath, sym); err != nil {
		if runtime.GOOS == "windows" {
			t.Logf("symlink creation not permitted (%v); same-directory symlink case skipped", err)
		} else {
			t.Fatalf("symlink: %v", err)
		}
	} else {
		assertAliasProtected(t, ws, "alias.txt", "same-directory symlink")
	}

	ctx := context.Background()
	grepRes, err := tools.Grep{Root: ws}.Run(ctx, json.RawMessage(`{"query":"DB_PASSWORD","path":"."}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(grepRes.Output, "DB_PASSWORD") || strings.Contains(grepRes.Output, "notes.txt") {
		t.Fatalf("grep returned a credential alias:\n%s", grepRes.Output)
	}
	if !strings.Contains(grepRes.Output, "skipped") {
		t.Fatalf("grep did not report a skipped count:\n%s", grepRes.Output)
	}

	globRes, err := tools.Glob{Root: ws}.Run(ctx, json.RawMessage(`{"pattern":"**/*.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(globRes.Output, "notes.txt") || strings.Contains(globRes.Output, "alias.txt") {
		t.Fatalf("glob listed a credential alias:\n%s", globRes.Output)
	}
	if !strings.Contains(globRes.Output, "skipped") {
		t.Fatalf("glob did not report a skipped count:\n%s", globRes.Output)
	}
	if !strings.Contains(globRes.Output, "readme.txt") {
		t.Fatalf("glob dropped an ordinary file:\n%s", globRes.Output)
	}

	listRes, err := tools.ListDir{Root: ws}.Run(ctx, json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listRes.Output, "notes.txt") || strings.Contains(listRes.Output, ".env") {
		t.Fatalf("list_dir listed a protected name:\n%s", listRes.Output)
	}
	if !strings.Contains(listRes.Output, "skipped") {
		t.Fatalf("list_dir did not report a skipped count:\n%s", listRes.Output)
	}
	if !strings.Contains(listRes.Output, "readme.txt") {
		t.Fatalf("list_dir dropped an ordinary file:\n%s", listRes.Output)
	}
}

func assertAliasProtected(t *testing.T, ws, rel, label string) {
	t.Helper()
	rf := tools.ReadFile{Root: ws, Tracker: tools.NewReadTracker()}
	raw := json.RawMessage(fmt.Sprintf(`{"path":%q}`, rel))
	approval := rf.NeedsApproval(raw)
	if !approval.IsProtected {
		t.Fatalf("%s: read_file did not mark %s protected (approval=%+v)", label, rel, approval)
	}
	if got := (permission.Policy{Mode: permission.ModePlan}).Check("read_file", approval); got != permission.Ask {
		t.Fatalf("%s: plan mode verdict %v, want Ask", label, got)
	}
	if got := (permission.Policy{Mode: permission.ModeYOLO}).Check("read_file", approval); got != permission.Ask {
		t.Fatalf("%s: yolo mode verdict %v, want Ask", label, got)
	}
	ev := policy.Evaluate(policy.EvalRequest{
		ToolName:    "read_file",
		Path:        approval.Path,
		IsProtected: approval.IsProtected,
		Mode:        permission.ModePlan,
		IsHeadless:  true,
	}, &policy.PolicySet{})
	if ev.Verdict != permission.Deny || !ev.IsFloor {
		t.Fatalf("%s: headless plan verdict %+v, want floor deny", label, ev)
	}
}
