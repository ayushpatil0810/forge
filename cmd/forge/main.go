package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/config"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/anthropic"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
	"github.com/ayushpatil0810/forge/internal/tui"
)

var (
	version = "0.1.0-dev"

	rootFlag  string
	demoFlag  bool
	yesFlag   bool
	modelFlag string
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

	cmd.SetVersionTemplate("forge {{.Version}}\n")
	cmd.Flags().StringVar(&rootFlag, "root", ".", "workspace root directory")
	cmd.Flags().BoolVar(&demoFlag, "demo", false, "use a scripted mock provider (no API key needed)")
	cmd.Flags().BoolVar(&yesFlag, "yes", false, "auto-approve every action")
	cmd.Flags().StringVar(&modelFlag, "model", "", "model ID to use (overrides config)")

	return cmd
}

func runForge(ctx context.Context, prompt string) error {
	cfg, err := config.Load(config.LoadOptions{
		WorkspaceRoot: rootFlag,
		Model:         modelFlag,
	})
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var provider llm.Provider
	if demoFlag {
		provider = mock.New(
			mock.Turn{
				mock.Text("Let me look at go.mod first.\n"),
				mock.ToolUse("demo1", "read_file", map[string]any{"path": "go.mod"}),
			},
			mock.Turn{
				mock.Text("That's a Go module. The engine loop works end to end.\n"),
			},
			mock.Turn{
				mock.Text("Here is an example code block:\n\n```go\npackage main\n\nfunc main() {\n    println(\"Hello from Forge!\")\n}\n```\n"),
			},
		)
	} else {
		anthropicProvider, err := anthropic.New(anthropic.Config{
			Model: cfg.Model,
		})
		if err != nil {
			return err
		}
		provider = anthropicProvider
	}

	s := agent.NewLocal(agent.Config{
		Provider: provider,
		Tools:    tools.NewRegistry(tools.ReadFile{Root: cfg.WorkspaceRoot}),
		Policy:   permission.Policy{Mode: permission.ModeDefault},
		System:   cfg.SystemPrompt,
	})
	defer s.Close()

	// If no prompt is provided, start the interactive Bubble Tea TUI
	if strings.TrimSpace(prompt) == "" {
		m := tui.New(s, tui.AdaptiveTheme())
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

	stdin := bufio.NewReader(os.Stdin)
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
			d := agent.DecisionAllow
			if !yesFlag && ev.Approval != nil {
				fmt.Printf("  allow %s? [y/N] ", ev.Approval.Tool)
				line, _ := stdin.ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
					d = agent.DecisionDeny
				}
			}
			if ev.Approval != nil {
				_ = s.Respond(ev.Approval.ID, d)
			}
		case agent.EventToolFinished:
			if ev.ToolDone != nil {
				fmt.Printf("  ↳ %s (%dms)\n", ev.ToolDone.Status, ev.ToolDone.DurationMs)
			}
		case agent.EventError:
			if ev.Err != nil {
				return fmt.Errorf("error [%s]: %s", ev.Err.Code, ev.Err.Message)
			}
			return fmt.Errorf("unknown error")
		case agent.EventDone:
			return nil
		}
	}
	return nil
}

func main() {
	cmd := newRootCmd()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
