package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/tools"
)

func runListDir(t *testing.T, root, path string, recursive bool) tools.Result {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"path": path, "recursive": recursive})
	res, err := tools.ListDir{Root: root}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}
	return res
}

func TestListDirBasicAndSorting(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)

	res := runListDir(t, dir, ".", false)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}

	lines := strings.Split(res.Output, "\n")
	expected := []string{"a.txt", "b.txt", "sub/"}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d:\n%s", len(expected), len(lines), res.Output)
	}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, lines[i])
		}
	}
}

func TestListDirRespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n*.log\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "ignored"), 0o755)
	os.WriteFile(filepath.Join(dir, "ignored", "secret.txt"), []byte("secret"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.log"), []byte("log"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)

	res := runListDir(t, dir, ".", false)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if strings.Contains(res.Output, "ignored") || strings.Contains(res.Output, "app.log") {
		t.Errorf("expected ignored files to be excluded, got:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "main.go") {
		t.Errorf("expected main.go in output, got:\n%s", res.Output)
	}
}

func TestListDirRecursive(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "lib.go"), []byte("package pkg"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)

	res := runListDir(t, dir, ".", true)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Output, "src/pkg/lib.go") {
		t.Errorf("expected src/pkg/lib.go in recursive listing, got:\n%s", res.Output)
	}
}

