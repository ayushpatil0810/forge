package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ayushpatil0810/forge/internal/agent"
)

type ToolCard struct {
	ID         string
	Name       string
	Input      string
	Summary    string
	Status     agent.ToolStatus
	DurationMs int64
	Output     string
}

func NewToolCard(id, name, input string) *ToolCard {
	return &ToolCard{
		ID:      id,
		Name:    name,
		Input:   input,
		Summary: summarizeToolInput(name, input),
		Status:  agent.ToolRunning,
	}
}

func summarizeToolInput(name, rawInput string) string {
	var inputMap map[string]any
	if err := json.Unmarshal([]byte(rawInput), &inputMap); err != nil {
		if len(rawInput) > 40 {
			return rawInput[:40] + "..."
		}
		return rawInput
	}

	switch name {
	case "read_file":
		if p, ok := inputMap["path"].(string); ok {
			return fmt.Sprintf("Read %s", p)
		}
	case "list_dir":
		p := "."
		if pathVal, ok := inputMap["path"].(string); ok && pathVal != "" {
			p = pathVal
		}
		return fmt.Sprintf("List %s", p)
	case "glob":
		if pattern, ok := inputMap["pattern"].(string); ok {
			return fmt.Sprintf("Find %s", pattern)
		}
	case "grep":
		q, _ := inputMap["query"].(string)
		p, _ := inputMap["path"].(string)
		if p != "" && p != "." {
			return fmt.Sprintf("Search %q in %s", q, p)
		}
		return fmt.Sprintf("Search %q", q)
	}

	if len(rawInput) > 50 {
		return rawInput[:50] + "..."
	}
	return rawInput
}

func (tc *ToolCard) Render(width int, expanded bool, styles Styles) string {
	var icon string
	var statusSuffix string

	switch tc.Status {
	case agent.ToolRunning:
		icon = styles.ToolCardRunning.Render("⟳")
		statusSuffix = styles.SystemInfo.Render("(running...)")
	case agent.ToolOK:
		icon = styles.ToolCardSuccess.Render("✔")
		statusSuffix = styles.SystemInfo.Render(fmt.Sprintf("(%dms)", tc.DurationMs))
	case agent.ToolError:
		icon = styles.ToolCardError.Render("✖")
		statusSuffix = styles.ToolCardError.Render(fmt.Sprintf("(failed, %dms)", tc.DurationMs))
	case agent.ToolDenied:
		icon = styles.ToolCardError.Render("⊘")
		statusSuffix = styles.ToolCardError.Render("(denied)")
	case agent.ToolCancelled:
		icon = styles.SystemInfo.Render("⊘")
		statusSuffix = styles.SystemInfo.Render("(cancelled)")
	default:
		icon = "•"
	}

	header := fmt.Sprintf("%s %s %s %s", icon, styles.AssistantText.Bold(true).Render(tc.Name), tc.Summary, statusSuffix)

	if !expanded {
		return header
	}

	// Expanded view: display card with output snippet
	var content strings.Builder
	content.WriteString(header)
	content.WriteString("\n")

	out := strings.TrimSpace(tc.Output)
	if out == "" {
		if tc.Status == agent.ToolRunning {
			content.WriteString(styles.ToolCardOutput.Render("  [Running tool operation...]"))
		} else {
			content.WriteString(styles.ToolCardOutput.Render("  (no output)"))
		}
	} else {
		lines := strings.Split(out, "\n")
		maxLines := 10
		for i := 0; i < len(lines) && i < maxLines; i++ {
			line := lines[i]
			if width > 10 && len(line) > width-8 {
				line = line[:width-8] + "..."
			}
			content.WriteString("  ")
			content.WriteString(styles.ToolCardOutput.Render(line))
			content.WriteString("\n")
		}
		if len(lines) > maxLines {
			content.WriteString("  ")
			content.WriteString(styles.SystemInfo.Render(fmt.Sprintf("... [%d more lines]", len(lines)-maxLines)))
		}
	}

	cardBox := styles.BorderBox.Width(width - 4).Render(strings.TrimRight(content.String(), "\n"))
	return cardBox
}
