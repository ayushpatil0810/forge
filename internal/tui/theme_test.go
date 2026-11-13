package tui_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/tui"
	"github.com/ayushpatil0810/forge/themes"
)

// parseHexColor parses a #rrggbb string into sRGB components (0.0 to 1.0).
func parseHexColor(hex string) (r, g, b float64, err error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid hex color length: %q", hex)
	}
	ri, err := strconv.ParseUint(hex[0:2], 16, 8)
	if err != nil {
		return 0, 0, 0, err
	}
	gi, err := strconv.ParseUint(hex[2:4], 16, 8)
	if err != nil {
		return 0, 0, 0, err
	}
	bi, err := strconv.ParseUint(hex[4:6], 16, 8)
	if err != nil {
		return 0, 0, 0, err
	}
	return float64(ri) / 255.0, float64(gi) / 255.0, float64(bi) / 255.0, nil
}

// relativeLuminance calculates WCAG 2.1 relative luminance from sRGB.
func relativeLuminance(r, g, b float64) float64 {
	convert := func(c float64) float64 {
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*convert(r) + 0.7152*convert(g) + 0.0722*convert(b)
}

// contrastRatio returns the WCAG 2.1 contrast ratio between two hex colors.
func contrastRatio(hex1, hex2 string) (float64, error) {
	r1, g1, b1, err := parseHexColor(hex1)
	if err != nil {
		return 0, err
	}
	r2, g2, b2, err := parseHexColor(hex2)
	if err != nil {
		return 0, err
	}
	l1 := relativeLuminance(r1, g1, b1)
	l2 := relativeLuminance(r2, g2, b2)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05), nil
}

func TestThemeContrastRatio(t *testing.T) {
	themesToTest := []string{"dark", "light", "catppuccin-mocha", "tokyo-night"}

	for _, name := range themesToTest {
		t.Run(name, func(t *testing.T) {
			th, err := tui.LoadTheme(name)
			if err != nil {
				t.Fatalf("failed to load theme %q: %v", name, err)
			}

			bg := string(th.Background)
			tokens := map[string]string{
				"foreground": string(th.Foreground),
				"primary":    string(th.Primary),
				"secondary":  string(th.Secondary),
				"accent":     string(th.Accent),
				"muted":      string(th.Muted),
				"error":      string(th.Error),   // diff del
				"success":    string(th.Success), // diff add
				"warning":    string(th.Warning),
			}

			for tokenName, hex := range tokens {
				ratio, err := contrastRatio(hex, bg)
				if err != nil {
					t.Fatalf("contrast calculation failed for %s (%s): %v", tokenName, hex, err)
				}
				if ratio < 4.5 {
					t.Errorf("theme %q %s contrast ratio %.2f:1 < 4.5:1 against background %s (color: %s)", name, tokenName, ratio, bg, hex)
				}
			}
		})
	}
}

func TestThemeRequiredTokens(t *testing.T) {
	entries, err := themes.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("failed to read embedded themes dir: %v", err)
	}

	requiredTokens := []string{
		"name",
		"is_dark",
		"background",
		"foreground",
		"primary",
		"secondary",
		"accent",
		"muted",
		"error",
		"success",
		"warning",
		"border",
		"glamour_style",
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := themes.FS.ReadFile(entry.Name())
			if err != nil {
				t.Fatalf("failed to read %s: %v", entry.Name(), err)
			}
			var m map[string]interface{}
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("failed to unmarshal %s: %v", entry.Name(), err)
			}

			for _, token := range requiredTokens {
				val, exists := m[token]
				if !exists {
					t.Errorf("theme %s is missing required token %q", entry.Name(), token)
					continue
				}
				if token == "is_dark" {
					if _, ok := val.(bool); !ok {
						t.Errorf("theme %s token %q expected bool, got %T (%v)", entry.Name(), token, val, val)
					}
				} else {
					strVal, ok := val.(string)
					if !ok || strings.TrimSpace(strVal) == "" {
						t.Errorf("theme %s token %q must be non-empty string, got %T (%v)", entry.Name(), token, val, val)
					}
				}
			}
		})
	}
}

func TestThemeLoading(t *testing.T) {
	for _, name := range []string{"dark", "light", "catppuccin-mocha", "tokyo-night", "auto", "monochrome"} {
		th, err := tui.LoadTheme(name)
		if err != nil {
			t.Fatalf("LoadTheme(%q) failed: %v", name, err)
		}
		if name != "monochrome" && string(th.Background) == "" {
			t.Errorf("expected non-empty background for theme %q", name)
		}
	}

	_, err := tui.LoadTheme("nonexistent-theme-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent theme")
	}
}

func TestNoHardcodedColorLiteralsInTUI(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("failed to glob go files: %v", err)
	}

	hexColorRegex := regexp.MustCompile(`#[0-9a-fA-F]{6}\b`)

	for _, file := range files {
		// Only theme.go and test files are allowed to contain color literals
		if file == "theme.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("failed to read %s: %v", file, err)
		}
		matches := hexColorRegex.FindAllString(string(data), -1)
		if len(matches) > 0 {
			t.Errorf("file %s contains hardcoded color literals %v; use semantic tokens from Theme/Styles instead", file, matches)
		}
	}
}
