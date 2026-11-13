package policy

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ProtectedPathPatterns defines path patterns that represent credentials or repository metadata.
var ProtectedPathPatterns = []string{
	".git",
	".git/**",
	".env",
	".env.*",
	".env*",
	".ssh",
	".ssh/**",
	".aws",
	".aws/**",
	"*.pem",
	"*.key",
	"id_rsa",
	"id_rsa.*",
	"id_ed25519",
	"id_ed25519.*",
	"id_ecdsa",
	"id_ecdsa.*",
	"credentials",
	"credentials.json",
	"credentials.ini",
}

// IsProtectedPath returns true if the relative or base path matches protected patterns (P-6).
func IsProtectedPath(path string) bool {
	norm := filepath.ToSlash(strings.TrimSpace(path))
	norm = strings.TrimPrefix(norm, "./")
	norm = strings.TrimPrefix(norm, "/")

	if norm == "" {
		return false
	}

	base := filepath.Base(norm)
	lowerNorm := strings.ToLower(norm)
	lowerBase := strings.ToLower(base)

	// Check sensitive path prefixes/names
	if strings.HasPrefix(lowerNorm, ".git/") || lowerNorm == ".git" {
		return true
	}
	if strings.HasPrefix(lowerNorm, ".ssh/") || lowerNorm == ".ssh" {
		return true
	}
	if strings.HasPrefix(lowerNorm, ".aws/") || lowerNorm == ".aws" {
		return true
	}
	if strings.HasPrefix(lowerBase, ".env") {
		return true
	}
	if strings.HasSuffix(lowerBase, ".pem") || strings.HasSuffix(lowerBase, ".key") {
		return true
	}
	if strings.HasPrefix(lowerBase, "id_rsa") || strings.HasPrefix(lowerBase, "id_ed25519") || strings.HasPrefix(lowerBase, "id_ecdsa") {
		return true
	}
	if lowerBase == "credentials" || lowerBase == "credentials.json" || lowerBase == "credentials.ini" {
		return true
	}

	for _, pattern := range ProtectedPathPatterns {
		if matched, _ := doublestar.Match(pattern, norm); matched {
			return true
		}
		if matched, _ := doublestar.Match(pattern, base); matched {
			return true
		}
	}
	return false
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
