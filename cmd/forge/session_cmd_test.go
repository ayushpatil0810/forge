package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/checkpoint"
	"github.com/ayushpatil0810/forge/internal/session"
)

func TestSessionsCmd_ListShowDeletePrune(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("USERPROFILE", tempDir)
	t.Setenv("HOME", tempDir)

	forgeDir := filepath.Join(tempDir, ".forge")
	_ = os.MkdirAll(forgeDir, 0755)
	dbPath := filepath.Join(forgeDir, "sessions.db")

	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	ws1 := filepath.Join(tempDir, "ws1")
	ws2 := filepath.Join(tempDir, "ws2")
	_ = os.MkdirAll(ws1, 0755)
	_ = os.MkdirAll(ws2, 0755)

	s1 := session.SessionRecord{
		ID:        "s_test_1",
		Workspace: session.CanonicalWorkspace(ws1),
		Provider:  "mock",
		Model:     "mock-v1",
		Title:     "Refactor auth logic",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-1 * time.Hour),
	}
	s2 := session.SessionRecord{
		ID:        "s_test_2",
		Workspace: session.CanonicalWorkspace(ws2),
		Provider:  "openai",
		Model:     "gpt-4o",
		Title:     "Fix database deadlock",
		CreatedAt: time.Now().Add(-100 * 24 * time.Hour),
		UpdatedAt: time.Now().Add(-95 * 24 * time.Hour),
	}

	if err := store.CreateSession(ctx, s1); err != nil {
		t.Fatalf("CreateSession s1 failed: %v", err)
	}
	if err := store.CreateSession(ctx, s2); err != nil {
		t.Fatalf("CreateSession s2 failed: %v", err)
	}

	_ = store.SaveMessage(ctx, session.MessageRecord{
		ID:        "m1",
		SessionID: s1.ID,
		Role:      "user",
		Content:   "Refactor auth",
		CreatedAt: time.Now(),
	})
	_ = store.SaveUsage(ctx, session.UsageRecord{
		SessionID:    s1.ID,
		Model:        s1.Model,
		InputTokens:  150,
		OutputTokens: 250,
		Cost:         0.0125,
		CreatedAt:    time.Now(),
	})
	_ = store.Close()

	// 1. Test List (Workspace scoped)
	rootFlag = ws1
	var outBuf bytes.Buffer
	cmd := newSessionsCmd()
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions list failed: %v", err)
	}
	outStr := outBuf.String()
	if !strings.Contains(outStr, "s_test_1") {
		t.Errorf("expected sessions list to contain s_test_1, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "s_test_2") {
		t.Errorf("expected sessions list to NOT contain s_test_2 from ws2, got:\n%s", outStr)
	}

	// 2. Test List --all
	outBuf.Reset()
	cmd = newSessionsCmd()
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"list", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions list --all failed: %v", err)
	}
	allStr := outBuf.String()
	if !strings.Contains(allStr, "s_test_1") || !strings.Contains(allStr, "s_test_2") {
		t.Errorf("expected sessions list --all to contain both sessions, got:\n%s", allStr)
	}

	// 3. Test Show
	outBuf.Reset()
	cmd = newSessionsCmd()
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"show", "s_test_1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions show failed: %v", err)
	}
	showStr := outBuf.String()
	if !strings.Contains(showStr, "s_test_1") || !strings.Contains(showStr, "Refactor auth logic") || !strings.Contains(showStr, "0.0125") {
		t.Errorf("expected show output with id, title and cost, got:\n%s", showStr)
	}

	// 4. Test Prune (aborts without --yes on empty stdin)
	outBuf.Reset()
	cmd = newSessionsCmd()
	cmd.SetOut(&outBuf)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"prune", "--older-than", "30"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions prune failed: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Prune aborted") {
		t.Errorf("expected prune to abort without --yes, got:\n%s", outBuf.String())
	}

	// 4b. Test Prune with --yes
	outBuf.Reset()
	cmd = newSessionsCmd()
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"prune", "--older-than", "30", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions prune with --yes failed: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Pruned 1 sessions") {
		t.Errorf("expected prune with --yes to remove 1 session, got:\n%s", outBuf.String())
	}

	// 5. Test Delete (aborts without --yes on empty stdin)
	outBuf.Reset()
	cmd = newSessionsCmd()
	cmd.SetOut(&outBuf)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"delete", "s_test_1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions delete failed: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Deletion aborted") {
		t.Errorf("expected delete to abort without --yes, got:\n%s", outBuf.String())
	}

	// 5b. Test Delete with --yes
	outBuf.Reset()
	cmd = newSessionsCmd()
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"delete", "s_test_1", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions delete with --yes failed: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Session s_test_1 deleted") {
		t.Errorf("expected delete confirmation, got:\n%s", outBuf.String())
	}

	// 6. Test Doctor
	outBuf.Reset()
	cmd = newSessionsCmd()
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions doctor failed: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Database integrity: ok") {
		t.Errorf("expected doctor to report ok, got:\n%s", outBuf.String())
	}
}

func TestUndo_UncompactsCompactions(t *testing.T) {
	resetCLIFlags()
	t.Cleanup(resetCLIFlags)

	ws := t.TempDir()
	store, err := session.NewStore("")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	sessID := "s_undo_compact_test"
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID:        sessID,
		Workspace: session.CanonicalWorkspace(ws),
		Provider:  "mock",
		Model:     "mock-model",
		Title:     "Undo compact test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	chkMgr, err := checkpoint.New(ws)
	if err != nil {
		t.Fatalf("checkpoint.New failed: %v", err)
	}
	_, err = chkMgr.Create(ctx, "Checkpoint 1")
	if err != nil {
		t.Fatalf("Create cp1 failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	compTime := time.Now()
	_ = store.SaveCompaction(ctx, session.CompactionRecord{
		ID:             "cmp_after_cp1",
		SessionID:      sessID,
		Tier:           2,
		Summary:        "Compaction created after cp1",
		FirstMessageID: "m1",
		LastMessageID:  "m2",
		CreatedAt:      compTime,
	})

	time.Sleep(10 * time.Millisecond)

	_, err = chkMgr.Create(ctx, "Checkpoint 2")
	if err != nil {
		t.Fatalf("Create cp2 failed: %v", err)
	}

	compactionsBefore, _ := store.GetCompactions(ctx, sessID)
	if len(compactionsBefore) != 1 {
		t.Fatalf("expected 1 compaction before undo, got %d", len(compactionsBefore))
	}

	undoCmd := newUndoCmd()
	rootFlag = ws
	var out bytes.Buffer
	undoCmd.SetOut(&out)
	if err := undoCmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("undoCmd.ExecuteContext failed: %v", err)
	}

	compactionsAfter, _ := store.GetCompactions(ctx, sessID)
	if len(compactionsAfter) != 0 {
		t.Fatalf("expected 0 compactions after undo reverting before compaction, got %d: %+v", len(compactionsAfter), compactionsAfter)
	}
}
