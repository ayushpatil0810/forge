package policy

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ayushpatil0810/forge/internal/tools"
	"github.com/bmatcuk/doublestar/v4"
)

// IsProtectedPath reports protected paths using the single classifier in tools.
// The previous policy-local checker did not match nested or case-variant paths.
func IsProtectedPath(path string) bool {
	return tools.IsProtectedPath(path)
}

// MatchPathPattern tests whether targetPath matches the given pattern.
// Target path is relative to the workspace root.
// On Windows, matching is case-insensitive.
func MatchPathPattern(pattern, targetPath string) bool {
	normPattern := filepath.ToSlash(strings.TrimSpace(pattern))
	normPattern = strings.TrimPrefix(normPattern, "./")
	normPattern = strings.TrimPrefix(normPattern, "/")

	normTarget := filepath.ToSlash(strings.TrimSpace(targetPath))
	normTarget = strings.TrimPrefix(normTarget, "./")
	normTarget = strings.TrimPrefix(normTarget, "/")

	if runtime.GOOS == "windows" {
		normPattern = strings.ToLower(normPattern)
		normTarget = strings.ToLower(normTarget)
	}

	if normPattern == "*" || normPattern == "**" {
		return true
	}

	// Match entire relative path or base name
	matchedPath, err1 := doublestar.Match(normPattern, normTarget)
	if err1 == nil && matchedPath {
		return true
	}

	matchedBase, err2 := doublestar.Match(normPattern, filepath.Base(normTarget))
	if err2 == nil && matchedBase {
		return true
	}

	return false
}
