package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type ReadFile struct {
	Root string
}

const readFileSchema = `{
  "type": "object",
  "properties": {
    "path":   {"type": "string",  "description": "File path, relative to the workspace root"},
    "offset": {"type": "integer", "description": "1-based line to start from (default 1)"},
    "limit":  {"type": "integer", "description": "Max lines to return (default 2000)"}
  },
  "required": ["path"]
}`

func (ReadFile) Name() string {
	return "read_file"
}

func (ReadFile) Description() string {
	return "Read a text file from the workspace. Returns numbered lines."
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
		return Result{Output: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if args.Offset < 1 {
		args.Offset = 1
	}
	if args.Limit < 1 {
		args.Limit = 2000
	}
	path, err := ResolveInRoot(r.Root, args.Path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	lines := strings.Split(string(data), "\n")
	start := args.Offset - 1
	if start >= len(lines) {
		return Result{Output: "offset is past end of file", IsError: true}, nil
	}
	end := min(start+args.Limit, len(lines))
	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, lines[i])
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "... (%d more lines, use offset=%d)\n", len(lines)-end, end+1)
	}
	return Result{Output: b.String()}, nil
}
