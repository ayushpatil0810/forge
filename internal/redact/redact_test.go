package redact

import (
	"strings"
	"testing"
)

func TestRedact_Positives(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "OpenAI sk-proj key",
			input:    "Authorization: Bearer sk-proj-1234567890abcdef1234567890abcdef1234567890abcdef",
			expected: "Authorization: Bearer [REDACTED:openai_key]",
		},
		{
			name:     "OpenAI standard sk- key",
			input:    "key = sk-1234567890abcdef1234567890abcdef12",
			expected: "key = [REDACTED:openai_key]",
		},
		{
			name:     "Anthropic sk-ant key",
			input:    "x-api-key: sk-ant-api03-1234567890abcdef1234567890abcdef1234567890",
			expected: "x-api-key: [REDACTED:anthropic_key]",
		},
		{
			name:     "Gemini AIzaSy key",
			input:    "curl https://generativelanguage.googleapis.com/v1beta/models?key=AIzaSyA1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q",
			expected: "curl https://generativelanguage.googleapis.com/v1beta/models?key=[REDACTED:gemini_key]",
		},
		{
			name:     "AWS Access Key",
			input:    "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
			expected: "AWS_ACCESS_KEY_ID=[REDACTED:aws_key]",
		},
		{
			name:     "GitHub Classic PAT",
			input:    "ghp_1234567890abcdefghijklmnopqrstuvwxyz",
			expected: "[REDACTED:github_token]",
		},
		{
			name:     "GitHub Fine-Grained PAT",
			input:    "github_pat_11ABCDEFG0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopq_rstuvwxyz",
			expected: "[REDACTED:github_token]",
		},
		{
			name:     "PEM Private Key Block",
			input:    "header\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA0Y123456...\n-----END RSA PRIVATE KEY-----\nfooter",
			expected: "header\n[REDACTED:private_key]\nfooter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.input)
			if got != tt.expected {
				t.Fatalf("Redact(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRedact_Negatives(t *testing.T) {
	negatives := []struct {
		name  string
		input string
	}{
		{
			name:  "Git commit 40-char SHA",
			input: "commit 4844ead0c09260c6c8e370a2f7c006ef50a9ff3d",
		},
		{
			name:  "Git 64-char SHA256",
			input: "sha256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:  "UUID v4",
			input: "session id: 123e4567-e89b-12d3-a456-426614174000",
		},
		{
			name:  "Base64 encoded payload without key prefix",
			input: "data: SGVsbG8gV29ybGQhIFRoaXMgaXMgYSB0ZXN0IG1lc3NhZ2Uu",
		},
		{
			name:  "Regular URL",
			input: "https://api.github.com/repos/ayushpatil0810/forge/issues",
		},
		{
			name:  "Source code identifier",
			input: "type SessionRecord struct { ID string }",
		},
	}

	for _, tt := range negatives {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.input)
			if got != tt.input {
				t.Fatalf("false positive redaction on %s: got %q, want %q", tt.name, got, tt.input)
			}
		})
	}
}

func TestRedact_DynamicSecrets(t *testing.T) {
	ClearSecrets()
	RegisterSecret("my-super-secret-password-12345")
	defer ClearSecrets()

	input := "connecting with pass my-super-secret-password-12345 to db"
	want := "connecting with pass [REDACTED:secret] to db"
	got := Redact(input)
	if got != want {
		t.Fatalf("dynamic secret not redacted: got %q, want %q", got, want)
	}
}

func TestRedact_Idempotency(t *testing.T) {
	input := "Bearer sk-proj-1234567890abcdef1234567890abcdef1234567890abcdef and AKIAIOSFODNN7EXAMPLE"
	first := Redact(input)
	second := Redact(first)
	if first != second {
		t.Fatalf("Redact is not idempotent: first = %q, second = %q", first, second)
	}
}

func FuzzRedact(f *testing.F) {
	f.Add("plain text")
	f.Add("sk-proj-1234567890abcdef1234567890abcdef1234567890abcdef")
	f.Add("AKIAIOSFODNN7EXAMPLE")
	f.Add("-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----")
	f.Add("4844ead0c09260c6c8e370a2f7c006ef50a9ff3d")
	f.Add("")
	f.Add(strings.Repeat("a", 10000))

	f.Fuzz(func(t *testing.T, s string) {
		r1 := Redact(s)
		r2 := Redact(r1)
		if r1 != r2 {
			t.Fatalf("Redact was not idempotent for input %q: %q vs %q", s, r1, r2)
		}
	})
}
