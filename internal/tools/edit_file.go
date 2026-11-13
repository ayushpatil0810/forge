package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type EditFile struct {
	Root    string
	Tracker *ReadTracker
}

const editFileSchema = `{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "File path, relative to the workspace root"
    },
    "old_string": {
      "type": "string",
      "description": "The exact text to replace. Must match uniquely unless replace_all is true."
    },
    "new_string": {
      "type": "string",
      "description": "The replacement text."
    },
    "replace_all": {
      "type": "boolean",
      "description": "If true, replace every occurrence of old_string. Default is false."
    }
  },
  "required": ["path", "old_string", "new_string"]
} `

func (EditFile) Name() string {
	return "edit_file"
}

func (EditFile) Description() string {
	return "Replace text in an existing file. Requires an exact, unique match unless replace_all is true. Preserves line endings and verifies changes."
}

func (EditFile) Schema() json.RawMessage {
	return json.RawMessage(editFileSchema)
}

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func (e EditFile) parseArgs(input json.RawMessage) (editArgs, error) {
	var args editArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return args, fmt.Errorf("invalid input for edit_file: %w", err)
	}
	if strings.TrimSpace(args.Path) == "" {
		return args, fmt.Errorf("edit_file error: 'path' argument cannot be empty")
	}
	if args.OldString == "" {
		return args, fmt.Errorf("edit_file error: 'old_string' cannot be empty")
	}
	if args.OldString == args.NewString {
		return args, fmt.Errorf("edit_file error: 'old_string' and 'new_string' are identical; no changes to make")
	}
	return args, nil
}

func (e EditFile) NeedsApproval(input json.RawMessage) ApprovalRequest {
	args, err := e.parseArgs(input)
	if err != nil {
		return ApprovalRequest{}
	}

	targetPath, err := ResolveInRoot(e.Root, args.Path)
	if err != nil {
		return ApprovalRequest{}
	}

	contentBytes, err := os.ReadFile(targetPath)
	if err != nil {
		return ApprovalRequest{}
	}

	content := string(contentBytes)
	newContent, err := applyReplacement(content, args.OldString, args.NewString, args.ReplaceAll)
	if err != nil {
		// If replacement cannot be applied cleanly, let Run handle reporting the error
		return ApprovalRequest{}
	}

	diff := GenerateDiff(args.Path, args.Path, content, newContent)
	return ApprovalRequest{
		Description: fmt.Sprintf("Edit %s", args.Path),
		Diff:        diff,
		IsProtected: protectedPath(e.Root, args.Path, targetPath),
	}
}

func (e EditFile) Run(ctx context.Context, input json.RawMessage) (Result, error) {
	args, err := e.parseArgs(input)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}

	targetPath, err := ResolveInRoot(e.Root, args.Path)
	if err != nil {
		return Result{Output: fmt.Sprintf("edit_file path error: %s", err), IsError: true}, nil
	}

	// Read-before-write guard
	if e.Tracker != nil && !e.Tracker.HasBeenRead(args.Path) {
		return Result{
			Output:  fmt.Sprintf("edit_file error: file %q must be read using read_file before editing; please read it first", args.Path),
			IsError: true,
		}, nil
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Output: fmt.Sprintf("edit_file error: file %q does not exist; use write_file to create new files", args.Path), IsError: true}, nil
		}
		return Result{Output: fmt.Sprintf("edit_file stat error: %s", err), IsError: true}, nil
	}
	if info.IsDir() {
		return Result{Output: fmt.Sprintf("edit_file error: %q is a directory, not a file", args.Path), IsError: true}, nil
	}

	contentBytes, err := os.ReadFile(targetPath)
	if err != nil {
		return Result{Output: fmt.Sprintf("edit_file read error: %s", err), IsError: true}, nil
	}

	content := string(contentBytes)
	newContent, err := applyReplacement(content, args.OldString, args.NewString, args.ReplaceAll)
	if err != nil {
		return Result{Output: fmt.Sprintf("edit_file error in %q: %s", args.Path, err), IsError: true}, nil
	}

	// Atomic write
	if err := atomicWriteFile(targetPath, []byte(newContent), info.Mode().Perm()); err != nil {
		return Result{Output: fmt.Sprintf("edit_file write error: %s", err), IsError: true}, nil
	}

	if e.Tracker != nil {
		e.Tracker.MarkRead(args.Path)
	}

	// Post-edit verification snippet
	snippet := formatSnippet(newContent, 60)
	return Result{
		Output: fmt.Sprintf("Successfully edited %s\nUpdated content:\n%s", args.Path, snippet),
	}, nil
}

// applyReplacement handles exact matching, line endings, and counts.
func applyReplacement(content, oldStr, newStr string, replaceAll bool) (string, error) {
	// Line ending normalization: if file is CRLF, adapt replacement string
	isCRLF := strings.Contains(content, "\r\n")
	if isCRLF {
		// Normalize newStr to CRLF if it contains bare \n
		newStr = strings.ReplaceAll(newStr, "\r\n", "\n")
		newStr = strings.ReplaceAll(newStr, "\n", "\r\n")
		oldStr = strings.ReplaceAll(oldStr, "\r\n", "\n")
		oldStr = strings.ReplaceAll(oldStr, "\n", "\r\n")
	} else {
		// If file is LF, normalize newStr to LF
		newStr = strings.ReplaceAll(newStr, "\r\n", "\n")
		oldStr = strings.ReplaceAll(oldStr, "\r\n", "\n")
	}

	count := strings.Count(content, oldStr)
	if count == 0 {
		return "", fmt.Errorf("old_string not found; check exact lines, indentation, and whitespace using read_file")
	}
	if count > 1 && !replaceAll {
		return "", fmt.Errorf("old_string matched %d times; provide more surrounding lines to make the match unique, or set replace_all: true", count)
	}

	if replaceAll {
		return strings.ReplaceAll(content, oldStr, newStr), nil
	}
	return strings.Replace(content, oldStr, newStr, 1), nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".forge_tmp_*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName) // safe if renamed
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	_ = os.Chmod(tmpName, perm)

	// On Windows, rename fails if destination exists unless using atomic replace
	if err := replaceFile(tmpName, path); err != nil {
		return fmt.Errorf("replacing file: %w", err)
	}
	return nil
}

func replaceFile(src, dst string) error {
	// Attempt direct rename first
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Fallback for Windows if destination exists
	_ = os.Remove(dst)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Final fallback: copy bytes
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

func formatSnippet(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	var b strings.Builder
	limit := min(len(lines), maxLines)
	for i := 0; i < limit; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, lines[i])
	}
	if len(lines) > limit {
		fmt.Fprintf(&b, "... [%d more lines]\n", len(lines)-limit)
	}
	return b.String()
}
