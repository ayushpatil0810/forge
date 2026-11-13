package tools

import (
	"fmt"
	"path/filepath"
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

// IsCredentialPath checks if a path matches credential patterns:
// .env*, .ssh/, .aws/, *.pem, *.key, id_rsa*, credentials files (SEC-5, P-6).
func IsCredentialPath(p string) bool {
	norm := filepath.ToSlash(strings.TrimSpace(p))
	norm = strings.TrimPrefix(norm, "./")
	norm = strings.TrimPrefix(norm, "/")
	if norm == "" {
		return false
	}
	base := filepath.Base(norm)
	lowerNorm := strings.ToLower(norm)
	lowerBase := strings.ToLower(base)

	if strings.HasPrefix(lowerNorm, ".ssh/") || lowerNorm == ".ssh" ||
		strings.HasPrefix(lowerNorm, ".aws/") || lowerNorm == ".aws" ||
		strings.HasPrefix(lowerNorm, ".git/") || lowerNorm == ".git" {
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
	return false
}
