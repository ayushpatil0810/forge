package tui_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/tui"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func TestTUIResizeWithoutPanic(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())

	// Send resize events of various dimensions
	sizes := []tea.WindowSizeMsg{
		{Width: 80, Height: 24},
		{Width: 120, Height: 40},
		{Width: 40, Height: 10},
	}

	for _, size := range sizes {
		newM, _ := m.Update(size)
		m = newM.(tui.Model)
		view := m.View()
		if len(view) == 0 {
			t.Fatal("empty view after resize")
		}
	}
}

func TestTUIStreamAndEscInterrupt(t *testing.T) {
	p := mock.New(
		mock.Turn{
			mock.Text("Paragraph 1.\n\n"),
			mock.Text("```go\nfunc Hello() {}\n```\n"),
		},
	)
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())

	// Initialize window
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = newM.(tui.Model)

	// Send a message
	if err := s.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	// Drain events through Update
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			newM, _ = m.Update(ev)
			m = newM.(tui.Model)
			if ev.Type == agent.EventDone {
				cleanView := stripANSI(m.View())
				if !strings.Contains(cleanView, "Paragraph 1") {
					t.Fatalf("expected view to contain streamed text, got:\n%s", cleanView)
				}
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for stream events")
		}
	}
}

func TestTUIEscInterruptWhileStreaming(t *testing.T) {
	p := mock.New(
		mock.Turn{
			mock.Text("Partial text"),
		},
	)
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = newM.(tui.Model)

	_ = s.Send(context.Background(), "start")

	// Receive one token
	ev := <-s.Events()
	newM, _ = m.Update(ev)
	m = newM.(tui.Model)

	// User presses Esc
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newM.(tui.Model)

	cleanView := stripANSI(m.View())
	if !strings.Contains(cleanView, "Interrupted") {
		t.Fatalf("expected view to reflect interrupted state, got:\n%s", cleanView)
	}
}

func TestMarkdownRendererIncompleteFence(t *testing.T) {
	renderer := tui.NewMarkdownRenderer("dark", 80)
	raw := "Here is code:\n```go\nfmt.Println(\"test\")"
	rendered, err := renderer.Render(raw)
	if err != nil {
		t.Fatalf("rendering failed on unclosed fence: %v", err)
	}
	if !strings.Contains(rendered, "test") {
		t.Fatalf("expected output to contain code content, got:\n%s", rendered)
	}
}

func TestTUIApprovalFlow(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = newM.(tui.Model)

	// Simulate incoming ApprovalRequested event
	appEvent := agent.Event{
		Type: agent.EventApprovalRequested,
		Approval: &agent.ApprovalRequestedPayload{
			ID:          "app-1",
			Tool:        "edit_file",
			Description: "Edit main.go",
			Diff:        "--- a\n+++ b\n@@ -1 +1 @@\n-bug\n+fix",
		},
	}
	newM, _ = m.Update(appEvent)
	m = newM.(tui.Model)

	cleanView := stripANSI(m.View())
	if !strings.Contains(cleanView, "Approval Required") {
		t.Fatalf("expected approval prompt in view, got:\n%s", cleanView)
	}
	if !strings.Contains(cleanView, "Edit main.go") {
		t.Fatalf("expected tool description in view, got:\n%s", cleanView)
	}

	// Press 'y' to approve
	keyY := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	newM, _ = m.Update(keyY)
	m = newM.(tui.Model)

	afterY := stripANSI(m.View())
	if strings.Contains(afterY, "Approval Required") {
		t.Fatalf("expected approval prompt to clear after 'y', got:\n%s", afterY)
	}
}
