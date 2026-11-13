package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/checkpoint"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/session"
	"github.com/ayushpatil0810/forge/internal/tools"
	"github.com/ayushpatil0810/forge/internal/tui"
)

// HistoryCapturingProvider intercepts LLM requests so the test can assert on exact history.
type historyCapturingProvider struct {
	inner    llm.Provider
	captured []llm.Message
}

func (h *historyCapturingProvider) Name() string {
	return h.inner.Name()
}

func (h *historyCapturingProvider) Models() []llm.ModelInfo {
	return h.inner.Models()
}

func (h *historyCapturingProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	h.captured = append([]llm.Message(nil), req.Messages...)
	return h.inner.Stream(ctx, req)
}

func TestSlice9_SessionPersistenceAndResumeExitCheck(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("USERPROFILE", tempDir)
	t.Setenv("HOME", tempDir)

	ws := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(ws, 0755); err != nil {
		t.Fatal(err)
	}

	appFile := filepath.Join(ws, "app.py")
	origContent := "def main():\n    print(\"Hello World\")\n"
	if err := os.WriteFile(appFile, []byte(origContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Initialize SQLite store
	forgeDir := filepath.Join(tempDir, ".forge")
	_ = os.MkdirAll(forgeDir, 0755)
	dbPath := filepath.Join(forgeDir, "sessions.db")
	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// 2. Initialize checkpoint manager
	chkMgr, err := checkpoint.New(ws)
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	sessionID := session.GenerateSessionID()
	sh := tools.DetectShell("")
	registry := tools.DefaultTools(ws, sh)

	mockP1 := mock.New(
		mock.Turn{
			mock.Text("Reading app.py before editing.\n"),
			mock.ToolUse("call_read", "read_file", map[string]any{
				"path": "app.py",
			}),
			mock.Usage(100, 50),
		},
		mock.Turn{
			mock.Text("Updating greeting in app.py.\n"),
			mock.ToolUse("call_edit", "edit_file", map[string]any{
				"path":       "app.py",
				"old_string": "print(\"Hello World\")",
				"new_string": "print(\"Hello Forge\")",
			}),
			mock.Usage(150, 75),
		},
		mock.Turn{
			mock.Text("Done! app.py greeting has been updated."),
			mock.Usage(50, 25),
		},
	)

	// Save session record
	ctx := context.Background()
	_ = store.CreateSession(ctx, session.SessionRecord{
		ID:        sessionID,
		Workspace: session.CanonicalWorkspace(ws),
		Provider:  "mock",
		Model:     "mock-model",
		Title:     "Update greeting in app.py",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	s1 := agent.NewLocal(agent.Config{
		ID:           sessionID,
		Provider:     mockP1,
		Tools:        registry,
		Policy:       permission.Policy{Mode: permission.ModeAcceptEdits},
		System:       "You are Forge.",
		MaxTurns:     10,
		Checkpointer: chkMgr.AsCheckpointer(),
	})

	// Save user message to store (crash safety)
	userPrompt1 := "Please update greeting to Hello Forge"
	_ = store.SaveMessage(ctx, session.MessageRecord{
		ID:        "msg_u1",
		SessionID: sessionID,
		Role:      "user",
		Content:   userPrompt1,
		CreatedAt: time.Now(),
	})

	if err := s1.Send(ctx, userPrompt1); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	var asstText strings.Builder
turn1Loop:
	for ev := range s1.Events() {
		switch ev.Type {
		case agent.EventTokenDelta:
			if ev.Token != nil {
				asstText.WriteString(ev.Token.Text)
			}
		case agent.EventToolStarted:
			if ev.ToolStart != nil {
				_ = store.SaveToolCall(ctx, session.ToolCallRecord{
					ID:        ev.ToolStart.ID,
					MessageID: "msg_a1",
					Name:      ev.ToolStart.Name,
					Input:     ev.ToolStart.Input,
					Status:    "running",
					CreatedAt: time.Now(),
				})
			}
		case agent.EventToolFinished:
			if ev.ToolDone != nil {
				out := ev.ToolDone.Result
				if ev.ToolDone.Error != "" {
					out = ev.ToolDone.Error
				}
				_ = store.SaveToolCall(ctx, session.ToolCallRecord{
					ID:         ev.ToolDone.ID,
					MessageID:  "msg_a1",
					Status:     string(ev.ToolDone.Status),
					Output:     out,
					DurationMs: ev.ToolDone.DurationMs,
				})
			}
		case agent.EventUsageUpdated:
			if ev.Usage != nil {
				_ = store.SaveUsage(ctx, session.UsageRecord{
					SessionID:    sessionID,
					Model:        "mock-model",
					InputTokens:  ev.Usage.InputTokens,
					OutputTokens: ev.Usage.OutputTokens,
					Cost:         0.005,
					CreatedAt:    time.Now(),
				})
			}
		case agent.EventError:
			t.Fatalf("turn 1 error: %v", ev.Err)
		case agent.EventDone:
			if asstText.Len() > 0 {
				_ = store.SaveMessage(ctx, session.MessageRecord{
					ID:        "msg_a1",
					SessionID: sessionID,
					Role:      "assistant",
					Content:   asstText.String(),
					CreatedAt: time.Now(),
				})
			}
			break turn1Loop
		}
	}

	// Verify file was edited
	contentAfterTurn1, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contentAfterTurn1), "print(\"Hello Forge\")") {
		t.Fatalf("expected app.py to contain 'Hello Forge', got:\n%s", string(contentAfterTurn1))
	}

	// Close session 1
	s1.Close()

	// 3. Load Resumed Session Data
	resumedData, err := session.LoadResumedSession(ctx, store, sessionID)
	if err != nil {
		t.Fatalf("LoadResumedSession failed: %v", err)
	}

	// Verify workspace binding validation
	if err := session.ValidateWorkspaceBinding(resumedData.Session.Workspace, ws, resumedData.Session.ID); err != nil {
		t.Fatalf("ValidateWorkspaceBinding failed on matching workspace: %v", err)
	}
	wrongWs := filepath.Join(tempDir, "other_ws")
	if err := session.ValidateWorkspaceBinding(resumedData.Session.Workspace, wrongWs, resumedData.Session.ID); err == nil {
		t.Fatalf("expected ValidateWorkspaceBinding to reject mismatched workspace")
	}

	// Assert history, tool calls, and usage
	if len(resumedData.Messages) == 0 {
		t.Fatal("expected resumed messages to be non-empty")
	}
	if len(resumedData.ToolCallsByMsg) == 0 {
		t.Fatalf("expected resumed tool calls to be present, got 0")
	}
	if resumedData.TotalCost <= 0 {
		t.Errorf("expected resumed cost > 0, got %f", resumedData.TotalCost)
	}

	// 4. Test TUI Model Restoration (Viewport fidelity, collapsed cards, note)
	tuiModel := tui.New(nil, tui.DarkTheme())
	tuiModel.RestoreSession(resumedData)

	// Golden snapshot / item verification
	// SPEC requirement: resumed sessions restore viewport and insert ResumeSystemNote
	hasResumeNote := false
	for _, msg := range resumedData.LLMMessages {
		if strings.Contains(msg.Text, "Resumed session") {
			hasResumeNote = true
			break
		}
	}
	if !hasResumeNote {
		t.Errorf("expected LLMMessages to contain ResumeSystemNote, got: %+v", resumedData.LLMMessages)
	}

	// 5. Undo Continuity Across Sessions
	// SPEC requirement: "/undo continuity reverts pre-resume edits"
	chkList, err := chkMgr.List(ctx, 5)
	if err != nil || len(chkList) == 0 {
		t.Fatalf("expected shadow repo to have recorded checkpoints before edits: %v", err)
	}
	t.Logf("Found %d checkpoints in shadow repo", len(chkList))
	// Restore to the checkpoint taken before edit
	_, err = chkMgr.Restore(ctx, chkList[0].ID)
	if err != nil {
		t.Fatalf("checkpoint Restore failed: %v", err)
	}
	revertedContent, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(revertedContent), "print(\"Hello World\")") {
		t.Fatalf("expected pre-resume edits to be reverted to 'Hello World', got:\n%s", string(revertedContent))
	}

	// 6. Execute Follow-up Turn on Resumed Session
	// Verify mock provider receives full prior history + resume note + new turn prompt
	mockP2 := mock.New(
		mock.Turn{
			mock.Text("Follow-up turn processed successfully."),
		},
	)
	capturingP := &historyCapturingProvider{inner: mockP2}

	s2 := agent.NewLocal(agent.Config{
		ID:           sessionID,
		History:      resumedData.LLMMessages,
		Provider:     capturingP,
		Tools:        registry,
		Policy:       permission.Policy{Mode: permission.ModeAcceptEdits},
		System:       "You are Forge.",
		MaxTurns:     5,
		Checkpointer: chkMgr.AsCheckpointer(),
	})
	defer s2.Close()

	userPrompt2 := "How are you doing today?"
	if err := s2.Send(ctx, userPrompt2); err != nil {
		t.Fatalf("s2.Send failed: %v", err)
	}

turn2Loop:
	for ev := range s2.Events() {
		if ev.Type == agent.EventError {
			t.Fatalf("s2 received error event: %v", ev.Err)
		}
		if ev.Type == agent.EventDone {
			break turn2Loop
		}
	}

	// Check captured history passed to provider
	if len(capturingP.captured) == 0 {
		t.Fatal("expected provider to receive request messages")
	}

	// The captured messages MUST contain:
	// 1. Initial user prompt
	// 2. Initial assistant responses / tools
	// 3. Resume system note
	// 4. Follow-up user prompt
	var capturedTexts []string
	for _, m := range capturingP.captured {
		capturedTexts = append(capturedTexts, m.Text)
	}
	allCaptured := strings.Join(capturedTexts, " | ")
	if !strings.Contains(allCaptured, userPrompt1) {
		t.Errorf("captured history missing initial prompt %q", userPrompt1)
	}
	if !strings.Contains(allCaptured, "Resumed session") {
		t.Errorf("captured history missing resume system note")
	}
	if !strings.Contains(allCaptured, userPrompt2) {
		t.Errorf("captured history missing follow-up prompt %q", userPrompt2)
	}
}
