package tui_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/tui"
)

type mockApprovalSession struct {
	agent.Session
	lastApprovalID string
	lastDecision   agent.Decision
	lastFeedback   string
}

func (m *mockApprovalSession) Respond(id string, d agent.Decision, feedback ...string) error {
	m.lastApprovalID = id
	m.lastDecision = d
	if len(feedback) > 0 {
		m.lastFeedback = feedback[0]
	}
	return nil
}

func TestTeatest_AlwaysAllowPreviewAndConfirm(t *testing.T) {
	p := mock.New()
	realSess := agent.NewLocal(agent.Config{Provider: p})
	defer realSess.Close()

	mockS := &mockApprovalSession{Session: realSess}
	m := tui.New(mockS, tui.DarkTheme())

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = newM.(tui.Model)

	// Inject ApprovalRequested event
	appEv := agent.Event{
		Type: agent.EventApprovalRequested,
		Approval: &agent.ApprovalRequestedPayload{
			ID:      "call_123",
			Tool:    "bash",
			Command: "go test ./...",
		},
	}
	newM, _ = m.Update(appEv)
	m = newM.(tui.Model)

	view1 := tui.StripANSI(m.View())
	if !strings.Contains(view1, "[A] Always allow") {
		t.Fatalf("expected '[A] Always allow' in view, got:\n%s", view1)
	}

	// Press 'A' to trigger preview card
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = newM.(tui.Model)

	viewPreview := tui.StripANSI(m.View())
	if !strings.Contains(viewPreview, "Save Rule to User Policy?") {
		t.Fatalf("expected preview card, got:\n%s", viewPreview)
	}
	if !strings.Contains(viewPreview, "bash(go test ./...)") {
		t.Fatalf("expected exact rule 'bash(go test ./...)' in preview, got:\n%s", viewPreview)
	}

	// Press 'y' to confirm
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = newM.(tui.Model)

	if mockS.lastApprovalID != "call_123" {
		t.Fatalf("expected approval ID 'call_123', got %q", mockS.lastApprovalID)
	}
	if mockS.lastDecision != agent.DecisionAlwaysAllow {
		t.Fatalf("expected DecisionAlwaysAllow, got %v", mockS.lastDecision)
	}

	viewAfter := tui.StripANSI(m.View())
	if strings.Contains(viewAfter, "Save Rule to User Policy?") {
		t.Fatalf("expected preview card to clear after confirmation")
	}
}

func TestTeatest_DenyWithFeedback(t *testing.T) {
	p := mock.New()
	realSess := agent.NewLocal(agent.Config{Provider: p})
	defer realSess.Close()

	mockS := &mockApprovalSession{Session: realSess}
	m := tui.New(mockS, tui.DarkTheme())

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = newM.(tui.Model)

	appEv := agent.Event{
		Type: agent.EventApprovalRequested,
		Approval: &agent.ApprovalRequestedPayload{
			ID:          "call_456",
			Tool:        "edit_file",
			Path:        "main.go",
			Description: "edit main.go",
		},
	}
	newM, _ = m.Update(appEv)
	m = newM.(tui.Model)

	view1 := tui.StripANSI(m.View())
	if !strings.Contains(view1, "[f] Feedback") {
		t.Fatalf("expected '[f] Feedback' in view, got:\n%s", view1)
	}

	// Press 'f' to enter feedback mode
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = newM.(tui.Model)

	viewFeedback := tui.StripANSI(m.View())
	if !strings.Contains(viewFeedback, "Deny With Feedback") {
		t.Fatalf("expected feedback input card, got:\n%s", viewFeedback)
	}

	// Type feedback: "do not touch main"
	for _, r := range "do not touch main" {
		newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = newM.(tui.Model)
	}

	// Press Enter to submit
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(tui.Model)

	if mockS.lastApprovalID != "call_456" {
		t.Fatalf("expected approval ID 'call_456', got %q", mockS.lastApprovalID)
	}
	if mockS.lastDecision != agent.DecisionDenyWithFeedback {
		t.Fatalf("expected DecisionDenyWithFeedback, got %v", mockS.lastDecision)
	}
	if mockS.lastFeedback != "do not touch main" {
		t.Fatalf("expected feedback 'do not touch main', got %q", mockS.lastFeedback)
	}

	viewAfter := tui.StripANSI(m.View())
	if strings.Contains(viewAfter, "Deny With Feedback") {
		t.Fatalf("expected feedback card to clear after Enter")
	}
}

func TestApprovalCardsGoldenSnapshots_Widths(t *testing.T) {
	widths := []int{60, 80, 120}

	for _, w := range widths {
		t.Run(fmt.Sprintf("approval_card_%d", w), func(t *testing.T) {
			styles := tui.NewStyles(tui.DarkTheme())
			app := &agent.ApprovalRequestedPayload{
				ID:             "test_id",
				Tool:           "bash",
				Command:        "go test -v ./...",
				Cwd:            "/workspace",
				TimeoutSeconds: 30,
				Description:    "Run package unit tests",
			}
			card := tui.RenderApprovalCard(app, "ask", false, w, styles)
			clean := tui.StripANSI(card)

			goldenPath := filepath.Join("testdata", "golden", fmt.Sprintf("approval_card_%d.golden", w))
			compareOrUpdateGolden(t, goldenPath, clean)
		})

		t.Run(fmt.Sprintf("preview_card_%d", w), func(t *testing.T) {
			styles := tui.NewStyles(tui.DarkTheme())
			card := tui.RenderAlwaysAllowPreviewCard("bash(go test -v ./...)", w, styles)
			clean := tui.StripANSI(card)

			goldenPath := filepath.Join("testdata", "golden", fmt.Sprintf("preview_card_%d.golden", w))
			compareOrUpdateGolden(t, goldenPath, clean)
		})

		t.Run(fmt.Sprintf("feedback_card_%d", w), func(t *testing.T) {
			styles := tui.NewStyles(tui.DarkTheme())
			card := tui.RenderFeedbackCard("Do not run this test suite in production", w, styles)
			clean := tui.StripANSI(card)

			goldenPath := filepath.Join("testdata", "golden", fmt.Sprintf("feedback_card_%d.golden", w))
			compareOrUpdateGolden(t, goldenPath, clean)
		})
	}
}
