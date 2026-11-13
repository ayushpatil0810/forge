package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAudit_RecordShapesAndSecrets(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger := New(Config{
		Enabled: true,
		Path:    logPath,
		MaxMB:   10,
	})

	ctx := context.Background()
	secretKey := "sk-proj-1234567890abcdef1234567890abcdef1234567890abcdef"
	commandWithSecret := fmt.Sprintf("curl -H 'Authorization: Bearer %s' https://api.openai.com", secretKey)

	exitCode := 0
	dur := int64(150)
	rawOutput := "Success! Process finished with return code 0"

	err := logger.Log(ctx, "sess-1", "tool_requested", "bash", commandWithSecret, "allow", "rule:bash(*)", &exitCode, &dur, rawOutput)
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	// Read file contents directly
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)

	// Assert secret is NEVER present in the audit log file
	if strings.Contains(content, secretKey) {
		t.Fatalf("secret key leaked into audit log! content: %s", content)
	}
	if !strings.Contains(content, "[REDACTED:openai_key]") {
		t.Fatalf("expected redacted key marker, got: %s", content)
	}

	// Assert raw tool output is NOT in the audit log (only output_sha256 and output_size)
	if strings.Contains(content, "Success! Process finished") {
		t.Fatalf("raw tool output leaked into audit log! content: %s", content)
	}

	// Parse record and check shapes
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if rec.SessionID != "sess-1" || rec.Seq != 1 {
		t.Fatalf("unexpected record metadata: %+v", rec)
	}
	if rec.PrevSHA256 != GenesisHash {
		t.Fatalf("expected genesis hash for first record, got %s", rec.PrevSHA256)
	}
	if rec.OutputSHA256 == "" || rec.OutputSize == nil {
		t.Fatalf("expected output sha256 and size, got %+v", rec)
	}

	// Verify file permissions (0600 on posix, check readable/writeable)
	fi, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0077 != 0 {
		t.Logf("Note: Windows file mode permissions %v", fi.Mode().Perm())
	}
}

func TestAudit_ChainVerificationAndTamperDetection(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger := New(Config{
		Enabled: true,
		Path:    logPath,
		MaxMB:   10,
	})

	ctx := context.Background()
	// Write 5 records in a session
	for i := 1; i <= 5; i++ {
		cmd := fmt.Sprintf("echo step-%d", i)
		if err := logger.Log(ctx, "sess-test", "tool_result", "bash", cmd, "allow", "rule:bash(*)", nil, nil, ""); err != nil {
			t.Fatalf("Log #%d failed: %v", i, err)
		}
	}

	// 1. Pristine log must verify cleanly
	count, err := Verify(logPath, "sess-test")
	if err != nil {
		t.Fatalf("Verify failed on valid log: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5 verified records, got %d", count)
	}

	// 2. Tampered record content (e.g. modified command)
	tamperedDir := t.TempDir()
	tamperedPath := filepath.Join(tamperedDir, "audit_tampered.jsonl")
	records, err := ReadRecords(logPath, "", 0)
	if err != nil {
		t.Fatal(err)
	}

	// Modify record 3
	records[2].ActionSummary = "echo modified"
	writeRecords(t, tamperedPath, records)

	_, err = Verify(tamperedPath, "sess-test")
	if err == nil {
		t.Fatal("expected Verify to detect modified record, but it passed!")
	}
	if !errors.Is(err, ErrTampered) {
		t.Fatalf("expected ErrTampered, got: %v", err)
	}

	// 3. Deleted record (record 3 deleted)
	deletedPath := filepath.Join(tamperedDir, "audit_deleted.jsonl")
	deletedRecords := append([]Record{}, records[:2]...)
	deletedRecords = append(deletedRecords, records[3:]...)
	writeRecords(t, deletedPath, deletedRecords)

	_, err = Verify(deletedPath, "sess-test")
	if err == nil {
		t.Fatal("expected Verify to detect deleted record, but it passed!")
	}
	if !errors.Is(err, ErrTampered) {
		t.Fatalf("expected ErrTampered, got: %v", err)
	}

	// 4. Reordered records (swap record 1 and record 2)
	reorderedPath := filepath.Join(tamperedDir, "audit_reordered.jsonl")
	originalRecords, _ := ReadRecords(logPath, "", 0)
	reordered := append([]Record{}, originalRecords...)
	reordered[0], reordered[1] = reordered[1], reordered[0]
	writeRecords(t, reorderedPath, reordered)

	_, err = Verify(reorderedPath, "sess-test")
	if err == nil {
		t.Fatal("expected Verify to detect reordered records, but it passed!")
	}
	if !errors.Is(err, ErrTampered) {
		t.Fatalf("expected ErrTampered, got: %v", err)
	}
}

func TestAudit_ConcurrentSessionsUnderRace(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit_concurrent.jsonl")

	logger := New(Config{
		Enabled: true,
		Path:    logPath,
		MaxMB:   10,
	})

	var wg sync.WaitGroup
	numSessions := 4
	recordsPerSession := 20

	for s := 0; s < numSessions; s++ {
		sessID := fmt.Sprintf("session-%d", s)
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for i := 1; i <= recordsPerSession; i++ {
				_ = logger.Log(context.Background(), id, "tool_result", "bash", fmt.Sprintf("run %d", i), "allow", "mode", nil, nil, "")
			}
		}(sessID)
	}

	wg.Wait()

	// Verify all sessions independently
	for s := 0; s < numSessions; s++ {
		sessID := fmt.Sprintf("session-%d", s)
		count, err := Verify(logPath, sessID)
		if err != nil {
			t.Fatalf("Verify failed on concurrent session %s: %v", sessID, err)
		}
		if count != recordsPerSession {
			t.Fatalf("expected %d records for session %s, got %d", recordsPerSession, sessID, count)
		}
	}
}

func TestAudit_FailOpenVsRequired(t *testing.T) {
	// Attempt write to invalid path (directory instead of file)
	tmpDir := t.TempDir()

	// Fail-open: warns, returns nil error
	loggerOpen := New(Config{
		Enabled:  true,
		Path:     tmpDir, // invalid because tmpDir is a directory
		Required: false,
	})
	err := loggerOpen.Log(context.Background(), "s1", "event", "bash", "cmd", "allow", "mode", nil, nil, "")
	if err != nil {
		t.Fatalf("expected fail-open logger to return nil on write error, got: %v", err)
	}

	// Fail-closed: returns ErrRequired
	loggerClosed := New(Config{
		Enabled:  true,
		Path:     tmpDir, // invalid
		Required: true,
	})
	err = loggerClosed.Log(context.Background(), "s1", "event", "bash", "cmd", "allow", "mode", nil, nil, "")
	if err == nil {
		t.Fatal("expected fail-closed logger to return error, got nil")
	}
	if !errors.Is(err, ErrRequired) {
		t.Fatalf("expected ErrRequired, got: %v", err)
	}
}

func writeRecords(t *testing.T, path string, records []Record) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	for _, r := range records {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write(append(data, '\n'))
	}
}
