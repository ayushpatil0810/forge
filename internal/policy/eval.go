package policy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ayushpatil0810/forge/internal/permission"
)

// EvalRequest provides all context needed to evaluate a policy decision.
type EvalRequest struct {
	ToolName    string
	Input       string
	Path        string
	Command     string
	IsProtected bool
	IsDangerous bool
	Mode        permission.Mode
	IsHeadless  bool
	AutoApprove bool // e.g. --yes flag
}

// EvalResult contains the verdict, source, rule, and explanation.
type EvalResult struct {
	Verdict      permission.Verdict
	Source       string // "floor", "rule:<text>", "mode", "headless-auto-deny"
	Reason       string
	MatchedRule  *Rule
	IsFloor      bool
	BashAnalysis *BashAnalysis
}

// Evaluate determines whether a tool execution is Allowed, Asked, or Denied according to SPEC §3.3.
func Evaluate(req EvalRequest, set *PolicySet) EvalResult {
	canonicalTool := mapToolName(req.ToolName)
	var bashAnalysis *BashAnalysis

	// Extract command or path from input if not already explicitly provided
	switch canonicalTool {
	case ToolBash:
		cmd := req.Command
		if cmd == "" && req.Input != "" {
			var args struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal([]byte(req.Input), &args)
			cmd = args.Command
		}
		analysis := SplitAndAnalyzeCommand(cmd)
		bashAnalysis = &analysis
		req.Command = cmd
	case ToolEdit, ToolRead:
		if req.Path == "" && req.Input != "" {
			var args struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal([]byte(req.Input), &args)
			req.Path = args.Path
		}
	}

	// 1. HARD SAFETY FLOOR
	// Floor triggers if:
	// - req.IsDangerous or matched dangerous command pattern
	// - req.IsProtected or matched protected path pattern
	// - For bash: opaque command or argument hazard
	isFloor := false
	floorReason := ""

	if req.IsDangerous {
		isFloor = true
		floorReason = "action flagged as dangerous"
	} else if canonicalTool == ToolBash && req.Command != "" {
		isDang, _, desc := permission.IsDangerousCommand(req.Command)
		if isDang {
			isFloor = true
			floorReason = "dangerous command: " + desc
		} else if bashAnalysis != nil {
			if bashAnalysis.IsOpaque {
				isFloor = true
				floorReason = "opaque command cannot be analyzed (" + bashAnalysis.OpaqueReason + ")"
			} else if bashAnalysis.IsHazard {
				isFloor = true
				floorReason = "argument hazard (" + bashAnalysis.HazardReason + ")"
			}
		}
	}

	if req.IsProtected || IsProtectedPath(req.Path) {
		isFloor = true
		if floorReason == "" {
			floorReason = fmt.Sprintf("protected path access (%s)", req.Path)
		}
	}

	if isFloor {
		// Floor is never auto-allowed by any rule.
		// Plan mode denies all writes/commands.
		if req.Mode == permission.ModePlan && canonicalTool != ToolRead {
			return EvalResult{
				Verdict:      permission.Deny,
				Source:       "floor",
				Reason:       floorReason + " (denied in plan mode)",
				IsFloor:      true,
				BashAnalysis: bashAnalysis,
			}
		}

		// In headless mode without human interaction:
		if req.IsHeadless {
			return EvalResult{
				Verdict:      permission.Deny,
				Source:       "headless-auto-deny",
				Reason:       floorReason + " (cannot auto-approve safety floor in headless mode)",
				IsFloor:      true,
				BashAnalysis: bashAnalysis,
			}
		}

		// Interactive: safety floor always prompts (Ask)
		return EvalResult{
			Verdict:      permission.Ask,
			Source:       "floor",
			Reason:       floorReason,
			IsFloor:      true,
			BashAnalysis: bashAnalysis,
		}
	}

	// 2. DENY RULES (Project and User rules; Deny always takes precedence and applies even in yolo)
	if set != nil {
		allDenies := append([]Rule{}, set.UserRules...)
		allDenies = append(allDenies, set.ProjectRules...)
		for _, r := range allDenies {
			if r.Action != ActionDeny || r.Tool != canonicalTool {
				continue
			}
			if matchesRule(r, canonicalTool, req.Path, req.Command, bashAnalysis) {
				return EvalResult{
					Verdict:      permission.Deny,
					Source:       "rule:" + r.Raw,
					Reason:       fmt.Sprintf("matches %s deny rule: %s", r.Scope, r.Raw),
					MatchedRule:  &r,
					IsFloor:      false,
					BashAnalysis: bashAnalysis,
				}
			}
		}
	}

	// 3. ASK RULES (Project and User rules)
	if set != nil {
		allAsks := append([]Rule{}, set.UserRules...)
		allAsks = append(allAsks, set.ProjectRules...)
		for _, r := range allAsks {
			if r.Action != ActionAsk || r.Tool != canonicalTool {
				continue
			}
			if matchesRule(r, canonicalTool, req.Path, req.Command, bashAnalysis) {
				if req.IsHeadless && !req.AutoApprove {
					return EvalResult{
						Verdict:      permission.Deny,
						Source:       "headless-auto-deny",
						Reason:       fmt.Sprintf("matches %s ask rule: %s (auto-denied in headless)", r.Scope, r.Raw),
						MatchedRule:  &r,
						IsFloor:      false,
						BashAnalysis: bashAnalysis,
					}
				}
				if req.AutoApprove {
					return EvalResult{
						Verdict:      permission.Allow,
						Source:       "rule:" + r.Raw,
						Reason:       fmt.Sprintf("matches %s ask rule (approved via --yes): %s", r.Scope, r.Raw),
						MatchedRule:  &r,
						IsFloor:      false,
						BashAnalysis: bashAnalysis,
					}
				}
				return EvalResult{
					Verdict:      permission.Ask,
					Source:       "rule:" + r.Raw,
					Reason:       fmt.Sprintf("matches %s ask rule: %s", r.Scope, r.Raw),
					MatchedRule:  &r,
					IsFloor:      false,
					BashAnalysis: bashAnalysis,
				}
			}
		}
	}

	// 4. ALLOW RULES (User rules only; project allow rules are ignored)
	if set != nil {
		for _, r := range set.UserRules {
			if r.Action != ActionAllow || r.Tool != canonicalTool {
				continue
			}
			if matchesRule(r, canonicalTool, req.Path, req.Command, bashAnalysis) {
				// Plan mode denies writes/exec regardless of allow rules
				if req.Mode == permission.ModePlan && canonicalTool != ToolRead {
					return EvalResult{
						Verdict:      permission.Deny,
						Source:       "mode",
						Reason:       "plan mode denies mutating actions regardless of allow rules",
						MatchedRule:  &r,
						IsFloor:      false,
						BashAnalysis: bashAnalysis,
					}
				}
				return EvalResult{
					Verdict:      permission.Allow,
					Source:       "rule:" + r.Raw,
					Reason:       fmt.Sprintf("matches user allow rule: %s", r.Raw),
					MatchedRule:  &r,
					IsFloor:      false,
					BashAnalysis: bashAnalysis,
				}
			}
		}
	}

	// 5. MODE DEFAULT
	switch req.Mode {
	case permission.ModePlan:
		if canonicalTool == ToolRead {
			return EvalResult{Verdict: permission.Allow, Source: "mode", Reason: "plan mode allows read operations", BashAnalysis: bashAnalysis}
		}
		return EvalResult{Verdict: permission.Deny, Source: "mode", Reason: "plan mode denies writes and command execution", BashAnalysis: bashAnalysis}

	case permission.ModeYOLO:
		return EvalResult{Verdict: permission.Allow, Source: "mode", Reason: "yolo mode auto-approves non-floor actions", BashAnalysis: bashAnalysis}

	case permission.ModeAcceptEdits:
		if canonicalTool == ToolEdit || canonicalTool == ToolRead {
			return EvalResult{Verdict: permission.Allow, Source: "mode", Reason: "accept-edits mode allows file edits", BashAnalysis: bashAnalysis}
		}
		// Bash still asks in accept-edits mode
		if req.IsHeadless && !req.AutoApprove {
			return EvalResult{Verdict: permission.Deny, Source: "headless-auto-deny", Reason: "bash asks in accept-edits mode (auto-denied in headless)", BashAnalysis: bashAnalysis}
		}
		if req.AutoApprove {
			return EvalResult{Verdict: permission.Allow, Source: "mode", Reason: "approved via --yes", BashAnalysis: bashAnalysis}
		}
		return EvalResult{Verdict: permission.Ask, Source: "mode", Reason: "accept-edits mode asks before running bash", BashAnalysis: bashAnalysis}

	default: // ModeAsk
		if canonicalTool == ToolRead {
			return EvalResult{Verdict: permission.Allow, Source: "mode", Reason: "read operations are free in ask mode", BashAnalysis: bashAnalysis}
		}
		if req.IsHeadless && !req.AutoApprove {
			return EvalResult{Verdict: permission.Deny, Source: "headless-auto-deny", Reason: "action requires approval in headless mode (use --yes)", BashAnalysis: bashAnalysis}
		}
		if req.AutoApprove {
			return EvalResult{Verdict: permission.Allow, Source: "mode", Reason: "approved via --yes", BashAnalysis: bashAnalysis}
		}
		return EvalResult{Verdict: permission.Ask, Source: "mode", Reason: "ask mode requires confirmation for mutating actions", BashAnalysis: bashAnalysis}
	}
}

func mapToolName(name string) ToolType {
	switch strings.ToLower(name) {
	case "read", "read_file":
		return ToolRead
	case "edit", "edit_file", "write_file":
		return ToolEdit
	case "bash":
		return ToolBash
	default:
		return ToolType(name)
	}
}

func matchesRule(r Rule, tool ToolType, path, cmd string, analysis *BashAnalysis) bool {
	if r.Tool != tool {
		return false
	}
	switch tool {
	case ToolRead, ToolEdit:
		return MatchPathPattern(r.Pattern, path)
	case ToolBash:
		if analysis == nil {
			a := SplitAndAnalyzeCommand(cmd)
			analysis = &a
		}
		if r.Action == ActionAllow {
			return MatchBashAllow(*analysis, r.Pattern)
		}
		return MatchBashDeny(cmd, *analysis, r.Pattern)
	default:
		return false
	}
}
