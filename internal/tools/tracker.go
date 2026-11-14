package tools

import (
	"path/filepath"
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

// UnmarkRead removes a recorded read for p, requiring a fresh read before writes.
func (t *ReadTracker) UnmarkRead(p string) {
	if t == nil {
		return
	}
	clean := filepath.Clean(p)
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.reads, clean)
	delete(t.reads, filepath.ToSlash(clean))
}
