package agent

import (
	"os"
	"regexp"
	"strings"
	"sync"
)

var (
	registeredMu      sync.RWMutex
	registeredSecrets []string
)

// RegisterSecret registers a sensitive string with the redactor.
func RegisterSecret(s string) {
	s = strings.TrimSpace(s)
	if len(s) < 6 {
		return
	}
	registeredMu.Lock()
	defer registeredMu.Unlock()
	for _, existing := range registeredSecrets {
		if existing == s {
			return
		}
	}
	registeredSecrets = append(registeredSecrets, s)
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(sk-ant-[a-zA-Z0-9_\-]{10,})\b`),
	regexp.MustCompile(`(?i)\b(sk-proj-[a-zA-Z0-9_\-]{10,})\b`),
	regexp.MustCompile(`(?i)\b(sk-[a-zA-Z0-9_\-]{20,})\b`),
	regexp.MustCompile(`(?i)\b(AIza[0-9A-Za-z\-_]{30,})\b`),
	regexp.MustCompile(`(?i)\b(ghp_[a-zA-Z0-9]{30,})\b`),
	regexp.MustCompile(`(?i)\b(gho_[a-zA-Z0-9]{30,})\b`),
	regexp.MustCompile(`(?i)\b(bearer\s+)([a-zA-Z0-9_\-\.]{20,})\b`),
}

// RedactSecrets replaces sensitive API keys, tokens, and credentials with safe placeholders.
func RedactSecrets(s string) string {
	res := s

	// Check dynamically registered secrets (e.g. from user input, CLI login, or keychain)
	registeredMu.RLock()
	for _, sec := range registeredSecrets {
		if len(sec) >= 6 && strings.Contains(res, sec) {
			res = strings.ReplaceAll(res, sec, "[REDACTED SECRET]")
		}
	}
	registeredMu.RUnlock()

	// Check known environment API keys
	for _, envVar := range []string{
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"GEMINI_API_KEY",
		"GOOGLE_API_KEY",
	} {
		val := strings.TrimSpace(os.Getenv(envVar))
		if len(val) >= 8 && strings.Contains(res, val) {
			res = strings.ReplaceAll(res, val, "[REDACTED "+envVar+"]")
		}
	}

	for _, pat := range secretPatterns {
		res = pat.ReplaceAllStringFunc(res, func(match string) string {
			if strings.HasPrefix(strings.ToLower(match), "sk-ant-") {
				return "sk-ant-***"
			}
			if strings.HasPrefix(strings.ToLower(match), "sk-proj-") {
				return "sk-proj-***"
			}
			if strings.HasPrefix(strings.ToLower(match), "sk-") {
				return "sk-***"
			}
			if strings.HasPrefix(match, "AIza") {
				return "AIza***"
			}
			if strings.HasPrefix(match, "ghp_") || strings.HasPrefix(match, "gho_") {
				return "gh***"
			}
			if strings.HasPrefix(strings.ToLower(match), "bearer ") {
				return "Bearer [REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return res
}
