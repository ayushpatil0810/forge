package agent

import (
	"errors"
	"testing"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		err      error
		wantCode string
	}{
		{errors.New("anthropic: ANTHROPIC_API_KEY environment variable is required"), ErrCodeAuth},
		{errors.New("HTTP 401: Unauthorized access"), ErrCodeAuth},
		{errors.New("HTTP 429: rate limit exceeded, retry after 20s"), ErrCodeRateLimit},
		{errors.New("insufficient quota or credits remaining"), ErrCodeQuota},
		{errors.New("context length exceeded maximum allowed 128000 tokens"), ErrCodeContextLength},
		{errors.New("dial tcp 1.2.3.4:443: connect: connection refused"), ErrCodeNetwork},
		{errors.New("loop guard: runaway loop detected"), ErrCodeLoopGuard},
		{errors.New("something totally unexpected happened"), ErrCodeUnknown},
	}

	for _, tc := range cases {
		code, guidance := ClassifyError(tc.err)
		if code != tc.wantCode {
			t.Errorf("ClassifyError(%v) code = %q, want %q", tc.err, code, tc.wantCode)
		}
		if guidance == "" {
			t.Errorf("expected guidance for code %q, got empty", code)
		}
	}
}
