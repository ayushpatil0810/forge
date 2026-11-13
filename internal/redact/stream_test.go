package redact

import (
	"strings"
	"testing"
)

func TestStream_SplitSecretAndKey(t *testing.T) {
	ClearSecrets()
	t.Cleanup(ClearSecrets)
	const secret = "super-secret-value"
	const key = "sk-abcdefghijklmnopqrstuvwxyz123456"
	RegisterSecret(secret)
	payload := "prefix " + secret + " middle " + key + " suffix"

	for _, step := range []int{1, 2, 3, 5, 8, len(payload)} {
		var deltas []string
		st := NewStream(func(chunk string) {
			deltas = append(deltas, chunk)
		})
		for i := 0; i < len(payload); i += step {
			end := i + step
			if end > len(payload) {
				end = len(payload)
			}
			st.Write(payload[i:end])
		}
		st.Flush()
		joined := strings.Join(deltas, "")
		for _, d := range deltas {
			if strings.Contains(d, secret) || strings.Contains(d, key) {
				t.Fatalf("step %d leaked in delta %q", step, d)
			}
		}
		if strings.Contains(joined, secret) || strings.Contains(joined, key) {
			t.Fatalf("step %d leaked across deltas: %q", step, joined)
		}
		if !strings.Contains(joined, "[REDACTED:secret]") || !strings.Contains(joined, "[REDACTED:openai_key]") {
			t.Fatalf("step %d missing redaction: %q", step, joined)
		}
	}
}

func FuzzStreamRedact(f *testing.F) {
	f.Add("hello sk-abcdefghijklmnopqrstuvwxyz123456\n")
	f.Add("plain text without a secret")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 1<<16 {
			t.Skip()
		}
		st := NewStream(func(string) {})
		step := 1
		if len(s) > 0 {
			step = int(s[0])%7 + 1
		}
		for i := 0; i < len(s); i += step {
			end := i + step
			if end > len(s) {
				end = len(s)
			}
			st.Write(s[i:end])
		}
		st.Flush()
	})
}
