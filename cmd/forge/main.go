package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/config"
	"github.com/ayushpatil0810/forge/internal/llm"
	_ "github.com/ayushpatil0810/forge/internal/llm/anthropic"
	_ "github.com/ayushpatil0810/forge/internal/llm/gemini"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	_ "github.com/ayushpatil0810/forge/internal/llm/openai"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
	"github.com/ayushpatil0810/forge/internal/tui"
)

var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"

	rootFlag     string
	demoFlag     bool
	yesFlag      bool
	yoloFlag     bool
	providerFlag string
	modelFlag    string
	shellFlag    string
	themeFlag    string
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "forge [flags] [prompt]",
		Short:        "Forge is a provider-agnostic terminal AI coding agent",
		Version:      version,
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.Join(args, " ")
			return runForge(cmd.Context(), prompt)
		},
	}

	cmd.SetVersionTemplate(fmt.Sprintf("forge {{.Version}} (commit: %s, date: %s)\n", commit, date))
	cmd.PersistentFlags().StringVar(&rootFlag, "root", ".", "workspace root directory")
	cmd.PersistentFlags().BoolVar(&demoFlag, "demo", false, "use a scripted mock provider (no API key needed)")
	cmd.PersistentFlags().BoolVar(&yesFlag, "yes", false, "auto-approve file edits and ordinary commands in headless mode")
	cmd.PersistentFlags().BoolVar(&yoloFlag, "yolo", false, "run in YOLO mode (auto-approve all actions, including dangerous commands)")
	cmd.PersistentFlags().StringVar(&providerFlag, "provider", "", "LLM provider: anthropic, openai, gemini, mock (overrides config)")
	cmd.PersistentFlags().StringVar(&modelFlag, "model", "", "model ID to use (overrides config)")
	cmd.PersistentFlags().StringVar(&shellFlag, "shell", "", "shell to use for command execution (e.g. pwsh, powershell.exe, bash)")
	cmd.PersistentFlags().StringVar(&themeFlag, "theme", "", "color theme: dark, light, catppuccin-mocha, tokyo-night, auto, monochrome")

	cmd.AddCommand(newConfigCmd())

	return cmd
}

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Print the effective configuration with secrets redacted",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.LoadOptions{
				WorkspaceRoot: rootFlag,
				Provider:      providerFlag,
				Model:         modelFlag,
				Shell:         shellFlag,
				Theme:         themeFlag,
			})
			if err != nil {
				return err
			}
			for _, w := range cfg.Warnings {
				cmd.PrintErrf("Warning: %s\n", w)
			}
			data, err := json.MarshalIndent(cfg.Redacted(), "", "  ")
			if err != nil {
				return err
			}
			cmd.Println(string(data))
			return nil
		},
	}
}

func decideHeadlessApproval(isDangerous, yes, yolo bool) (agent.Decision, string) {
	if isDangerous {
		if yolo {
			return agent.DecisionAllow, ""
		}
		return agent.DecisionDeny, "dangerous command requires --yolo"
	}
	if yes || yolo {
		return agent.DecisionAllow, ""
	}
	return agent.DecisionDeny, "approval requested in headless mode; use --yes to approve"
}

func setupDemoWorkspace() (string, error) {
	dir, err := os.MkdirTemp("", "forge-demo-*")
	if err != nil {
		return "", err
	}
	goMod := "module example.com/demo\n\ngo 1.22\n"
	code := `package demo

// CalculateDiscount returns the discount percentage for a given order amount.
func CalculateDiscount(amount float64) float64 {
	if amount > 100 {
		return 0.15
	}
	return 0.0
}
`
	test := `package demo

import "testing"

func TestDiscount(t *testing.T) {
	// Orders >= $100 should receive a 15% discount
	if d := CalculateDiscount(100.0); d != 0.15 {
		t.Fatalf("expected 15%% discount for orders >= 100, got %.0f%%", d*100)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "discount.go"), []byte(code), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "discount_test.go"), []byte(test), 0644); err != nil {
		return "", err
	}
	return dir, nil
}

func runForge(ctx context.Context, prompt string) error {
	cfg, err := config.Load(config.LoadOptions{
		WorkspaceRoot: rootFlag,
		Provider:      providerFlag,
		Model:         modelFlag,
		Shell:         shellFlag,
		Theme:         themeFlag,
	})
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	for _, w := range cfg.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}

	var provider llm.Provider
	var providerName, modelName string

	if demoFlag {
		demoDir, err := setupDemoWorkspace()
		if err == nil && rootFlag == "." {
			cfg.WorkspaceRoot = demoDir
		}
		providerName = "mock"
		modelName = "demo"
		provider = mock.New(
			mock.Turn{
				mock.Text("I will inspect the failing tests in this repository using `bash`.\n"),
				mock.ToolUse("demo1", "bash", map[string]any{"command": "go test ./..."}),
			},
			mock.Turn{
				mock.Text("The test failed: `TestDiscount` expected 15% discount for orders >= 100. Let me read `discount.go`.\n"),
				mock.ToolUse("demo2", "read_file", map[string]any{"path": "discount.go"}),
			},
			mock.Turn{
				mock.Text("The logic currently uses `amount > 100`, excluding orders of exactly 100. Let me fix the boundary condition.\n"),
				mock.ToolUse("demo3", "edit_file", map[string]any{
					"path":       "discount.go",
					"old_string": "if amount > 100 {",
					"new_string": "if amount >= 100 {",
				}),
			},
			mock.Turn{
				mock.Text("The code has been corrected. Re-running the test suite to verify.\n"),
				mock.ToolUse("demo4", "bash", map[string]any{"command": "go test -v ./..."}),
			},
			mock.Turn{
				mock.Text("All tests are passing! I have fixed the boundary condition in `discount.go` so that orders of exactly 100 receive the intended 15% discount."),
			},
		)
	} else {
		providerName = cfg.Provider
		modelName = cfg.Model
		p, err := llm.NewProvider(llm.ProviderConfig{
			Provider: cfg.Provider,
			Model:    cfg.Model,
		})
		if err != nil {
			return err
		}
		provider = p
	}

	sh := tools.DetectShell(cfg.Shell)

	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = agent.BuildSystemPrompt(cfg.WorkspaceRoot, sh)
	}

	permMode := permission.ModeDefault
	if yoloFlag {
		permMode = permission.ModeYOLO
	} else if yesFlag {
		permMode = permission.ModeAcceptEdits
	} else if cfg.PermissionMode != "" {
		permMode = permission.Mode(cfg.PermissionMode)
	}

	s := agent.NewLocal(agent.Config{
		Provider: provider,
		Tools:    tools.DefaultTools(cfg.WorkspaceRoot, sh),
		Policy:   permission.Policy{Mode: permMode},
		System:   systemPrompt,
	})
	defer s.Close()

	// If no prompt is provided, start the interactive Bubble Tea TUI
	if strings.TrimSpace(prompt) == "" {
		th, err := tui.LoadTheme(cfg.Theme)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v (falling back to dark)\n", err)
			th = tui.DarkTheme()
		}
		m := tui.New(s, th)
		m.SetProviderInfo(providerName, modelName)
		m.SetVersion(version)
		m.SetGitBranch(tui.DetectGitBranch(cfg.WorkspaceRoot))
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running TUI: %w", err)
		}
		return nil
	}

	// Headless / non-interactive execution for direct prompt
	if err := s.Send(ctx, prompt); err != nil {
		return err
	}

	for ev := range s.Events() {
		switch ev.Type {
		case agent.EventTokenDelta:
			if ev.Token != nil {
				fmt.Print(ev.Token.Text)
			}
		case agent.EventToolStarted:
			if ev.ToolStart != nil {
				fmt.Printf("\n● %s %s\n", ev.ToolStart.Name, ev.ToolStart.Input)
			}
		case agent.EventApprovalRequested:
			if ev.Approval != nil {
				d, reason := decideHeadlessApproval(ev.Approval.IsDangerous, yesFlag, yoloFlag)
				if reason != "" {
					fmt.Printf("  [denied: %s]\n", reason)
				}
				_ = s.Respond(ev.Approval.ID, d)
			}
		case agent.EventToolFinished:
			if ev.ToolDone != nil {
				fmt.Printf("  ↳ %s (%dms)\n", ev.ToolDone.Status, ev.ToolDone.DurationMs)
			}
		case agent.EventDone:
			return nil
		case agent.EventError:
			if ev.Err != nil {
				return fmt.Errorf("error [%s]: %s", ev.Err.Code, ev.Err.Message)
			}
			return fmt.Errorf("agent error")
		}
	}
	return nil
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
