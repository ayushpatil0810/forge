package tui_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/credentials"
	"github.com/ayushpatil0810/forge/internal/llm"
	_ "github.com/ayushpatil0810/forge/internal/llm/anthropic"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/session"
	"github.com/ayushpatil0810/forge/internal/tools"
	"github.com/ayushpatil0810/forge/internal/tui"
	"github.com/zalando/go-keyring"
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

func TestTabDuringApprovalDoesNotChangeMode(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()
	before := s.Mode()

	m := tui.New(s, tui.DarkTheme())
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = newM.(tui.Model)
	newM, _ = m.Update(agent.Event{
		Type: agent.EventApprovalRequested,
		Approval: &agent.ApprovalRequestedPayload{
			ID: "call", Tool: "bash", Command: "echo hi",
		},
	})
	m = newM.(tui.Model)
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = newM.(tui.Model)
	if s.Mode() != before {
		t.Fatalf("tab changed permission mode from %s to %s while an approval card was open", before, s.Mode())
	}
	if !strings.Contains(stripANSI(m.View()), "Approval Required") {
		t.Fatal("approval card closed when tab was pressed")
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

func TestTUISecurityNoticeOnFirstScreen(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	m.SetProviderInfo("anthropic", "claude-sonnet-5-5")
	notice := `SEC-6: Forbidden key "permission_mode" in project config ignored (a cloned repository must not grant a permission mode)`
	m.SetSecurityNotices([]string{notice})

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = newM.(tui.Model)

	view := stripANSI(m.View())
	if !strings.Contains(view, "permission_mode") {
		t.Fatalf("first screen missing project-config warning:\n%s", view)
	}
	if !strings.Contains(view, "SEC-6") {
		t.Fatalf("first screen missing SEC-6 notice:\n%s", view)
	}
}

func TestSlashSettingsAndModelsOpenSetup(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = newM.(tui.Model)

	m = typeLine(t, m, "/settings")
	view := stripANSI(m.View())
	if !strings.Contains(view, "Welcome to Forge") {
		t.Fatalf("expected /settings to open setup, got:\n%s", view)
	}
	if strings.Contains(view, "ANTHROPIC_API_KEY") {
		t.Fatalf("setup screen must not ask for an environment variable:\n%s", view)
	}

	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newM.(tui.Model)

	m = typeLine(t, m, "/models")
	view = stripANSI(m.View())
	if !strings.Contains(view, "Switch model") {
		t.Fatalf("expected /models to open the switcher, got:\n%s", view)
	}
}

func TestMessageBeforeSetupReopensWizard(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	m.SetSetupRequired(true)
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = newM.(tui.Model)

	m = typeLine(t, m, "hello")
	view := stripANSI(m.View())
	if !strings.Contains(view, "Welcome to Forge") {
		t.Fatalf("expected a message before setup to open the wizard, got:\n%s", view)
	}
}

func TestWizardSavesProviderWithoutEnvVar(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(func() { _ = keyring.Delete(credentials.ServiceName, "anthropic") })

	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	m := tui.New(s, tui.DarkTheme())
	m.SetSetupRequired(true)
	m.SetWizard(tui.NewWizardState())
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = newM.(tui.Model)

	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(tui.Model)
	for _, r := range "sk-test-key-1234" {
		newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = newM.(tui.Model)
	}
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(tui.Model)
	modelView := stripANSI(m.View())
	if strings.Contains(modelView, "Recommended") || strings.Contains(modelView, "claude-sonnet-5-5") {
		t.Fatalf("model step should ask for an ID, not offer a list:\n%s", modelView)
	}
	for _, r := range "claude-opus-4-7" {
		newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = newM.(tui.Model)
	}
	for i := 0; i < 2; i++ {
		newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = newM.(tui.Model)
	}

	view := stripANSI(m.View())
	if strings.Contains(view, "Welcome to Forge") {
		t.Fatalf("wizard should close after the model is chosen:\n%s", view)
	}
	if strings.Contains(view, "sk-test-key-1234") {
		t.Fatal("API key leaked into the chat view")
	}
	if !strings.Contains(view, "claude-opus-4-7") {
		t.Fatalf("chat screen should show the model ID the user typed:\n%s", view)
	}

	data, err := os.ReadFile(filepath.Join(home, ".forge", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"provider": "anthropic"`) || !strings.Contains(text, `"model": "claude-opus-4-7"`) {
		t.Fatalf("user config was not saved:\n%s", text)
	}
	if strings.Contains(text, "sk-test-key") {
		t.Fatal("API key was written to the config file")
	}
}

func TestProviderSwitchValidatesBeforeChangingRoute(t *testing.T) {
	llm.RegisterProvider("rejected-route-fixture", func(llm.ProviderConfig) (llm.Provider, error) {
		return nil, errors.New("provider unavailable")
	})
	var target llm.ProviderConfig
	llm.RegisterProvider("new-route-fixture", func(cfg llm.ProviderConfig) (llm.Provider, error) {
		target = cfg
		return mock.New(), nil
	})
	s := agent.NewLocal(agent.Config{Provider: mock.New()})
	defer s.Close()
	m := tui.New(s, tui.DarkTheme())
	m.SetProviderInfo("mock", "original-model")
	m.SetBaseURL("https://original.example.test/v1")
	m.SetContextWindowOverride(2048)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = typeLine(t, ready.(tui.Model), "/provider rejected-route-fixture")
	view := stripANSI(m.View())
	if !strings.Contains(view, "provider unavailable") || !strings.Contains(view, "mock/original-model") {
		t.Fatalf("failed switch did not preserve the active route:\n%s", view)
	}
	m = typeLine(t, m, "/provider new-route-fixture")
	if target.Provider != "new-route-fixture" || target.BaseURL != "" || s.ContextUsage().Window != 2048 {
		t.Fatal("provider switch did not bind the new route and context window")
	}
}

func TestResumeBindsConversationAndWritesToActiveStore(t *testing.T) {
	root, foreign := t.TempDir(), t.TempDir()
	store, err := session.NewStore(filepath.Join(t.TempDir(), "custom.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	ctx := t.Context()
	const providerName = "resume-route-fixture"
	provider := mock.New(mock.Turn{mock.Text("restored reply"), mock.Usage(10, 2)})
	llm.RegisterProvider(providerName, func(llm.ProviderConfig) (llm.Provider, error) { return provider, nil })
	for i, fixture := range []struct{ id, workspace, provider string }{
		{"saved", root, providerName}, {"foreign", foreign, providerName}, {"other-provider", root, "openai"},
	} {
		now := time.Now().Add(-time.Duration(i) * time.Hour)
		if err := store.CreateSession(ctx, session.SessionRecord{
			ID: fixture.id, Workspace: session.CanonicalWorkspace(fixture.workspace), Provider: fixture.provider,
			Model: "saved-model", Title: fixture.id, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveMessage(ctx, session.MessageRecord{ID: "saved-message", SessionID: "saved", Role: "user", Content: "saved prompt"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUsage(ctx, session.UsageRecord{SessionID: "saved", Model: "saved-model", InputTokens: 20, OutputTokens: 4}); err != nil {
		t.Fatal(err)
	}
	registry := tools.DefaultTools(root)
	registry.Tracker().MarkRead("example.txt")
	s := agent.NewLocal(agent.Config{ID: "previous", Provider: mock.New(), Tools: registry, Policy: permission.Policy{Mode: permission.ModePlan}})
	defer s.Close()
	m := tui.New(s, tui.DarkTheme())
	m.SetProviderInfo(providerName, "original-model")
	m.SetWorkspaceRoot(root)
	m.SetSessionStore(store, false)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = typeLine(t, ready.(tui.Model), "/resume foreign")
	m = typeLine(t, m, "/resume other-provider")
	if s.ID() != "previous" || len(s.History()) != 0 {
		t.Fatal("a rejected resume replaced the current conversation")
	}
	m = typeLine(t, m, "/resume")
	chosen, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = chosen.(tui.Model)
	if s.ID() != "saved" || s.Mode() != permission.ModePlan || registry.Tracker().HasBeenRead("example.txt") {
		t.Fatal("resume did not change the session ID, keep plan mode, and clear read approvals")
	}
	m = typeLine(t, m, "continue")
	timeout := time.After(10 * time.Second)
	for done := false; !done; {
		select {
		case ev := <-s.Events():
			updated, _ := m.Update(ev)
			m = updated.(tui.Model)
			done = ev.Terminal()
		case <-timeout:
			t.Fatal("resumed turn did not finish")
		}
	}
	if len(provider.Requests) != 1 || provider.Requests[0].Messages[0].Text != "saved prompt" {
		t.Fatal("the follow-up request did not use the saved history")
	}
	data, err := session.LoadResumedSession(ctx, store, "saved")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Messages) != 3 || data.Messages[1].Content != "continue" || data.TotalUsage.InputTokens != 30 || data.TotalUsage.OutputTokens != 6 {
		t.Fatalf("follow-up message or usage missing from the resumed session: messages=%d input=%d output=%d", len(data.Messages), data.TotalUsage.InputTokens, data.TotalUsage.OutputTokens)
	}
}

func typeLine(t *testing.T, m tui.Model, line string) tui.Model {
	t.Helper()
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(line)})
	m = newM.(tui.Model)
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return newM.(tui.Model)
}
