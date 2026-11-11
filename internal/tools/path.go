package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveInRoot resolves rel against root and guarantees the real path
// (after symlink evaluation) stays inside root. The target must exist.
func ResolveInRoot(root, rel string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if absRoot, err = filepath.EvalSymlinks(absRoot); err != nil {
		return "", err
	}
	p := rel
	if !filepath.IsAbs(p) {
		p = filepath.Join(absRoot, p)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	r, err := filepath.Rel(absRoot, real)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the workspace", rel)
	}
	return real, nil
}
