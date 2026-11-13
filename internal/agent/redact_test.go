package agent

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-actual-secret-key-123456789")

	cases := []struct {
		input string
		want  string
	}{
		{
			input: "Error with key sk-ant-api03-actual-secret-key-123456789 in request",
			want:  "[REDACTED:api_key]",
		},
		{
			input: "Failed using openai key sk-proj-abcdef12345678901234567890",
			want:  "[REDACTED:openai_key]",
		},
		{
			input: "Google API error with AIzaSyD1234567890abcdef1234567890ab",
			want:  "[REDACTED:gemini_key]",
		},
		{
			input: "GitHub auth failed for ghp_0123456789abcdefghijklmnopqrstuvwxyz",
			want:  "[REDACTED:github_token]",
		},
		{
			input: "Safe message: file not found at path /src/main.go",
			want:  "Safe message: file not found at path /src/main.go",
		},
	}

	for _, tc := range cases {
		got := RedactSecrets(tc.input)
		if !strings.Contains(got, tc.want) {
			t.Errorf("RedactSecrets(%q) = %q, expected to contain %q", tc.input, got, tc.want)
		}
	}
}
