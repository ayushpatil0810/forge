package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFile(t *testing.T) {
	tempDir := t.TempDir()
	tracker := NewReadTracker()
	tool := WriteFile{Root: tempDir, Tracker: tracker}

	// 1. New file creation succeeds without prior read
	newJSON, _ := json.Marshal(map[string]any{
		"path":    "sub/newfile.go",
		"content": "package sub\n\nfunc Hello() {}\n",
	})
	req := tool.NeedsApproval(newJSON)
	if req.Diff == "" {
		t.Fatalf("expected non-empty diff for new file")
	}
	if !strings.Contains(req.Diff, "+package sub") {
		t.Fatalf("unexpected diff content:\n%s", req.Diff)
	}

	res, err := tool.Run(context.Background(), newJSON)
	if err != nil || res.IsError {
		t.Fatalf("expected write to succeed: res=%+v, err=%v", res, err)
	}

	created, err := os.ReadFile(filepath.Join(tempDir, "sub", "newfile.go"))
	if err != nil || string(created) != "package sub\n\nfunc Hello() {}\n" {
		t.Fatalf("file content mismatch: %s", string(created))
	}

	// 2. Overwrite without reading must fail
	// Create another tracker that has not read it
	freshTracker := NewReadTracker()
	toolNoRead := WriteFile{Root: tempDir, Tracker: freshTracker}

	overwriteJSON, _ := json.Marshal(map[string]any{
		"path":    "sub/newfile.go",
		"content": "package sub\n\nfunc Overwritten() {}\n",
	})
	res, _ = toolNoRead.Run(context.Background(), overwriteJSON)
	if !res.IsError || !strings.Contains(res.Output, "already exists and must be read using read_file before overwriting") {
		t.Fatalf("expected overwrite guard error, got: %+v", res)
	}

	// 3. Overwrite with tracker having read it succeeds
	freshTracker.MarkRead("sub/newfile.go")
	res, err = toolNoRead.Run(context.Background(), overwriteJSON)
	if err != nil || res.IsError {
		t.Fatalf("expected overwrite to succeed after reading: %+v", res)
	}

	overwritten, _ := os.ReadFile(filepath.Join(tempDir, "sub", "newfile.go"))
	if string(overwritten) != "package sub\n\nfunc Overwritten() {}\n" {
		t.Fatalf("file content mismatch: %s", string(overwritten))
	}
}

