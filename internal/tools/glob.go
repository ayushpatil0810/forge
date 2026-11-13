package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const MaxGlobEntries = 500

type Glob struct {
	Root string
}

const globSchema = `{
  "type": "object",
  "properties": {
    "pattern": {
      "type": "string",
      "description": "Glob pattern matching files relative to the workspace root (supports ** for recursive globbing, e.g. '**/*.go')"
    }
  },
  "required": ["pattern"]
}`

func (Glob) Name() string {
	return "glob"
}

func (Glob) Description() string {
	return "Find files matching a glob pattern (supports ** for recursive directory search), respecting .gitignore."
}

func (Glob) Schema() json.RawMessage {
	return json.RawMessage(globSchema)
}

func (Glob) NeedsApproval(_ json.RawMessage) ApprovalRequest {
	return ApprovalRequest{}
}

func (g Glob) Run(_ context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return Result{Output: fmt.Sprintf("invalid input for glob: %s", err), IsError: true}, nil
	}
	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return Result{Output: "glob error: 'pattern' argument cannot be empty", IsError: true}, nil
	}

	// Normalize pattern to slash format
	normPattern := filepath.ToSlash(pattern)
	normPattern = strings.TrimPrefix(normPattern, "./")
	normPattern = strings.TrimPrefix(normPattern, "/")

	// Validate pattern syntax
	if !doublestar.ValidatePattern(normPattern) {
		return Result{Output: fmt.Sprintf("glob error: invalid pattern %q", pattern), IsError: true}, nil
	}

	ign := LoadGitIgnore(g.Root)
	fsys := os.DirFS(g.Root)

	matches, err := doublestar.Glob(fsys, normPattern)
	if err != nil {
		return Result{Output: fmt.Sprintf("glob error evaluating pattern: %s", err), IsError: true}, nil
	}

	var results []string
	truncated := 0
	skippedCredentials := 0

	for _, m := range matches {
		if IsIgnored(ign, m) {
			continue
		}
		full := filepath.Join(g.Root, filepath.FromSlash(m))
		if entryProtected(g.Root, m, full) {
			skippedCredentials++
			continue
		}
		// Confirm path is inside root via ResolveInRoot
		if _, err := ResolveInRoot(g.Root, m); err != nil {
			continue
		}

		if len(results) >= MaxGlobEntries {
			truncated++
			continue
		}
		results = append(results, m)
	}

	sort.Strings(results)

	if len(results) == 0 {
		out := "No files matched the pattern"
		if skippedCredentials > 0 {
			out += fmt.Sprintf("\n(skipped %d credential files)", skippedCredentials)
		}
		return Result{Output: out}, nil
	}

	var b strings.Builder
	for _, r := range results {
		b.WriteString(r)
		b.WriteString("\n")
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "... [%d more matching files truncated; cap is %d entries. Narrow your pattern]\n", truncated, MaxGlobEntries)
	}
	if skippedCredentials > 0 {
		fmt.Fprintf(&b, "(skipped %d credential files)\n", skippedCredentials)
	}

	return Result{Output: strings.TrimRight(b.String(), "\n")}, nil
}
