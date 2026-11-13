package session_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/config"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/session"
	_ "modernc.org/sqlite"
)

func TestStore_Migrations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_migrations.db")

	// 1. Fresh database initializes to CurrentSchemaVersion
	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	db, err := store.Open()
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("querying schema version failed: %v", err)
	}
	if version != session.CurrentSchemaVersion {
		t.Fatalf("expected version %d, got %d", session.CurrentSchemaVersion, version)
	}

	// 2. Newer schema refusal
	newerDBPath := filepath.Join(tempDir, "newer.db")
	newerDB, err := sql.Open("sqlite", newerDBPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = newerDB.Exec("CREATE TABLE schema_version (version INT PRIMARY KEY, applied_at TIMESTAMP)")
	_, _ = newerDB.Exec("INSERT INTO schema_version (version, applied_at) VALUES (?, ?)", session.CurrentSchemaVersion+1, time.Now())
	_ = newerDB.Close()

	newerStore, _ := session.NewStore(newerDBPath)
	_, openErr := newerStore.Open()
	if openErr == nil {
		t.Fatal("expected error opening database with newer schema version, got nil")
	}
	if !strings.Contains(openErr.Error(), "upgrade forge") {
		t.Fatalf("expected error to mention 'upgrade forge', got: %v", openErr)
	}

	// 3. Corrupt database refusal (graceful, no panic)
	corruptDBPath := filepath.Join(tempDir, "corrupt.db")
	_ = os.WriteFile(corruptDBPath, []byte("NOT A VALID SQLITE DATABASE FILE HEADER"), 0600)

	corruptStore, _ := session.NewStore(corruptDBPath)
	_, corruptErr := corruptStore.Open()
	if corruptErr == nil {
		t.Fatal("expected error opening corrupt database, got nil")
	}
	if !strings.Contains(corruptErr.Error(), "corrupt") {
		t.Fatalf("expected error to mention 'corrupt', got: %v", corruptErr)
	}
}

func TestStore_RedactionOnWrite(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_redact.db")

	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	fakeSecretKey := "sk-live-super-secret-key-12345"
	agent.RegisterSecret(fakeSecretKey)

	ctx := context.Background()
	sessID := session.GenerateSessionID()

	// 1. Session with secret in title
	err = store.CreateSession(ctx, session.SessionRecord{
		ID:        sessID,
		Workspace: tempDir,
		Provider:  "openai",
		Model:     "gpt-4o",
		Title:     "Debug turn with " + fakeSecretKey,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Message with secret in content
	msgID := "msg_1"
	err = store.SaveMessage(ctx, session.MessageRecord{
		ID:        msgID,
		SessionID: sessID,
		Role:      "user",
		Content:   "Please use key: " + fakeSecretKey,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Tool call with secret in input and output
	err = store.SaveToolCall(ctx, session.ToolCallRecord{
		ID:         "call_1",
		MessageID:  msgID,
		Name:       "bash",
		Input:      fmt.Sprintf(`{"command":"echo %s"}`, fakeSecretKey),
		Output:     fmt.Sprintf(`API key %s authenticated`, fakeSecretKey),
		Status:     "success",
		DurationMs: 15,
		CreatedAt:  time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Query raw SQLite database directly to assert secret NEVER exists in rows
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rawDB.Close() }()

	var title string
	_ = rawDB.QueryRow("SELECT title FROM sessions WHERE id = ?", sessID).Scan(&title)
	if strings.Contains(title, fakeSecretKey) {
		t.Fatalf("CRITICAL SECURITY LEAK: secret found in sessions.title: %s", title)
	}

	var content string
	_ = rawDB.QueryRow("SELECT content FROM messages WHERE id = ?", msgID).Scan(&content)
	if strings.Contains(content, fakeSecretKey) {
		t.Fatalf("CRITICAL SECURITY LEAK: secret found in messages.content: %s", content)
	}

	var input, output string
	_ = rawDB.QueryRow("SELECT input, output FROM tool_calls WHERE id = 'call_1'").Scan(&input, &output)
	if strings.Contains(input, fakeSecretKey) {
		t.Fatalf("CRITICAL SECURITY LEAK: secret found in tool_calls.input: %s", input)
	}
	if strings.Contains(output, fakeSecretKey) {
		t.Fatalf("CRITICAL SECURITY LEAK: secret found in tool_calls.output: %s", output)
	}
}

func TestStore_ToolOutputCapping(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_cap.db")

	store, _ := session.NewStore(dbPath)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	sessID := session.GenerateSessionID()
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID: sessID, Workspace: tempDir, Provider: "mock", Model: "test", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// Massive 100KB output
	giantOutput := strings.Repeat("A", 100*1024)
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m1", SessionID: sessID, Role: "assistant", Content: "done", CreatedAt: time.Now()})
	_ = store.SaveToolCall(ctx, session.ToolCallRecord{
		ID: "tc1", MessageID: "m1", Name: "read_file", Output: giantOutput, Status: "success", CreatedAt: time.Now(),
	})

	tcs, err := store.GetToolCalls(ctx, "m1")
	if err != nil || len(tcs) != 1 {
		t.Fatalf("expected 1 tool call: %v", err)
	}

	out := tcs[0].Output
	if len(out) > session.MaxToolOutputBytes+100 {
		t.Fatalf("output length %d exceeded cap %d", len(out), session.MaxToolOutputBytes)
	}
	if !strings.Contains(out, "[truncated: tool output exceeded 64KB]") {
		t.Fatal("expected truncation marker in capped output")
	}
}

func TestStore_RetentionPruneAndCascadeDelete(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_retention.db")

	store, _ := session.NewStore(dbPath)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	// 1. Session A: old (100 days ago)
	oldID := "sess_old"
	oldTime := time.Now().AddDate(0, 0, -100)
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID: oldID, Workspace: tempDir, Provider: "openai", Model: "gpt-4o", CreatedAt: oldTime, UpdatedAt: oldTime,
	})
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m_old", SessionID: oldID, Role: "user", Content: "old", CreatedAt: oldTime})
	_ = store.SaveToolCall(ctx, session.ToolCallRecord{ID: "tc_old", MessageID: "m_old", Name: "bash", Status: "ok", CreatedAt: oldTime})

	// 2. Session B: recent (today)
	recentID := "sess_recent"
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID: recentID, Workspace: tempDir, Provider: "openai", Model: "gpt-4o", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m_rec", SessionID: recentID, Role: "user", Content: "recent", CreatedAt: time.Now()})

	// Prune sessions older than 90 days
	deleted, err := store.PruneSessions(ctx, 90)
	if err != nil {
		t.Fatalf("PruneSessions failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 session deleted, got %d", deleted)
	}

	// Verify old session and its cascaded messages/tool calls are gone
	_, err = store.GetSession(ctx, oldID)
	if err == nil {
		t.Fatal("expected old session to be deleted")
	}
	oldMsgs, _ := store.GetMessages(ctx, oldID)
	if len(oldMsgs) != 0 {
		t.Fatalf("expected cascaded deletion of messages, got %d", len(oldMsgs))
	}

	// Verify recent session is intact
	rec, err := store.GetSession(ctx, recentID)
	if err != nil || rec == nil {
		t.Fatalf("expected recent session to remain, got err: %v", err)
	}

	// Test explicit delete on recent session
	err = store.DeleteSession(ctx, recentID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	_, err = store.GetSession(ctx, recentID)
	if err == nil {
		t.Fatal("expected session to be deleted")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_concurrent.db")

	// Verify two distinct Store instances (simulating two processes)
	store1, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store1.Close() }()

	store2, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store2.Close() }()

	if _, err := store1.Open(); err != nil {
		t.Fatal(err)
	}
	if _, err := store2.Open(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	for i := 0; i < 10; i++ {
		wg.Add(2)
		idx := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("p1_sess_%d", idx)
			if err := store1.CreateSession(ctx, session.SessionRecord{
				ID: id, Workspace: tempDir, Provider: "openai", Model: "gpt-4o", CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}); err != nil {
				errCh <- err
			}
			_ = store1.SaveMessage(ctx, session.MessageRecord{
				ID: fmt.Sprintf("p1_m_%d", idx), SessionID: id, Role: "user", Content: "msg", CreatedAt: time.Now(),
			})
		}()

		go func() {
			defer wg.Done()
			id := fmt.Sprintf("p2_sess_%d", idx)
			if err := store2.CreateSession(ctx, session.SessionRecord{
				ID: id, Workspace: tempDir, Provider: "anthropic", Model: "claude-sonnet-5-5", CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}); err != nil {
				errCh <- err
			}
			_ = store2.SaveMessage(ctx, session.MessageRecord{
				ID: fmt.Sprintf("p2_m_%d", idx), SessionID: id, Role: "user", Content: "msg", CreatedAt: time.Now(),
			})
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent SQLite error: %v", err)
	}

	list, err := store1.ListSessions(ctx, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 20 {
		t.Fatalf("expected 20 concurrent sessions, got %d", len(list))
	}
}

func TestSession_CrashRecoverySynthesis(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_crash.db")

	store, _ := session.NewStore(dbPath)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	sessID := session.GenerateSessionID()
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID: sessID, Workspace: tempDir, Provider: "openai", Model: "gpt-4o", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// Turn 1: User prompt
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m1", SessionID: sessID, Role: "user", Content: "edit main.go", CreatedAt: time.Now()})

	// Assistant dispatches two tool calls: tc1 and tc2
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m2", SessionID: sessID, Role: "assistant", Content: "Editing file...", CreatedAt: time.Now()})
	_ = store.SaveToolCall(ctx, session.ToolCallRecord{ID: "tc1", MessageID: "m2", Name: "read_file", Input: `{"path":"main.go"}`, Status: "running", CreatedAt: time.Now()})
	_ = store.SaveToolCall(ctx, session.ToolCallRecord{ID: "tc2", MessageID: "m2", Name: "edit_file", Input: `{"path":"main.go"}`, Status: "running", CreatedAt: time.Now()})

	// tc1 completed before crash
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m3", SessionID: sessID, Role: "tool", Content: "package main", ToolCallID: "tc1", CreatedAt: time.Now()})
	// FORGE WAS KILLED / CRASHED HERE BEFORE tc2 RESULT WAS PERSISTED!

	// Resume session: LoadResumedSession must synthesize the interrupted tool result
	resumed, err := session.LoadResumedSession(ctx, store, sessID)
	if err != nil {
		t.Fatalf("LoadResumedSession failed: %v", err)
	}

	// Verify synthesized message exists
	var foundSynth bool
	for _, m := range resumed.Messages {
		if m.Role == "tool" && m.ToolCallID == "tc2" {
			foundSynth = true
			if !strings.Contains(m.Content, "interrupted") {
				t.Fatalf("expected synthetic message to mention 'interrupted', got: %s", m.Content)
			}
		}
	}
	if !foundSynth {
		t.Fatal("expected synthetic tool result for dangling tool call tc2")
	}

	// Verify LLM request history includes the synthetic result and the resume note
	var llmSynthFound bool
	var resumeNoteFound bool
	for _, lm := range resumed.LLMMessages {
		if lm.Role == llm.RoleTool && lm.ToolCallID == "tc2" {
			llmSynthFound = true
		}
		if lm.Role == llm.RoleUser && strings.Contains(lm.Text, "[Resumed session") {
			resumeNoteFound = true
		}
	}
	if !llmSynthFound {
		t.Fatal("expected LLM history to contain synthetic tool result for tc2")
	}
	if !resumeNoteFound {
		t.Fatal("expected LLM history to contain injected resume system note")
	}
}

func TestSession_ResumeAcrossProviderSwitch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_switch.db")

	store, _ := session.NewStore(dbPath)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	sessID := session.GenerateSessionID()
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID: sessID, Workspace: tempDir, Provider: "openai", Model: "gpt-4o", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m1", SessionID: sessID, Role: "user", Content: "Hello from OpenAI turn", CreatedAt: time.Now()})
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m2", SessionID: sessID, Role: "assistant", Content: "Understood", CreatedAt: time.Now()})

	// Resume and switch provider to mock (simulating switching from OpenAI to Anthropic/Mock)
	resumed, err := session.LoadResumedSession(ctx, store, sessID)
	if err != nil {
		t.Fatal(err)
	}

	// Assert mock provider receives the complete prior history plus resume note
	var capturedHistory []llm.Message
	mockProv := mock.New(mock.Turn{
		mock.Text("Continuing conversation seamlessly after provider switch."),
	})

	// Wrap in test handler that captures request history
	req := llm.Request{
		Messages: append(resumed.LLMMessages, llm.Message{Role: llm.RoleUser, Text: "Now continue"}),
	}
	capturedHistory = req.Messages

	stream, err := mockProv.Stream(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}

	if len(capturedHistory) != 4 {
		t.Fatalf("expected 4 messages in captured history (user, assistant, resume note, follow-up), got %d", len(capturedHistory))
	}
	if capturedHistory[0].Text != "Hello from OpenAI turn" {
		t.Errorf("expected original user text, got %q", capturedHistory[0].Text)
	}
	if capturedHistory[1].Text != "Understood" {
		t.Errorf("expected original assistant text, got %q", capturedHistory[1].Text)
	}
	if !strings.Contains(capturedHistory[2].Text, "[Resumed session") {
		t.Errorf("expected resume system note at index 2, got %q", capturedHistory[2].Text)
	}
	if capturedHistory[3].Text != "Now continue" {
		t.Errorf("expected follow-up prompt, got %q", capturedHistory[3].Text)
	}
}

func TestSession_HostileCases(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_hostile.db")

	store, _ := session.NewStore(dbPath)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	sessID := session.GenerateSessionID()
	origWS := filepath.Join(tempDir, "original-workspace")
	_ = os.MkdirAll(origWS, 0755)

	_ = store.CreateSession(ctx, session.SessionRecord{
		ID: sessID, Workspace: origWS, Provider: "openai", Model: "gpt-4o", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// 1. Hostile case: Resume from wrong workspace root
	attackerWS := filepath.Join(tempDir, "attacker-workspace")
	_ = os.MkdirAll(attackerWS, 0755)

	err := session.ValidateWorkspaceBinding(origWS, attackerWS, sessID)
	if err == nil {
		t.Fatal("expected error resuming from wrong workspace, got nil")
	}
	if !strings.Contains(err.Error(), "--root") {
		t.Fatalf("expected error to explain how to proceed with '--root', got: %v", err)
	}

	// 2. Hostile case: Non-existent session ID
	_, err = session.LoadResumedSession(ctx, store, "non-existent-session-id")
	if err == nil {
		t.Fatal("expected error for nonexistent session id")
	}

	// 3. Hostile case: Project config attempting to override sessions database or config
	projectHostileDir := filepath.Join(tempDir, "project-hostile")
	_ = os.MkdirAll(filepath.Join(projectHostileDir, ".forge"), 0755)
	hostileJSON := `{
		"model": "safe-model",
		"sessions.enabled": false,
		"sessions.database_path": "/etc/shadow",
		"session_db": "hacked.db"
	}`
	_ = os.WriteFile(filepath.Join(projectHostileDir, ".forge", "config.json"), []byte(hostileJSON), 0644)

	cfg, loadErr := config.Load(config.LoadOptions{WorkspaceRoot: projectHostileDir})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	// Verify project config keys were safely ignored and flagged as disallowed
	if len(cfg.IgnoredProjectKeys) < 3 {
		t.Fatalf("expected hostile session keys to be in IgnoredProjectKeys, got: %v", cfg.IgnoredProjectKeys)
	}
}

func BenchmarkSession_Resume500Messages(b *testing.B) {
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench_500.db")

	store, err := session.NewStore(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	sessID := session.GenerateSessionID()
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID: sessID, Workspace: tempDir, Provider: "openai", Model: "gpt-4o", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// Pre-populate 500 messages with tool calls
	for i := 0; i < 250; i++ {
		uID := fmt.Sprintf("u_%d", i)
		aID := fmt.Sprintf("a_%d", i)
		_ = store.SaveMessage(ctx, session.MessageRecord{ID: uID, SessionID: sessID, Role: "user", Content: fmt.Sprintf("Query %d", i), CreatedAt: time.Now()})
		_ = store.SaveMessage(ctx, session.MessageRecord{ID: aID, SessionID: sessID, Role: "assistant", Content: fmt.Sprintf("Answer %d", i), CreatedAt: time.Now()})
		_ = store.SaveToolCall(ctx, session.ToolCallRecord{ID: fmt.Sprintf("tc_%d", i), MessageID: aID, Name: "bash", Input: `{"cmd":"test"}`, Output: "pass", Status: "success", CreatedAt: time.Now()})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resumed, err := session.LoadResumedSession(ctx, store, sessID)
		if err != nil {
			b.Fatalf("LoadResumedSession benchmark failed: %v", err)
		}
		if len(resumed.Messages) < 500 {
			b.Fatalf("expected at least 500 messages, got %d", len(resumed.Messages))
		}
	}
}
