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
