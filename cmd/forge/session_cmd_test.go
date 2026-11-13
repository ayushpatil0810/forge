package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
