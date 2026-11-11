package tools

import (
	"path/filepath"
	"strings"
	"sync"
)

// ReadTracker records files that have been read in the current session.
// This enforces the read-before-write safety guard for edit_file and write_file.
type ReadTracker struct {
	mu    sync.RWMutex
	reads map[string]bool
}

// NewReadTracker creates a new ReadTracker.
func NewReadTracker() *ReadTracker {
	return &ReadTracker{reads: make(map[string]bool)}
}

// MarkRead records that a path has been read.
func (t *ReadTracker) MarkRead(p string) {
	if t == nil {
		return
	}
	clean := filepath.Clean(p)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reads[clean] = true
	// Also store forward-slash variant for cross-platform matching
	t.reads[filepath.ToSlash(clean)] = true
}

// HasBeenRead reports whether p has been read in this session.
func (t *ReadTracker) HasBeenRead(p string) bool {
	if t == nil {
		return true // If tracking is omitted, do not block operations
	}
	clean := filepath.Clean(p)
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.reads[clean] || t.reads[filepath.ToSlash(clean)]
}

// IsProtectedPath checks if a relative or clean path targets protected files
// like .git/, .env*, or private keys (SPEC §3.3 P-6).
func IsProtectedPath(p string) bool {
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == "." || clean == "/" {
		return false
	}
	// Direct .git directory or files inside .git
	if clean == ".git" || strings.HasPrefix(clean, ".git/") || strings.Contains(clean, "/.git/") || strings.HasSuffix(clean, "/.git") {
		return true
	}
	base := filepath.Base(clean)
	// .env and .env.* files
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	// SSH / credential key files
	if base == "id_rsa" || base == "id_ed25519" || base == "id_ecdsa" || base == "id_dsa" {
		return true
	}
	return false
}
