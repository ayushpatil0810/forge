// Package checkpoint implements an isolated, shadow-git repository snapshot and undo engine (SPEC §3.4 S-3, S-4, S-7).
package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
)

const (
	DefaultMaxCheckpoints = 50
	DefaultMaxFileSize    = 10 * 1024 * 1024 // 10MB
)

// Checkpoint represents a recorded snapshot in the shadow repository.
type Checkpoint struct {
	ID           string    `json:"id"`
	Commit       string    `json:"commit"`
	Tree         string    `json:"tree"`
	Message      string    `json:"message"`
	Timestamp    time.Time `json:"timestamp"`
	SkippedFiles []string  `json:"skipped_files,omitempty"`
}

// Manager manages shadow git checkpoints for a specific workspace.
type Manager struct {
	mu             sync.Mutex
	WorkspaceRoot  string
	ShadowDir      string
	MaxCheckpoints int
	MaxFileSize    int64
	initialized    bool
}

// New creates a new checkpoint manager for the given workspace root.
func New(workspaceRoot string) (*Manager, error) {
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving workspace root: %w", err)
	}
	cleanRoot := filepath.Clean(absRoot)

	// Canonical workspace path hash
	hash := sha256.Sum256([]byte(strings.ToLower(cleanRoot)))
	hashHex := hex.EncodeToString(hash[:16])

	cacheDir, err := os.UserCacheDir()
	if err != nil || cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "forge-cache")
	}

	shadowDir := filepath.Join(cacheDir, "forge", "checkpoints", hashHex)

	return &Manager{
		WorkspaceRoot:  cleanRoot,
		ShadowDir:      shadowDir,
		MaxCheckpoints: DefaultMaxCheckpoints,
		MaxFileSize:    DefaultMaxFileSize,
	}, nil
}

// ensureInit initializes the isolated bare shadow repository if not yet created.
func (m *Manager) ensureInit(ctx context.Context) error {
	if m.initialized {
		return nil
	}

	if _, err := os.Stat(filepath.Join(m.ShadowDir, "HEAD")); os.IsNotExist(err) {
		if err := os.MkdirAll(m.ShadowDir, 0700); err != nil {
			return fmt.Errorf("creating shadow git dir %q: %w", m.ShadowDir, err)
		}

		// Initialize bare shadow repository
		cmd := exec.CommandContext(ctx, "git", "init", "--bare", m.ShadowDir)
		cmd.Env = []string{
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=" + os.DevNull,
			"PATH=" + os.Getenv("PATH"),
			"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git init bare failed: %s (%w)", string(out), err)
		}

		// Setup info/exclude to ignore secrets, credentials, and user's .git
		infoDir := filepath.Join(m.ShadowDir, "info")
		_ = os.MkdirAll(infoDir, 0700)
		excludeContent := strings.Join([]string{
			"# Forge automated exclusion",
			".git",
			".git/**",
			".env*",
			"*.pem",
			"*.key",
			"*.pfx",
			"*.p12",
			"id_rsa*",
			"id_ed25519*",
			"id_ecdsa*",
			"id_dsa*",
			".ssh",
			".ssh/**",
			".aws",
			".aws/**",
			"credentials",
			"credentials.json",
			"",
		}, "\n")
		_ = os.WriteFile(filepath.Join(infoDir, "exclude"), []byte(excludeContent), 0600)
	}

	m.initialized = true
	return nil
}

func (m *Manager) isolatedEnv() []string {
	indexPath := filepath.Join(m.ShadowDir, "index")
	return []string{
		"GIT_DIR=" + m.ShadowDir,
		"GIT_WORK_TREE=" + m.WorkspaceRoot,
		"GIT_INDEX_FILE=" + indexPath,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_AUTHOR_NAME=Forge",
		"GIT_AUTHOR_EMAIL=forge@localhost",
		"GIT_COMMITTER_NAME=Forge",
		"GIT_COMMITTER_EMAIL=forge@localhost",
		"PATH=" + os.Getenv("PATH"),
		"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
	}
}

func (m *Manager) git(ctx context.Context, args ...string) (string, error) {
	// Explicit flags guarantee safety even on Git < 2.32 (where GIT_CONFIG_GLOBAL is ignored)
	fullArgs := append([]string{
		"-c", "core.hooksPath=",
		"-c", "core.autocrlf=false",
		"-c", "commit.gpgsign=false",
		"-c", "commit.template=",
		"-c", "user.useConfigOnly=false",
		"-c", "user.name=Forge",
		"-c", "user.email=forge@localhost",
	}, args...)

	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	cmd.Dir = m.WorkspaceRoot
	cmd.Env = m.isolatedEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %s (%w)", strings.Join(args, " "), strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// scanSkippedLargeFiles checks for files exceeding MaxFileSize.
func (m *Manager) scanSkippedLargeFiles() []string {
	if m.MaxFileSize <= 0 {
		return nil
	}
	var skipped []string
	_ = filepath.WalkDir(m.WorkspaceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".ssh" || name == ".aws" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err == nil && info.Size() > m.MaxFileSize {
			rel, _ := filepath.Rel(m.WorkspaceRoot, path)
			skipped = append(skipped, filepath.ToSlash(rel))
		}
		return nil
	})
	return skipped
}

// Create records a new snapshot of the workspace.
func (m *Manager) Create(ctx context.Context, message string) (*Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	lock, err := acquireLock(ctx, m.ShadowDir+".lock", 10*time.Second)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Release() }()

	if err := m.ensureInit(ctx); err != nil {
		return nil, err
	}

	skipped := m.scanSkippedLargeFiles()

	// Stage workspace files into isolated index (respecting info/exclude and .gitignore)
	if _, err := m.git(ctx, "add", "-A"); err != nil {
		return nil, fmt.Errorf("staging workspace files: %w", err)
	}

	// Write tree
	treeSHA, err := m.git(ctx, "write-tree")
	if err != nil {
		return nil, fmt.Errorf("writing tree: %w", err)
	}

	// Get current HEAD commit if any
	parentCommit, _ := m.git(ctx, "rev-parse", "--verify", "refs/heads/main")

	// If parent commit exists and has identical tree, avoid duplicate snapshot
	if parentCommit != "" {
		parentTree, _ := m.git(ctx, "rev-parse", parentCommit+"^{tree}")
		if parentTree == treeSHA {
			// No changes compared to last checkpoint
			chks, _ := m.listUnlocked(ctx, 1)
			if len(chks) > 0 {
				return &chks[0], nil
			}
		}
	}

	// Create commit
	commitArgs := []string{"commit-tree", treeSHA}
	if parentCommit != "" {
		commitArgs = append(commitArgs, "-p", parentCommit)
	}
	if message == "" {
		message = fmt.Sprintf("Checkpoint %s", time.Now().Format(time.RFC3339))
	}
	commitArgs = append(commitArgs, "-m", message)

	commitSHA, err := m.git(ctx, commitArgs...)
	if err != nil {
		return nil, fmt.Errorf("creating commit-tree: %w", err)
	}

	// Update main ref and timestamped checkpoint ref
	shortID := commitSHA
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	refName := fmt.Sprintf("refs/forge/checkpoints/%s", shortID)

	if _, err := m.git(ctx, "update-ref", "refs/heads/main", commitSHA); err != nil {
		return nil, fmt.Errorf("updating main ref: %w", err)
	}
	_, _ = m.git(ctx, "update-ref", refName, commitSHA)

	m.pruneOldCheckpoints(ctx)

	return &Checkpoint{
		ID:           shortID,
		Commit:       commitSHA,
		Tree:         treeSHA,
		Message:      message,
		Timestamp:    time.Now(),
		SkippedFiles: skipped,
	}, nil
}

// Restore restores workspace files to the specified checkpoint ID.
// A pre-restore checkpoint is automatically taken first.
func (m *Manager) Restore(ctx context.Context, id string) (*Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	lock, err := acquireLock(ctx, m.ShadowDir+".lock", 10*time.Second)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Release() }()

	if err := m.ensureInit(ctx); err != nil {
		return nil, err
	}

	targetCommit, err := m.git(ctx, "rev-parse", "--verify", id)
	if err != nil {
		// Try resolving short ID
		targetCommit, err = m.git(ctx, "rev-parse", "--verify", "refs/forge/checkpoints/"+id)
		if err != nil {
			return nil, fmt.Errorf("unknown checkpoint %q: %w", id, err)
		}
	}

	// Pre-restore snapshot so the undo itself can be undone or inspected
	preRestoreMsg := fmt.Sprintf("Pre-restore snapshot before reverting to %s", id)
	preRestoreCP, _ := m.createUnlocked(ctx, preRestoreMsg)

	// Read tree of target commit and checkout index into workspace
	targetTree, err := m.git(ctx, "rev-parse", targetCommit+"^{tree}")
	if err != nil {
		return nil, fmt.Errorf("resolving target tree: %w", err)
	}

	// Remove any files that were created after targetTree
	if preRestoreCP != nil && preRestoreCP.Tree != "" {
		diffOut, diffErr := m.git(ctx, "diff-tree", "-r", "--name-status", "--no-commit-id", targetTree, preRestoreCP.Tree)
		if diffErr == nil {
			for _, line := range strings.Split(diffOut, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "A\t") {
					relPath := strings.TrimPrefix(line, "A\t")
					relPath = strings.Trim(relPath, "\"")
					fullPath := filepath.Join(m.WorkspaceRoot, filepath.FromSlash(relPath))
					_ = os.Remove(fullPath)
				}
			}
		}
	}

	if _, err := m.git(ctx, "read-tree", targetTree); err != nil {
		return nil, fmt.Errorf("reading tree into index: %w", err)
	}

	if _, err := m.git(ctx, "checkout-index", "-a", "-f"); err != nil {
		return nil, fmt.Errorf("checking out files into workspace: %w", err)
	}

	// Update refs/heads/main to restored commit
	_, _ = m.git(ctx, "update-ref", "refs/heads/main", targetCommit)

	shortID := targetCommit
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	return &Checkpoint{
		ID:        shortID,
		Commit:    targetCommit,
		Tree:      targetTree,
		Message:   "Restored to " + id,
		Timestamp: time.Now(),
	}, nil
}

// List returns the recorded checkpoints, newest first.
func (m *Manager) List(ctx context.Context, limit int) ([]Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listUnlocked(ctx, limit)
}

func (m *Manager) listUnlocked(ctx context.Context, limit int) ([]Checkpoint, error) {
	if err := m.ensureInit(ctx); err != nil {
		return nil, err
	}

	out, err := m.git(ctx, "log", "refs/heads/main", "--format=%H|%T|%ct|%s", fmt.Sprintf("-n%d", limit))
	if err != nil || strings.TrimSpace(out) == "" {
		return nil, nil
	}

	var results []Checkpoint
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		commitSHA := parts[0]
		treeSHA := parts[1]
		sec, _ := strconv.ParseInt(parts[2], 10, 64)
		msg := parts[3]

		shortID := commitSHA
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		results = append(results, Checkpoint{
			ID:        shortID,
			Commit:    commitSHA,
			Tree:      treeSHA,
			Message:   msg,
			Timestamp: time.Unix(sec, 0),
		})
	}
	return results, nil
}

func (m *Manager) createUnlocked(ctx context.Context, message string) (*Checkpoint, error) {
	if _, err := m.git(ctx, "add", "-A"); err != nil {
		return nil, err
	}
	treeSHA, err := m.git(ctx, "write-tree")
	if err != nil {
		return nil, err
	}
	parentCommit, _ := m.git(ctx, "rev-parse", "--verify", "refs/heads/main")
	commitArgs := []string{"commit-tree", treeSHA}
	if parentCommit != "" {
		commitArgs = append(commitArgs, "-p", parentCommit)
	}
	commitArgs = append(commitArgs, "-m", message)
	commitSHA, err := m.git(ctx, commitArgs...)
	if err != nil {
		return nil, err
	}
	_, _ = m.git(ctx, "update-ref", "refs/heads/main", commitSHA)
	shortID := commitSHA
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return &Checkpoint{
		ID:        shortID,
		Commit:    commitSHA,
		Tree:      treeSHA,
		Message:   message,
		Timestamp: time.Now(),
	}, nil
}

func (m *Manager) pruneOldCheckpoints(ctx context.Context) {
	if m.MaxCheckpoints <= 0 {
		return
	}
	out, err := m.git(ctx, "rev-list", "refs/heads/main")
	if err != nil {
		return
	}
	commits := strings.Fields(out)
	if len(commits) > m.MaxCheckpoints {
		// Prune references beyond MaxCheckpoints
		for _, oldCommit := range commits[m.MaxCheckpoints:] {
			shortID := oldCommit
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			_, _ = m.git(ctx, "update-ref", "-d", fmt.Sprintf("refs/forge/checkpoints/%s", shortID))
		}
	}
}

// AsCheckpointer wraps Manager to satisfy the agent.Checkpointer interface.
type checkpointerAdapter struct {
	m *Manager
}

func (m *Manager) AsCheckpointer() agent.Checkpointer {
	return &checkpointerAdapter{m: m}
}

func (a *checkpointerAdapter) Create(ctx context.Context, message string) (agent.CheckpointInfo, error) {
	cp, err := a.m.Create(ctx, message)
	if err != nil {
		return agent.CheckpointInfo{}, err
	}
	return agent.CheckpointInfo{
		ID:           cp.ID,
		Message:      cp.Message,
		Timestamp:    cp.Timestamp,
		SkippedFiles: cp.SkippedFiles,
	}, nil
}

func (a *checkpointerAdapter) Restore(ctx context.Context, id string) (agent.CheckpointInfo, error) {
	cp, err := a.m.Restore(ctx, id)
	if err != nil {
		return agent.CheckpointInfo{}, err
	}
	return agent.CheckpointInfo{
		ID:        cp.ID,
		Message:   cp.Message,
		Timestamp: cp.Timestamp,
	}, nil
}

func (a *checkpointerAdapter) List(ctx context.Context, limit int) ([]agent.CheckpointInfo, error) {
	list, err := a.m.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	res := make([]agent.CheckpointInfo, len(list))
	for i, cp := range list {
		res[i] = agent.CheckpointInfo{
			ID:           cp.ID,
			Message:      cp.Message,
			Timestamp:    cp.Timestamp,
			SkippedFiles: cp.SkippedFiles,
		}
	}
	return res, nil
}
