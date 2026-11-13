package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	_ "modernc.org/sqlite"
)

const MaxToolOutputBytes = 65536 // 64KB cap

type SessionRecord struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SessionSummary struct {
	ID           string    `json:"id"`
	Workspace    string    `json:"workspace"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	TotalCost    float64   `json:"total_cost"`
}

type MessageRecord struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type ToolCallRecord struct {
	ID         string    `json:"id"`
	MessageID  string    `json:"message_id"`
	Name       string    `json:"name"`
	Input      string    `json:"input"`
	Output     string    `json:"output"`
	Status     string    `json:"status"`
	DurationMs int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

type CheckpointRecord struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	TurnIndex int       `json:"turn_index"`
	GitRef    string    `json:"git_ref"`
	Files     string    `json:"files,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type UsageRecord struct {
	ID           int64     `json:"id"`
	SessionID    string    `json:"session_id"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Cost         float64   `json:"cost"`
	CreatedAt    time.Time `json:"created_at"`
}

// Store provides thread-safe access to session storage in SQLite.
type Store struct {
	dbPath string
	mu     sync.Mutex
	db     *sql.DB
}

// DefaultDBPath returns the session SQLite database path (or FORGE_SESSIONS_PATH if set).
func DefaultDBPath() (string, error) {
	if env := os.Getenv("FORGE_SESSIONS_PATH"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("user home directory not found")
	}
	dir := filepath.Join(home, ".forge")
	return filepath.Join(dir, "sessions.db"), nil
}

// NewStore creates a Store with lazy database initialization.
func NewStore(dbPath string) (*Store, error) {
	if dbPath == "" {
		def, err := DefaultDBPath()
		if err != nil {
			return nil, err
		}
		dbPath = def
	}
	return &Store{dbPath: dbPath}, nil
}

// Open lazily opens and initializes the SQLite database with WAL and migrations.
func (s *Store) Open() (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return s.db, nil
	}

	dir := filepath.Dir(s.dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating database directory %s: %w", dir, err)
	}

	// Mode 0600 if file does not exist yet
	if _, err := os.Stat(s.dbPath); os.IsNotExist(err) {
		f, err := os.OpenFile(s.dbPath, os.O_CREATE|os.O_RDWR, 0600)
		if err == nil {
			_ = f.Close()
		}
	}

	// SQLite connection string with busy_timeout and foreign_keys
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)", s.dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	_, _ = db.Exec("PRAGMA journal_mode = WAL;")
	_, _ = db.Exec("PRAGMA busy_timeout = 5000;")
	_, _ = db.Exec("PRAGMA foreign_keys = ON;")

	// Run migrations
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	s.db = db
	return s.db, nil
}

// UpdateSessionTitle updates the title of an existing session if currently unset or default.
func (s *Store) UpdateSessionTitle(ctx context.Context, id, title string) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	title = TruncateTitle(title)
	if title == "" {
		return nil
	}
	_, err = db.ExecContext(ctx, "UPDATE sessions SET title = ?, updated_at = ? WHERE id = ? AND (title IS NULL OR title = '' OR title = 'New Session')", title, time.Now().UTC(), id)
	return err
}

// Doctor performs an integrity check (PRAGMA quick_check) on the database file.
func (s *Store) Doctor(ctx context.Context) (string, error) {
	db, err := s.Open()
	if err != nil {
		return "", err
	}
	var res string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&res); err != nil {
		return "", fmt.Errorf("database integrity check failed: %w", err)
	}
	return res, nil
}

// Close closes the underlying SQLite database.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}

// DBPath returns the configured file path.
func (s *Store) DBPath() string {
	return s.dbPath
}

func sanitize(s string) string {
	return agent.RedactSecrets(s)
}

func capToolOutput(output string) string {
	output = sanitize(output)
	if len(output) > MaxToolOutputBytes {
		return output[:MaxToolOutputBytes] + "\n\n[truncated: tool output exceeded 64KB]"
	}
	return output
}

// CreateSession persists a new session.
func (s *Store) CreateSession(ctx context.Context, rec SessionRecord) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	query := `INSERT INTO sessions (id, workspace, provider, model, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err = db.ExecContext(ctx, query,
		rec.ID,
		rec.Workspace,
		rec.Provider,
		rec.Model,
		sanitize(rec.Title),
		rec.CreatedAt.UTC(),
		rec.UpdatedAt.UTC(),
	)
	return err
}

// UpdateSession updates session title, model, and updated_at.
func (s *Store) UpdateSession(ctx context.Context, id, title, model string, updatedAt time.Time) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	query := `UPDATE sessions SET title = ?, model = ?, updated_at = ? WHERE id = ?`
	_, err = db.ExecContext(ctx, query, sanitize(title), model, updatedAt.UTC(), id)
	return err
}

// GetSession retrieves a single session record by ID.
func (s *Store) GetSession(ctx context.Context, id string) (*SessionRecord, error) {
	db, err := s.Open()
	if err != nil {
		return nil, err
	}
	query := `SELECT id, workspace, provider, model, title, created_at, updated_at FROM sessions WHERE id = ?`
	row := db.QueryRowContext(ctx, query, id)

	var rec SessionRecord
	if err := row.Scan(&rec.ID, &rec.Workspace, &rec.Provider, &rec.Model, &rec.Title, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session %q not found", id)
		}
		return nil, err
	}
	return &rec, nil
}

// ListSessions lists sessions, optionally filtered by workspace root.
func (s *Store) ListSessions(ctx context.Context, workspace string, all bool) ([]SessionSummary, error) {
	db, err := s.Open()
	if err != nil {
		return nil, err
	}

	var rows *sql.Rows
	if all || workspace == "" {
		query := `
			SELECT s.id, s.workspace, s.provider, s.model, s.title, s.created_at, s.updated_at,
			       COUNT(DISTINCT m.id) as msg_count,
			       COALESCE(SUM(u.cost), 0.0) as total_cost
			FROM sessions s
			LEFT JOIN messages m ON s.id = m.session_id
			LEFT JOIN usage u ON s.id = u.session_id
			GROUP BY s.id
			ORDER BY s.updated_at DESC
		`
		rows, err = db.QueryContext(ctx, query)
	} else {
		query := `
			SELECT s.id, s.workspace, s.provider, s.model, s.title, s.created_at, s.updated_at,
			       COUNT(DISTINCT m.id) as msg_count,
			       COALESCE(SUM(u.cost), 0.0) as total_cost
			FROM sessions s
			LEFT JOIN messages m ON s.id = m.session_id
			LEFT JOIN usage u ON s.id = u.session_id
			WHERE s.workspace = ?
			GROUP BY s.id
			ORDER BY s.updated_at DESC
		`
		rows, err = db.QueryContext(ctx, query, workspace)
	}

	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var summaries []SessionSummary
	for rows.Next() {
		var sum SessionSummary
		if err := rows.Scan(
			&sum.ID,
			&sum.Workspace,
			&sum.Provider,
			&sum.Model,
			&sum.Title,
			&sum.CreatedAt,
			&sum.UpdatedAt,
			&sum.MessageCount,
			&sum.TotalCost,
		); err != nil {
			return nil, err
		}
		summaries = append(summaries, sum)
	}
	return summaries, rows.Err()
}

// DeleteSession deletes a session and cascades to messages, tool_calls, checkpoints, and usage.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	_, _ = db.ExecContext(ctx, "DELETE FROM tool_calls WHERE message_id IN (SELECT id FROM messages WHERE session_id = ?)", id)
	_, _ = db.ExecContext(ctx, "DELETE FROM checkpoints WHERE session_id = ?", id)
	_, _ = db.ExecContext(ctx, "DELETE FROM usage WHERE session_id = ?", id)
	_, _ = db.ExecContext(ctx, "DELETE FROM messages WHERE session_id = ?", id)
	res, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %q not found", id)
	}
	return nil
}

// PruneSessions removes sessions older than retentionDays. Returns count of deleted sessions.
func (s *Store) PruneSessions(ctx context.Context, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	db, err := s.Open()
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	res, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE updated_at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// SaveMessage stores a provider-neutral message record.
func (s *Store) SaveMessage(ctx context.Context, m MessageRecord) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	query := `INSERT INTO messages (id, session_id, role, content, tool_call_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err = db.ExecContext(ctx, query,
		m.ID,
		m.SessionID,
		m.Role,
		sanitize(m.Content),
		m.ToolCallID,
		m.CreatedAt.UTC(),
	)
	if err == nil {
		// update session updated_at
		_, _ = db.ExecContext(ctx, "UPDATE sessions SET updated_at = ? WHERE id = ?", m.CreatedAt.UTC(), m.SessionID)
	}
	return err
}

// GetMessages loads all messages for a session in chronological order.
func (s *Store) GetMessages(ctx context.Context, sessionID string) ([]MessageRecord, error) {
	db, err := s.Open()
	if err != nil {
		return nil, err
	}
	query := `SELECT id, session_id, role, content, tool_call_id, created_at FROM messages WHERE session_id = ? ORDER BY created_at ASC`
	rows, err := db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var msgs []MessageRecord
	for rows.Next() {
		var m MessageRecord
		var toolCallID sql.NullString
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &toolCallID, &m.CreatedAt); err != nil {
			return nil, err
		}
		if toolCallID.Valid {
			m.ToolCallID = toolCallID.String
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// UpdateMessageContent updates the content of an existing message.
func (s *Store) UpdateMessageContent(ctx context.Context, id, content string) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "UPDATE messages SET content = ? WHERE id = ?", sanitize(content), id)
	return err
}

// SaveToolCall stores a tool call record with inputs and capped outputs.
// Supports UPSERT so tool calls can be written on start and updated on finish.
func (s *Store) SaveToolCall(ctx context.Context, tc ToolCallRecord) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	query := `
		INSERT INTO tool_calls (id, message_id, name, input, output, status, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			message_id = CASE WHEN excluded.message_id != '' THEN excluded.message_id ELSE tool_calls.message_id END,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE tool_calls.name END,
			input = CASE WHEN excluded.input != '' THEN excluded.input ELSE tool_calls.input END,
			output = CASE WHEN excluded.output != '' THEN excluded.output ELSE tool_calls.output END,
			status = excluded.status,
			duration_ms = CASE WHEN excluded.duration_ms > 0 THEN excluded.duration_ms ELSE tool_calls.duration_ms END
	`
	_, err = db.ExecContext(ctx, query,
		tc.ID,
		tc.MessageID,
		tc.Name,
		sanitize(tc.Input),
		capToolOutput(tc.Output),
		tc.Status,
		tc.DurationMs,
		tc.CreatedAt.UTC(),
	)
	return err
}

// GetToolCalls retrieves all tool calls for a given assistant message.
func (s *Store) GetToolCalls(ctx context.Context, messageID string) ([]ToolCallRecord, error) {
	db, err := s.Open()
	if err != nil {
		return nil, err
	}
	query := `SELECT id, message_id, name, input, output, status, duration_ms, created_at FROM tool_calls WHERE message_id = ? ORDER BY created_at ASC`
	rows, err := db.QueryContext(ctx, query, messageID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tcs []ToolCallRecord
	for rows.Next() {
		var tc ToolCallRecord
		if err := rows.Scan(&tc.ID, &tc.MessageID, &tc.Name, &tc.Input, &tc.Output, &tc.Status, &tc.DurationMs, &tc.CreatedAt); err != nil {
			return nil, err
		}
		tcs = append(tcs, tc)
	}
	return tcs, rows.Err()
}

// SaveCheckpoint stores a checkpoint ID and metadata for a turn.
func (s *Store) SaveCheckpoint(ctx context.Context, cp CheckpointRecord) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	query := `INSERT INTO checkpoints (id, session_id, turn_index, git_ref, files, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err = db.ExecContext(ctx, query,
		cp.ID,
		cp.SessionID,
		cp.TurnIndex,
		cp.GitRef,
		sanitize(cp.Files),
		cp.CreatedAt.UTC(),
	)
	return err
}

// GetCheckpoints returns all checkpoints recorded for a session.
func (s *Store) GetCheckpoints(ctx context.Context, sessionID string) ([]CheckpointRecord, error) {
	db, err := s.Open()
	if err != nil {
		return nil, err
	}
	query := `SELECT id, session_id, turn_index, git_ref, files, created_at FROM checkpoints WHERE session_id = ? ORDER BY turn_index ASC`
	rows, err := db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var cps []CheckpointRecord
	for rows.Next() {
		var cp CheckpointRecord
		var files sql.NullString
		if err := rows.Scan(&cp.ID, &cp.SessionID, &cp.TurnIndex, &cp.GitRef, &files, &cp.CreatedAt); err != nil {
			return nil, err
		}
		if files.Valid {
			cp.Files = files.String
		}
		cps = append(cps, cp)
	}
	return cps, rows.Err()
}

// SaveUsage records token usage and cost for a model turn.
func (s *Store) SaveUsage(ctx context.Context, u UsageRecord) error {
	db, err := s.Open()
	if err != nil {
		return err
	}
	query := `INSERT INTO usage (session_id, model, input_tokens, output_tokens, cost, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err = db.ExecContext(ctx, query,
		u.SessionID,
		u.Model,
		u.InputTokens,
		u.OutputTokens,
		u.Cost,
		u.CreatedAt.UTC(),
	)
	return err
}

// GetUsage retrieves all usage segments for a session.
func (s *Store) GetUsage(ctx context.Context, sessionID string) ([]UsageRecord, error) {
	db, err := s.Open()
	if err != nil {
		return nil, err
	}
	query := `SELECT id, session_id, model, input_tokens, output_tokens, cost, created_at FROM usage WHERE session_id = ? ORDER BY id ASC`
	rows, err := db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var usages []UsageRecord
	for rows.Next() {
		var u UsageRecord
		if err := rows.Scan(&u.ID, &u.SessionID, &u.Model, &u.InputTokens, &u.OutputTokens, &u.Cost, &u.CreatedAt); err != nil {
			return nil, err
		}
		usages = append(usages, u)
	}
	return usages, rows.Err()
}
