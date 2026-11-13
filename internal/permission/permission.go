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
	ModeAsk         Mode = "ask"          // reads free; writes and exec ask
	ModeDefault     Mode = ModeAsk        // canonical default is ask
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
	// In plan mode, all mutating and execution operations are denied (read-only)
	if p.Mode == ModePlan {
		return Deny
	}
	// Dangerous commands (e.g. rm -rf /, pipe-to-shell, fork bombs) always ask regardless of mode (even in yolo or accept-edits)
	if req.IsDangerous || (req.Command != "" && isCommandDangerous(req.Command)) {
		return Ask
	}
	// Protected paths (.git/, .env*, .ssh, .aws, credentials) always ask in ask, accept-edits, and yolo (SPEC §3.3 P-6)
	if req.IsProtected {
		return Ask
	}
	switch p.Mode {
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

func isCommandDangerous(cmd string) bool {
	isDang, _, _ := IsDangerousCommand(cmd)
	return isDang
}
