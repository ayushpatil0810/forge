package policy

import (
	"regexp"
	"strings"
)

// BashAnalysis contains the results of analyzing a shell command.
type BashAnalysis struct {
	RawSegments  []string
	Segments     []string // trimmed non-empty segments
	IsOpaque     bool
	OpaqueReason string
	IsHazard     bool
	HazardReason string
}

var (
	reEval           = regexp.MustCompile(`(?i)\b(eval)\b`)
	rePowershellIEX  = regexp.MustCompile(`(?i)\b(iex|Invoke-Expression)\b`)
	reEncodedCommand = regexp.MustCompile(`(?i)(-(?:encodedcommand|enc|e))\b`)
)

// SplitAndAnalyzeCommand parses a command string into segments and evaluates opacity and hazards.
func SplitAndAnalyzeCommand(rawCmd string) BashAnalysis {
	res := BashAnalysis{}
	cmd := strings.TrimSpace(rawCmd)
	if cmd == "" {
		return res
	}

	var segments []string
	var cur strings.Builder

	inSingle := false
	inDouble := false
	escaped := false
	isOpaque := false
	opaqueReason := ""

	runes := []rune(cmd)
	n := len(runes)

	for i := 0; i < n; i++ {
		r := runes[i]

		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' && !inSingle {
			escaped = true
			cur.WriteRune(r)
			continue
		}

		if r == '\'' && !inDouble {
			inSingle = !inSingle
			cur.WriteRune(r)
			continue
		}

		if r == '"' && !inSingle {
			inDouble = !inDouble
			cur.WriteRune(r)
			continue
		}

		// Inside single quotes, everything is literal
		if inSingle {
			cur.WriteRune(r)
			continue
		}

		// Check for command substitution even inside double quotes: $( or `
		if r == '`' {
			isOpaque = true
			if opaqueReason == "" {
				opaqueReason = "unquoted or expandable backtick command substitution"
			}
			cur.WriteRune(r)
			continue
		}

		if r == '$' && i+1 < n && runes[i+1] == '(' {
			isOpaque = true
			if opaqueReason == "" {
				opaqueReason = "command substitution '$(' detected"
			}
			cur.WriteRune(r)
			continue
		}

		// The checks below apply strictly OUTSIDE both single and double quotes
		if !inDouble {
			// Process substitution: <( or >(
			if (r == '<' || r == '>') && i+1 < n && runes[i+1] == '(' {
				isOpaque = true
				if opaqueReason == "" {
					opaqueReason = "process substitution detected"
				}
			}

			// Here-docs or redirections: <<, >>, <, >
			if r == '>' || r == '<' {
				isOpaque = true
				if opaqueReason == "" {
					opaqueReason = "unquoted shell redirection or here-doc"
				}
			}

			// Segment separators: ;, \n, &&, ||, |, &
			if r == ';' || r == '\n' {
				segments = append(segments, cur.String())
				cur.Reset()
				continue
			}

			if r == '&' {
				if i+1 < n && runes[i+1] == '&' {
					segments = append(segments, cur.String())
					cur.Reset()
					i++ // skip second &
					continue
				}
				// Single & (background or cmd separator)
				segments = append(segments, cur.String())
				cur.Reset()
				continue
			}

			if r == '|' {
				if i+1 < n && runes[i+1] == '|' {
					segments = append(segments, cur.String())
					cur.Reset()
					i++ // skip second |
					continue
				}
				// Single pipe |
				segments = append(segments, cur.String())
				cur.Reset()
				continue
			}
		}

		cur.WriteRune(r)
	}

	if inSingle || inDouble {
		isOpaque = true
		if opaqueReason == "" {
			opaqueReason = "unbalanced quotes in command string"
		}
	}

	if cur.Len() > 0 {
		segments = append(segments, cur.String())
	}

	res.RawSegments = segments
	for _, s := range segments {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			res.Segments = append(res.Segments, trimmed)
		}
	}

	// Check execution primitives in raw command
	if reEval.MatchString(cmd) {
		isOpaque = true
		if opaqueReason == "" {
			opaqueReason = "eval primitive invocation"
		}
	}
	if rePowershellIEX.MatchString(cmd) {
		isOpaque = true
		if opaqueReason == "" {
			opaqueReason = "PowerShell Invoke-Expression/iex primitive"
		}
	}
	if reEncodedCommand.MatchString(cmd) {
		isOpaque = true
		if opaqueReason == "" {
			opaqueReason = "PowerShell -EncodedCommand flag"
		}
	}

	res.IsOpaque = isOpaque
	res.OpaqueReason = opaqueReason

	// Check argument hazards across all segments
	isHazard, hazardReason := CheckArgumentHazards(res.Segments)
	res.IsHazard = isHazard
	res.HazardReason = hazardReason

	return res
}

// CheckArgumentHazards identifies argument hazards like `go test -exec` or `git -c`.
func CheckArgumentHazards(segments []string) (bool, string) {
	for _, seg := range segments {
		tokens := strings.Fields(seg)
		if len(tokens) == 0 {
			continue
		}

		cmdName := strings.ToLower(tokens[0])
		if cmdName == "go" && len(tokens) >= 2 && strings.ToLower(tokens[1]) == "test" {
			for _, arg := range tokens[2:] {
				lower := strings.ToLower(arg)
				if lower == "-exec" || strings.HasPrefix(lower, "-exec=") ||
					lower == "-toolexec" || strings.HasPrefix(lower, "-toolexec=") {
					return true, "go test contains dangerous execution flag: " + arg
				}
			}
		}

		if cmdName == "git" {
			for _, arg := range tokens[1:] {
				lower := strings.ToLower(arg)
				if lower == "-c" || strings.HasPrefix(lower, "-c=") ||
					strings.HasPrefix(lower, "--upload-pack") ||
					strings.HasPrefix(lower, "--exec-path") {
					return true, "git contains dangerous configuration or execution option: " + arg
				}
			}
		}
	}
	return false, ""
}

// MatchBashAllow checks if EVERY segment in analysis matches the allow rule pattern.
// Matching is conservative for allow: any failure to match denies allow.
func MatchBashAllow(analysis BashAnalysis, pattern string) bool {
	if analysis.IsOpaque || analysis.IsHazard {
		return false
	}
	if len(analysis.Segments) == 0 {
		return false
	}

	pat := strings.TrimSpace(pattern)
	for _, seg := range analysis.Segments {
		if !matchSingleBashSegment(seg, pat) {
			return false
		}
	}
	return true
}

// MatchBashDeny checks if ANY segment matches the deny pattern, OR if the raw command
// contains the denied pattern. Matching is aggressive for deny.
func MatchBashDeny(rawCmd string, analysis BashAnalysis, pattern string) bool {
	pat := strings.TrimSpace(pattern)
	if pat == "" {
		return false
	}

	// 1. Raw string containment check (aggressive deny)
	cleanPat := strings.TrimSuffix(pat, ":*")
	cleanPat = strings.TrimSuffix(cleanPat, "*")
	cleanPat = strings.TrimSpace(cleanPat)
	if cleanPat != "" && strings.Contains(rawCmd, cleanPat) {
		return true
	}

	// 2. Segment-level check
	for _, seg := range analysis.Segments {
		if matchSingleBashSegment(seg, pat) {
			return true
		}
	}
	return false
}

func matchSingleBashSegment(seg, pattern string) bool {
	s := strings.TrimSpace(seg)
	pat := strings.TrimSpace(pattern)

	if pat == "*" {
		return true
	}

	if strings.HasSuffix(pat, ":*") {
		prefix := strings.TrimSuffix(pat, ":*")
		prefix = strings.TrimSpace(prefix)
		return strings.HasPrefix(s, prefix)
	}

	if strings.HasSuffix(pat, "*") {
		prefix := strings.TrimSuffix(pat, "*")
		return strings.HasPrefix(s, prefix)
	}

	// Exact match
	return s == pat
}
