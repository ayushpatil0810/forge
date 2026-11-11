package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/tools"
)

func runReadFile(t *testing.T, root, path string, offset, limit int) tools.Result {
	t.Helper()
	inputMap := map[string]any{"path": path}
	if offset > 0 {
		inputMap["offset"] = offset
	}
	if limit > 0 {
		inputMap["limit"] = limit
	}
	in, _ := json.Marshal(inputMap)
	res, err := tools.ReadFile{Root: root}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected Run error (should return Result with IsError): %v", err)
	}
	return res
}

func TestReadFileNumbersLinesAndPagination(t *testing.T) {
	dir := t.TempDir()
	lines := make([]string, 50)
	for i := 0; i < 50; i++ {
		lines[i] = fmt.Sprintf("line number %d", i+1)
	}
	content := strings.Join(lines, "\n")
	os.WriteFile(filepath.Join(dir, "numbers.txt"), []byte(content), 0o644)

	// Read lines 10 to 19 (offset 10, limit 10)
	res := runReadFile(t, dir, "numbers.txt", 10, 10)
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	if !strings.Contains(res.Output, "    10\tline number 10") {
		t.Errorf("expected line 10, got:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "    19\tline number 19") {
		t.Errorf("expected line 19, got:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "    20\tline number 20") {
		t.Errorf("unexpected line 20 in paginated output:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "more lines available") {
		t.Errorf("expected truncation notice in output:\n%s", res.Output)
	}
}

func TestReadFileDetectsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	binData := []byte("hello\x00world\x01\x02binary")
	os.WriteFile(filepath.Join(dir, "test.bin"), binData, 0o644)

	res := runReadFile(t, dir, "test.bin", 1, 100)
	if !res.IsError {
		t.Fatalf("expected error for binary file, got success: %+v", res)
	}
	if !strings.Contains(res.Output, "binary file") {
		t.Errorf("expected 'binary file' error message, got: %s", res.Output)
	}
}

func TestReadFileExceedsSizeCap(t *testing.T) {
	dir := t.TempDir()
	largeData := bytes.Repeat([]byte("a"), tools.MaxFileSize+10)
	os.WriteFile(filepath.Join(dir, "large.txt"), largeData, 0o644)

	res := runReadFile(t, dir, "large.txt", 1, 100)
	if !res.IsError {
		t.Fatalf("expected error for file exceeding size cap, got success: %+v", res)
	}
	if !strings.Contains(res.Output, "exceeds") {
		t.Errorf("expected 'exceeds' in error message, got: %s", res.Output)
	}
}

func TestReadFileActionableErrors(t *testing.T) {
	dir := t.TempDir()
	// Directory instead of file
	subDir := filepath.Join(dir, "subdir")
	os.MkdirAll(subDir, 0o755)

	resDir := runReadFile(t, dir, "subdir", 1, 100)
	if !resDir.IsError || !strings.Contains(resDir.Output, "directory") {
		t.Errorf("expected directory error, got: %+v", resDir)
	}

	// Non-existent file
	resMissing := runReadFile(t, dir, "missing.txt", 1, 100)
	if !resMissing.IsError || !strings.Contains(resMissing.Output, "does not exist") {
		t.Errorf("expected non-existent error, got: %+v", resMissing)
	}

	// Offset out of range
	os.WriteFile(filepath.Join(dir, "small.txt"), []byte("line1\nline2"), 0o644)
	resOffset := runReadFile(t, dir, "small.txt", 10, 10)
	if !resOffset.IsError || !strings.Contains(resOffset.Output, "past end of file") {
		t.Errorf("expected past end of file error, got: %+v", resOffset)
	}
}
