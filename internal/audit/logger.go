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

	// Rotate if needed
	if err := l.maybeRotate(); err != nil {
		return l.handleError(err)
	}

	// Write record
	if err := l.appendRecord(rec); err != nil {
		return l.handleError(err)
	}

	l.sessionSeq[sessionID] = seq
	l.sessionLastHash[sessionID] = recHash
	return nil
}

func (l *Logger) handleError(err error) error {
	if l.cfg.Required {
		return fmt.Errorf("%w: %v", ErrRequired, err)
	}
	// Fail-open: warn and continue
	fmt.Fprintf(os.Stderr, "warning: audit log write failed: %v\n", err)
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
	if fi.Size() < maxBytes {
		return nil
	}

	// Rotate files: path.1 -> path.2, etc.
	rotated := fmt.Sprintf("%s.%d", path, time.Now().Unix())
	return os.Rename(path, rotated)
}

// Verify validates the cryptographic hash chain of the audit log.
// If sessionID is specified, only that session is verified; otherwise all sessions are checked.
func Verify(path string, sessionID string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	// Allow large lines
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	lineNum := 0
	verifiedCount := 0
	sessionLastSeq := make(map[string]int)
	sessionLastHash := make(map[string]string)

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			return verifiedCount, fmt.Errorf("%w: line %d is invalid JSON: %v", ErrTampered, lineNum, err)
		}

		if sessionID != "" && r.SessionID != sessionID {
			continue
		}

		// 1. Verify record internal hash
		expectedHash, err := ComputeRecordHash(r)
		if err != nil {
			return verifiedCount, fmt.Errorf("%w: line %d error computing hash: %v", ErrTampered, lineNum, err)
		}
		if r.RecordSHA256 != expectedHash {
			return verifiedCount, fmt.Errorf("%w: line %d record hash mismatch (modified record): got %s, want %s", ErrTampered, lineNum, r.RecordSHA256, expectedHash)
		}

		// 2. Verify sequence number
		lastSeq := sessionLastSeq[r.SessionID]
		if r.Seq != lastSeq+1 {
			return verifiedCount, fmt.Errorf("%w: line %d session %s sequence jump/gap (deleted or reordered record): got seq %d, expected %d", ErrTampered, lineNum, r.SessionID, r.Seq, lastSeq+1)
		}

		// 3. Verify previous record hash link
		lastHash := sessionLastHash[r.SessionID]
		if lastHash == "" {
			if r.PrevSHA256 != GenesisHash {
				return verifiedCount, fmt.Errorf("%w: line %d session %s initial record does not link to genesis hash", ErrTampered, lineNum, r.SessionID)
			}
		} else {
			if r.PrevSHA256 != lastHash {
				return verifiedCount, fmt.Errorf("%w: line %d session %s hash chain broken: got prev %s, expected %s", ErrTampered, lineNum, r.SessionID, r.PrevSHA256, lastHash)
			}
		}

		sessionLastSeq[r.SessionID] = r.Seq
		sessionLastHash[r.SessionID] = r.RecordSHA256
		verifiedCount++
	}

	if err := scanner.Err(); err != nil {
		return verifiedCount, err
	}

	return verifiedCount, nil
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
