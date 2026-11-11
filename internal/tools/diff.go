package tools

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// GenerateDiff creates a unified diff between oldContent and newContent.
// If oldContent is empty, it treats it as a newly created file.
func GenerateDiff(oldPath, newPath, oldContent, newContent string) string {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldContent),
		B:        difflib.SplitLines(newContent),
		FromFile: oldPath,
		ToFile:   newPath,
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

