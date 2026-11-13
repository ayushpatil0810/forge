package permission

import (
	"regexp"
	"strings"
)

// DangerousRule specifies a rule for detecting commands that should never run without confirmation (SEC-3).
//
// NOTE: Dangerous command detection is a regex-based heuristic (speed bump)
// designed to catch obvious footguns and require confirmation even in YOLO or
// accept-edits modes. It is NOT a security sandbox. Evasions are trivially possible:
//   - String concatenation, variable substitution, or eval (e.g. `cmd="rm"; $cmd -rf /`, `eval $(...)`)
//   - Base64 / hex decoding before execution (e.g. `echo ... | base64 -d | sh`)
//   - Subshells and indirect interpreters (e.g. `python3 -c "import shutil; shutil.rmtree('/')"`)
//   - Alias or symlink indirection (e.g. `/bin/./rm`, custom function overrides)
//   - PowerShell encoded commands (`powershell -EncodedCommand ...`)
//   - Path traversal tricks or wildcards obscuring target paths
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
		Name:        "recursive_deletion",
		Pattern:     regexp.MustCompile(`(?i)(rm\s+-(?:[a-z]*r[a-z]*f|[a-z]*f[a-z]*r)\b|rm\s+-r\s+-f\b|rm\s+-f\s+-r\b|Remove-Item\b.*-(?:Recurse|r)\b.*-(?:Force|f)\b|Remove-Item\b.*-(?:Force|f)\b.*-(?:Recurse|r)\b|\b(?:del|erase)\b.*\/s\b|\b(?:rd|rmdir)\b.*\/s\b)`),
		Description: "recursive or forced deletion (rm -rf, Remove-Item -Recurse -Force, del /s, rd /s)",
	},
	{
		Name:        "git_destructive",
		Pattern:     regexp.MustCompile(`(?i)(rm\s+-(?:rf|fr)\s+\.git|git\s+reset\s+--hard|git\s+push\s+.*(?:--force|-f\b)|git\s+clean\s+.*(?:-[a-z]*f[a-z]*d|-[a-z]*d[a-z]*f|--force|-f\b))`),
		Description: "destructive git operation (repository deletion, hard reset, force push, or clean -fd)",
	},
	{
		Name:        "pipe_to_shell",
		Pattern:     regexp.MustCompile(`(?i)(?:curl|wget)\b.*\|\s*(?:ba|z)?sh|(?:irm|iwr|Invoke-WebRequest|Invoke-RestMethod)\b.*\|\s*(?:iex|Invoke-Expression)`),
		Description: "remote script execution via pipe to shell (curl|sh, iwr|iex)",
	},
	{
		Name:        "broad_permissions",
		Pattern:     regexp.MustCompile(`(?i)(?:chmod|chown)\s+-[a-z]*R[a-z]*\b.*?(?:^|\s)(?:/|~|\.\.|/\*|\*|\\)(?:\s|$)`),
		Description: "broad recursive permission or ownership change on root, home, or wildcards",
	},
	{
		Name:        "credential_or_git_tamper",
		Pattern:     regexp.MustCompile(`(?i)(?:>|>>|tee\b|Out-File|Set-Content|Add-Content|cp\b|copy\b|mv\b|move\b|rm\b|del\b).*(?:\.git[/\\].*|\.env(?:\.|$|\s|")|\.(?:ssh|aws)[/\\])`),
		Description: "modification or overwrite targeting .git directory or credentials (.env, .ssh, .aws)",
	},
	{
		Name:        "credential_read",
		Pattern:     regexp.MustCompile(`(?i)\b(?:cat|type|Get-Content|gc|head|tail)\b.*(?:\.env(?:\.|$|\s|")|\.(?:ssh|aws)[/\\]|id_rsa|\.pem\b|\.key\b|credentials)`),
		Description: "attempted command-line read of credential file (.env, .ssh, .aws, key, pem, credentials)",
	},
	{
		Name:        "outside_workspace",
		Pattern:     regexp.MustCompile(`(?i)(?:\.\.[/\\]|[/\\]etc[/\\]|[/\\]var[/\\]|[/\\]usr[/\\]|[/\\]root\b|[a-z]:\\Windows|[a-z]:\\Program Files)`),
		Description: "reference to system directories or path traversal outside workspace",
	},
	{
		Name:        "fork_bomb",
		Pattern:     regexp.MustCompile(`(?::\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:|%0\|%0)`),
		Description: "fork bomb or runaway recursive process invocation",
	},
	{
		Name:        "system_altering",
		Pattern:     regexp.MustCompile(`(?i)(\bformat\s+[a-z]:|\bmkfs\b|\bdd\b(?:\s+.*)?\b(?:if|of)=)`),
		Description: "low-level disk format, filesystem creation, or raw block write (dd)",
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
