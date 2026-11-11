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
	// Dangerous commands (e.g. rm -rf /, pipe-to-shell, fork bombs) always ask regardless of mode
	if req.IsDangerous || (req.Command != "" && isCommandDangerous(req.Command)) {
		return Ask
	}
	switch p.Mode {
	case ModePlan:
		return Deny
	case ModeYOLO:
		return Allow
	case ModeAcceptEdits:
		// Protected paths (.git/, .env*, keys) still require confirmation (SPEC §3.3 P-6)
		if req.IsProtected {
			return Ask
		}
		// If it's an edit with diff, allow without prompting; exec asks.
		if req.Diff != "" {
			return Allow
		}
		return Ask
	default:
		return Ask
	}
}

func isCommandDangerous(cmd string) bool {
	isDang, _, _ := IsDangerousCommand(cmd)
	return isDang
}
