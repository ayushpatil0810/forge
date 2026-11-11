package tools

import (
	"strings"
	"testing"
)

func TestGenerateDiff(t *testing.T) {
	oldContent := "line 1\nline 2\nline 3\n"
	newContent := "line 1\nline 2 modified\nline 3\n"

	diff := GenerateDiff("a.txt", "b.txt", oldContent, newContent)
	if !strings.Contains(diff, "-line 2") {
		t.Fatalf("expected diff to contain -line 2, got:\n%s", diff)
	}
	if !strings.Contains(diff, "+line 2 modified") {
		t.Fatalf("expected diff to contain +line 2 modified, got:\n%s", diff)
	}

	// Identical files produce empty diff
	sameDiff := GenerateDiff("a.txt", "b.txt", oldContent, oldContent)
	if sameDiff != "" {
		t.Fatalf("expected empty diff for identical content, got:\n%s", sameDiff)
	}

	// New file diff
	newFileDiff := GenerateDiff("/dev/null", "new.txt", "", "first line\n")
	if !strings.Contains(newFileDiff, "+first line") {
		t.Fatalf("expected new file diff to contain +first line, got:\n%s", newFileDiff)
	}
}

