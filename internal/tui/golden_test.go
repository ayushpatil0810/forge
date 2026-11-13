package tui_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tui"
)

func isUpdateGolden() bool {
	f := flag.Lookup("update")
	if f != nil {
		if b, ok := f.Value.(flag.Getter); ok {
			if v, ok := b.Get().(bool); ok {
				return v
			}
		}
	}
	return false
}

func compareOrUpdateGolden(t *testing.T, goldenPath string, actual string) {
	t.Helper()
	actualNorm := strings.ReplaceAll(actual, "\r\n", "\n")
	if isUpdateGolden() {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("failed to create golden directory: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(actualNorm), 0o644); err != nil {
			t.Fatalf("failed to write golden file %s: %v", goldenPath, err)
		}
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
				t.Fatalf("failed to create golden directory: %v", err)
			}
			if err := os.WriteFile(goldenPath, []byte(actualNorm), 0o644); err != nil {
				t.Fatalf("failed to write initial golden file %s: %v", goldenPath, err)
			}
			return
		}
		t.Fatalf("failed to read golden file %s: %v", goldenPath, err)
	}

	expectedNorm := strings.ReplaceAll(string(expected), "\r\n", "\n")
	if expectedNorm != actualNorm {
		t.Errorf("golden mismatch for %s:\n--- Expected ---\n%s\n--- Actual ---\n%s", goldenPath, expectedNorm, actualNorm)
	}
}

func TestMainViewGoldenSnapshots(t *testing.T) {
	widths := []int{60, 80, 120}
	themes := []struct {
		name  string
		theme tui.Theme
	}{
		{"dark", tui.DarkTheme()},
		{"light", tui.LightTheme()},
	}

	for _, th := range themes {
		for _, w := range widths {
			testName := fmt.Sprintf("%s_%d", th.name, w)
			t.Run(testName, func(t *testing.T) {
				p := mock.New()
				s := agent.NewLocal(agent.Config{Provider: p})
				defer s.Close()

				m := tui.New(s, th.theme)
				m.SetProviderInfo("openai", "gpt-4o")
				m.SetVersion("0.1.0")
				m.SetGitBranch("main")

				newM, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
				m = newM.(tui.Model)

				view := tui.StripANSI(m.View())
				goldenPath := filepath.Join("testdata", "golden", fmt.Sprintf("main_%d_%s.golden", w, th.name))
				compareOrUpdateGolden(t, goldenPath, view)
			})
		}
	}
}

func TestErrorClassGoldenSnapshots(t *testing.T) {
	errorClasses := []struct {
		code     string
		message  string
		guidance string
	}{
		{
			code:     "auth_failed",
			message:  "Invalid or missing API key for openai",
			guidance: "Set OPENAI_API_KEY environment variable. See docs/configuration.md.",
		},
		{
			code:     "rate_limit_exceeded",
			message:  "HTTP 429: Requests per minute limit exceeded",
			guidance: "Provider rate limit reached. Forge automatically retries with exponential backoff.",
		},
		{
			code:     "network_error",
			message:  "dial tcp 1.2.3.4:443: connect: connection refused",
			guidance: "Failed to connect to model provider. Check network connection or proxy.",
		},
		{
			code:     "quota_exhausted",
			message:  "Insufficient quota or credit balance",
			guidance: "Your provider balance is exhausted. Check provider billing console.",
		},
		{
			code:     "context_length_exceeded",
			message:  "Conversation exceeded maximum model tokens (128000)",
			guidance: "Switch to a higher-context model (e.g. --model) or start a new session.",
		},
		{
			code:     "loop_guard",
			message:  "Execution halted: agent took 10 tool calls without user intervention",
			guidance: "Loop guard triggered to prevent runaway execution.",
		},
		{
			code:     "config_error",
			message:  "Unknown configuration parameter 'max_depth' in .forge/config.json",
			guidance: "Run 'forge config' to inspect current settings and schema.",
		},
		{
			code:     "tool_error",
			message:  "bash: exit status 127: command not found: pytest",
			guidance: "Ensure test runner is installed in active environment or PATH.",
		},
	}

	for _, ec := range errorClasses {
		t.Run(ec.code, func(t *testing.T) {
			p := mock.New()
			s := agent.NewLocal(agent.Config{Provider: p})
			defer s.Close()

			m := tui.New(s, tui.DarkTheme())
			m.SetProviderInfo("openai", "gpt-4o")
			m.SetVersion("0.1.0")
			m.SetGitBranch("main")

			newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m = newM.(tui.Model)

			// Dispatch error event
			newM, _ = m.Update(agent.Event{
				Type: agent.EventError,
				Err: &agent.ErrorPayload{
					Code:     ec.code,
					Message:  ec.message,
					Guidance: ec.guidance,
				},
			})
			m = newM.(tui.Model)

			view := tui.StripANSI(m.View())
			goldenPath := filepath.Join("testdata", "golden", fmt.Sprintf("error_%s.golden", ec.code))
			compareOrUpdateGolden(t, goldenPath, view)
		})
	}
}

func TestNoFooterOrBannerLineExceedsTerminalWidth(t *testing.T) {
	widths := []int{40, 60, 80, 120}
	modes := []permission.Mode{
		permission.ModeAsk,
		permission.ModeAcceptEdits,
		permission.ModePlan,
		permission.ModeYOLO,
	}

	for _, w := range widths {
		for _, m := range modes {
			t.Run(fmt.Sprintf("w%d_%s", w, m), func(t *testing.T) {
				p := mock.New()
				s := agent.NewLocal(agent.Config{Provider: p})
				defer s.Close()
				s.SetMode(m)

				model := tui.New(s, tui.DarkTheme())
				model.SetProviderInfo("anthropic", "claude-3-5-sonnet-20241022")
				model.SetVersion("0.1.0-alpha")
				model.SetGitBranch("feature/checkpoints-and-undo")

				newM, _ := model.Update(tea.WindowSizeMsg{Width: w, Height: 24})
				model = newM.(tui.Model)

				view := tui.StripANSI(model.View())
				lines := strings.Split(view, "\n")
				for idx, line := range lines {
					lineVisualWidth := runewidth.StringWidth(line)
					if lineVisualWidth > w {
						t.Errorf("line %d visual width %d > terminal width %d:\n%q", idx+1, lineVisualWidth, w, line)
					}
				}
			})
		}
	}
}

func TestASCIIFallbackForDumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")

	p := mock.New()
	s := agent.NewLocal(agent.Config{Provider: p})
	defer s.Close()

	model := tui.New(s, tui.DarkTheme())
	model.SetProviderInfo("openai", "gpt-4o")
	model.SetVersion("0.1.0")
	model.SetGitBranch("main")

	newM, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = newM.(tui.Model)

	// Add error card and user message
	newM, _ = model.Update(agent.Event{
		Type: agent.EventError,
		Err: &agent.ErrorPayload{
			Code:     "auth_failed",
			Message:  "No API key provided",
			Guidance: "Set OPENAI_API_KEY",
		},
	})
	model = newM.(tui.Model)

	view := tui.StripANSI(model.View())

	// Verify all runes in output are plain ASCII (<= 127)
	for i, r := range view {
		if r > 127 && !unicode.IsSpace(r) {
			t.Errorf("found non-ASCII rune %q (U+%04X) at index %d in dumb terminal mode:\n%s", r, r, i, view)
			break
		}
	}

	// Verify tool card ASCII fallback
	tc := tui.NewToolCard("call_1", "read_file", `{"path":"main.go"}`)
	tc.Status = agent.ToolOK
	styles := tui.NewStyles(tui.DarkTheme())
	renderedTC := tui.StripANSI(tc.Render(80, false, styles))

	for i, r := range renderedTC {
		if r > 127 && !unicode.IsSpace(r) {
			t.Errorf("tool card contains non-ASCII rune %q (U+%04X) at index %d under TERM=dumb", r, r, i)
			break
		}
	}
}
