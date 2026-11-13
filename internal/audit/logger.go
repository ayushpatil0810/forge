package audit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ayushpatil0810/forge/internal/redact"
)

var (
	ErrTampered = errors.New("audit log integrity verification failed")
	ErrRequired = errors.New("audit logging failed and audit.required is true")
)

type Config struct {
	Enabled  bool   `json:"enabled"`
	Path     string `json:"path"`
	MaxMB    int    `json:"max_mb"`
	Required bool   `json:"required"`
	Root     string `json:"root"`
}

type Logger struct {
	cfg             Config
	mu              sync.Mutex
	sessionSeq      map[string]int
	sessionLastHash map[string]string
	warning         string
	maxBytes        int64
}

type chainState struct {
	Sessions map[string]sessionCursor `json:"sessions"`
}

type sessionCursor struct {
	Seq  int    `json:"seq"`
	Hash string `json:"hash"`
}

// New creates a new audit logger.
func New(cfg Config) *Logger {
	if cfg.MaxMB <= 0 {
		cfg.MaxMB = 50
	}
	return &Logger{
		cfg:             cfg,
		sessionSeq:      make(map[string]int),
		sessionLastHash: make(map[string]string),
	}
}

// Log writes an audit event. It automatically redacts secrets, assigns sequence numbers,
// and computes the tamper-evident per-session SHA-256 hash chain.
func (l *Logger) Log(_ context.Context, sessionID, event, tool, actionSummary, decision, decisionSource string, exitCode *int, durationMs *int64, output string) error {
	if !l.cfg.Enabled {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	seq := l.sessionSeq[sessionID] + 1
	prevHash := l.sessionLastHash[sessionID]
	if prevHash == "" {
		prevHash = GenesisHash
	}

	// Redact action summary
	cleanSummary := redact.Redact(actionSummary)

	// Tool output hash and size (never raw output in audit log)
	var outSHA string
	var outSize *int
	if output != "" {
		s := len(output)
		outSize = &s
		h := sha256.Sum256([]byte(output))
		outSHA = hex.EncodeToString(h[:])
	}

	rec := Record{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:      sessionID,
		Seq:            seq,
		Event:          event,
		Tool:           tool,
		ActionSummary:  cleanSummary,
		Decision:       decision,
		DecisionSource: decisionSource,
		ExitCode:       exitCode,
		DurationMs:     durationMs,
		OutputSize:     outSize,
		OutputSHA256:   outSHA,
		PrevSHA256:     prevHash,
	}

	recHash, err := ComputeRecordHash(rec)
	if err != nil {
		return l.handleError(err)
	}
	rec.RecordSHA256 = recHash

	if err := l.withFileLock(func() error {
		if err := l.maybeRotate(); err != nil {
			return err
		}
		if err := l.appendRecord(rec); err != nil {
			return err
		}
		return l.writeState(sessionID, seq, recHash)
	}); err != nil {
		return l.handleError(err)
	}

	l.sessionSeq[sessionID] = seq
	l.sessionLastHash[sessionID] = recHash
	return nil
}

func (l *Logger) withFileLock(fn func() error) error {
	if l.cfg.Path == "" {
		return fn()
	}
	if err := os.MkdirAll(filepath.Dir(l.cfg.Path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.cfg.Path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := lockExclusive(f); err != nil {
		return err
	}
	defer func() { _ = unlockExclusive(f) }()
	return fn()
}

func statePath(logPath string) string {
	return logPath + ".state.json"
}

func (l *Logger) writeState(sessionID string, seq int, hash string) error {
	path := statePath(l.cfg.Path)
	st := chainState{Sessions: map[string]sessionCursor{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st)
		if st.Sessions == nil {
			st.Sessions = map[string]sessionCursor{}
		}
	}
	st.Sessions[sessionID] = sessionCursor{Seq: seq, Hash: hash}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

// Warning returns the fail-open audit warning, if a write has failed while
// audit.required is false. It is set once.
func (l *Logger) Warning() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.warning
}

func (l *Logger) handleError(err error) error {
	if l.cfg.Required {
		return fmt.Errorf("%w: %v", ErrRequired, err)
	}
	if l.warning == "" {
		l.warning = fmt.Sprintf("audit log write failed: %v", err)
		fmt.Fprintf(os.Stderr, "warning: %s\n", l.warning)
	}
	return nil
}

func (l *Logger) appendRecord(r Record) error {
	path := l.cfg.Path
	if path == "" {
		return errors.New("no audit log path configured")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(r)
	if err != nil {
		return err
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (l *Logger) maybeRotate() error {
	path := l.cfg.Path
	if path == "" {
		return nil
	}

	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	maxBytes := int64(l.cfg.MaxMB) * 1024 * 1024
	if l.maxBytes > 0 {
		maxBytes = l.maxBytes
	}
	if fi.Size() < maxBytes {
		return nil
	}

	// Rotate to a unique sibling so two rotations in the same second cannot
	// overwrite each other. Verify reads these siblings back in name order.
	stamp := time.Now().UnixNano()
	for {
		rotated := fmt.Sprintf("%s.%d", path, stamp)
		if _, err := os.Stat(rotated); os.IsNotExist(err) {
			return os.Rename(path, rotated)
		}
		stamp++
	}
}

// Verify validates the hash chain across the current log and any rotated files,
// then compares the end of each session with the state file written beside the log.
// A log with its tail deleted fails. An attacker who can rewrite both the log and
// the state file can still present a consistent history.
func Verify(path string, sessionID string) (int, error) {
	st, err := readState(statePath(path))
	if err != nil {
		return 0, err
	}
	files, err := chainFiles(path)
	if err != nil {
		return 0, err
	}

	verifiedCount := 0
	sessionLastSeq := make(map[string]int)
	sessionLastHash := make(map[string]string)
	lineNum := 0

	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			if os.IsNotExist(err) && file == path {
				continue
			}
			return verifiedCount, err
		}
		scanner := bufio.NewScanner(f)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 10*1024*1024)
		for scanner.Scan() {
			lineNum++
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var r Record
			if err := json.Unmarshal(line, &r); err != nil {
				_ = f.Close()
				return verifiedCount, fmt.Errorf("%w: line %d is invalid JSON: %v", ErrTampered, lineNum, err)
			}
			if sessionID != "" && r.SessionID != sessionID {
				continue
			}
			expectedHash, err := ComputeRecordHash(r)
			if err != nil {
				_ = f.Close()
				return verifiedCount, fmt.Errorf("%w: line %d error computing hash: %v", ErrTampered, lineNum, err)
			}
			if r.RecordSHA256 != expectedHash {
				_ = f.Close()
				return verifiedCount, fmt.Errorf("%w: line %d record hash mismatch (modified record): got %s, want %s", ErrTampered, lineNum, r.RecordSHA256, expectedHash)
			}
			lastSeq := sessionLastSeq[r.SessionID]
			if r.Seq != lastSeq+1 {
				_ = f.Close()
				return verifiedCount, fmt.Errorf("%w: line %d session %s sequence jump/gap (deleted or reordered record): got seq %d, expected %d", ErrTampered, lineNum, r.SessionID, r.Seq, lastSeq+1)
			}
			lastHash := sessionLastHash[r.SessionID]
			if lastHash == "" {
				if r.PrevSHA256 != GenesisHash {
					_ = f.Close()
					return verifiedCount, fmt.Errorf("%w: line %d session %s initial record does not link to genesis hash", ErrTampered, lineNum, r.SessionID)
				}
			} else if r.PrevSHA256 != lastHash {
				_ = f.Close()
				return verifiedCount, fmt.Errorf("%w: line %d session %s hash chain broken: got prev %s, expected %s", ErrTampered, lineNum, r.SessionID, r.PrevSHA256, lastHash)
			}
			sessionLastSeq[r.SessionID] = r.Seq
			sessionLastHash[r.SessionID] = r.RecordSHA256
			verifiedCount++
		}
		scanErr := scanner.Err()
		_ = f.Close()
		if scanErr != nil {
			return verifiedCount, scanErr
		}
	}

	check := st.Sessions
	if sessionID != "" {
		cur, ok := st.Sessions[sessionID]
		if !ok {
			return verifiedCount, fmt.Errorf("%w: state file has no cursor for session %s", ErrTampered, sessionID)
		}
		check = map[string]sessionCursor{sessionID: cur}
	}
	for id, want := range check {
		if sessionLastSeq[id] != want.Seq || sessionLastHash[id] != want.Hash {
			return verifiedCount, fmt.Errorf("%w: session %s is missing records from the end (log seq %d, state seq %d)", ErrTampered, id, sessionLastSeq[id], want.Seq)
		}
	}
	return verifiedCount, nil
}

func readState(path string) (chainState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return chainState{}, fmt.Errorf("%w: audit state file missing; cannot confirm the log was not truncated", ErrTampered)
		}
		return chainState{}, err
	}
	var st chainState
	if err := json.Unmarshal(data, &st); err != nil {
		return chainState{}, fmt.Errorf("%w: audit state file is invalid: %v", ErrTampered, err)
	}
	if st.Sessions == nil {
		st.Sessions = map[string]sessionCursor{}
	}
	return st, nil
}

func chainFiles(path string) ([]string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var rotated []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base+".") {
			continue
		}
		suf := strings.TrimPrefix(name, base+".")
		if _, err := strconv.ParseInt(suf, 10, 64); err != nil {
			continue
		}
		rotated = append(rotated, filepath.Join(dir, name))
	}
	sort.Strings(rotated)
	return append(rotated, path), nil
}

// ReadRecords reads records from the audit file, optionally filtering by session and tail count.
func ReadRecords(path string, sessionID string, tailCount int) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var all []Record
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err == nil {
			if sessionID == "" || r.SessionID == sessionID {
				all = append(all, r)
			}
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	if tailCount > 0 && len(all) > tailCount {
		all = all[len(all)-tailCount:]
	}
	return all, nil
}
