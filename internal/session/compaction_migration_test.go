package session_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/session"
	_ "modernc.org/sqlite"
)

func TestCompactionSchemaMigration(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_sessions.db")

	// 1. Fresh database creation initializes to CurrentSchemaVersion (2)
	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create fresh store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	sessID := "s_compaction_test"
	if err := store.CreateSession(ctx, session.SessionRecord{
		ID:        sessID,
		Workspace: tempDir,
		Provider:  "anthropic",
		Model:     "claude-sonnet-5-5",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Save and retrieve a compaction record
	comp := session.CompactionRecord{
		ID:             "c_1",
		SessionID:      sessID,
		Tier:           2,
		Summary:        "Summary of earlier turns",
		FirstMessageID: "m_1",
		LastMessageID:  "m_10",
		TokensBefore:   12000,
		TokensAfter:    2500,
		CreatedAt:      time.Now(),
	}
	if err := store.SaveCompaction(ctx, comp); err != nil {
		t.Fatalf("failed to save compaction: %v", err)
	}

	compactions, err := store.GetCompactions(ctx, sessID)
	if err != nil {
		t.Fatalf("failed to get compactions: %v", err)
	}
	if len(compactions) != 1 || compactions[0].Summary != "Summary of earlier turns" {
		t.Fatalf("expected 1 compaction record, got: %+v", compactions)
	}
}

func TestCompactionSchemaUpgradeFromV1(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "v1_sessions.db")

	// Manually create a V1 database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at TIMESTAMP NOT NULL);
INSERT INTO schema_version (version, applied_at) VALUES (1, CURRENT_TIMESTAMP);
CREATE TABLE sessions (id TEXT PRIMARY KEY, workspace TEXT NOT NULL, provider TEXT NOT NULL, model TEXT NOT NULL, title TEXT, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL);
CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, tool_call_id TEXT, created_at TIMESTAMP NOT NULL);
CREATE TABLE tool_calls (id TEXT PRIMARY KEY, message_id TEXT, name TEXT NOT NULL, input TEXT, output TEXT, status TEXT NOT NULL, duration_ms INTEGER, created_at TIMESTAMP NOT NULL);
CREATE TABLE checkpoints (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, turn_index INTEGER NOT NULL, git_ref TEXT NOT NULL, files TEXT, created_at TIMESTAMP NOT NULL);
CREATE TABLE usage (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, model TEXT NOT NULL, input_tokens INTEGER NOT NULL, output_tokens INTEGER NOT NULL, cost REAL NOT NULL, created_at TIMESTAMP NOT NULL);
`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()

	// Opening with NewStore should migrate from V1 to V2
	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to open and migrate V1 database: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	sessID := "s_upgrade_test"
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID:        sessID,
		Workspace: tempDir,
		Provider:  "anthropic",
		Model:     "claude-sonnet-5-5",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	err = store.SaveCompaction(ctx, session.CompactionRecord{
		ID:             "comp_upgrade_1",
		SessionID:      sessID,
		Tier:           1,
		Summary:        "Tool outputs cleared",
		FirstMessageID: "m_1",
		LastMessageID:  "m_5",
		TokensBefore:   15000,
		TokensAfter:    8000,
		CreatedAt:      time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save compaction after migration: %v", err)
	}
}

func TestCompactionSchemaRefusesNewer(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "future_sessions.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at TIMESTAMP NOT NULL);
INSERT INTO schema_version (version, applied_at) VALUES (99, CURRENT_TIMESTAMP);
`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}

	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	_, err = store.Open()
	if err == nil {
		t.Fatalf("expected error opening database with newer schema version")
	}
	if !contains(err.Error(), "newer than this Forge binary supports") {
		t.Fatalf("expected newer schema error message, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return filepath.Clean(s) != "" && (len(s) >= len(substr)) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && (stringContains(s, substr))))
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
