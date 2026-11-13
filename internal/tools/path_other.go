//go:build !windows

package tools

import "path/filepath"

func expandLongPath(p string) string { return p }

func isReparsePoint(string) bool { return false }

func resolveLink(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
