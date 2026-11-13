package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// resolvedPath is a workspace path after symlink and junction resolution.
type resolvedPath struct {
	Path      string
	Traversed bool // a symlink or junction was followed
}

// ResolveInRoot resolves a relative or absolute path against the workspace root
// and guarantees the real path stays inside root. The nearest existing ancestor
// is resolved with EvalSymlinks before any missing suffix is reattached, so a
// junction or symlink whose final component does not exist cannot escape (F-001).
func ResolveInRoot(root, p string) (string, error) {
	r, err := resolvePath(root, p)
	if err != nil {
		return "", err
	}
	return r.Path, nil
}

func resolvePath(root, p string) (resolvedPath, error) {
	if root == "" {
		root = "."
	}
	realRoot, err := canonicalRoot(root)
	if err != nil {
		return resolvedPath{}, err
	}
	p = normalizeInputPath(p)
	if p == "" || p == "." {
		return resolvedPath{Path: realRoot}, nil
	}

	var parts []string
	if filepath.IsAbs(stripExtendedPrefix(p)) {
		abs := stripExtendedPrefix(p)
		if !sameVolume(realRoot, abs) {
			return resolvedPath{}, fmt.Errorf("path %q is outside the workspace (different drive or UNC root)", p)
		}
		rel, ok := relativeUnder(realRoot, abs)
		if !ok {
			return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
		}
		parts = splitPath(rel)
	} else {
		parts = splitPath(p)
	}

	current := realRoot
	var pending []string
	traversed := false
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(pending) > 0 {
				return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
			}
			parent := filepath.Dir(current)
			if !withinRoot(realRoot, parent) {
				return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
			}
			current = parent
			continue
		}
		if len(pending) > 0 {
			pending = append(pending, part)
			continue
		}
		next := filepath.Join(current, part)
		fi, statErr := os.Lstat(next)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				pending = append(pending, part)
				continue
			}
			return resolvedPath{}, fmt.Errorf("path %q: %w", p, statErr)
		}
		if isLink(fi, next) {
			resolved, err := resolveLink(next)
			if err != nil {
				return resolvedPath{}, fmt.Errorf("path %q: %w", p, err)
			}
			resolved = stripExtendedPrefix(resolved)
			if !withinRoot(realRoot, resolved) {
				return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
			}
			traversed = true
			current = resolved
			continue
		}
		if !withinRoot(realRoot, next) {
			return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
		}
		current = next
	}

	final := current
	for _, part := range pending {
		if part == "" || part == "." || part == ".." || strings.Contains(part, "..") {
			return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
		}
		final = filepath.Join(final, part)
	}
	if !withinRoot(realRoot, final) {
		return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
	}
	if expanded := expandLongPath(final); expanded != "" {
		final = stripExtendedPrefix(expanded)
		if !withinRoot(realRoot, final) {
			return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
		}
	}
	if fi, err := os.Lstat(final); err == nil && isLink(fi, final) {
		resolved, err := resolveLink(final)
		if err != nil {
			return resolvedPath{}, fmt.Errorf("path %q: %w", p, err)
		}
		resolved = stripExtendedPrefix(resolved)
		if !withinRoot(realRoot, resolved) {
			return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
		}
		traversed = true
		final = stripExtendedPrefix(expandLongPath(resolved))
	}
	return resolvedPath{Path: final, Traversed: traversed}, nil
}

func canonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving workspace root: %w", err)
	}
	abs = filepath.Clean(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = stripExtendedPrefix(real)
	}
	return abs, nil
}

func splitPath(p string) []string {
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.Split(p, "/")
}

func sameVolume(root, p string) bool {
	rv := strings.ToUpper(filepath.VolumeName(stripExtendedPrefix(root)))
	pv := strings.ToUpper(filepath.VolumeName(stripExtendedPrefix(p)))
	return rv == pv
}

// relativeUnder reports the path of abs relative to root without cleaning ".."
// away, so a symlink followed by ".." is still visible to the walker.
func relativeUnder(root, abs string) (string, bool) {
	root = stripExtendedPrefix(filepath.Clean(root))
	abs = stripExtendedPrefix(abs)
	if runtime.GOOS == "windows" {
		if strings.EqualFold(abs, root) {
			return "", true
		}
		if len(abs) > len(root) && strings.EqualFold(abs[:len(root)], root) {
			sep := abs[len(root)]
			if sep == '\\' || sep == '/' {
				return abs[len(root)+1:], true
			}
		}
		return "", false
	}
	if abs == root {
		return "", true
	}
	sep := string(filepath.Separator)
	if strings.HasPrefix(abs, root+sep) {
		return abs[len(root)+len(sep):], true
	}
	return "", false
}

func withinRoot(root, target string) bool {
	root = stripExtendedPrefix(filepath.Clean(root))
	target = stripExtendedPrefix(filepath.Clean(target))
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		target = strings.ToLower(target)
	}
	if root == target {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isLink(fi os.FileInfo, path string) bool {
	if fi != nil && fi.Mode()&os.ModeSymlink != 0 {
		return true
	}
	return isReparsePoint(path)
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
