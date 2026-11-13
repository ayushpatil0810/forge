package repocheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIPinsGolangciV2AndGoMatrix(t *testing.T) {
	root := repoRoot(t)
	yml, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(yml)
	if !strings.Contains(text, "golangci/golangci-lint-action@v9") {
		t.Fatal("CI is not using golangci-lint-action v9, which supports golangci-lint v2")
	}
	if !strings.Contains(text, "version: v2.15.0") && !strings.Contains(text, "version: v2.1") {
		t.Fatal("CI golangci-lint pin is older than v2.15.0")
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	line := ""
	for _, l := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(l, "go ") {
			line = strings.TrimPrefix(l, "go ")
			break
		}
	}
	majorMinor := line
	if i := strings.LastIndex(majorMinor, "."); i > 0 {
		parts := strings.Split(majorMinor, ".")
		if len(parts) >= 2 {
			majorMinor = parts[0] + "." + parts[1]
		}
	}
	if !strings.Contains(text, majorMinor+".x") || !strings.Contains(text, "stable") {
		t.Fatalf("CI Go matrix should pin %s.x and stable", majorMinor)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
