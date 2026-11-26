package agent

import (
	"crypto/sha256"
	"fmt"

	"github.com/ayushpatil0810/forge/internal/llm"
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

func redactID(id string) string {
	if redact.Redact(id) == id {
		return id
	}
	hash := sha256.Sum256([]byte(id))
	return fmt.Sprintf("redacted_%x", hash[:12])
}

func redactMessage(message llm.Message) llm.Message {
	message = cloneMessage(message)
	message.ID = redactID(message.ID)
	message.ToolCallID = redactID(message.ToolCallID)
	message.Text = redact.Redact(message.Text)
	for i := range message.ToolCalls {
		call := &message.ToolCalls[i]
		call.ID = redactID(call.ID)
		call.Name = redact.Redact(call.Name)
		call.Input = []byte(redact.JSON(string(call.Input)))
	}
	return message
}
