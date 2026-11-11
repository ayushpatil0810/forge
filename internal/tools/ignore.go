package tools

import (
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// LoadGitIgnore loads the .gitignore from the workspace root if it exists,
// along with standard ignores (.git directory).
func LoadGitIgnore(root string) *gitignore.GitIgnore {
	gitIgnorePath := filepath.Join(root, ".gitignore")
	var patterns []string
	// Always ignore .git directory
	patterns = append(patterns, ".git", ".git/**")

	if data, err := os.ReadFile(gitIgnorePath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, line)
			}
		}
	}

	return gitignore.CompileIgnoreLines(patterns...)
}

// IsIgnored checks whether a path relative to workspace root is ignored.
func IsIgnored(ign *gitignore.GitIgnore, relPath string) bool {
	if ign == nil {
		return false
	}
	// Normalize path separators to '/' for gitignore matching
	slashPath := filepath.ToSlash(relPath)
	slashPath = strings.TrimPrefix(slashPath, "./")
	if ign.MatchesPath(slashPath) {
		return true
	}
	if !strings.HasSuffix(slashPath, "/") && ign.MatchesPath(slashPath+"/") {
		return true
	}
	return false
}
