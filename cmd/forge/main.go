package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
)

var (
	version = "0.1.0-dev"

	rootFlag string
	demoFlag bool
	yesFlag  bool
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
			if strings.TrimSpace(prompt) == "" {
				return cmd.Help()
			}
			return runForge(cmd.Context(), prompt)
		},
	}

	cmd.SetVersionTemplate("forge {{.Version}}\n")
	cmd.Flags().StringVar(&rootFlag, "root", ".", "workspace root directory")
	cmd.Flags().BoolVar(&demoFlag, "demo", false, "use a scripted mock provider (no API key needed)")
	cmd.Flags().BoolVar(&yesFlag, "yes", false, "auto-approve every action")

	return cmd
}

func runForge(ctx context.Context, prompt string) error {
	var provider llm.Provider
	if demoFlag {
		provider = mock.New(
			mock.Turn{mock.Text("Let me look at go.mod first.\n"), mock.ToolUse("demo1", "read_file", map[string]any{"path": "go.mod"})},
			mock.Turn{mock.Text("That's a Go module. The engine loop works end to end.\n")},
		)
	} else {
		return fmt.Errorf("no real provider wired up yet; run with --demo (next step: internal/llm/anthropic)")
	}

	s := agent.NewLocal(agent.Config{
		Provider: provider,
		Tools:    tools.NewRegistry(tools.ReadFile{Root: rootFlag}),
		Policy:   permission.Policy{Mode: permission.ModeDefault},
		System:   "You are a careful coding agent.",
	})
	defer s.Close()

	if err := s.Send(prompt); err != nil {
		return err
	}

	stdin := bufio.NewReader(os.Stdin)
	for ev := range s.Events() {
		switch ev.Type {
		case agent.EventTextDelta:
			fmt.Print(ev.Text)
		case agent.EventToolStarted:
			if ev.Tool != nil {
				fmt.Printf("\n● %s %s\n", ev.Tool.Name, ev.Tool.Input)
			}
		case agent.EventApprovalRequested:
			d := agent.DecisionAllow
			if !yesFlag && ev.Approval != nil {
				fmt.Printf("  allow %s? [y/N] ", ev.Approval.Tool.Name)
				line, _ := stdin.ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
					d = agent.DecisionDeny
				}
			}
			if ev.Approval != nil {
				_ = s.Respond(ev.Approval.ID, d)
			}
		case agent.EventToolFinished:
			if ev.Tool != nil {
				fmt.Printf("  ↳ %s (%dms)\n", ev.Tool.Status, ev.Tool.DurationMs)
			}
		case agent.EventError:
			return fmt.Errorf("error: %s", ev.Error)
		case agent.EventDone, agent.EventCancelled:
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
