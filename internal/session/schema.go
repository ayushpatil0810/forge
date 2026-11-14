package session

import (
	"database/sql"
	"fmt"
	"time"
)

const CurrentSchemaVersion = 2

const schemaV1 = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    workspace TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    title TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_workspace ON sessions(workspace);
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    tool_call_id TEXT,
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);

CREATE TABLE IF NOT EXISTS tool_calls (
    id TEXT PRIMARY KEY,
    message_id TEXT,
    name TEXT NOT NULL,
    input TEXT,
    output TEXT,
    status TEXT NOT NULL,
    duration_ms INTEGER,
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tool_calls_message ON tool_calls(message_id);

CREATE TABLE IF NOT EXISTS checkpoints (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn_index INTEGER NOT NULL,
    git_ref TEXT NOT NULL,
    files TEXT,
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_checkpoints_session ON checkpoints(session_id);

CREATE TABLE IF NOT EXISTS usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    input_tokens INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    cost REAL NOT NULL,
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_usage_session ON usage(session_id);
`

const schemaV2 = `
CREATE TABLE IF NOT EXISTS compactions (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    tier INTEGER NOT NULL,
    summary TEXT NOT NULL,
    first_message_id TEXT NOT NULL,
    last_message_id TEXT NOT NULL,
    tokens_before INTEGER NOT NULL,
    tokens_after INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_compactions_session ON compactions(session_id);
`

func migrate(db *sql.DB) error {
	// 1. Check if schema_version table exists
	var tableExists int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_version'").Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("checking schema_version table: %w", err)
	}

	currentVersion := 0
	if tableExists > 0 {
		var v int
		err := db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&v)
		if err == nil {
			currentVersion = v
		}
	}

	// 2. Refuse if DB has newer schema than binary understands
	if currentVersion > CurrentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than this Forge binary supports (version %d); please upgrade forge", currentVersion, CurrentSchemaVersion)
	}

	// 3. Forward-only migrations
	if currentVersion < 1 {
		if _, err := db.Exec(schemaV1); err != nil {
			return fmt.Errorf("applying schema version 1: %w", err)
		}
		if _, err := db.Exec("INSERT INTO schema_version (version, applied_at) VALUES (?, ?)", 1, time.Now().UTC()); err != nil {
			return fmt.Errorf("recording schema version 1: %w", err)
		}
	}

	if currentVersion < 2 {
		if _, err := db.Exec(schemaV2); err != nil {
			return fmt.Errorf("applying schema version 2: %w", err)
		}
		if _, err := db.Exec("INSERT INTO schema_version (version, applied_at) VALUES (?, ?)", 2, time.Now().UTC()); err != nil {
			return fmt.Errorf("recording schema version 2: %w", err)
		}
	}

	return nil
}
