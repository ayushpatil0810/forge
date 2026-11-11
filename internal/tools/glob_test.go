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

func runGlob(t *testing.T, root, pattern string) tools.Result {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"pattern": pattern})
	res, err := tools.Glob{Root: root}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}
	return res
}

func TestGlobDoubleStar(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b", "c"), 0o755)
	os.WriteFile(filepath.Join(dir, "root.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "a", "b", "c", "deep.go"), []byte("package c"), 0o644)
	os.WriteFile(filepath.Join(dir, "a", "b", "c", "notes.txt"), []byte("notes"), 0o644)

	res := runGlob(t, dir, "**/*.go")
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Output, "root.go") {
		t.Errorf("expected root.go in output, got:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "a/b/c/deep.go") {
		t.Errorf("expected a/b/c/deep.go in output, got:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "notes.txt") {
		t.Errorf("unexpected notes.txt in *.go glob output:\n%s", res.Output)
	}
}

func TestGlobRespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("vendor/\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "vendor", "mod"), 0o755)
	os.WriteFile(filepath.Join(dir, "vendor", "mod", "mod.go"), []byte("package mod"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main"), 0o644)

	res := runGlob(t, dir, "**/*.go")
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if strings.Contains(res.Output, "vendor") {
		t.Errorf("expected vendor to be excluded by gitignore, got:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "app.go") {
		t.Errorf("expected app.go in output, got:\n%s", res.Output)
	}
}

