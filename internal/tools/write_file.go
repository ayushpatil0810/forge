package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type WriteFile struct {
	Root    string
	Tracker *ReadTracker
}

const writeFileSchema = `{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "File path, relative to the workspace root"
    },
    "content": {
      "type": "string",
      "description": "The full content to write into the file"
    }
  },
  "required": ["path", "content"]
}`

func (WriteFile) Name() string {
	return "write_file"
}

func (WriteFile) Description() string {
	return "Create a new file or overwrite an existing file with the provided content. Overwrites require prior reading via read_file."
}

func (WriteFile) Schema() json.RawMessage {
	return json.RawMessage(writeFileSchema)
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (w WriteFile) parseArgs(input json.RawMessage) (writeArgs, error) {
	var args writeArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return args, fmt.Errorf("invalid input for write_file: %w", err)
	}
	if strings.TrimSpace(args.Path) == "" {
		return args, fmt.Errorf("write_file error: 'path' argument cannot be empty")
	}
	return args, nil
}

func (w WriteFile) NeedsApproval(input json.RawMessage) ApprovalRequest {
	args, err := w.parseArgs(input)
	if err != nil {
		return ApprovalRequest{}
	}

	targetPath, err := ResolveInRoot(w.Root, args.Path)
	if err != nil {
		return ApprovalRequest{}
	}

	var oldContent string
	if data, err := os.ReadFile(targetPath); err == nil {
		oldContent = string(data)
	}

	diff := GenerateDiff(args.Path, args.Path, oldContent, args.Content)
	return ApprovalRequest{
		Description: fmt.Sprintf("Write %s", args.Path),
		Diff:        diff,
		IsProtected: IsProtectedPath(args.Path),
	}
}

func (w WriteFile) Run(ctx context.Context, input json.RawMessage) (Result, error) {
	args, err := w.parseArgs(input)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}

	targetPath, err := ResolveInRoot(w.Root, args.Path)
	if err != nil {
		return Result{Output: fmt.Sprintf("write_file path error: %s", err), IsError: true}, nil
	}

	// Check if file exists; if so, enforce read-before-write guard
	if _, err := os.Stat(targetPath); err == nil {
		if w.Tracker != nil && !w.Tracker.HasBeenRead(args.Path) {
			return Result{
				Output:  fmt.Sprintf("write_file error: file %q already exists and must be read using read_file before overwriting; please read it first", args.Path),
				IsError: true,
			}, nil
		}
	}

	// Ensure parent directory exists
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return Result{Output: fmt.Sprintf("write_file error creating directory %s: %s", dir, err), IsError: true}, nil
	}

	// Atomic write
	if err := atomicWriteFile(targetPath, []byte(args.Content), 0644); err != nil {
		return Result{Output: fmt.Sprintf("write_file error: %s", err), IsError: true}, nil
	}

	if w.Tracker != nil {
		w.Tracker.MarkRead(args.Path)
	}

	return Result{
		Output: fmt.Sprintf("Successfully wrote %d bytes to %s", len(args.Content), args.Path),
	}, nil
}
