package tui

import (
	"fmt"
	"strings"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/charmbracelet/lipgloss"
)

// RenderApprovalCard renders an interactive approval prompt card for human review.
func RenderApprovalCard(app *agent.ApprovalRequestedPayload, mode string, showDiff bool, width int, styles Styles) string {
	if app == nil {
		return ""
	}

	boxWidth := width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	var sb strings.Builder
	title := styles.ToolCardError.Bold(true).Render("⚠️  Approval Required")
	sb.WriteString(title)
	sb.WriteString("\n")

	if app.Command != "" {
		fmt.Fprintf(&sb, "%s: %s\n", styles.AssistantText.Bold(true).Render(app.Tool), styles.ToolCardOutput.Render("$ "+app.Command))
		if app.Cwd != "" {
			fmt.Fprintf(&sb, "  %s %s\n", styles.SystemInfo.Render("cwd:"), app.Cwd)
		}
		if app.TimeoutSeconds > 0 {
			fmt.Fprintf(&sb, "  %s %ds\n", styles.SystemInfo.Render("timeout:"), app.TimeoutSeconds)
		}
		if app.Description != "" {
			fmt.Fprintf(&sb, "  %s %s\n", styles.SystemInfo.Render("purpose:"), app.Description)
		}
	} else {
		toolInfo := fmt.Sprintf("%s: %s", styles.AssistantText.Bold(true).Render(app.Tool), app.Description)
		sb.WriteString(toolInfo)
		sb.WriteString("\n")
	}

	if app.Diff != "" {
		if showDiff {
			sb.WriteString("\n")
			diffBox := renderColorizedDiff(app.Diff, boxWidth-4)
			sb.WriteString(diffBox)
			sb.WriteString("\n")
		} else {
			sb.WriteString(styles.SystemInfo.Render("\n  [Diff available; press 'e' to preview]\n"))
		}
	}

	actions := fmt.Sprintf(
		"%s  %s  %s  %s",
		styles.ToolCardSuccess.Bold(true).Render("[y] Approve"),
		styles.ToolCardError.Bold(true).Render("[n] Reject"),
		styles.SystemInfo.Render("[e] Diff"),
		styles.StatusBar.Render(fmt.Sprintf("[Tab] Mode: %s", mode)),
	)
	sb.WriteString("\n" + actions)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#E0AF68")). // Warning amber
		Padding(0, 1).
		Width(boxWidth)

	return boxStyle.Render(sb.String())
}

func renderColorizedDiff(diffText string, maxWidth int) string {
	lines := strings.Split(diffText, "\n")
	var b strings.Builder

	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A")) // green
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F7768E")) // red
	hunkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")) // cyan
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89"))

	maxLines := 25
	for i := 0; i < len(lines) && i < maxLines; i++ {
		line := lines[i]
		if maxWidth > 10 && len(line) > maxWidth {
			line = line[:maxWidth-3] + "..."
		}

		if strings.HasPrefix(line, "+") {
			b.WriteString(addStyle.Render(line))
		} else if strings.HasPrefix(line, "-") {
			b.WriteString(delStyle.Render(line))
		} else if strings.HasPrefix(line, "@@") {
			b.WriteString(hunkStyle.Render(line))
		} else {
			b.WriteString(dimStyle.Render(line))
		}
		b.WriteString("\n")
	}

	if len(lines) > maxLines {
		b.WriteString(dimStyle.Render(fmt.Sprintf("... [%d more diff lines]", len(lines)-maxLines)))
	}

	return strings.TrimRight(b.String(), "\n")
}
