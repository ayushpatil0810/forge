package tools

import (
	"strings"
	"testing"
)

func FuzzEditMatcher(f *testing.F) {
	// Seed corpus
	f.Add("hello world\nthis is a test\nfoo bar\n", "this is a test", "this is verified", false)
	f.Add("repeat repeat repeat\n", "repeat", "single", true)
	f.Add("line1\r\nline2\r\nline3\r\n", "line2", "modified2", false)
	f.Add("exact indentation:\n    code here\nend", "    code here", "    new code here", false)

	f.Fuzz(func(t *testing.T, content, oldStr, newStr string, replaceAll bool) {
		if oldStr == "" || oldStr == newStr {
			return
		}
		// If oldStr is in content
		res, err := applyReplacement(content, oldStr, newStr, replaceAll)
		if err == nil {
			// Replacement succeeded
			if !replaceAll {
				// Should have replaced exactly 1 occurrence
				if strings.Count(content, oldStr) != 1 {
					t.Fatalf("expected exactly 1 occurrence for non-replaceAll, got %d", strings.Count(content, oldStr))
				}
			}
			// Result should not contain unmodified oldStr if replaceAll
			if replaceAll && !strings.Contains(newStr, oldStr) {
				if strings.Contains(res, oldStr) {
					t.Fatalf("res still contains oldStr after replaceAll")
				}
			}
		}
	})
}
