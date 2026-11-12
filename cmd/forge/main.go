package main

import (
	"context"
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

	rootFlag     string
	demoFlag     bool
	yesFlag      bool
	yoloFlag     bool
	providerFlag string
	modelFlag    string
	shellFlag    string
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
	cmd.Flags().BoolVar(&yesFlag, "yes", false, "auto-approve file edits and ordinary commands in headless mode")
	cmd.Flags().BoolVar(&yoloFlag, "yolo", false, "run in YOLO mode (auto-approve all actions, including dangerous commands)")
	cmd.Flags().StringVar(&providerFlag, "provider", "", "LLM provider: anthropic, openai, gemini, mock (overrides config)")
	cmd.Flags().StringVar(&modelFlag, "model", "", "model ID to use (overrides config)")
	cmd.Flags().StringVar(&shellFlag, "shell", "", "shell to use for command execution (e.g. pwsh, powershell.exe, bash)")

	return cmd
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

func runForge(ctx context.Context, prompt string) error {
	cfg, err := config.Load(config.LoadOptions{
		WorkspaceRoot: rootFlag,
		Provider:      providerFlag,
		Model:         modelFlag,
		Shell:         shellFlag,
	})
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var provider llm.Provider
	var providerName, modelName string

	if demoFlag {
		providerName = "mock"
		modelName = "demo"
		sampleFile := "go.mod"
		if _, err := os.Stat(filepath.Join(cfg.WorkspaceRoot, "go.mod")); err != nil {
			if _, err := os.Stat(filepath.Join(cfg.WorkspaceRoot, "main.go")); err == nil {
				sampleFile = "main.go"
			}
		}
		provider = mock.New(
			mock.Turn{
				mock.Text("Let me inspect the workspace directory structure first.\n"),
				mock.ToolUse("demo1", "list_dir", map[string]any{"path": "."}),
			},
			mock.Turn{
				mock.Text("Now searching for Go entry points...\n"),
				mock.ToolUse("demo2", "grep", map[string]any{"query": "func main"}),
			},
			mock.Turn{
				mock.Text(fmt.Sprintf("Reading project configuration file `%s`:\n", sampleFile)),
				mock.ToolUse("demo3", "read_file", map[string]any{"path": sampleFile, "limit": 10}),
			},
			mock.Turn{
				mock.Text("I have explored your workspace! Here is what I discovered:\n\n- Directory contents retrieved using `list_dir`.\n- Located application entrypoints using `grep`.\n- Inspected module dependencies with `read_file`.\n\nPress **Ctrl+O** to toggle between collapsed and expanded tool call cards!"),
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
		m := tui.New(s, tui.AdaptiveTheme())
		m.SetProviderInfo(providerName, modelName)
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
