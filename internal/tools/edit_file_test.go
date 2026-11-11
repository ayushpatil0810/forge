package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFile_Table(t *testing.T) {
	tempDir := t.TempDir()
	tracker := NewReadTracker()
	tool := EditFile{Root: tempDir, Tracker: tracker}

	// Setup initial test file
	filePath := filepath.Join(tempDir, "sample.txt")
	initialContent := "func Add(a, b int) int {\n\treturn a - b // bug\n}\n"
	if err := os.WriteFile(filePath, []byte(initialContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Read-before-write guard: editing without reading first must fail
	inputJSON, _ := json.Marshal(map[string]any{
		"path":       "sample.txt",
		"old_string": "return a - b // bug",
		"new_string": "return a + b",
	})
	res, err := tool.Run(context.Background(), inputJSON)
	if err != nil || !res.IsError || !strings.Contains(res.Output, "must be read using read_file before editing") {
		t.Fatalf("expected read-before-write guard error, got res=%+v, err=%v", res, err)
	}

	// Mark as read
	tracker.MarkRead("sample.txt")

	// 2. Successful unique edit
	req := tool.NeedsApproval(inputJSON)
	if req.Diff == "" {
		t.Fatalf("expected non-empty diff in NeedsApproval")
	}
	if !strings.Contains(req.Diff, "-	return a - b // bug") || !strings.Contains(req.Diff, "+	return a + b") {
		t.Fatalf("unexpected diff content:\n%s", req.Diff)
	}

	res, err = tool.Run(context.Background(), inputJSON)
	if err != nil || res.IsError {
		t.Fatalf("unexpected error on valid edit: res=%+v, err=%v", res, err)
	}
	updated, err := os.ReadFile(filePath)
	if err != nil || !strings.Contains(string(updated), "return a + b") {
		t.Fatalf("file not updated correctly: %s", string(updated))
	}

	// 3. Match missing error
	missingJSON, _ := json.Marshal(map[string]any{
		"path":       "sample.txt",
		"old_string": "non_existent_code_line",
		"new_string": "new_code",
	})
	res, _ = tool.Run(context.Background(), missingJSON)
	if !res.IsError || !strings.Contains(res.Output, "old_string not found") {
		t.Fatalf("expected 'old_string not found' error, got: %s", res.Output)
	}

	// 4. Multiple matches error
	multiContent := "foo\nbar\nfoo\n"
	multiPath := filepath.Join(tempDir, "multi.txt")
	_ = os.WriteFile(multiPath, []byte(multiContent), 0644)
	tracker.MarkRead("multi.txt")

	multiJSON, _ := json.Marshal(map[string]any{
		"path":       "multi.txt",
		"old_string": "foo",
		"new_string": "baz",
	})
	res, _ = tool.Run(context.Background(), multiJSON)
	if !res.IsError || !strings.Contains(res.Output, "matched 2 times") {
		t.Fatalf("expected multiple match error, got: %s", res.Output)
	}

	// 5. Replace all succeeds
	replaceAllJSON, _ := json.Marshal(map[string]any{
		"path":        "multi.txt",
		"old_string":  "foo",
		"new_string":  "baz",
		"replace_all": true,
	})
	res, err = tool.Run(context.Background(), replaceAllJSON)
	if err != nil || res.IsError {
		t.Fatalf("expected replace_all to succeed: %+v", res)
	}
	multiUpdated, _ := os.ReadFile(multiPath)
	if string(multiUpdated) != "baz\nbar\nbaz\n" {
		t.Fatalf("replace_all failed: got %q", string(multiUpdated))
	}

	// 6. CRLF preservation
	crlfPath := filepath.Join(tempDir, "crlf.txt")
	_ = os.WriteFile(crlfPath, []byte("hello\r\nworld\r\n"), 0644)
	tracker.MarkRead("crlf.txt")

	crlfJSON, _ := json.Marshal(map[string]any{
		"path":       "crlf.txt",
		"old_string": "world",
		"new_string": "universe\nand beyond",
	})
	res, err = tool.Run(context.Background(), crlfJSON)
	if err != nil || res.IsError {
		t.Fatalf("crlf edit failed: %+v", res)
	}
	crlfUpdated, _ := os.ReadFile(crlfPath)
	if !strings.Contains(string(crlfUpdated), "universe\r\nand beyond\r\n") {
		t.Fatalf("expected CRLF line endings preserved, got %q", string(crlfUpdated))
	}
}

func TestEditFile_ProtectedPaths(t *testing.T) {
	tempDir := t.TempDir()
	tool := EditFile{Root: tempDir}

	gitDir := filepath.Join(tempDir, ".git")
	_ = os.MkdirAll(gitDir, 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0644)

	inputJSON, _ := json.Marshal(map[string]any{
		"path":       ".git/config",
		"old_string": "[core]",
		"new_string": "[core]\n\tbare = false",
	})
	req := tool.NeedsApproval(inputJSON)
	if !req.IsProtected {
		t.Fatalf("expected .git/config to be flagged as protected")
	}

	envFile := filepath.Join(tempDir, ".env.production")
	_ = os.WriteFile(envFile, []byte("SECRET=123\n"), 0644)
	envJSON, _ := json.Marshal(map[string]any{
		"path":       ".env.production",
		"old_string": "SECRET=123",
		"new_string": "SECRET=456",
	})
	reqEnv := tool.NeedsApproval(envJSON)
	if !reqEnv.IsProtected {
		t.Fatalf("expected .env.production to be flagged as protected")
	}
}

