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
	ModeDefault     Mode = "default"     // reads free; writes and exec ask
	ModeAcceptEdits Mode = "acceptEdits" // reads and writes free; exec asks
	ModePlan        Mode = "plan"        // read-only: writes and exec denied
)

// Policy is intentionally tiny now. Next: allow/deny rules from a policy
// file (e.g. "bash(go test:*)"), loaded per project and per user.
type Policy struct{ Mode Mode }

func (p Policy) Check(toolName string, risk tools.Risk) Verdict {
	switch risk {
	case tools.RiskRead:
		return Allow
	case tools.RiskWrite:
		switch p.Mode {
		case ModeAcceptEdits:
			return Allow
		case ModePlan:
			return Deny
		}
		return Ask
	default: // exec and anything unknown: fail closed
		if p.Mode == ModePlan {
			return Deny
		}
		return Ask
	}
}
