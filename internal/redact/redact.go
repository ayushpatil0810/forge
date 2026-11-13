package redact

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

// RegisterSecret registers a sensitive string to be redacted.
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

// ClearSecrets clears dynamically registered secrets (useful for tests).
func ClearSecrets() {
	registeredMu.Lock()
	defer registeredMu.Unlock()
	registeredSecrets = nil
}

// High-confidence regex patterns for sensitive credentials.
// Negatives (Git commit SHAs, UUIDs, generic hex/base64) must not be matched.
var (
	// Private key blocks (PEM format)
	rePrivateKey = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)

	// AWS Access Key ID (AKIA or ASIA followed by 16 uppercase alphanumeric chars)
	reAWSKey = regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)

	// GitHub personal access tokens and OAuth tokens
	reGitHubToken = regexp.MustCompile(`\b(ghp_[0-9a-zA-Z]{36}|gho_[0-9a-zA-Z]{36}|ghu_[0-9a-zA-Z]{36}|ghs_[0-9a-zA-Z]{36}|ghr_[0-9a-zA-Z]{36}|github_pat_[0-9a-zA-Z_]{82})\b`)

	// OpenAI API keys (project and legacy)
	reOpenAIKey = regexp.MustCompile(`\b(sk-proj-[0-9a-zA-Z_\-]{20,}|sk-[0-9a-zA-Z]{20,})\b`)

	// Anthropic API keys
	reAnthropicKey = regexp.MustCompile(`\b(sk-ant-[0-9a-zA-Z_\-]{20,})\b`)

	// Google Gemini API keys
	reGeminiKey = regexp.MustCompile(`\b(AIza[0-9A-Za-z_\-]{20,})\b`)
)

// Redact replaces recognized secrets and credentials in text with [REDACTED:<type>].
// It is guaranteed to be idempotent and safe against panics.
func Redact(s string) string {
	if s == "" {
		return ""
	}

	res := s

	// 1. Registered dynamic secrets
	registeredMu.RLock()
	for _, sec := range registeredSecrets {
		if len(sec) >= 6 && strings.Contains(res, sec) {
			res = strings.ReplaceAll(res, sec, "[REDACTED:secret]")
		}
	}
	registeredMu.RUnlock()

	// 2. Known environment variables carrying keys
	for _, envVar := range []string{
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"GEMINI_API_KEY",
		"GOOGLE_API_KEY",
		"AWS_SECRET_ACCESS_KEY",
		"GITHUB_TOKEN",
	} {
		val := strings.TrimSpace(os.Getenv(envVar))
		if len(val) >= 8 && strings.Contains(res, val) {
			res = strings.ReplaceAll(res, val, "[REDACTED:api_key]")
		}
	}

	// 3. High-confidence patterns
	res = rePrivateKey.ReplaceAllString(res, "[REDACTED:private_key]")
	res = reAWSKey.ReplaceAllString(res, "[REDACTED:aws_key]")
	res = reGitHubToken.ReplaceAllString(res, "[REDACTED:github_token]")
	res = reOpenAIKey.ReplaceAllString(res, "[REDACTED:openai_key]")
	res = reAnthropicKey.ReplaceAllString(res, "[REDACTED:anthropic_key]")
	res = reGeminiKey.ReplaceAllString(res, "[REDACTED:gemini_key]")

	return res
}
