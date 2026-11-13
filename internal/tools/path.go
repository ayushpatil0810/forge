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
	lexical, err := lexicalRoot(root)
	if err != nil {
		return resolvedPath{}, err
	}
	realRoot, err := canonicalRoot(root)
	if err != nil {
		return resolvedPath{}, err
	}
	p = normalizeInputPath(p)
	if p == "" || p == "." {
		return resolvedPath{Path: lexical}, nil
	}

	var parts []string
	if filepath.IsAbs(stripExtendedPrefix(p)) {
		abs := stripExtendedPrefix(p)
		if !sameVolume(realRoot, abs) && !sameVolume(lexical, abs) {
			return resolvedPath{}, fmt.Errorf("path %q is outside the workspace (different drive or UNC root)", p)
		}
		relParts, ok := partsUnderRoot(realRoot, lexical, abs)
		if !ok {
			return resolvedPath{}, fmt.Errorf("path %q is outside the workspace", p)
		}
		parts = relParts
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
	return resolvedPath{Path: rebaseOnto(lexical, realRoot, final), Traversed: traversed}, nil
}

func lexicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving workspace root: %w", err)
	}
	return filepath.Clean(abs), nil
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
	if expanded := expandLongPath(abs); expanded != "" {
		abs = stripExtendedPrefix(expanded)
	}
	return abs, nil
}

// partsUnderRoot turns an absolute path into walker components. The caller's
// spelling and the canonical spelling are both accepted, so /var and
// /private/var, or an 8.3 root and its long name, describe the same workspace.
func partsUnderRoot(realRoot, lexicalRoot, abs string) ([]string, bool) {
	if rel, ok := relativeUnder(realRoot, abs); ok {
		return splitPath(rel), true
	}
	if rel, ok := relativeUnder(lexicalRoot, abs); ok {
		return splitPath(rel), true
	}
	if pathHasDotDot(abs) {
		return nil, false
	}
	canon := canonicalizeForCompare(abs)
	root := canonicalizeForCompare(realRoot)
	if !withinRootLexical(root, canon) {
		return nil, false
	}
	rel, err := filepath.Rel(root, canon)
	if err != nil || escapes(rel) {
		return nil, false
	}
	return splitPath(rel), true
}

// rebaseOnto expresses target, which was resolved under the canonical root,
// using the workspace path the caller passed. Ancestor symlinks such as
// /var -> /private/var stay in the caller's spelling. The relative suffix,
// including an expanded 8.3 file name, is preserved.
func rebaseOnto(lexical, real, target string) string {
	target = stripExtendedPrefix(target)
	real = stripExtendedPrefix(real)
	if sameDir(target, real) {
		return lexical
	}
	rel, err := filepath.Rel(real, target)
	if err != nil || escapes(rel) {
		ct := canonicalizeForCompare(target)
		cr := canonicalizeForCompare(real)
		rel, err = filepath.Rel(cr, ct)
		if err != nil || escapes(rel) {
			return target
		}
	}
	if rel == "." {
		return lexical
	}
	return filepath.Join(lexical, rel)
}

func canonicalizeForCompare(p string) string {
	p = stripExtendedPrefix(filepath.Clean(p))
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = stripExtendedPrefix(real)
	} else {
		var missing []string
		cur := p
		for {
			parent := filepath.Dir(cur)
			if parent == cur {
				return p
			}
			if _, err := os.Lstat(parent); err == nil {
				if real, err := filepath.EvalSymlinks(parent); err == nil {
					cur = stripExtendedPrefix(real)
				} else {
					cur = parent
				}
				for i := len(missing) - 1; i >= 0; i-- {
					cur = filepath.Join(cur, missing[i])
				}
				p = cur
				break
			}
			missing = append(missing, filepath.Base(cur))
			cur = parent
		}
	}
	if expanded := expandLongPath(p); expanded != "" {
		p = stripExtendedPrefix(expanded)
	}
	return p
}

func pathHasDotDot(p string) bool {
	for _, part := range splitPath(p) {
		if part == ".." {
			return true
		}
	}
	return false
}

func escapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
	if withinRootLexical(root, target) {
		return true
	}
	return withinRootLexical(canonicalizeForCompare(root), canonicalizeForCompare(target))
}

func withinRootLexical(root, target string) bool {
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
	return !escapes(rel)
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

// protectedPath classifies the requested name, the resolved name, and — when the
// file has extra links or the path went through a symlink — its identity against
// protected names in the workspace root and the parent directory (F-002).
func protectedPath(root, requested, resolved string) bool {
	if IsProtectedPath(requested) || (resolved != "" && IsProtectedPath(resolved)) {
		return true
	}
	r, err := resolvePath(root, requested)
	if err != nil {
		return false
	}
	if IsProtectedPath(r.Path) {
		return true
	}
	return identityProtected(root, r)
}

// entryProtected classifies a path a directory walk already found.
func entryProtected(root, rel, full string) bool {
	if IsProtectedPath(rel) || IsProtectedPath(full) {
		return true
	}
	fi, err := os.Lstat(full)
	if err != nil {
		return false
	}
	resolved := full
	traversed := isLink(fi, full)
	if traversed {
		if target, err := resolveLink(full); err == nil {
			resolved = stripExtendedPrefix(target)
			if IsProtectedPath(resolved) {
				return true
			}
		}
	}
	if fi.IsDir() && !traversed {
		return false
	}
	if info, err := os.Stat(resolved); err == nil && info.IsDir() {
		return false
	}
	return identityProtected(root, resolvedPath{Path: resolved, Traversed: traversed})
}

// hasExtraLinks reports whether the resolved file is a regular file with more
// than one directory entry. read_file, write_file, and edit_file treat that as
// protected even when the other name is outside the workspace (F-002).
func hasExtraLinks(root, requested string) bool {
	r, err := resolvePath(root, requested)
	if err != nil || r.Path == "" {
		return false
	}
	info, err := os.Lstat(r.Path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	_, nlink, err := statIdentity(r.Path)
	return err == nil && nlink > 1
}

func identityProtected(root string, r resolvedPath) bool {
	if r.Path == "" {
		return false
	}
	key, nlink, err := statIdentity(r.Path)
	if err != nil || !key.ok {
		return false
	}
	if nlink <= 1 && !r.Traversed {
		return false
	}
	realRoot, err := canonicalRoot(root)
	if err != nil {
		realRoot = root
	}
	dirs := []string{realRoot}
	parent := filepath.Dir(r.Path)
	if !sameDir(parent, realRoot) {
		dirs = append(dirs, parent)
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !IsProtectedPath(e.Name()) {
				continue
			}
			other := filepath.Join(dir, e.Name())
			if sameIdentity(r.Path, other, key) {
				return true
			}
		}
	}
	return false
}

func sameIdentity(aPath, bPath string, aKey fileKey) bool {
	bKey, _, err := statIdentity(bPath)
	if err == nil && bKey.ok && aKey == bKey {
		return true
	}
	aInfo, err1 := os.Stat(aPath)
	bInfo, err2 := os.Stat(bPath)
	return err1 == nil && err2 == nil && os.SameFile(aInfo, bInfo)
}

func sameDir(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

type fileKey struct {
	dev uint64
	ino uint64
	ok  bool
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
