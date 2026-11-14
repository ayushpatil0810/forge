package agent_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/session"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestScripted50TurnConversation_CrossingTiers(t *testing.T) {
	tempDir := t.TempDir()

	// Prepare a file for tools to read (large tool output: ~2000 chars per read)
	sampleFilePath := filepath.Join(tempDir, "data.txt")
	fileContent := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. Data block.\n", 30)
	if err := os.WriteFile(sampleFilePath, []byte(fileContent), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tempDir, "session.db")
	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	sessionID := session.GenerateSessionID()
	if err := store.CreateSession(ctx, session.SessionRecord{
		ID:        sessionID,
		Workspace: tempDir,
		Provider:  "mock",
		Model:     "mock-model",
		Title:     "50-turn Compaction Verification",
	}); err != nil {
		t.Fatal(err)
	}

	var callCounter atomic.Int64
	var summarizerCallCount atomic.Int64
	var tier1Seen atomic.Bool
	var tier2Seen atomic.Bool

	p := &mock.Provider{
		StrictValidator: true, // Strict validation on EVERY request sent to model
	}

	p.Handler = func(req llm.Request) (mock.Turn, error) {
		// Summarizer call detection
		if strings.Contains(req.System, "Forge's context summarizer") {
			summarizerCallCount.Add(1)
			return mock.Turn{
				mock.Usage(200, 80),
				mock.Text("Summary of prior conversation: User executed commands and inspected data files."),
			}, nil
		}

		// Compute growing input tokens from request messages (Rule 4: scripted usage numbers)
		reqChars := 0
		for _, m := range req.Messages {
			reqChars += len(m.Text)
			for _, tc := range m.ToolCalls {
				reqChars += len(tc.Name) + len(tc.Input)
			}
		}
		inTok := int(float64(reqChars) * 0.85)
		if inTok < 100 {
			inTok = 100
		}

		lastMsg := req.Messages[len(req.Messages)-1]
		if lastMsg.Role == llm.RoleUser {
			id := fmt.Sprintf("call_%d", callCounter.Add(1))
			return mock.Turn{
				mock.Usage(inTok, 40),
				mock.ToolUse(id, "read_file", map[string]string{"path": sampleFilePath}),
			}, nil
		}
		if lastMsg.Role == llm.RoleTool {
			return mock.Turn{
				mock.Usage(inTok, 25),
				mock.Text("Tool output received and processed successfully."),
			}, nil
		}

		return mock.Turn{
			mock.Usage(inTok, 20),
			mock.Text("Acknowledged."),
		}, nil
	}

	toolReg := tools.DefaultTools(tempDir)

	s := agent.NewLocal(agent.Config{
		ID:                 sessionID,
		Provider:           p,
		Tools:              toolReg,
		ContextWindow:      20000, // 20,000 token window
		Tier1Threshold:     0.60,  // Tier 1 at 60% (12k tokens)
		Tier2Threshold:     0.80,  // Tier 2 at 80% (16k tokens)
		EmergencyThreshold: 0.95,
		KeepRecentTurns:    6, // Verbatim preserve last 6 turns
		SessionStore:       store,
	})
	defer s.Close()

	// Execute 50 turns
	for turn := 1; turn <= 50; turn++ {
		prompt := fmt.Sprintf("Turn %d: Analyze project data block", turn)
		if err := s.Send(ctx, prompt); err != nil {
			t.Fatalf("turn %d Send failed: %v", turn, err)
		}

		for ev := range s.Events() {
			if ev.Type == agent.EventCompactionStarted {
				if ev.CompactionStart.Tier == 1 {
					tier1Seen.Store(true)
				}
				if ev.CompactionStart.Tier == 2 {
					tier2Seen.Store(true)
				}
			}
			if ev.Terminal() {
				if ev.Type == agent.EventError {
					t.Fatalf("turn %d ended with error: %v", turn, ev.Err.Message)
				}
				break
			}
		}
	}

	// 1. Verify Tier 1 and Tier 2 compactions both fired during the 50 turns
	if !tier1Seen.Load() {
		t.Errorf("expected Tier 1 compaction to trigger across 50 turns")
	}
	if !tier2Seen.Load() || summarizerCallCount.Load() == 0 {
		t.Errorf("expected Tier 2 summarization compaction to trigger, summarizer calls = %d", summarizerCallCount.Load())
	}

	// 2. Verify Amendment E: raw in-memory history keeps EVERY message uncompacted
	fullHistory := s.History()
	// 50 turns, each has: 1 User message + 1 Assistant tool call + 1 Tool result + 1 Assistant response = 4 messages per turn = 200 messages
	if len(fullHistory) < 200 {
		t.Fatalf("expected at least 200 messages in uncompacted transcript history, got %d", len(fullHistory))
	}

	// 3. Verify ModelView is compacted (fewer messages than full history)
	modelView := s.ModelView()
	if len(modelView) >= len(fullHistory) {
		t.Fatalf("expected compacted model view to have fewer messages than full history (%d), got %d", len(fullHistory), len(modelView))
	}

	// 4. Verify the last 6 turns are kept verbatim in ModelView
	// Find the 6th user message from the end in fullHistory
	var fullUserIndices []int
	for i, m := range fullHistory {
		if m.Role == llm.RoleUser {
			fullUserIndices = append(fullUserIndices, i)
		}
	}
	if len(fullUserIndices) < 6 {
		t.Fatalf("expected at least 6 user messages, got %d", len(fullUserIndices))
	}
	cutoffFullIdx := fullUserIndices[len(fullUserIndices)-6]
	last6TurnsFull := fullHistory[cutoffFullIdx:]

	// Find the 6th user message from the end in modelView
	var viewUserIndices []int
	for i, m := range modelView {
		if m.Role == llm.RoleUser {
			viewUserIndices = append(viewUserIndices, i)
		}
	}
	cutoffViewIdx := viewUserIndices[len(viewUserIndices)-6]
	last6TurnsView := modelView[cutoffViewIdx:]

	if len(last6TurnsFull) != len(last6TurnsView) {
		t.Fatalf("expected last 6 turns length to match: full=%d, view=%d", len(last6TurnsFull), len(last6TurnsView))
	}
	for i := range last6TurnsFull {
		if last6TurnsFull[i].Role != last6TurnsView[i].Role || last6TurnsFull[i].Text != last6TurnsView[i].Text {
			t.Fatalf("mismatch in verbatim turn %d: full=%+v, view=%+v", i, last6TurnsFull[i], last6TurnsView[i])
		}
	}

	// 5. Verify compaction records exist in SQLite store
	compactions, err := store.GetCompactions(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to query compactions from store: %v", err)
	}
	if len(compactions) == 0 {
		t.Fatalf("expected compaction records persisted in SQLite database")
	}

	// 6. Resume verification: LoadResumedSession reconstructs the exact compacted view
	resumed, err := session.LoadResumedSession(ctx, store, sessionID)
	if err != nil {
		t.Fatalf("failed to load resumed session: %v", err)
	}
	if len(resumed.Compactions) != len(compactions) {
		t.Fatalf("expected %d compactions in resumed session, got %d", len(compactions), len(resumed.Compactions))
	}
}
