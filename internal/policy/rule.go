package policy

import (
	"fmt"
	"strings"
)

type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
	ActionAsk   Action = "ask"
)

type ToolType string

const (
	ToolRead ToolType = "read"
	ToolEdit ToolType = "edit"
	ToolBash ToolType = "bash"
)

// Scope represents where a rule originated or should be saved.
type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

// Rule represents a parsed policy rule.
type Rule struct {
	Action  Action   `json:"action"`
	Tool    ToolType `json:"tool"`
	Pattern string   `json:"pattern"`
	Scope   Scope    `json:"scope"`
	Raw     string   `json:"raw"`
}

// ParseRule parses a rule string of the form "tool(pattern)" with a given action and scope.
// It returns an error with the rule index, file path, and a fix hint if invalid.
func ParseRule(action Action, raw string, scope Scope, filePath string, index int) (Rule, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Rule{}, fmt.Errorf("invalid policy in %s (rule #%d): empty rule string; hint: use 'tool(pattern)' e.g. 'bash(go test:*)' or 'edit(docs/**)'", filePath, index)
	}

	openParen := strings.IndexByte(trimmed, '(')
	if openParen == -1 || !strings.HasSuffix(trimmed, ")") {
		return Rule{}, fmt.Errorf("invalid policy in %s (rule #%d: %q): missing parentheses; hint: format must be 'tool(pattern)' like 'edit(docs/**)'", filePath, index, raw)
	}

	toolStr := strings.TrimSpace(trimmed[:openParen])
	pattern := strings.TrimSpace(trimmed[openParen+1 : len(trimmed)-1])

	var tool ToolType
	switch strings.ToLower(toolStr) {
	case "read":
		tool = ToolRead
	case "edit":
		tool = ToolEdit
	case "bash":
		tool = ToolBash
	default:
		return Rule{}, fmt.Errorf("invalid policy in %s (rule #%d: %q): unknown tool %q; hint: valid tools are 'read', 'edit', or 'bash'", filePath, index, raw, toolStr)
	}

	if pattern == "" {
		return Rule{}, fmt.Errorf("invalid policy in %s (rule #%d: %q): empty pattern for tool %q; hint: specify a glob or command prefix, or '*' for all", filePath, index, raw, tool)
	}

	// For read and edit, path patterns must not contain path traversal ".."
	if tool == ToolRead || tool == ToolEdit {
		if strings.Contains(pattern, "..") {
			return Rule{}, fmt.Errorf("invalid policy in %s (rule #%d: %q): path traversal '..' is prohibited in path patterns; hint: use workspace-relative paths e.g. 'docs/**'", filePath, index, raw)
		}
	}

	return Rule{
		Action:  action,
		Tool:    tool,
		Pattern: pattern,
		Scope:   scope,
		Raw:     fmt.Sprintf("%s(%s)", tool, pattern),
	}, nil
}
