package tui

import (
	"fmt"
	"strings"

	"github.com/ayushpatil0810/forge/internal/agent"
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
	title := styles.ToolCardError.Bold(true).Render("[!] Approval Required")
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
			diffBox := renderColorizedDiff(app.Diff, boxWidth-4, styles)
			sb.WriteString(diffBox)
			sb.WriteString("\n")
		} else {
			sb.WriteString(styles.SystemInfo.Render("\n  [Diff available; press 'e' to preview]\n"))
		}
	}

	var actionItems []string
	actionItems = append(actionItems, styles.ToolCardSuccess.Bold(true).Render("[y] Approve"))
	if !app.IsDangerous && !app.IsProtected {
		actionItems = append(actionItems, styles.ToolCardSuccess.Render("[a] Session allow"))
		actionItems = append(actionItems, styles.ToolCardSuccess.Render("[A] Always allow"))
	}
	actionItems = append(actionItems, styles.ToolCardError.Bold(true).Render("[n] Reject"))
	actionItems = append(actionItems, styles.ToolCardError.Render("[f] Feedback"))
	if app.Diff != "" {
		actionItems = append(actionItems, styles.SystemInfo.Render("[e] Diff"))
	}
	actionItems = append(actionItems, styles.StatusBar.Render(fmt.Sprintf("[Tab] Mode: %s", mode)))

	sb.WriteString("\n" + strings.Join(actionItems, "  "))

	boxStyle := styles.BorderBox.
		Padding(0, 1).
		Width(boxWidth)

	return boxStyle.Render(sb.String())
}

// RenderAlwaysAllowPreviewCard renders the preview card for saving a rule to user policy.
func RenderAlwaysAllowPreviewCard(rule string, width int, styles Styles) string {
	boxWidth := width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}
	var sb strings.Builder
	sb.WriteString(styles.ToolCardSuccess.Bold(true).Render("[A] Save Rule to User Policy?\n\n"))
	fmt.Fprintf(&sb, "Rule to persist: %s\n\n", styles.AssistantText.Bold(true).Render(rule))
	sb.WriteString(styles.SystemInfo.Render("This rule will be saved to your user policy (~/.forge/policy.json)\nand will automatically allow matching actions in future sessions.\n\n"))
	sb.WriteString(styles.ToolCardSuccess.Bold(true).Render("[y] Confirm and Save") + "  " + styles.ToolCardError.Render("[n/Esc] Cancel"))

	boxStyle := styles.BorderBox.Padding(0, 1).Width(boxWidth)
	return boxStyle.Render(sb.String())
}

// RenderFeedbackCard renders the prompt card for entering rejection feedback.
func RenderFeedbackCard(currentText string, width int, styles Styles) string {
	boxWidth := width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}
	var sb strings.Builder
	sb.WriteString(styles.ToolCardError.Bold(true).Render("[f] Deny With Feedback\n\n"))
	sb.WriteString("Feedback to send to model (max 500 chars):\n")
	fmt.Fprintf(&sb, "> %s\n\n", currentText)
	sb.WriteString(styles.ToolCardSuccess.Bold(true).Render("[Enter] Submit Denial") + "  " + styles.ToolCardError.Render("[Esc] Cancel"))

	boxStyle := styles.BorderBox.Padding(0, 1).Width(boxWidth)
	return boxStyle.Render(sb.String())
}

func renderColorizedDiff(diffText string, maxWidth int, styles Styles) string {
	lines := strings.Split(diffText, "\n")
	var b strings.Builder

	addStyle := styles.ToolCardSuccess
	delStyle := styles.ToolCardError
	hunkStyle := styles.UserPrompt
	dimStyle := styles.SystemInfo

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
