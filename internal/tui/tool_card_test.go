package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/tui"
)

func TestToolCardRendering(t *testing.T) {
	styles := tui.NewStyles(tui.DarkTheme())

	card := tui.NewToolCard("call_1", "read_file", `{"path":"main.go"}`)
	card.Status = agent.ToolOK
	card.DurationMs = 45
	card.Output = "package main\nfunc main() {}\n"

	// Collapsed rendering
	collapsed := stripANSI(card.Render(80, false, styles))
	if !strings.Contains(collapsed, "read_file") || !strings.Contains(collapsed, "Read main.go") {
		t.Fatalf("unexpected collapsed card: %s", collapsed)
	}
	if !strings.Contains(collapsed, "45ms") {
		t.Fatalf("expected duration in collapsed card: %s", collapsed)
	}
	if strings.Contains(collapsed, "package main") {
		t.Fatalf("collapsed card should not include file content: %s", collapsed)
	}

	// Expanded rendering
	expanded := stripANSI(card.Render(80, true, styles))
	if !strings.Contains(expanded, "read_file") || !strings.Contains(expanded, "Read main.go") {
		t.Fatalf("unexpected expanded card: %s", expanded)
	}
	if !strings.Contains(expanded, "package main") {
		t.Fatalf("expanded card should include file content: %s", expanded)
	}
}

func TestTUIToolCardLifecycleAndExpandToggle(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = newM.(tui.Model)

	// 1. Tool started
	newM, _ = m.Update(agent.Event{
		Type: agent.EventToolStarted,
		ToolStart: &agent.ToolStartedPayload{
			ID:    "call_123",
			Name:  "list_dir",
			Input: `{"path":"internal"}`,
		},
	})
	m = newM.(tui.Model)

	view1 := stripANSI(m.View())
	if !strings.Contains(view1, "list_dir") || !strings.Contains(view1, "List internal") {
		t.Fatalf("expected running tool card in view: %s", view1)
	}

	// 2. Tool output delta
	newM, _ = m.Update(agent.Event{
		Type: agent.EventToolOutputDelta,
		ToolOut: &agent.ToolOutputDeltaPayload{
			ID:    "call_123",
			Chunk: "agent/\nconfig/\n",
		},
	})
	m = newM.(tui.Model)

	// 3. Tool finished
	newM, _ = m.Update(agent.Event{
		Type: agent.EventToolFinished,
		ToolDone: &agent.ToolFinishedPayload{
			ID:         "call_123",
			Name:       "list_dir",
			Result:     "agent/\nconfig/\nllm/\n",
			DurationMs: 38,
			Status:     agent.ToolOK,
		},
	})
	m = newM.(tui.Model)

	view2 := stripANSI(m.View())
	if !strings.Contains(view2, "38ms") {
		t.Fatalf("expected duration in finished card view: %s", view2)
	}
	// By default, collapsed, so body lines are not rendered
	if strings.Contains(view2, "config/") {
		t.Fatalf("expected collapsed view not to show full output lines: %s", view2)
	}

	// 4. Press Ctrl+O to expand tools
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = newM.(tui.Model)

	viewExpanded := stripANSI(m.View())
	if !strings.Contains(viewExpanded, "config/") {
		t.Fatalf("expected expanded view to show output lines: %s", viewExpanded)
	}

	// 5. Press Ctrl+O again to collapse tools
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = newM.(tui.Model)

	viewCollapsed := stripANSI(m.View())
	if strings.Contains(viewCollapsed, "config/") {
		t.Fatalf("expected collapsed view again: %s", viewCollapsed)
	}
}

