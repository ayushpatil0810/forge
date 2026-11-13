package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestExplainRepoStructureEndToEnd(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Locate testdata/fixture
	fixtureDir := filepath.Join(wd, "..", "..", "testdata", "fixture")
	if _, err := os.Stat(fixtureDir); err != nil {
		// Try relative to workspace root if running from elsewhere
		fixtureDir = filepath.Join(wd, "testdata", "fixture")
		if _, err := os.Stat(fixtureDir); err != nil {
			t.Fatalf("could not find fixtureDir: %v", err)
		}
	}

	// Capture initial modification times of all files in fixtureDir to ensure no writes occur
	initialModTimes := make(map[string]time.Time)
	err = filepath.Walk(fixtureDir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr == nil {
			initialModTimes[p] = info.ModTime()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	systemPrompt := agent.BuildSystemPrompt(fixtureDir)
	if !strings.Contains(systemPrompt, "Project-provided guidance (AGENTS.md)") {
		t.Fatalf("expected AGENTS.md instructions in system prompt")
	}

	toolSet := tools.DefaultTools(fixtureDir)

	// Scripted mock provider:
	// Turn 1: Call list_dir
	// Turn 2: Call grep for main
	// Turn 3: Call read_file for cmd/server/main.go
	// Turn 4: Final explanation answer
	p := mock.New(
		mock.Turn{
			mock.ToolUse("tc_1", "list_dir", map[string]any{"path": "."}),
		},
		mock.Turn{
			mock.ToolUse("tc_2", "grep", map[string]any{"query": "func main"}),
		},
		mock.Turn{
			mock.ToolUse("tc_3", "read_file", map[string]any{"path": "cmd/server/main.go"}),
		},
		mock.Turn{
			mock.Text("This repository is structured as a Go service with an entrypoint in cmd/server and modular packages in pkg/."),
		},
	)

	sess := agent.NewLocal(agent.Config{
		Provider: p,
		Tools:    toolSet,
		System:   systemPrompt,
	})
	defer sess.Close()

	if err := sess.Send(context.Background(), "Explain how this repo is structured"); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	var (
		seenToolStarts   []string
		seenToolFinishes []string
		streamedText     strings.Builder
		doneReason       string
	)

	timeout := time.After(5 * time.Second)
	for doneReason == "" {
		select {
		case ev, ok := <-sess.Events():
			if !ok {
				t.Fatal("events channel closed before Done")
			}
			switch ev.Type {
			case agent.EventToolStarted:
				seenToolStarts = append(seenToolStarts, ev.ToolStart.Name)
			case agent.EventToolFinished:
				seenToolFinishes = append(seenToolFinishes, ev.ToolDone.Name)
				if ev.ToolDone.Status != agent.ToolOK {
					t.Fatalf("tool %s failed: %s", ev.ToolDone.Name, ev.ToolDone.Error)
				}
			case agent.EventTokenDelta:
				streamedText.WriteString(ev.Token.Text)
			case agent.EventDone:
				doneReason = ev.Done.Reason
			case agent.EventError:
				t.Fatalf("unexpected error event: %s", ev.Err.Message)
			}
		case <-timeout:
			t.Fatal("timed out waiting for agent loop completion")
		}
	}

	if doneReason != "completed" {
		t.Fatalf("expected done reason 'completed', got %q", doneReason)
	}

	// Verify all 3 tools ran
	if len(seenToolStarts) != 3 || len(seenToolFinishes) != 3 {
		t.Fatalf("expected 3 tool calls, started=%v, finished=%v", seenToolStarts, seenToolFinishes)
	}
	expectedTools := []string{"list_dir", "grep", "read_file"}
	for i, expected := range expectedTools {
		if seenToolStarts[i] != expected || seenToolFinishes[i] != expected {
			t.Errorf("tool %d mismatch: got start %q, finish %q, want %q", i, seenToolStarts[i], seenToolFinishes[i], expected)
		}
	}

	// Verify streamed answer
	if !strings.Contains(streamedText.String(), "cmd/server") {
		t.Errorf("expected streamed text to mention cmd/server, got: %s", streamedText.String())
	}

	// Verify no files were modified in fixtureDir
	for p, origTime := range initialModTimes {
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("file %s was removed or became inaccessible: %v", p, err)
			continue
		}
		if !info.ModTime().Equal(origTime) {
			t.Errorf("file %s was modified during read-only session! Orig: %v, Now: %v", p, origTime, info.ModTime())
		}
	}
}
