package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const MaxListDirEntries = 500

type ListDir struct {
	Root string
}

const listDirSchema = `{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Directory path relative to the workspace root (defaults to '.')"
    },
    "recursive": {
      "type": "boolean",
      "description": "Whether to list recursively (defaults to false)"
    }
  }
}`

func (ListDir) Name() string {
	return "list_dir"
}

func (ListDir) Description() string {
	return "List files and directories in a given path, respecting .gitignore. Returns sorted entries."
}

func (ListDir) Schema() json.RawMessage {
	return json.RawMessage(listDirSchema)
}

func (ListDir) NeedsApproval(_ json.RawMessage) ApprovalRequest {
	return ApprovalRequest{}
}

func (l ListDir) Run(_ context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return Result{Output: fmt.Sprintf("invalid input for list_dir: %s", err), IsError: true}, nil
		}
	}
	if args.Path == "" {
		args.Path = "."
	}

	targetDir, err := ResolveInRoot(l.Root, args.Path)
	if err != nil {
		return Result{Output: fmt.Sprintf("list_dir path error: %s", err), IsError: true}, nil
	}

	info, err := os.Stat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Output: fmt.Sprintf("list_dir error: directory %q does not exist", args.Path), IsError: true}, nil
		}
		return Result{Output: fmt.Sprintf("list_dir stat error: %s", err), IsError: true}, nil
	}
	if !info.IsDir() {
		return Result{Output: fmt.Sprintf("list_dir error: %q is a file, not a directory; use read_file instead", args.Path), IsError: true}, nil
	}

	ign := LoadGitIgnore(l.Root)

	var entries []string
	truncated := 0

	if !args.Recursive {
		rawEntries, err := os.ReadDir(targetDir)
		if err != nil {
			return Result{Output: fmt.Sprintf("list_dir read error: %s", err), IsError: true}, nil
		}

		for _, e := range rawEntries {
			fullPath := filepath.Join(targetDir, e.Name())
			relToRoot, err := filepath.Rel(l.Root, fullPath)
			if err != nil {
				continue
			}
			if IsIgnored(ign, relToRoot) {
				continue
			}
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			entries = append(entries, name)
		}
	} else {
		err := filepath.WalkDir(targetDir, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if p == targetDir {
				return nil
			}
			relToRoot, err := filepath.Rel(l.Root, p)
			if err != nil {
				return nil
			}
			if IsIgnored(ign, relToRoot) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			relToTarget, err := filepath.Rel(targetDir, p)
			if err != nil {
				return nil
			}
			entryPath := filepath.ToSlash(relToTarget)
			if d.IsDir() {
				entryPath += "/"
			}

			if len(entries) >= MaxListDirEntries {
				truncated++
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			entries = append(entries, entryPath)
			return nil
		})
		if err != nil {
			return Result{Output: fmt.Sprintf("list_dir walk error: %s", err), IsError: true}, nil
		}
	}

	sort.Strings(entries)

	if len(entries) == 0 {
		return Result{Output: "(empty directory)"}, nil
	}

	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e)
		b.WriteString("\n")
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "... [%d more entries truncated; cap is %d entries. Use a more specific path to narrow down]\n", truncated, MaxListDirEntries)
	}

	return Result{Output: strings.TrimRight(b.String(), "\n")}, nil
}
