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

func runGrep(t *testing.T, root, query, path, glob string) tools.Result {
	t.Helper()
	inputMap := map[string]any{"query": query}
	if path != "" {
		inputMap["path"] = path
	}
	if glob != "" {
		inputMap["glob"] = glob
	}
	in, _ := json.Marshal(inputMap)
	res, err := tools.Grep{Root: root}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}
	return res
}

func TestGrepFindsMatches(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg", "hello.go"), []byte("package pkg\n\nfunc SayHello() string {\n\treturn \"Hello World\"\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Title\nSayHello example here\n"), 0o644)

	res := runGrep(t, dir, "SayHello", "", "")
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Output, "pkg/hello.go:3:func SayHello() string {") {
		t.Errorf("expected hello.go match, got:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "README.md:2:SayHello example here") {
		t.Errorf("expected README.md match, got:\n%s", res.Output)
	}
}

func TestGrepWithGlobFilter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "code.go"), []byte("target string"), 0o644)
	os.WriteFile(filepath.Join(dir, "code.md"), []byte("target string"), 0o644)

	res := runGrep(t, dir, "target", "", "*.go")
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Output, "code.go:1:target string") {
		t.Errorf("expected code.go match, got:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "code.md") {
		t.Errorf("unexpected code.md match when filtering with *.go:\n%s", res.Output)
	}
}

func TestGrepSkipsBinaryAndIgnored(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("needle in ignored"), 0o644)
	os.WriteFile(filepath.Join(dir, "binary.dat"), []byte("needle\x00binary"), 0o644)
	os.WriteFile(filepath.Join(dir, "valid.txt"), []byte("needle in valid"), 0o644)

	res := runGrep(t, dir, "needle", "", "")
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Output, "valid.txt:1:needle in valid") {
		t.Errorf("expected valid.txt match, got:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "ignored.txt") {
		t.Errorf("expected ignored.txt to be skipped, got:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "binary.dat") {
		t.Errorf("expected binary.dat to be skipped, got:\n%s", res.Output)
	}
}

func TestGrepInvalidRegex(t *testing.T) {
	dir := t.TempDir()
	res := runGrep(t, dir, "[unclosed-bracket", "", "")
	if !res.IsError || !strings.Contains(res.Output, "invalid regular expression") {
		t.Errorf("expected invalid regex error, got: %+v", res)
	}
}
