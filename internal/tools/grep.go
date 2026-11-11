package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	MaxGrepMatches = 200
	maxLineLen     = 1000 // avoid huge minified lines
)

type Grep struct {
	Root string
}

const grepSchema = `{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Regular expression pattern to search for"
    },
    "path": {
      "type": "string",
      "description": "Directory or file path to search within, relative to workspace root (defaults to workspace root)"
    },
    "glob": {
      "type": "string",
      "description": "Optional glob pattern to filter file names (e.g. '*.go' or '**/*.md')"
    }
  },
  "required": ["query"]
}`

func (Grep) Name() string {
	return "grep"
}

func (Grep) Description() string {
	return "Search file contents using regular expressions. Returns matching lines as file:line:text, respecting .gitignore."
}

func (Grep) Schema() json.RawMessage {
	return json.RawMessage(grepSchema)
}

func (Grep) NeedsApproval(_ json.RawMessage) ApprovalRequest {
	return ApprovalRequest{}
}

func (g Grep) Run(ctx context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Query string `json:"query"`
		Path  string `json:"path"`
		Glob  string `json:"glob"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return Result{Output: fmt.Sprintf("invalid input for grep: %s", err), IsError: true}, nil
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return Result{Output: "grep error: 'query' argument cannot be empty", IsError: true}, nil
	}

	re, err := regexp.Compile(query)
	if err != nil {
		return Result{Output: fmt.Sprintf("grep error: invalid regular expression %q: %s", query, err), IsError: true}, nil
	}

	searchPath := args.Path
	if searchPath == "" {
		searchPath = "."
	}

	targetPath, err := ResolveInRoot(g.Root, searchPath)
	if err != nil {
		return Result{Output: fmt.Sprintf("grep path error: %s", err), IsError: true}, nil
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Output: fmt.Sprintf("grep error: path %q does not exist", searchPath), IsError: true}, nil
		}
		return Result{Output: fmt.Sprintf("grep stat error: %s", err), IsError: true}, nil
	}

	ign := LoadGitIgnore(g.Root)
	var globPattern string
	if args.Glob != "" {
		globPattern = filepath.ToSlash(args.Glob)
	}

	var matches []string
	truncated := 0

	searchFile := func(fullPath string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		relToRoot, err := filepath.Rel(g.Root, fullPath)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(relToRoot)

		if IsIgnored(ign, relSlash) {
			return nil
		}

		if globPattern != "" {
			matchedName, _ := doublestar.Match(globPattern, filepath.Base(fullPath))
			matchedPath, _ := doublestar.Match(globPattern, relSlash)
			if !matchedName && !matchedPath {
				return nil
			}
		}

		f, err := os.Open(fullPath)
		if err != nil {
			return nil
		}
		defer f.Close()

		// Binary check: read first 512 bytes
		head := make([]byte, 512)
		n, _ := f.Read(head)
		if n > 0 && bytes.IndexByte(head[:n], 0) != -1 {
			return nil // Skip binary files
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil
		}

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				if len(matches) >= MaxGrepMatches {
					truncated++
					continue
				}
				trimmedLine := line
				if len(trimmedLine) > maxLineLen {
					trimmedLine = trimmedLine[:maxLineLen] + "..."
				}
				matches = append(matches, fmt.Sprintf("%s:%d:%s", relSlash, lineNum, trimmedLine))
			}
		}
		return nil
	}

	if !info.IsDir() {
		if err := searchFile(targetPath); err != nil && err != context.Canceled {
			return Result{Output: fmt.Sprintf("grep error reading file: %s", err), IsError: true}, nil
		}
	} else {
		err = filepath.WalkDir(targetPath, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || ctx.Err() != nil {
				return walkErr
			}
			relToRoot, err := filepath.Rel(g.Root, p)
			if err != nil {
				return nil
			}
			relSlash := filepath.ToSlash(relToRoot)

			if p != g.Root && IsIgnored(ign, relSlash) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			if d.IsDir() {
				return nil
			}

			return searchFile(p)
		})
		if err != nil && err != context.Canceled {
			return Result{Output: fmt.Sprintf("grep walk error: %s", err), IsError: true}, nil
		}
	}

	if len(matches) == 0 {
		return Result{Output: "No matches found"}, nil
	}

	var b strings.Builder
	for _, m := range matches {
		b.WriteString(m)
		b.WriteString("\n")
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "... [%d more matches truncated; cap is %d. Use more specific query or path filter]\n", truncated, MaxGrepMatches)
	}

	return Result{Output: strings.TrimRight(b.String(), "\n")}, nil
}

