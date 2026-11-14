package agent

import (
	"errors"
	"testing"

	"github.com/ayushpatil0810/forge/internal/llm"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode string
	}{
		{
			name:     "Auth missing env",
			err:      errors.New("anthropic: ANTHROPIC_API_KEY environment variable is required"),
			wantCode: ErrCodeAuth,
		},
		{
			name:     "Auth 401 unauthorized",
			err:      errors.New("HTTP 401: Unauthorized access"),
			wantCode: ErrCodeAuth,
		},
		{
			name:     "Rate limit 429",
			err:      errors.New("HTTP 429: rate limit exceeded, retry after 20s"),
			wantCode: ErrCodeRateLimit,
		},
		{
			name:     "Quota exhausted",
			err:      errors.New("insufficient quota or credits remaining"),
			wantCode: ErrCodeQuota,
		},
		{
			name:     "Network error connection refused",
			err:      errors.New("dial tcp 1.2.3.4:443: connect: connection refused"),
			wantCode: ErrCodeNetwork,
		},
		{
			name:     "Loop guard stalled",
			err:      errors.New("loop guard: runaway loop detected"),
			wantCode: ErrCodeLoopGuard,
		},
		{
			name:     "Unexpected error",
			err:      errors.New("something totally unexpected happened"),
			wantCode: ErrCodeUnknown,
		},
		// Typed context-length errors from provider adapters (Amendment D)
		{
			name: "Anthropic prompt too long",
			err: &llm.ContextLengthError{
				Provider: "anthropic",
				Message:  "anthropic API error (400 invalid_request_error): prompt is too long: 205000 tokens > 200000 maximum",
			},
			wantCode: ErrCodeContextLength,
		},
		{
			name: "OpenAI context length exceeded",
			err: &llm.ContextLengthError{
				Provider: "openai",
				Message:  "openai API error (400 invalid_request_error): This model's maximum context length is 16385 tokens. However, your messages resulted in 44366 tokens.",
			},
			wantCode: ErrCodeContextLength,
		},
		{
			name: "Gemini tokens exceeded",
			err: &llm.ContextLengthError{
				Provider: "gemini",
				Message:  "gemini API error (400 INVALID_ARGUMENT): The input token count (1,632,254) exceeds the maximum number of tokens allowed (1,048,576).",
			},
			wantCode: ErrCodeContextLength,
		},
		// Negative cases that must NOT classify as context length exceeded (Amendment D)
		{
			name:     "Rate limit mentioning tokens per minute",
			err:      errors.New("openai API error (429 rate_limit_exceeded): Rate limit reached for default-gpt-4o in organization org-123 on tokens per minute (TPM): Limit 30,000, Used 28,000, Requested 4,000."),
			wantCode: ErrCodeRateLimit, // NOT ErrCodeContextLength
		},
		{
			name:     "max_tokens parameter too large",
			err:      errors.New("openai API error (400 invalid_request_error): max_tokens is too large: 50000. This model supports at most 16384 completion tokens."),
			wantCode: ErrCodeUnknown, // NOT ErrCodeContextLength
		},
		{
			name:     "Invalid request on temperature",
			err:      errors.New("openai API error (400 invalid_request_error): Invalid value for 'temperature': 3.0. Expected a value <= 2.0."),
			wantCode: ErrCodeUnknown, // NOT ErrCodeContextLength
		},
		{
			name:     "Untyped generic string mentioning context length must NOT trigger context error without typed error",
			err:      errors.New("failed to fetch context length from server"),
			wantCode: ErrCodeUnknown, // NOT ErrCodeContextLength
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, guidance := ClassifyError(tc.err)
			if code != tc.wantCode {
				t.Errorf("ClassifyError(%v) code = %q, want %q", tc.err, code, tc.wantCode)
			}
			if guidance == "" {
				t.Errorf("expected guidance for code %q, got empty", code)
			}
		})
	}
}
