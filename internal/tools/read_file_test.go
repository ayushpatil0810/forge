package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, root, path string) Result {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"path": path})
	res, err := ReadFile{Root: root}.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestReadFileNumbersLines(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo"), 0o644)
	res := run(t, dir, "a.txt")
	if res.IsError || !strings.Contains(res.Output, "     2\ttwo") {
		t.Fatalf("bad output: %+v", res)
	}
}

func TestReadFileBlocksEscapes(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "root")
	os.Mkdir(root, 0o755)
	os.WriteFile(filepath.Join(outer, "secret.txt"), []byte("s3cret"), 0o644)
	os.Symlink(filepath.Join(outer, "secret.txt"), filepath.Join(root, "link.txt"))

	for _, p := range []string{"../secret.txt", filepath.Join(outer, "secret.txt"), "link.txt"} {
		if res := run(t, root, p); !res.IsError || strings.Contains(res.Output, "s3cret") {
			t.Errorf("path %q escaped the workspace: %+v", p, res)
		}
	}
}
