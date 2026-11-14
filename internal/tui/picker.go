package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ayushpatil0810/forge/internal/session"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionPicker manages the interactive session resumption picker.
type SessionPicker struct {
	Sessions []session.SessionSummary
	Cursor   int
	Width    int
	NowFunc  func() time.Time
}

func (p *SessionPicker) now() time.Time {
	if p.NowFunc != nil {
		return p.NowFunc()
	}
	return time.Now()
}

func NewSessionPicker(sessions []session.SessionSummary) *SessionPicker {
	return &SessionPicker{
		Sessions: sessions,
		Cursor:   0,
		Width:    80,
	}
}

func (p *SessionPicker) Update(msg tea.Msg) (selectedID string, done bool, cmd tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", false, nil
	}

	switch keyMsg.String() {
	case "esc", "q":
		return "", true, nil
	case "up", "k":
		if p.Cursor > 0 {
			p.Cursor--
		}
	case "down", "j":
		if p.Cursor < len(p.Sessions)-1 {
			p.Cursor++
		}
	case "enter":
		if len(p.Sessions) > 0 && p.Cursor < len(p.Sessions) {
			return p.Sessions[p.Cursor].ID, true, nil
		}
		return "", true, nil
	}
	return "", false, nil
}

func (p *SessionPicker) Render(width int, styles Styles) string {
	boxWidth := width - 6
	if boxWidth < 50 {
		boxWidth = 50
	}
	if boxWidth > 110 {
		boxWidth = 110
	}

	var sb strings.Builder
	title := styles.ActivityLabel.Bold(true).Render(" Resume Session — Select a Previous Conversation ")
	sb.WriteString(title + "\n\n")

	if len(p.Sessions) == 0 {
		sb.WriteString(styles.SystemInfo.Render("No previous sessions recorded for this workspace.") + "\n\n")
		sb.WriteString(styles.UserPrompt.Render("Press Esc to return to terminal."))
		return styles.BorderBox.Width(boxWidth).Padding(1, 2).Render(sb.String())
	}

	sb.WriteString("Use ↑/↓ to navigate, Enter to resume, Esc to cancel:\n\n")

	for i, s := range p.Sessions {
		prefix := "  "
		if i == p.Cursor {
			prefix = "❯ "
		}

		timeStr := s.UpdatedAt.Format("Jan 02 15:04")
		if p.now().Sub(s.UpdatedAt) < 24*time.Hour {
			timeStr = s.UpdatedAt.Format("15:04:05")
		}

		titleText := s.Title
		if titleText == "" {
			titleText = "Untitled session"
		}
		if len(titleText) > 40 {
			titleText = titleText[:37] + "..."
		}

		line := fmt.Sprintf("%s[%s] %-38s (%s | %s | %d msgs | $%.4f)",
			prefix,
			s.ID[:min(len(s.ID), 14)],
			titleText,
			timeStr,
			s.Model,
			s.MessageCount,
			s.TotalCost,
		)

		if i == p.Cursor {
			sb.WriteString(styles.UserPrompt.Render(line) + "\n")
		} else {
			sb.WriteString(styles.AssistantText.Render(line) + "\n")
		}
	}

	sb.WriteString("\n" + styles.SystemInfo.Render("Tip: sessions are automatically restored with message history, tool cards, and undo state."))

	content := sb.String()
	return styles.BorderBox.Width(boxWidth).Padding(1, 2).Render(content)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RenderPickerInPlace places the rendered session picker into the center of the terminal.
func RenderPickerInPlace(p *SessionPicker, width, height int, styles Styles) string {
	rendered := p.Render(width, styles)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, rendered)
}
