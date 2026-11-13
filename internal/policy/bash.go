package policy

import (
	"regexp"
	"strings"
	"unicode"
)

// BashAnalysis contains the results of analyzing a shell command.
type BashAnalysis struct {
	Raw          string
	RawSegments  []string
	Segments     []string   // trimmed non-empty segments
	Tokens       [][]string // quote-stripped tokens for each segment
	IsOpaque     bool
	OpaqueReason string
	IsHazard     bool
	HazardReason string
	// IsSimple is false when the command is opaque or contains an unquoted metacharacter.
	IsSimple bool
}

var (
	reEval           = regexp.MustCompile(`(?i)(^|[^[:alnum:]_])(eval)([^[:alnum:]_]|$)`)
	rePowershellIEX  = regexp.MustCompile(`(?i)(^|[^[:alnum:]_])(iex|Invoke-Expression)([^[:alnum:]_]|$)`)
	reEncodedCommand = regexp.MustCompile(`(?i)(^|[\s])(-encodedcommand|-enc|-ec)([^[:alnum:]_]|$)`)
	rePercentVar     = regexp.MustCompile(`%[A-Za-z_][A-Za-z0-9_]*%`)
)

// SplitAndAnalyzeCommand parses a command string into segments and evaluates opacity and hazards.
func SplitAndAnalyzeCommand(rawCmd string) BashAnalysis {
	res := BashAnalysis{Raw: rawCmd, Segments: []string{}, Tokens: [][]string{}, RawSegments: []string{}}
	cmd := strings.Trim(rawCmd, " \t\r")
	if cmd == "" {
		res.IsSimple = true
		return res
	}

	segments, isOpaque, opaqueReason, sawMeta := splitPreservingQuotes(cmd)
	res.RawSegments = segments
	for _, s := range segments {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		res.Segments = append(res.Segments, trimmed)
		toks, op, why := tokenizeSegment(trimmed)
		res.Tokens = append(res.Tokens, toks)
		if op {
			isOpaque = true
			if opaqueReason == "" {
				opaqueReason = why
			}
		}
	}

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
	if rePercentVar.MatchString(cmd) {
		isOpaque = true
		if opaqueReason == "" {
			opaqueReason = "%VAR% expansion"
		}
	}

	res.IsOpaque = isOpaque
	res.OpaqueReason = opaqueReason
	isHazard, hazardReason := checkTokenHazards(res.Tokens)
	res.IsHazard = isHazard
	res.HazardReason = hazardReason
	res.IsSimple = !isOpaque && !sawMeta && len(res.Segments) == 1
	return res
}

func splitPreservingQuotes(cmd string) (segments []string, isOpaque bool, opaqueReason string, sawMeta bool) {
	var cur strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
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
			if i+1 < n && strings.ContainsRune("&|;<>`$\n\"'", runes[i+1]) {
				isOpaque = true
				if opaqueReason == "" {
					opaqueReason = "escaped metacharacter"
				}
			}
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
		if inSingle {
			cur.WriteRune(r)
			continue
		}
		if r == '`' {
			isOpaque = true
			if opaqueReason == "" {
				opaqueReason = "unquoted or expandable backtick command substitution"
			}
			cur.WriteRune(r)
			continue
		}
		if r == '$' && i+1 < n && (runes[i+1] == '(' || runes[i+1] == '{' || isNameStart(runes[i+1])) {
			isOpaque = true
			if opaqueReason == "" {
				if runes[i+1] == '(' {
					opaqueReason = "command substitution '$(' detected"
				} else {
					opaqueReason = "variable expansion detected"
				}
			}
			cur.WriteRune(r)
			continue
		}
		if !inDouble {
			if (r == '<' || r == '>') && i+1 < n && runes[i+1] == '(' {
				isOpaque = true
				if opaqueReason == "" {
					opaqueReason = "process substitution detected"
				}
			}
			if r == '>' || r == '<' {
				isOpaque = true
				if opaqueReason == "" {
					opaqueReason = "unquoted shell redirection or here-doc"
				}
			}
			if r == ';' || r == '\n' || r == '&' || r == '|' {
				if (r == '&' || r == '|') && i+1 < n && runes[i+1] == r {
					i++
				}
				sawMeta = true
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
	return segments, isOpaque, opaqueReason, sawMeta
}

func isNameStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

// tokenizeSegment strips one layer of quotes so `go test "-exec"` is seen as -exec.
// Concatenated quotes, escaped quotes, and expansions are opaque.
func tokenizeSegment(seg string) (tokens []string, opaque bool, reason string) {
	var cur strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	quoted := false
	runes := []rune(seg)
	flush := func() {
		if cur.Len() == 0 && !quoted {
			quoted = false
			return
		}
		tokens = append(tokens, cur.String())
		cur.Reset()
		quoted = false
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			if r == '"' || r == '\'' || r == '`' || r == '$' {
				return tokens, true, "escaped quote or expansion"
			}
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			if cur.Len() > 0 && !inSingle {
				return tokens, true, "concatenated quotes"
			}
			inSingle = !inSingle
			quoted = true
			if !inSingle && i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
				return tokens, true, "concatenated quotes"
			}
			continue
		}
		if r == '"' && !inSingle {
			if cur.Len() > 0 && !inDouble {
				return tokens, true, "concatenated quotes"
			}
			inDouble = !inDouble
			quoted = true
			if !inDouble && i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
				return tokens, true, "concatenated quotes"
			}
			continue
		}
		if unicode.IsSpace(r) && !inSingle && !inDouble {
			flush()
			continue
		}
		cur.WriteRune(r)
	}
	if inSingle || inDouble || escaped {
		return tokens, true, "unbalanced quotes in command string"
	}
	flush()
	return tokens, false, ""
}

func checkTokenHazards(groups [][]string) (bool, string) {
	for _, tokens := range groups {
		if len(tokens) == 0 {
			continue
		}
		cmd := baseCommand(tokens[0])
		switch cmd {
		case "find":
			for _, arg := range tokens[1:] {
				switch strings.ToLower(arg) {
				case "-exec", "-execdir", "-ok", "-delete":
					return true, "find contains " + arg
				}
			}
		case "xargs":
			return true, "xargs can run an arbitrary command"
		case "env":
			if envRunsCommand(tokens[1:]) {
				return true, "env runs a command"
			}
		case "sh", "bash", "zsh", "cmd", "powershell", "pwsh":
			if shellCarriesCommand(tokens[1:]) {
				return true, cmd + " carries a command"
			}
		case "eval":
			return true, "eval primitive invocation"
		case "iex", "invoke-expression":
			return true, "PowerShell Invoke-Expression"
		case "git":
			for _, arg := range tokens[1:] {
				low := strings.ToLower(arg)
				name := low
				if i := strings.IndexByte(low, '='); i >= 0 {
					name = low[:i]
				}
				if name == "-c" || name == "--config-env" || strings.HasPrefix(low, "--upload-pack") || strings.HasPrefix(low, "--exec-path") || strings.Contains(low, "alias.") {
					return true, "git contains dangerous configuration or execution option: " + arg
				}
			}
		case "go":
			for _, arg := range tokens[1:] {
				low := strings.ToLower(arg)
				name := low
				if i := strings.IndexByte(low, '='); i >= 0 {
					name = low[:i]
				}
				switch name {
				case "-exec", "--exec", "-toolexec", "--toolexec", "-overlay", "--overlay":
					return true, "go contains dangerous execution flag: " + arg
				}
			}
		}
	}
	return false, ""
}

func envRunsCommand(args []string) bool {
	for _, arg := range args {
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !strings.Contains(arg, "=") {
			return true
		}
	}
	return false
}

func shellCarriesCommand(args []string) bool {
	for _, arg := range args {
		low := strings.ToLower(arg)
		name := low
		if i := strings.IndexByte(low, '='); i >= 0 {
			name = low[:i]
		}
		switch name {
		case "-c", "/c", "-command", "-encodedcommand", "-enc", "-ec", "-e":
			return true
		}
	}
	return false
}

func baseCommand(token string) string {
	base := token
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	return base
}

// CheckArgumentHazards identifies argument hazards like `go test -exec` or `git -c`.
func CheckArgumentHazards(segments []string) (bool, string) {
	var groups [][]string
	for _, seg := range segments {
		toks, _, _ := tokenizeSegment(seg)
		groups = append(groups, toks)
	}
	return checkTokenHazards(groups)
}

// MatchBashAllow reports whether an allow pattern matches this command.
// A wildcard (prefix:*) matches only a non-opaque command whose arguments are
// plain paths or flags from the built-in safe table. Commands with no table
// match only an exact saved command string. Floor items never match.
func MatchBashAllow(analysis BashAnalysis, pattern string) bool {
	if analysis.IsOpaque || analysis.IsHazard {
		return false
	}
	pat := strings.TrimSpace(pattern)
	if pat == "" || len(analysis.Segments) == 0 {
		return false
	}
	raw := strings.TrimSpace(analysis.Raw)
	if pat == raw {
		return analysis.IsSimple
	}
	if !analysis.IsSimple || !strings.HasSuffix(pat, ":*") {
		return false
	}
	prefix := strings.TrimSpace(strings.TrimSuffix(pat, ":*"))
	if prefix == "" || prefix == "*" {
		return false
	}
	for _, tokens := range analysis.Tokens {
		if !wildcardAllows(tokens, prefix) {
			return false
		}
	}
	return true
}

func wildcardAllows(tokens []string, prefix string) bool {
	if len(tokens) == 0 {
		return false
	}
	want := strings.Fields(prefix)
	if len(tokens) < len(want) {
		return false
	}
	for i, w := range want {
		if baseCommand(tokens[i]) != baseCommand(w) && !strings.EqualFold(tokens[i], w) {
			return false
		}
	}
	return commandArgsSafe(tokens)
}

func commandArgsSafe(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	switch baseCommand(tokens[0]) {
	case "go":
		if len(tokens) < 2 {
			return false
		}
		sub := strings.ToLower(tokens[1])
		if sub != "test" && sub != "build" && sub != "vet" {
			return false
		}
		return flagsSafe(tokens[2:], goSafeFlags, goValueFlags)
	case "git":
		if len(tokens) < 2 {
			return false
		}
		sub := strings.ToLower(tokens[1])
		flags, ok := gitSafeFlags[sub]
		if !ok {
			return false
		}
		return flagsSafe(tokens[2:], flags, gitValueFlags[sub])
	default:
		return false
	}
}

func flagsSafe(args []string, safe map[string]bool, valued map[string]bool) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "" || !strings.HasPrefix(a, "-") {
			continue
		}
		name := a
		hasEq := false
		if i := strings.IndexByte(a, '='); i >= 0 {
			name = a[:i]
			hasEq = true
		}
		name = normalizeFlag(name)
		if !safe[name] {
			return false
		}
		if valued[name] && !hasEq {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return false
			}
			i++
		}
	}
	return true
}

func normalizeFlag(name string) string {
	name = strings.ToLower(name)
	for strings.HasPrefix(name, "-") {
		name = strings.TrimPrefix(name, "-")
	}
	return "-" + name
}

var goSafeFlags = map[string]bool{
	"-v": true, "-race": true, "-count": true, "-run": true,
	"-cover": true, "-short": true, "-timeout": true, "-tags": true,
}

var goValueFlags = map[string]bool{
	"-count": true, "-run": true, "-timeout": true, "-tags": true,
}

var gitSafeFlags = map[string]map[string]bool{
	"status": {
		"-s": true, "-short": true, "-b": true, "-branch": true,
		"-porcelain": true, "-u": true, "-untracked-files": true,
	},
	"diff": {
		"-stat": true, "-name-only": true, "-cached": true, "-staged": true,
		"-p": true, "-numstat": true, "-w": true, "-ignore-all-space": true,
	},
	"log": {
		"-oneline": true, "-stat": true, "-name-only": true, "-n": true,
		"-max-count": true, "-p": true,
	},
	"show": {
		"-stat": true, "-name-only": true, "-p": true, "-oneline": true, "-format": true,
	},
	"branch": {
		"-v": true, "-vv": true, "-a": true, "-r": true, "-list": true, "-show-current": true,
	},
}

var gitValueFlags = map[string]map[string]bool{
	"status": {"-u": true, "-untracked-files": true},
	"log":    {"-n": true, "-max-count": true},
	"show":   {"-format": true},
}

// MatchBashDeny checks if ANY segment matches the deny pattern, OR if the raw command
// contains the denied pattern. Matching is aggressive for deny.
func MatchBashDeny(rawCmd string, analysis BashAnalysis, pattern string) bool {
	pat := strings.TrimSpace(pattern)
	if pat == "" {
		return false
	}
	cleanPat := strings.TrimSuffix(pat, ":*")
	cleanPat = strings.TrimSuffix(cleanPat, "*")
	cleanPat = strings.TrimSpace(cleanPat)
	if cleanPat != "" && strings.Contains(rawCmd, cleanPat) {
		return true
	}
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
		prefix := strings.TrimSpace(strings.TrimSuffix(pat, ":*"))
		return strings.HasPrefix(s, prefix)
	}
	if strings.HasSuffix(pat, "*") {
		prefix := strings.TrimSuffix(pat, "*")
		return strings.HasPrefix(s, prefix)
	}
	return s == pat
}
