package wrongimport

import (
	"fmt"
	"string" // BUG: should be "strings"
)

func FormatBanner(name string) string {
	clean := strings.TrimSpace(name)
	return fmt.Sprintf("=== %s ===", clean)
}
