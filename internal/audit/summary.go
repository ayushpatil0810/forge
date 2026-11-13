package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ayushpatil0810/forge/internal/redact"
)

const maxCommandSummary = 2048

// SummarizeAction is what the audit log stores for a tool call.
// write_file and edit_file contribute a path, content hashes, and a line-count
// delta. The file body is never included. Bash contributes the redacted command,
// capped at 2 KB.
func SummarizeAction(tool, input string) string {
	switch tool {
	case "bash":
		return capSummary(redact.Redact(bashCommand(input)))
	case "write_file", "edit_file":
		return fileSummary(tool, input)
	default:
		return capSummary(redact.Redact(input))
	}
}

func bashCommand(input string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(input), &args); err == nil && args.Command != "" {
		return args.Command
	}
	return input
}

func fileSummary(tool, input string) string {
	var body struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	_ = json.Unmarshal([]byte(input), &body)
	before := body.OldString
	after := body.NewString
	if tool == "write_file" {
		before = ""
		after = body.Content
	}
	return fmt.Sprintf("path=%s before_sha256=%s after_sha256=%s line_delta=%d",
		body.Path, shaHex(before), shaHex(after), lineCount(after)-lineCount(before))
}

func shaHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func capSummary(s string) string {
	if len(s) <= maxCommandSummary {
		return s
	}
	return s[:maxCommandSummary] + "…[truncated]"
}
