package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	tea "github.com/charmbracelet/bubbletea"
)

func stripANSI(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return re.ReplaceAllString(s, "")
}

func TestTUI_CompactionEventsAndFooter(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{
		Provider:      p,
		ContextWindow: 100000,
	})
	defer s.Close()

	m := New(s, DarkTheme())
	m.SetProviderInfo("anthropic", "claude-sonnet-5-5")
	m.SetBudget(10.00, 50000)

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = newM.(Model)

	// Emit context usage update: 45% used
	newM, _ = m.Update(agent.Event{
		Type: agent.EventContextUsageUpdated,
		ContextUsage: &agent.ContextUsagePayload{
			Used:    45000,
			Window:  100000,
			Percent: 45,
			Known:   true,
		},
	})
	m = newM.(Model)

	// Emit usage update: $2.50 spent
	newM, _ = m.Update(agent.Event{
		Type: agent.EventUsageUpdated,
		Usage: &agent.UsageUpdatedPayload{
			InputTokens:  10000,
			OutputTokens: 2000,
			Cost:         2.50,
		},
	})
	m = newM.(Model)

	view := StripANSI(m.View())

	// Verify footer contains ctx 45% and budget
	if !strings.Contains(view, "ctx 45%") {
		t.Fatalf("expected 'ctx 45%%' in footer, got:\n%s", view)
	}
	if !strings.Contains(view, "$2.50/$10.00") {
		t.Fatalf("expected '$2.50/$10.00' in footer, got:\n%s", view)
	}

	// Emit compaction finished event
	newM, _ = m.Update(agent.Event{
		Type: agent.EventCompactionFinished,
		CompactionDone: &agent.CompactionFinishedPayload{
			Tier:         2,
			TokensBefore: 80000,
			TokensAfter:  25000,
			OK:           true,
		},
	})
	m = newM.(Model)

	viewAfterCompact := stripANSI(m.View())
	if !strings.Contains(viewAfterCompact, "Compacted context: 80k → 25k tokens") {
		t.Fatalf("expected 'Compacted context: 80k → 25k tokens' in view, got:\n%s", viewAfterCompact)
	}
}

func TestTUI_Commands_CostAndBudget(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{
		Provider:      p,
		ContextWindow: 100000,
		MaxCostBudget: 5.0,
	})
	defer s.Close()

	m := New(s, DarkTheme())
	m.SetProviderInfo("anthropic", "claude-sonnet-5-5")
	m.SetBudget(5.0, 0)

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = newM.(Model)

	// Send /cost command
	m.textarea.SetValue("/cost")
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(Model)

	view := StripANSI(m.View())
	if !strings.Contains(view, "Session Usage & Cost:") {
		t.Fatalf("expected '/cost' output in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Cost Budget:") {
		t.Fatalf("expected budget info in /cost output, got:\n%s", view)
	}

	// Send /budget command to raise limit to $15.00
	m.textarea.SetValue("/budget $15.00")
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(Model)

	viewBudget := stripANSI(m.View())
	if !strings.Contains(viewBudget, "Cost budget set to $15.00") {
		t.Fatalf("expected budget updated confirmation in view, got:\n%s", viewBudget)
	}
}

func TestTUI_ModelSwitch_ApproachingWindowWarning(t *testing.T) {
	p := mock.New()
	s := agent.NewLocal(agent.Config{
		Provider:      p,
		ContextWindow: 200000,
	})
	defer s.Close()

	m := New(s, DarkTheme())
	m.SetProviderInfo("anthropic", "claude-sonnet-5-5")

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = newM.(Model)

	// Simulate current usage at 110,000 tokens (which exceeds 80% of gpt-4o's 128k window)
	newM, _ = m.Update(agent.Event{
		Type: agent.EventContextUsageUpdated,
		ContextUsage: &agent.ContextUsagePayload{
			Used:    110000,
			Window:  200000,
			Percent: 55,
			Known:   true,
		},
	})
	m = newM.(Model)

	// Try switching to openai model gpt-4o
	m.textarea.SetValue("/model gpt-4o")
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(Model)

	view := StripANSI(m.View())
	if !strings.Contains(view, "exceeds 80% of gpt-4o's window") {
		t.Fatalf("expected 80%% threshold warning when switching to gpt-4o, got:\n%s", view)
	}
	if !strings.Contains(view, "/compact") {
		t.Fatalf("expected suggestion to run /compact, got:\n%s", view)
	}

	// Now switch with --force
	m.textarea.SetValue("/model gpt-4o --force")
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(Model)

	viewForced := stripANSI(m.View())
	if !strings.Contains(viewForced, "Switched active model to gpt-4o") {
		t.Fatalf("expected forced switch to succeed, got:\n%s", viewForced)
	}
}
