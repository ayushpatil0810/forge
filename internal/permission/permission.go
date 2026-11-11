// Package permission decides whether a tool call runs, needs approval, or is blocked.
package permission

import "github.com/ayushpatil0810/forge/internal/tools"

type Verdict int

const (
	Allow Verdict = iota
	Ask
	Deny
)

type Mode string

const (
	ModeDefault     Mode = "ask"          // reads free; writes and exec ask
	ModeAcceptEdits Mode = "accept-edits" // reads and writes free; exec asks
	ModePlan        Mode = "plan"         // read-only: writes and exec denied
	ModeYOLO        Mode = "yolo"         // no prompts, auto-approve
)

// Policy checks tool executions against permission rules and modes (SPEC §3.3).
type Policy struct {
	Mode Mode
}

func (p Policy) Check(toolName string, req tools.ApprovalRequest) Verdict {
	if !req.Required() {
		return Allow
	}
	switch p.Mode {
	case ModePlan:
		return Deny
	case ModeYOLO:
		return Allow
	case ModeAcceptEdits:
		// If it's an edit with diff, allow without prompting; exec asks.
		if req.Diff != "" {
			return Allow
		}
		return Ask
	default:
		return Ask
	}
}
