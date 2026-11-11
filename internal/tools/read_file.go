package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	MaxFileSize   = 5 * 1024 * 1024 // 5 MB cap
	DefaultLimit  = 2000
	DefaultOffset = 1
)

type ReadFile struct {
	Root    string
	Tracker *ReadTracker
}

const readFileSchema = `{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "File path, relative to the workspace root"
    },
    "offset": {
      "type": "integer",
      "description": "1-based line number to start reading from (default 1)"
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of lines to return (default 2000)"
    }
  },
  "required": ["path"]
}`

func (ReadFile) Name() string {
	return "read_file"
}

func (ReadFile) Description() string {
	return "Read a text file from the workspace with line numbers. Supports line-range pagination and detects binary files."
}

func (ReadFile) Schema() json.RawMessage {
	return json.RawMessage(readFileSchema)
}

func (ReadFile) NeedsApproval(json.RawMessage) ApprovalRequest {
	return ApprovalRequest{}
}

func (r ReadFile) Run(_ context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return Result{Output: fmt.Sprintf("invalid input for read_file: %s", err), IsError: true}, nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return Result{Output: "read_file error: 'path' argument cannot be empty", IsError: true}, nil
	}

	if args.Offset < 1 {
		args.Offset = DefaultOffset
	}
	if args.Limit < 1 {
		args.Limit = DefaultLimit
	}

	targetPath, err := ResolveInRoot(r.Root, args.Path)
	if err != nil {
		return Result{Output: fmt.Sprintf("read_file path error: %s", err), IsError: true}, nil
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Output: fmt.Sprintf("read_file error: file %q does not exist", args.Path), IsError: true}, nil
		}
		return Result{Output: fmt.Sprintf("read_file stat error: %s", err), IsError: true}, nil
	}

	if info.IsDir() {
		return Result{Output: fmt.Sprintf("read_file error: %q is a directory, not a file; use list_dir instead", args.Path), IsError: true}, nil
	}

	if info.Size() > MaxFileSize {
		return Result{
			Output:  fmt.Sprintf("read_file error: file %q is %d bytes, which exceeds the %d byte size cap", args.Path, info.Size(), MaxFileSize),
			IsError: true,
		}, nil
	}

	file, err := os.Open(targetPath)
	if err != nil {
		return Result{Output: fmt.Sprintf("read_file error opening file: %s", err), IsError: true}, nil
	}
	defer file.Close()

	// Binary check: read first 512 bytes and check for null byte
	header := make([]byte, 512)
	n, _ := file.Read(header)
	if n > 0 && bytes.IndexByte(header[:n], 0) != -1 {
		return Result{Output: fmt.Sprintf("read_file error: %q is a binary file; cannot display text contents", args.Path), IsError: true}, nil
	}

	// Seek back to start and read full content
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Result{Output: fmt.Sprintf("read_file seek error: %s", err), IsError: true}, nil
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return Result{Output: fmt.Sprintf("read_file read error: %s", err), IsError: true}, nil
	}

	raw := string(data)
	lines := strings.Split(raw, "\n")
	totalLines := len(lines)

	start := args.Offset - 1
	if start >= totalLines {
		return Result{
			Output:  fmt.Sprintf("read_file error: offset %d is past end of file (file has %d lines)", args.Offset, totalLines),
			IsError: true,
		}, nil
	}

	end := min(start+args.Limit, totalLines)

	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, lines[i])
	}

	if end < totalLines {
		remaining := totalLines - end
		fmt.Fprintf(&b, "... [%d more lines available in %s; use offset=%d to read more]\n", remaining, args.Path, end+1)
	}

	if r.Tracker != nil {
		r.Tracker.MarkRead(args.Path)
	}

	return Result{Output: b.String()}, nil
}
