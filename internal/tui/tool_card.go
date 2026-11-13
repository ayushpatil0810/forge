package tui

import (
	"encoding/json"
	"fmt"
	"os"
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
	case "edit_file":
		if p, ok := inputMap["path"].(string); ok {
			return fmt.Sprintf("Edit %s", p)
		}
	case "write_file":
		if p, ok := inputMap["path"].(string); ok {
			return fmt.Sprintf("Write %s", p)
		}
	case "bash":
		cmd, _ := inputMap["command"].(string)
		desc, _ := inputMap["description"].(string)
		if desc != "" {
			return fmt.Sprintf("$ %s (%s)", cmd, desc)
		}
		return fmt.Sprintf("$ %s", cmd)
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

	dumb := os.Getenv("TERM") == "dumb" || os.Getenv("NO_COLOR") != ""

	switch tc.Status {
	case agent.ToolRunning:
		if dumb {
			icon = styles.ToolCardRunning.Render("*")
		} else {
			icon = styles.ToolCardRunning.Render("⟳")
		}
		statusSuffix = styles.SystemInfo.Render("(running...)")
	case agent.ToolOK:
		if dumb {
			icon = styles.ToolCardSuccess.Render("+")
		} else {
			icon = styles.ToolCardSuccess.Render("✔")
		}
		statusSuffix = styles.SystemInfo.Render(fmt.Sprintf("(%dms)", tc.DurationMs))
	case agent.ToolError:
		if dumb {
			icon = styles.ToolCardError.Render("x")
		} else {
			icon = styles.ToolCardError.Render("✖")
		}
		statusSuffix = styles.ToolCardError.Render(fmt.Sprintf("(failed, %dms)", tc.DurationMs))
	case agent.ToolDenied:
		if dumb {
			icon = styles.ToolCardError.Render("-")
		} else {
			icon = styles.ToolCardError.Render("⊘")
		}
		statusSuffix = styles.ToolCardError.Render("(denied)")
	case agent.ToolCancelled:
		if dumb {
			icon = styles.SystemInfo.Render("-")
		} else {
			icon = styles.SystemInfo.Render("⊘")
		}
		statusSuffix = styles.SystemInfo.Render("(cancelled)")
	default:
		if dumb {
			icon = "*"
		} else {
			icon = "•"
		}
	}

	header := fmt.Sprintf("%s %s %s %s", icon, styles.AssistantText.Bold(true).Render(tc.Name), tc.Summary, statusSuffix)

	// For read-only tools, collapsed shows only the header.
	// For bash, collapsed shows header plus last 10 lines of output; expanded shows all lines.
	if !expanded && tc.Name != "bash" {
		return header
	}

	// Expanded or bash view: display card with output snippet
	var content strings.Builder
	content.WriteString(header)
	content.WriteString("\n")

	out := strings.TrimSpace(tc.Output)
	if out == "" {
		if tc.Status == agent.ToolRunning {
			content.WriteString(styles.ToolCardOutput.Render("  [Running operation...]"))
		} else {
			content.WriteString(styles.ToolCardOutput.Render("  (no output)"))
		}
	} else {
		lines := strings.Split(out, "\n")
		maxLines := 10
		if expanded {
			maxLines = len(lines)
		}
		// For bash when not expanded, show the last 10 lines
		startIdx := 0
		if !expanded && tc.Name == "bash" && len(lines) > maxLines {
			startIdx = len(lines) - maxLines
			content.WriteString("  ")
			content.WriteString(styles.SystemInfo.Render(fmt.Sprintf("... [%d earlier lines hidden; Ctrl+O to expand]", startIdx)))
			content.WriteString("\n")
		}

		for i := startIdx; i < len(lines) && (expanded || i < startIdx+maxLines); i++ {
			line := lines[i]
			if width > 10 && len(line) > width-8 {
				line = line[:width-8] + "..."
			}
			content.WriteString("  ")
			content.WriteString(styles.ToolCardOutput.Render(line))
			content.WriteString("\n")
		}
		if !expanded && tc.Name != "bash" && len(lines) > maxLines {
			content.WriteString("  ")
			content.WriteString(styles.SystemInfo.Render(fmt.Sprintf("... [%d more lines; Ctrl+O to expand]", len(lines)-maxLines)))
		}
	}

	cardBox := styles.BorderBox.Width(width - 4).Render(strings.TrimRight(content.String(), "\n"))
	return cardBox
}
