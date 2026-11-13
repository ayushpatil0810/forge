package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestWriteFile_JunctionOrSymlinkDoesNotEscape(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	ws := filepath.Join(parent, "ws")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "exist.txt")
	if err := os.WriteFile(outsideFile, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "windows" {
		link := filepath.Join(ws, "link")
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J: %v %s", err, out)
		}
		assertNoEscape(t, ws, outside, "link/pwned.txt", "missing final component through a junction")
		assertNoEscape(t, ws, outside, "link/exist.txt", "existing outside target through a junction")
		assertNoEscape(t, ws, outside, "link/../exist.txt", ".. after a junction")
	}

	sym := filepath.Join(ws, "sym")
	if err := os.Symlink(outside, sym); err != nil {
		if runtime.GOOS == "windows" {
			t.Logf("symlink creation not permitted (%v); POSIX case skipped", err)
		} else {
			t.Skipf("symlink creation not permitted: %v", err)
		}
	} else {
		assertNoEscape(t, ws, outside, "sym/pwned.txt", "missing final component through a symlink")
		assertNoEscape(t, ws, outside, "sym/exist.txt", "existing outside target through a symlink")
		assertNoEscape(t, ws, outside, "sym/../exist.txt", ".. after a symlink")
	}
}

func TestResolveInRoot_CallerSpellingOfRoot(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J: %v %s", err, out)
		}
	} else if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink creation not permitted: %v", err)
	}

	got, err := tools.ResolveInRoot(link, "hello.txt")
	if err != nil {
		t.Fatalf("ResolveInRoot: %v", err)
	}
	if !stringsHasPrefixFold(got, link) {
		t.Fatalf("resolved %q is not under the caller's root %q", got, link)
	}
	body, err := os.ReadFile(got)
	if err != nil || string(body) != "hello" {
		t.Fatalf("read through caller spelling: %q err=%v", body, err)
	}
	if _, err := tools.ResolveInRoot(link, "../real/hello.txt"); err == nil {
		t.Fatal("path that leaves the linked root was accepted")
	}

	ctx := context.Background()
	listed, err := tools.ListDir{Root: link}.Run(ctx, json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.Output, "hello.txt") {
		t.Fatalf("list_dir through an alternate root spelling:\n%s", listed.Output)
	}
}

func TestResolveInRoot_WindowsSpecialPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC, \\\\?\\ and drive-letter checks are Windows-specific")
	}
	ws := t.TempDir()
	cases := []string{
		`\\?\C:\Windows\notepad.exe`,
		`\\localhost\c$\Windows\notepad.exe`,
		`\\127.0.0.1\share\secret.txt`,
	}
	vol := filepath.VolumeName(ws)
	if len(vol) >= 2 && vol[1] == ':' {
		other := "D:"
		if vol[0] == 'D' || vol[0] == 'd' {
			other = "C:"
		}
		cases = append(cases, other+`\outside.txt`)
	}
	for _, c := range cases {
		if _, err := tools.ResolveInRoot(ws, c); err == nil {
			t.Errorf("ResolveInRoot(%q) succeeded, want an outside-workspace error", c)
		}
	}
	// A \\?\ path that is inside the workspace is allowed.
	inside := `\\?\` + filepath.Join(ws, "ok.txt")
	got, err := tools.ResolveInRoot(ws, inside)
	if err != nil {
		t.Fatalf("inside \\\\?\\ path rejected: %v", err)
	}
	if !stringsHasPrefixFold(got, ws) {
		t.Fatalf("inside path resolved to %q, outside %q", got, ws)
	}
}

func assertNoEscape(t *testing.T, ws, outside, rel, label string) {
	t.Helper()
	leaked := filepath.Join(outside, filepath.Base(rel))
	before, beforeErr := os.ReadFile(leaked)
	w := tools.WriteFile{Root: ws, Tracker: tools.NewReadTracker()}
	payload, _ := json.Marshal(map[string]string{"path": rel, "content": "escaped"})
	res, err := w.Run(context.Background(), payload)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	after, afterErr := os.ReadFile(leaked)
	if afterErr == nil && string(after) == "escaped" && (beforeErr != nil || string(before) != "escaped") {
		t.Fatalf("%s: wrote %s outside the workspace (tool=%q)", label, leaked, res.Output)
	}
	if beforeErr != nil && afterErr == nil {
		t.Fatalf("%s: created %s outside the workspace (tool=%q)", label, leaked, res.Output)
	}
	if !res.IsError {
		t.Fatalf("%s: expected a path error, got %q", label, res.Output)
	}
}

func stringsHasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return len(s) >= len(prefix) && equalFold(s[:len(prefix)], prefix)
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
