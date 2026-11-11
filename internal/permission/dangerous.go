package permission

import (
	"regexp"
	"strings"
)

// DangerousRule specifies a rule for detecting commands that should never run without confirmation (SEC-3).
type DangerousRule struct {
	Name        string
	Pattern     *regexp.Regexp
	Description string
}

// DefaultDangerousRules defines patterns for dangerous commands.
var DefaultDangerousRules = []DangerousRule{
	{
		Name:        "root_deletion",
		Pattern:     regexp.MustCompile(`(?i)(rm\s+-(?:rf|fr|r\s+-f|f\s+-r)\s+/(?:\s|$)|rd\s+/[sq]\s+/[sq]\s+[a-z]:\\|Remove-Item\s+.*-Recurse.*[a-z]:\\)`),
		Description: "attempted recursive deletion of root directory or drive root",
	},
	{
		Name:        "git_destructive",
		Pattern:     regexp.MustCompile(`(?i)(rm\s+-(?:rf|fr)\s+\.git|git\s+reset\s+--hard|git\s+push\s+.*(?:--force|-f\b)|git\s+clean\s+-(?:fdx|fxd|fd|fx))`),
		Description: "destructive git operation (repository deletion, hard reset, or force push)",
	},
	{
		Name:        "pipe_to_shell",
		Pattern:     regexp.MustCompile(`(?i)(curl\b.*\|\s*(?:ba)?sh|wget\b.*\|\s*(?:ba)?sh|irm\b.*\|\s*iex|Invoke-RestMethod\b.*\|\s*Invoke-Expression)`),
		Description: "remote script execution via pipe to shell",
	},
	{
		Name:        "fork_bomb",
		Pattern:     regexp.MustCompile(`(?::\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:|%0\|%0)`),
		Description: "fork bomb or runaway recursive process invocation",
	},
	{
		Name:        "system_altering",
		Pattern:     regexp.MustCompile(`(?i)(\bformat\s+[a-z]:|\bmkfs\b|\bdd\s+if=/dev/)`),
		Description: "low-level disk format, filesystem creation, or raw block write",
	},
}

// IsDangerousCommand checks if cmd matches any known dangerous command patterns.
// It returns true along with the rule name and description if dangerous.
func IsDangerousCommand(cmd string) (bool, string, string) {
	trimmed := strings.TrimSpace(cmd)
	for _, rule := range DefaultDangerousRules {
		if rule.Pattern.MatchString(trimmed) {
			return true, rule.Name, rule.Description
		}
	}
	return false, "", ""
}

