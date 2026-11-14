package session_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/session"
	_ "modernc.org/sqlite"
)

func TestResumeRebuildsCompactedView(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "resume_test.db")

	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	sessID := "s_resume_compact"
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID:        sessID,
		Workspace: tempDir,
		Provider:  "anthropic",
		Model:     "claude-sonnet-5-5",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	// Save 4 turns (turns 1-2 will be compacted; turns 3-4 remain verbatim)
	// Turn 1
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m1", SessionID: sessID, Role: "user", Content: "Turn 1 prompt", CreatedAt: time.Now().Add(-4 * time.Minute)})
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m2", SessionID: sessID, Role: "assistant", Content: "Turn 1 response", CreatedAt: time.Now().Add(-4 * time.Minute)})
	// Turn 2
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m3", SessionID: sessID, Role: "user", Content: "Turn 2 prompt", CreatedAt: time.Now().Add(-3 * time.Minute)})
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m4", SessionID: sessID, Role: "assistant", Content: "Turn 2 response", CreatedAt: time.Now().Add(-3 * time.Minute)})
	// Turn 3
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m5", SessionID: sessID, Role: "user", Content: "Turn 3 prompt", CreatedAt: time.Now().Add(-2 * time.Minute)})
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m6", SessionID: sessID, Role: "assistant", Content: "Turn 3 response", CreatedAt: time.Now().Add(-2 * time.Minute)})
	// Turn 4
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m7", SessionID: sessID, Role: "user", Content: "Turn 4 prompt", CreatedAt: time.Now().Add(-1 * time.Minute)})
	_ = store.SaveMessage(ctx, session.MessageRecord{ID: "m8", SessionID: sessID, Role: "assistant", Content: "Turn 4 response", CreatedAt: time.Now().Add(-1 * time.Minute)})

	// Record compaction covering turns 1 and 2 (up to m4)
	compSummary := "User asked about setup in Turn 1 and 2. Decisions made: Go 1.24 used."
	_ = store.SaveCompaction(ctx, session.CompactionRecord{
		ID:             "c_resume_1",
		SessionID:      sessID,
		Tier:           2,
		Summary:        compSummary,
		FirstMessageID: "m1",
		LastMessageID:  "m4",
		TokensBefore:   5000,
		TokensAfter:    1200,
		CreatedAt:      time.Now(),
	})

	// Load resumed session
	resumed, err := session.LoadResumedSession(ctx, store, sessID)
	if err != nil {
		t.Fatalf("failed to load resumed session: %v", err)
	}

	if len(resumed.Compactions) != 1 {
		t.Fatalf("expected 1 compaction record, got %d", len(resumed.Compactions))
	}

	// Verify LLMMessages view:
	// First message should contain summary
	if len(resumed.LLMMessages) == 0 {
		t.Fatal("expected LLMMessages in resumed data")
	}

	firstMsg := resumed.LLMMessages[0]
	if firstMsg.Role != llm.RoleUser {
		t.Fatalf("expected first message to be RoleUser, got %q", firstMsg.Role)
	}
	if !strings.Contains(firstMsg.Text, "[Conversation summary, generated automatically from earlier turns") {
		t.Fatalf("expected summary wrapper in first message, got: %s", firstMsg.Text)
	}
	if !strings.Contains(firstMsg.Text, compSummary) {
		t.Fatalf("expected summary text in first message, got: %s", firstMsg.Text)
	}

	// Stored database transcript is complete and unmodified (all 8 messages exist)
	dbMsgs, err := store.GetMessages(ctx, sessID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dbMsgs) != 8 {
		t.Fatalf("expected all 8 messages stored in database (never deleted), got %d", len(dbMsgs))
	}
}
