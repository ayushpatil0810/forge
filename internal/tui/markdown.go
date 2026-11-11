package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

// MarkdownRenderer wraps glamour with width awareness and unclosed code fence recovery.
type MarkdownRenderer struct {
	style string
	width int
}

func NewMarkdownRenderer(style string, width int) *MarkdownRenderer {
	if width <= 0 {
		width = 80
	}
	return &MarkdownRenderer{
		style: style,
		width: width,
	}
}

func (m *MarkdownRenderer) SetWidth(w int) {
	if w > 0 {
		m.width = w
	}
}

// Render renders markdown text to terminal escape sequences.
// Incomplete code fences are temporarily closed so partial blocks render properly.
func (m *MarkdownRenderer) Render(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}

	sanitized := closeIncompleteFences(raw)
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(m.style),
		glamour.WithWordWrap(m.width),
	)
	if err != nil {
		return raw, err
	}

	out, err := r.Render(sanitized)
	if err != nil {
		return raw, err
	}
	return strings.TrimSpace(out), nil
}

func closeIncompleteFences(md string) string {
	count := 0
	lines := strings.Split(md, "\n")
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "```") {
			count++
		}
	}
	if count%2 != 0 {
		return md + "\n```"
	}
	return md
}

