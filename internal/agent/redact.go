package agent

import (
	"github.com/ayushpatil0810/forge/internal/redact"
)

// RegisterSecret registers a sensitive string with the redactor.
func RegisterSecret(s string) {
	redact.RegisterSecret(s)
}

// RedactSecrets replaces sensitive API keys, tokens, and credentials with safe placeholders.
func RedactSecrets(s string) string {
	return redact.Redact(s)
}
