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

func TestTUIErrorCardRendering(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = newM.(tui.Model)

	errEvent := agent.Event{
		Type: agent.EventError,
		Err: &agent.ErrorPayload{
			Code:      "auth_failed",
			Message:   "anthropic: ANTHROPIC_API_KEY environment variable is required",
			Guidance:  "Set your API key in environment variables.",
			Retryable: false,
		},
	}
	newM, _ = m.Update(errEvent)
	m = newM.(tui.Model)

	view := stripANSI(m.View())
	if !strings.Contains(view, "ERROR [auth_failed]") {
		t.Fatalf("expected error code header in view, got:\n%s", view)
	}
	if !strings.Contains(view, "ANTHROPIC_API_KEY environment variable is required") {
		t.Fatalf("expected error message in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Set your API key in environment variables") {
		t.Fatalf("expected guidance text in view, got:\n%s", view)
	}
}

func TestTUIRetryScheduledEvent(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = newM.(tui.Model)

	retryEvent := agent.Event{
		Type: agent.EventRetryScheduled,
		Retry: &agent.RetryScheduledPayload{
			Attempt:     2,
			MaxAttempts: 3,
			DelayMs:     1500,
			Error:       "temporary network blip",
		},
	}
	newM, _ = m.Update(retryEvent)
	m = newM.(tui.Model)

	view := stripANSI(m.View())
	if !strings.Contains(view, "Retrying in 1500ms (attempt 2/3)") {
		t.Fatalf("expected retry status indicator in view, got:\n%s", view)
	}
}

func TestTUIWelcomeBannerAndFooter(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	m.SetProviderInfo("anthropic", "claude-sonnet-5-5")
	m.SetVersion("0.1.0-test")
	m.SetGitBranch("main")

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = newM.(tui.Model)

	view := stripANSI(m.View())

	// Welcome banner checks
	if !strings.Contains(view, "Provider: anthropic") {
		t.Fatalf("expected provider in welcome banner, got:\n%s", view)
	}
	if !strings.Contains(view, "Model:    claude-sonnet-5-5") {
		t.Fatalf("expected model in welcome banner, got:\n%s", view)
	}
	if !strings.Contains(view, "Branch:   git:main") {
		t.Fatalf("expected branch in welcome banner, got:\n%s", view)
	}
	if !strings.Contains(view, "v0.1.0-test") {
		t.Fatalf("expected version in welcome banner, got:\n%s", view)
	}

	// Footer checks
	if !strings.Contains(view, "[anthropic/claude-sonnet-5-5]") {
		t.Fatalf("expected provider/model in footer, got:\n%s", view)
	}
	if !strings.Contains(view, "git:main") {
		t.Fatalf("expected branch in footer, got:\n%s", view)
	}
	if !strings.Contains(view, "tokens:") {
		t.Fatalf("expected tokens in footer, got:\n%s", view)
	}
}

func TestTUINarrowAndShortLayout(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	m.SetProviderInfo("openai", "gpt-4o")
	m.SetVersion("0.1.0-test")
	m.SetGitBranch("feature")

	// Narrow (<80) and short (<24)
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	m = newM.(tui.Model)

	view := stripANSI(m.View())

	// Compact banner header (FORGE instead of ascii art)
	if !strings.Contains(view, "FORGE") {
		t.Fatalf("expected compact banner for narrow terminal, got:\n%s", view)
	}
	// Under height 20, help text is omitted to maximize viewport
	if strings.Contains(view, "Alt+Enter: newline") {
		t.Fatalf("expected help bar to be hidden in short terminal (<20 lines), got:\n%s", view)
	}
}

func TestTUIApprovalSessionAllow(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = newM.(tui.Model)

	// Safe approval request (edit)
	safeApproval := agent.Event{
		Type: agent.EventApprovalRequested,
		Approval: &agent.ApprovalRequestedPayload{
			ID:          "call_safe",
			Tool:        "edit_file",
			Description: "Edit safe.go",
			IsDangerous: false,
		},
	}
	newM, _ = m.Update(safeApproval)
	m = newM.(tui.Model)

	viewSafe := stripANSI(m.View())
	if !strings.Contains(viewSafe, "[a] Session allow") {
		t.Fatalf("expected '[a] Session allow' for safe approval, got:\n%s", viewSafe)
	}

	// Pressing 'a' should approve for session
	keyA := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	newM, _ = m.Update(keyA)
	m = newM.(tui.Model)

	viewAfterA := stripANSI(m.View())
	if strings.Contains(viewAfterA, "Approval Required") {
		t.Fatalf("expected approval prompt to clear after 'a', got:\n%s", viewAfterA)
	}

	// Dangerous approval request (e.g. dangerous command)
	dangApproval := agent.Event{
		Type: agent.EventApprovalRequested,
		Approval: &agent.ApprovalRequestedPayload{
			ID:          "call_dang",
			Tool:        "bash",
			Description: "rm -rf /",
			Command:     "rm -rf /",
			IsDangerous: true,
		},
	}
	newM, _ = m.Update(dangApproval)
	m = newM.(tui.Model)

	viewDang := stripANSI(m.View())
	if strings.Contains(viewDang, "[a] Session allow") {
		t.Fatalf("never offer session allow '[a]' for dangerous commands, got:\n%s", viewDang)
	}

	// Pressing 'a' on dangerous approval must NOT clear the prompt
	newM, _ = m.Update(keyA)
	m = newM.(tui.Model)

	viewAfterDangA := stripANSI(m.View())
	if !strings.Contains(viewAfterDangA, "Approval Required") {
		t.Fatalf("expected dangerous approval to remain active after 'a', got:\n%s", viewAfterDangA)
	}
}
