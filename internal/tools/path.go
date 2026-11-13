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
	p = normalizeInputPath(p)
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

	if expanded := expandLongPath(target); expanded != "" {
		target = stripExtendedPrefix(expanded)
	}
	return target, nil
}

// normalizeInputPath applies Win32's trailing-dot and trailing-space stripping
// on Windows only. Other operating systems keep those characters (F-014).
func normalizeInputPath(p string) string {
	p = strings.TrimLeft(p, " \t\r\n")
	if runtime.GOOS != "windows" {
		return p
	}
	p = stripExtendedPrefix(p)
	vol := filepath.VolumeName(p)
	rest := p[len(vol):]
	sep := "/"
	if strings.Contains(rest, `\`) && !strings.Contains(rest, "/") {
		sep = `\`
	}
	parts := strings.Split(rest, sep)
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		parts[i] = strings.TrimRight(part, ". ")
	}
	return vol + strings.Join(parts, sep)
}

func stripExtendedPrefix(p string) string {
	switch {
	case strings.HasPrefix(p, `\\?\UNC\`):
		return `\\` + p[len(`\\?\UNC\`):]
	case strings.HasPrefix(p, `\\?\`):
		return p[len(`\\?\`):]
	default:
		return p
	}
}

// IsProtectedPath reports whether any path segment is a protected name.
// Comparison is case-insensitive on Windows and macOS. On Linux it is
// case-sensitive, except .git which is case-insensitive everywhere (F-015).
func IsProtectedPath(p string) bool {
	norm := filepath.ToSlash(normalizeInputPath(p))
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

// protectedPath classifies both the name the caller passed and the resolved path.
// root is the workspace root; identity checks (F-002) use it as well.
func protectedPath(root, requested, resolved string) bool {
	_ = root
	return IsProtectedPath(requested) || (resolved != "" && IsProtectedPath(resolved))
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
