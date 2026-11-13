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
// like .git/, .env*, .ssh, .aws, or private keys / credential files (SPEC §3.3 P-6).
func IsProtectedPath(p string) bool {
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == "." || clean == "/" {
		return false
	}

	// Check path segments for protected directory names
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == ".git" || part == ".ssh" || part == ".aws" {
			return true
		}
	}

	base := filepath.Base(clean)

	// .env and .env.* files
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasPrefix(base, ".env_") {
		return true
	}

	// SSH / credential key files
	if base == "id_rsa" || base == "id_ed25519" || base == "id_ecdsa" || base == "id_dsa" ||
		base == "credentials" || base == "credentials.json" {
		return true
	}

	// Sensitive certificates and private key extensions
	ext := filepath.Ext(base)
	if ext == ".pem" || ext == ".key" || ext == ".pfx" || ext == ".p12" {
		return true
	}

	return false
}
