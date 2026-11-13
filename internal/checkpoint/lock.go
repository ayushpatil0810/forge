package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrLocked is returned when the shadow repository lock cannot be acquired within the timeout.
var ErrLocked = errors.New("checkpoint: shadow repository locked by another forge process")

type fileLock struct {
	path string
	file *os.File
}

// isLockHeldError checks if an error from OpenFile indicates the lock file exists or is held.
func isLockHeldError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsExist(err) || os.IsPermission(err) {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "access is denied") ||
		strings.Contains(errStr, "sharing violation") ||
		strings.Contains(errStr, "already exists")
}

// acquireLock acquires an exclusive inter-process lock on the shadow repository.
// It retries until the timeout expires or the context is cancelled.
// If an existing lock file is stale (> 30s old), it is automatically cleaned up.
func acquireLock(ctx context.Context, lockPath string, timeout time.Duration) (*fileLock, error) {
	deadline := time.Now().Add(timeout)
	for {
		// Attempt atomic exclusive creation
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err == nil {
			// Record PID and timestamp into lock file
			_, _ = fmt.Fprintf(f, "pid: %d\ntime: %d\n", os.Getpid(), time.Now().Unix())
			_ = f.Sync()
			return &fileLock{path: lockPath, file: f}, nil
		}

		if !isLockHeldError(err) {
			// If parent directory didn't exist, create it and retry
			if mkErr := os.MkdirAll(filepath.Dir(lockPath), 0700); mkErr == nil {
				f2, err2 := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
				if err2 == nil {
					_, _ = fmt.Fprintf(f2, "pid: %d\ntime: %d\n", os.Getpid(), time.Now().Unix())
					_ = f2.Sync()
					return &fileLock{path: lockPath, file: f2}, nil
				}
			}
			return nil, fmt.Errorf("creating lock file: %w", err)
		}

		// Lock exists: check if stale (> 30s)
		if isLockStale(lockPath, 30*time.Second) {
			_ = os.Remove(lockPath)
			continue
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			if time.Now().After(deadline) {
				return nil, ErrLocked
			}
		}
	}
}

// Release releases the lock and removes the lock file.
func (l *fileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = l.file.Close()
	return os.Remove(l.path)
}

func isLockStale(lockPath string, maxAge time.Duration) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	if time.Since(info.ModTime()) > maxAge {
		return true
	}
	// Also attempt to read timestamp inside
	data, err := os.ReadFile(lockPath)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "time: ") {
				if ts, parseErr := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "time: ")), 10, 64); parseErr == nil {
					if time.Since(time.Unix(ts, 0)) > maxAge {
						return true
					}
				}
			}
		}
	}
	return false
}
