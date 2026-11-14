package agent

import (
	"errors"
	"strings"

	"github.com/ayushpatil0810/forge/internal/llm"
)

// Canonical error codes for classification and UI display.
const (
	ErrCodeAuth          = "auth_failed"
	ErrCodeRateLimit     = "rate_limit_exceeded"
	ErrCodeQuota         = "quota_exhausted"
	ErrCodeContextLength = "context_length_exceeded"
	ErrCodeNetwork       = "network_error"
	ErrCodeConfig        = "config_error"
	ErrCodeLoopGuard     = "loop_guard"
	ErrCodeUnknown       = "unknown_error"
)

// ClassifyError inspects an error and returns its canonical error code and actionable guidance.
func ClassifyError(err error) (code string, guidance string) {
	if err == nil {
		return "", ""
	}

	// 1. Typed context-length error from provider adapters (Amendment D)
	var ctxErr *llm.ContextLengthError
	if errors.As(err, &ctxErr) {
		return ErrCodeContextLength, "The conversation exceeded the model's context window. Switch to a higher-context model (e.g. --model) or start a new session."
	}

	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "api key") || strings.Contains(msg, "api_key") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "authentication") || strings.Contains(msg, "401"):
		return ErrCodeAuth, "Verify your API key is set in environment variables (e.g. ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY). See docs/configuration.md."

	case strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests") || strings.Contains(msg, "429"):
		return ErrCodeRateLimit, "Provider rate limit reached. Forge automatically retries with exponential backoff."

	case strings.Contains(msg, "quota") || strings.Contains(msg, "credit") || strings.Contains(msg, "billing") || strings.Contains(msg, "insufficient"):
		return ErrCodeQuota, "Your provider account balance or quota is exhausted. Check your provider's billing console."

	case strings.Contains(msg, "dial tcp") || strings.Contains(msg, "connect:") || strings.Contains(msg, "timeout") || strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host"):
		return ErrCodeNetwork, "Failed to connect to the model provider. Check your network connection, proxy settings, or DNS."

	case strings.Contains(msg, "loop") || strings.Contains(msg, "guard") || strings.Contains(msg, "runaway") || strings.Contains(msg, "stalled"):
		return ErrCodeLoopGuard, "Execution halted by agent loop guard to prevent runaway tool loops or stall."

	case strings.Contains(msg, "config") || strings.Contains(msg, "flag"):
		return ErrCodeConfig, "Configuration error. Run 'forge config' to inspect current settings."

	default:
		return ErrCodeUnknown, "An unexpected error occurred. Review the error details above."
	}
}
