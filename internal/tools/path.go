package tools

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveInRoot resolves a relative or absolute path against the workspace root
// and guarantees the real canonical path (after symlink evaluation) stays inside root.
// Handles Windows drive letters, case-insensitivity, and UNC paths (SEC-8).
func ResolveInRoot(root, p string) (string, error) {
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving workspace root: %w", err)
	}
	absRoot = filepath.Clean(absRoot)
	if realRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = realRoot
	}

	var target string
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		target = absRoot
	} else if filepath.IsAbs(p) {
		target = filepath.Clean(p)
	} else {
		target = filepath.Clean(filepath.Join(absRoot, p))
	}

	// Evaluate symlinks on target if it exists
	if realTarget, err := filepath.EvalSymlinks(target); err == nil {
		target = realTarget
	}

	// Windows drive letter check: volumes must match
	rootVol := strings.ToUpper(filepath.VolumeName(absRoot))
	targetVol := strings.ToUpper(filepath.VolumeName(target))
	if rootVol != targetVol {
		return "", fmt.Errorf("path %q is outside the workspace (different drive %q vs %q)", p, targetVol, rootVol)
	}

	// Compute relative path
	rel, err := filepath.Rel(absRoot, target)
	if err != nil {
		return "", fmt.Errorf("path %q is outside the workspace: %w", p, err)
	}

	// Check if path escapes upwards
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the workspace", p)
	}

	return target, nil
}

// IsProtectedPath reports whether any path segment is a protected name.
// Comparison is case-insensitive on Windows and macOS. On Linux it is
// case-sensitive, except .git which is case-insensitive everywhere (F-015).
func IsProtectedPath(p string) bool {
	norm := filepath.ToSlash(strings.TrimSpace(p))
	norm = strings.TrimPrefix(norm, "./")
	for _, part := range strings.Split(norm, "/") {
		if part == "" || part == "." || part == ".." {
			continue
		}
		if segmentProtected(part) {
			return true
		}
	}
	return false
}

// IsCredentialPath is the shared classifier. Callers that historically used
// the credential-only helper now use the same rules as the policy floor.
func IsCredentialPath(p string) bool {
	return IsProtectedPath(p)
}

func segmentProtected(seg string) bool {
	if strings.EqualFold(seg, ".git") {
		return true
	}
	cmp := seg
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		cmp = strings.ToLower(seg)
	}
	if cmp == ".env" || (strings.HasPrefix(cmp, ".env.") && len(cmp) > len(".env.")) {
		return true
	}
	switch cmp {
	case ".ssh", ".aws", ".gnupg", "credentials", ".npmrc", ".pypirc", ".netrc":
		return true
	}
	if strings.HasSuffix(cmp, ".pem") || strings.HasSuffix(cmp, ".key") ||
		strings.HasSuffix(cmp, ".pfx") || strings.HasSuffix(cmp, ".p12") {
		return true
	}
	if strings.HasPrefix(cmp, "id_rsa") || strings.HasPrefix(cmp, "id_ed25519") {
		return true
	}
	return false
}
