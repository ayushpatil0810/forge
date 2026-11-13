//go:build !windows

package tools

func expandLongPath(p string) string { return p }
