package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/audit"
	"github.com/ayushpatil0810/forge/internal/checkpoint"
	"github.com/ayushpatil0810/forge/internal/config"
	"github.com/ayushpatil0810/forge/internal/credentials"
	"github.com/ayushpatil0810/forge/internal/llm"
	_ "github.com/ayushpatil0810/forge/internal/llm/anthropic"
	_ "github.com/ayushpatil0810/forge/internal/llm/gemini"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	_ "github.com/ayushpatil0810/forge/internal/llm/openai"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/policy"
	"github.com/ayushpatil0810/forge/internal/session"
	"github.com/ayushpatil0810/forge/internal/tools"
	"github.com/ayushpatil0810/forge/internal/tui"
)

var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"

	rootFlag         string
	demoFlag         bool
	demoSpeedFlag    float64
	yesFlag          bool
	yoloFlag         bool
	providerFlag     string
	modelFlag        string
	baseURLFlag      string
	shellFlag        string
	themeFlag        string
	resumeFlag       string
	noSessionFlag    bool
	systemPromptFlag string

	// observeSystemPrompt records the prompt each CLI entry point would send.
	// Tests set it; production leaves it nil.
	observeSystemPrompt func(entryPoint, prompt string)
	// stopAfterSystemPrompt returns before credentials, the network, or the TUI.
	stopAfterSystemPrompt bool
)

// ExitCodeError indicates a specific process exit code required by SPEC §3.7.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e ExitCodeError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

func checkForbiddenKeyFlags(args []string) error {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if strings.HasPrefix(lower, "--") && strings.Contains(lower, "key") && !strings.Contains(lower, "with-key") {
			return ExitCodeError{
				Code: 2,
				Err:  fmt.Errorf("API keys cannot be passed via command-line flags for security. Use 'forge auth login' or environment variables (e.g. %s)", credentials.EnvVarForProvider("openai")),
			}
		}
	}
	return nil
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "forge [flags] [prompt]",
		Short:        "Forge is a provider-agnostic terminal AI coding agent",
		Version:      version,
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return checkForbiddenKeyFlags(os.Args[1:])
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				prompt := strings.Join(args, " ")
				return runHeadless(cmd.Context(), prompt, "text", "", 0)
			}
			return runInteractive(cmd.Context())
		},
	}

	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "key") && !strings.Contains(errMsg, "with-key") {
			return ExitCodeError{
				Code: 2,
				Err:  fmt.Errorf("API keys cannot be passed via command-line flags for security. Use 'forge auth login' or environment variables (e.g. %s)", credentials.EnvVarForProvider("openai")),
			}
		}
		if forbiddenErr := checkForbiddenKeyFlags(os.Args[1:]); forbiddenErr != nil {
			return forbiddenErr
		}
		return ExitCodeError{Code: 2, Err: err}
	})

	cmd.SetVersionTemplate(fmt.Sprintf("forge {{.Version}} (commit: %s, date: %s)\n", commit, date))
	cmd.PersistentFlags().StringVar(&rootFlag, "root", ".", "workspace root directory")
	cmd.PersistentFlags().BoolVar(&demoFlag, "demo", false, "use a scripted mock provider (no API key needed)")
	cmd.PersistentFlags().Float64Var(&demoSpeedFlag, "demo-speed", 1.0, "playback speed multiplier for demo (0 is instant)")
	cmd.PersistentFlags().BoolVar(&yesFlag, "yes", false, "auto-approve file edits and ordinary commands in headless mode")
	cmd.PersistentFlags().BoolVar(&yoloFlag, "yolo", false, "run in YOLO mode (auto-approve all actions, including dangerous commands)")
	cmd.PersistentFlags().StringVar(&providerFlag, "provider", "", "LLM provider: anthropic, openai, gemini, mock (overrides config)")
	cmd.PersistentFlags().StringVar(&modelFlag, "model", "", "model ID to use (overrides config)")
	cmd.PersistentFlags().StringVar(&baseURLFlag, "base-url", "", "custom API base URL (e.g. for Ollama http://localhost:11434/v1)")
	cmd.PersistentFlags().StringVar(&shellFlag, "shell", "", "shell to use for command execution (e.g. pwsh, powershell.exe, bash)")
	cmd.PersistentFlags().StringVar(&themeFlag, "theme", "", "color theme: dark, light, catppuccin-mocha, tokyo-night, auto, monochrome")
	cmd.PersistentFlags().StringVar(&resumeFlag, "resume", "", "resume a previous session by ID (or open picker if omitted)")
	cmd.PersistentFlags().Lookup("resume").NoOptDefVal = "last"
	cmd.PersistentFlags().BoolVar(&noSessionFlag, "no-session", false, "ephemeral run without persisting session to SQLite")
	cmd.PersistentFlags().StringVar(&systemPromptFlag, "system-prompt", "", "additional instructions appended after the built security prompt")

	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newCheckpointsCmd())
	cmd.AddCommand(newUndoCmd())
	cmd.AddCommand(newAuthCmd())
	cmd.AddCommand(newSessionsCmd())
	cmd.AddCommand(newPolicyCmd())
	cmd.AddCommand(newAuditCmd())

	return cmd
}

func newRunCmd() *cobra.Command {
	var (
		output         string
		permissionMode string
		maxTurns       int
	)

	cmd := &cobra.Command{
		Use:          "run [flags] <prompt>",
		Short:        "Run Forge headlessly on a single prompt (canonical headless form)",
		SilenceUsage: true,
		Args:         cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.Join(args, " ")
			return runHeadless(cmd.Context(), prompt, output, permissionMode, maxTurns)
		},
	}

	cmd.Flags().StringVar(&output, "output", "text", "output format: text, json, stream-json")
	cmd.Flags().StringVar(&permissionMode, "permission-mode", "", "permission mode: ask, accept-edits, plan, yolo")
	cmd.Flags().IntVar(&maxTurns, "max-turns", 0, "maximum number of conversation turns before stopping")
	cmd.Flags().StringVar(&resumeFlag, "resume", "", "resume a previous session by ID")

	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "key") && !strings.Contains(errMsg, "with-key") {
			return ExitCodeError{
				Code: 2,
				Err:  fmt.Errorf("API keys cannot be passed via command-line flags for security. Use 'forge auth login' or environment variables (e.g. %s)", credentials.EnvVarForProvider("openai")),
			}
		}
		if forbiddenErr := checkForbiddenKeyFlags(os.Args[1:]); forbiddenErr != nil {
			return forbiddenErr
		}
		return ExitCodeError{Code: 2, Err: err}
	})

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
				BaseURL:       baseURLFlag,
				Shell:         shellFlag,
				Theme:         themeFlag,
				SystemPrompt:  systemPromptFlag,
			})
			if err != nil {
				return ExitCodeError{Code: 2, Err: err}
			}
			for _, w := range cfg.Warnings {
				cmd.PrintErrf("Warning: %s\n", w)
			}
			if len(cfg.IgnoredProjectKeys) > 0 {
				cmd.PrintErrln("Ignored project config keys (security policy):")
				for k, reason := range cfg.IgnoredProjectKeys {
					cmd.PrintErrf("  - %s: %s\n", k, reason)
				}
			}
			data, err := json.MarshalIndent(cfg.Redacted(), "", "  ")
			if err != nil {
				return ExitCodeError{Code: 2, Err: err}
			}
			cmd.Println(string(data))
			return nil
		},
	}
}

func newCheckpointsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoints",
		Short: "Manage workspace shadow git checkpoints",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List recorded checkpoints for current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := checkpoint.New(rootFlag)
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			list, err := mgr.List(cmd.Context(), 25)
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			if len(list) == 0 {
				cmd.Println("No checkpoints found for this workspace.")
				return nil
			}
			cmd.Println("CHECKPOINT ID\tTIMESTAMP          \tMESSAGE")
			cmd.Println("------------------------------------------------------------")
			for _, cp := range list {
				cmd.Printf("%-13s\t%-19s\t%s\n", cp.ID, cp.Timestamp.Format("2006-01-02 15:04:05"), cp.Message)
			}
			return nil
		},
	}

	restoreCmd := &cobra.Command{
		Use:   "restore <checkpoint_id>",
		Short: "Restore workspace files to a specific checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			mgr, err := checkpoint.New(rootFlag)
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			info, err := mgr.Restore(cmd.Context(), id)
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			cmd.Printf("Successfully restored workspace to checkpoint %s\n", info.ID)
			return nil
		},
	}

	cmd.AddCommand(listCmd)
	cmd.AddCommand(restoreCmd)
	return cmd
}

func newUndoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "undo",
		Short: "Revert workspace files to the previous checkpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := checkpoint.New(rootFlag)
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			list, err := mgr.List(cmd.Context(), 2)
			if err != nil || len(list) == 0 {
				return ExitCodeError{Code: 1, Err: errors.New("no checkpoints available to undo")}
			}
			target := list[0]
			if len(list) > 1 {
				target = list[1] // Previous snapshot
			}
			info, err := mgr.Restore(cmd.Context(), target.ID)
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			cmd.Printf("Reverted workspace to checkpoint %s (%s)\n", info.ID, target.Message)
			return nil
		},
	}
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage model provider credentials and authentication",
	}

	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthLogoutCmd())

	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display authentication status across providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := credentials.NewManager(nil)
			providers := []string{"anthropic", "openai", "gemini", "ollama", "lmstudio"}

			cmd.Println("PROVIDER\tSTATUS        \tSOURCE    \tKEY")
			for _, p := range providers {
				if p == "ollama" || p == "lmstudio" {
					cmd.Printf("%-10s\t%-14s\t%-10s\t%s\n", p, "ready", "local", "(not required)")
					continue
				}
				res := mgr.Resolve(p)
				if res.Source != credentials.SourceNone && res.Key != "" {
					cmd.Printf("%-10s\t%-14s\t%-10s\t%s\n", p, "authenticated", string(res.Source), credentials.MaskKey(res.Key))
				} else {
					cmd.Printf("%-10s\t%-14s\t%-10s\t%s\n", p, "unconfigured", "none", "(not set)")
				}
			}
			return nil
		},
	}
}

func newAuthLoginCmd() *cobra.Command {
	var withKey bool
	var allowPlaintextFile bool
	cmd := &cobra.Command{
		Use:   "login [provider]",
		Short: "Store API credentials in the OS keychain",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := "anthropic"
			if len(args) > 0 {
				provider = strings.ToLower(args[0])
			}

			if provider == "ollama" || provider == "lmstudio" {
				cmd.Printf("Provider %q is a local server and does not require an API key.\n", provider)
				return nil
			}

			var key string
			if withKey {
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					key = strings.TrimSpace(scanner.Text())
				}
			} else {
				cmd.Printf("Enter API key for %s (input hidden): ", provider)
				bytePassword, err := term.ReadPassword(int(os.Stdin.Fd()))
				cmd.Println()
				if err != nil {
					return fmt.Errorf("reading key: %w", err)
				}
				key = strings.TrimSpace(string(bytePassword))
			}

			if key == "" {
				return errors.New("API key cannot be empty")
			}

			mgr := credentials.NewManager(nil)
			cmd.Printf("Verifying API key with %s...\n", provider)
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			if testErr := mgr.TestConnection(ctx, provider, key, ""); testErr != nil {
				cmd.PrintErrf("Warning: API key verification failed: %v\n", testErr)
			}

			source, err := mgr.Store(provider, key, allowPlaintextFile)
			if err != nil && credentials.IsFallbackFileSupported() && !allowPlaintextFile && !withKey && term.IsTerminal(int(os.Stdin.Fd())) {
				cmd.PrintErrln("WARNING: OS keychain is unavailable on this system.")
				cmd.PrintErr("Store key unencrypted in ~/.forge/credentials.json (mode 0600)? [y/N]: ")
				var answer string
				_, _ = fmt.Scanln(&answer)
				if strings.ToLower(strings.TrimSpace(answer)) == "y" {
					source, err = mgr.Store(provider, key, true)
				}
			}
			if err != nil {
				return fmt.Errorf("storing credential: %w", err)
			}

			if source == credentials.SourceFile {
				cmd.PrintErrln("NOTICE: Key stored unencrypted in ~/.forge/credentials.json (mode 0600).")
			}
			cmd.Printf("Successfully saved API key for %s (%s, masked: %s).\n", provider, source, credentials.MaskKey(key))
			return nil
		},
	}
	cmd.Flags().BoolVar(&withKey, "with-key", false, "read API key from stdin instead of interactive prompt")
	cmd.Flags().BoolVar(&allowPlaintextFile, "allow-plaintext-key-file", false, "consent to unencrypted 0600 file storage if OS keychain is unavailable (Linux only)")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout [provider]",
		Short: "Remove stored API credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := strings.ToLower(args[0])
			mgr := credentials.NewManager(nil)
			if err := mgr.Delete(provider); err != nil {
				return err
			}
			cmd.Printf("Successfully removed stored credentials for %s.\n", provider)
			return nil
		},
	}
}

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage saved conversation sessions",
	}

	var allFlag bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List recorded sessions for current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := session.NewStore("")
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			defer func() { _ = store.Close() }()

			list, err := store.ListSessions(cmd.Context(), session.CanonicalWorkspace(rootFlag), allFlag)
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			if len(list) == 0 {
				cmd.Println("No sessions found.")
				return nil
			}

			cmd.Printf("%-24s  %-19s  %-20s  %5s  %7s  %s\n", "SESSION ID", "UPDATED", "MODEL", "MSGS", "COST", "TITLE")
			cmd.Println(strings.Repeat("-", 90))
			for _, s := range list {
				title := s.Title
				if len(title) > 30 {
					title = title[:27] + "..."
				}
				cmd.Printf("%-24s  %-19s  %-20s  %5d  $%6.4f  %s\n",
					s.ID,
					s.UpdatedAt.Format("2006-01-02 15:04:05"),
					s.Model,
					s.MessageCount,
					s.TotalCost,
					title,
				)
			}
			return nil
		},
	}
	listCmd.Flags().BoolVar(&allFlag, "all", false, "list sessions across all workspaces")

	showCmd := &cobra.Command{
		Use:   "show <session_id>",
		Short: "Show details for a specific session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			store, err := session.NewStore("")
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			defer func() { _ = store.Close() }()

			resumed, err := session.LoadResumedSession(cmd.Context(), store, id)
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}

			cmd.Printf("Session ID:   %s\n", resumed.Session.ID)
			cmd.Printf("Workspace:    %s\n", resumed.Session.Workspace)
			cmd.Printf("Provider:     %s\n", resumed.Session.Provider)
			cmd.Printf("Model:        %s\n", resumed.Session.Model)
			cmd.Printf("Title:        %s\n", resumed.Session.Title)
			cmd.Printf("Created:      %s\n", resumed.Session.CreatedAt.Format("2006-01-02 15:04:05"))
			cmd.Printf("Updated:      %s\n", resumed.Session.UpdatedAt.Format("2006-01-02 15:04:05"))
			cmd.Printf("Messages:     %d\n", len(resumed.Messages))
			var toolCallCount int
			for _, tcs := range resumed.ToolCallsByMsg {
				toolCallCount += len(tcs)
			}
			cmd.Printf("Tool Calls:   %d\n", toolCallCount)
			cmd.Printf("Checkpoints:  %d\n", len(resumed.Checkpoints))
			cmd.Printf("Tokens:       %d\n", resumed.TotalTokens)
			cmd.Printf("Total Cost:   $%.4f\n", resumed.TotalCost)
			return nil
		},
	}

	var deleteYes bool
	deleteCmd := &cobra.Command{
		Use:   "delete <session_id>",
		Short: "Delete a session and all its stored history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !deleteYes {
				cmd.Printf("Are you sure you want to delete session %s? [y/N]: ", id)
				var resp string
				_, _ = fmt.Fscanln(cmd.InOrStdin(), &resp)
				if strings.ToLower(strings.TrimSpace(resp)) != "y" && strings.ToLower(strings.TrimSpace(resp)) != "yes" {
					cmd.Println("Deletion aborted.")
					return nil
				}
			}
			store, err := session.NewStore("")
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			defer func() { _ = store.Close() }()

			if err := store.DeleteSession(cmd.Context(), id); err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			cmd.Printf("Session %s deleted.\n", id)
			return nil
		},
	}
	deleteCmd.Flags().BoolVarP(&deleteYes, "yes", "y", false, "skip confirmation prompt")

	var olderThanDays int
	var pruneYes bool
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Prune old sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !pruneYes {
				cmd.Printf("Are you sure you want to prune sessions older than %d days? [y/N]: ", olderThanDays)
				var resp string
				_, _ = fmt.Fscanln(cmd.InOrStdin(), &resp)
				if strings.ToLower(strings.TrimSpace(resp)) != "y" && strings.ToLower(strings.TrimSpace(resp)) != "yes" {
					cmd.Println("Prune aborted.")
					return nil
				}
			}
			store, err := session.NewStore("")
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			defer func() { _ = store.Close() }()

			n, err := store.PruneSessions(cmd.Context(), olderThanDays)
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			cmd.Printf("Pruned %d sessions older than %d days.\n", n, olderThanDays)
			return nil
		},
	}
	pruneCmd.Flags().IntVar(&olderThanDays, "older-than", 30, "prune sessions older than N days")
	pruneCmd.Flags().BoolVarP(&pruneYes, "yes", "y", false, "skip confirmation prompt")

	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run SQLite database integrity check",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := session.NewStore("")
			if err != nil {
				return ExitCodeError{Code: 1, Err: err}
			}
			defer func() { _ = store.Close() }()

			res, err := store.Doctor(cmd.Context())
			if err != nil {
				cmd.PrintErrf("Database integrity error: %v\n", err)
				return ExitCodeError{Code: 1, Err: err}
			}
			cmd.Printf("Database integrity: %s\n", res)
			return nil
		},
	}

	cmd.AddCommand(listCmd, showCmd, deleteCmd, pruneCmd, doctorCmd)
	return cmd
}

func decideHeadlessApproval(isDangerous, isProtected, yes, yolo bool) (agent.Decision, string) {
	if isDangerous {
		if yolo {
			return agent.DecisionAllow, ""
		}
		return agent.DecisionDeny, "dangerous command requires --yolo"
	}
	if isProtected {
		return agent.DecisionDeny, "protected path modification cannot be auto-approved in headless mode"
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

type headlessToolRecord struct {
	Name       string `json:"name"`
	Input      any    `json:"input"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type headlessJSONOutput struct {
	SessionID string               `json:"session_id,omitempty"`
	Status    string               `json:"status"`
	Prompt    string               `json:"prompt"`
	Content   string               `json:"content"`
	Turns     int                  `json:"turns"`
	Tools     []headlessToolRecord `json:"tools"`
	Usage     agent.Usage          `json:"usage"`
	Error     string               `json:"error,omitempty"`
}

func runHeadless(ctx context.Context, prompt, outputFormat, permMode string, maxTurns int) error {
	outputFormat = strings.ToLower(strings.TrimSpace(outputFormat))
	if outputFormat == "" {
		outputFormat = "text"
	}
	if outputFormat != "text" && outputFormat != "json" && outputFormat != "stream-json" {
		return ExitCodeError{Code: 2, Err: fmt.Errorf("invalid --output format %q (expected text, json, or stream-json)", outputFormat)}
	}

	if permMode != "" {
		norm := strings.ToLower(permMode)
		if norm != "ask" && norm != "accept-edits" && norm != "plan" && norm != "yolo" {
			return ExitCodeError{Code: 2, Err: fmt.Errorf("invalid --permission-mode %q (expected ask, accept-edits, plan, or yolo)", permMode)}
		}
	}

	cfg, err := config.Load(config.LoadOptions{
		WorkspaceRoot:  rootFlag,
		Provider:       providerFlag,
		Model:          modelFlag,
		BaseURL:        baseURLFlag,
		Shell:          shellFlag,
		Theme:          themeFlag,
		SystemPrompt:   systemPromptFlag,
		MaxTurns:       maxTurns,
		PermissionMode: permMode,
	})
	if err != nil {
		return ExitCodeError{Code: 2, Err: fmt.Errorf("loading config: %w", err)}
	}
	for _, w := range cfg.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}

	if demoFlag && rootFlag == "." {
		if demoDir, err := setupDemoWorkspace(); err == nil {
			cfg.WorkspaceRoot = demoDir
		}
	}
	systemPrompt := finalizeSystemPrompt(cfg.WorkspaceRoot, cfg.SystemPrompt, cfg.Shell)
	if observeSystemPrompt != nil {
		observeSystemPrompt("run", systemPrompt)
	}
	if stopAfterSystemPrompt {
		return nil
	}

	var store *session.Store
	if !noSessionFlag {
		if s, err := session.NewStore(""); err == nil {
			store = s
			defer func() { _ = store.Close() }()
		}
	}

	var sessionID string
	var sessionHistory []llm.Message

	if resumeFlag != "" {
		if store == nil {
			return ExitCodeError{Code: 1, Err: errors.New("cannot resume session: session storage unavailable")}
		}
		targetID := resumeFlag
		if targetID == "last" {
			list, err := store.ListSessions(ctx, session.CanonicalWorkspace(cfg.WorkspaceRoot), false)
			if err != nil || len(list) == 0 {
				return ExitCodeError{Code: 1, Err: fmt.Errorf("no recent sessions found to resume in %s", cfg.WorkspaceRoot)}
			}
			targetID = list[0].ID
		}
		resumedData, err := session.LoadResumedSession(ctx, store, targetID)
		if err != nil {
			return ExitCodeError{Code: 1, Err: fmt.Errorf("resuming session %s: %w", targetID, err)}
		}
		explicitRoot := rootFlag != "" && rootFlag != "."
		if err := session.ValidateWorkspaceBinding(resumedData.Session.Workspace, cfg.WorkspaceRoot, resumedData.Session.ID); err != nil && !explicitRoot {
			return ExitCodeError{Code: 1, Err: err}
		}
		sessionID = resumedData.Session.ID
		sessionHistory = resumedData.LLMMessages
	} else {
		sessionID = session.GenerateSessionID()
		if store != nil {
			_ = store.CreateSession(ctx, session.SessionRecord{
				ID:        sessionID,
				Workspace: session.CanonicalWorkspace(cfg.WorkspaceRoot),
				Provider:  cfg.Provider,
				Model:     cfg.Model,
				Title:     session.TruncateTitle(prompt),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			})
		}
	}

	var provider llm.Provider
	var toolRegistry *tools.Registry

	if demoFlag {
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
		toolRegistry = tools.DemoTools(cfg.WorkspaceRoot, demoSpeedFlag)
	} else {
		credMgr := credentials.NewManager(nil)
		cred := credMgr.Resolve(cfg.Provider)
		p, err := llm.NewProvider(llm.ProviderConfig{
			Provider: cfg.Provider,
			Model:    cfg.Model,
			BaseURL:  cfg.BaseURL,
			APIKey:   cred.Key,
		})
		if err != nil {
			return ExitCodeError{Code: 2, Err: err}
		}
		provider = p
		sh := tools.DetectShell(cfg.Shell)
		toolRegistry = tools.DefaultTools(cfg.WorkspaceRoot, sh)
	}

	resolvedMode := permission.ModeDefault
	if yoloFlag {
		resolvedMode = permission.ModeYOLO
	} else if yesFlag {
		resolvedMode = permission.ModeAcceptEdits
	} else if cfg.PermissionMode != "" {
		resolvedMode = permission.Mode(cfg.PermissionMode)
	}

	chkMgr, _ := checkpoint.New(cfg.WorkspaceRoot)
	var checkpointer agent.Checkpointer
	if chkMgr != nil {
		checkpointer = chkMgr.AsCheckpointer()
	}

	ps, err := loadEffectivePolicy(cfg.WorkspaceRoot)
	if err != nil {
		if errors.Is(err, policy.ErrInvalidPolicy) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(2)
		}
	}
	if ps != nil && len(ps.Warnings) > 0 {
		for _, w := range ps.Warnings {
			fmt.Fprintf(os.Stderr, "%s\n", w)
		}
	}

	auditPath := ""
	auditEnabled := true
	auditRequired := false
	auditMaxMB := 50
	if cfg.Audit != nil {
		if cfg.Audit.Enabled != nil {
			auditEnabled = *cfg.Audit.Enabled
		}
		if cfg.Audit.Path != "" {
			auditPath = cfg.Audit.Path
		}
		if cfg.Audit.MaxMB > 0 {
			auditMaxMB = cfg.Audit.MaxMB
		}
		auditRequired = cfg.Audit.Required
	}
	if auditPath == "" {
		if def, err := defaultAuditPath(cfg.WorkspaceRoot); err == nil {
			auditPath = def
		}
	}
	auditLogger := audit.New(audit.Config{
		Enabled:  auditEnabled,
		Path:     auditPath,
		MaxMB:    auditMaxMB,
		Required: auditRequired,
		Root:     cfg.WorkspaceRoot,
	})

	s := agent.NewLocal(agent.Config{
		ID:           sessionID,
		History:      sessionHistory,
		Provider:     provider,
		Tools:        toolRegistry,
		Policy:       permission.Policy{Mode: resolvedMode},
		PolicySet:    ps,
		AuditLogger:  auditLogger,
		System:       systemPrompt,
		MaxTurns:     cfg.MaxTurns,
		Checkpointer: checkpointer,
		IsHeadless:   true,
		AutoApprove:  yesFlag || yoloFlag,
	})
	defer s.Close()

	if store != nil {
		userMsgID := fmt.Sprintf("msg_%d_u", time.Now().UnixNano())
		_ = store.SaveMessage(ctx, session.MessageRecord{
			ID:        userMsgID,
			SessionID: sessionID,
			Role:      "user",
			Content:   prompt,
			CreatedAt: time.Now(),
		})
	}

	if err := s.Send(ctx, prompt); err != nil {
		return ExitCodeError{Code: 1, Err: err}
	}

	var (
		tokenText           strings.Builder
		toolsCalled         []headlessToolRecord
		hadPermissionDenial bool
		finalErr            *agent.ErrorPayload
		finalDone           *agent.DonePayload
		cumulativeUsage     agent.Usage
		turnCount           int
	)

eventLoop:
	for ev := range s.Events() {
		if outputFormat == "stream-json" {
			data, _ := json.Marshal(ev)
			fmt.Println(string(data))
		}

		switch ev.Type {
		case agent.EventTokenDelta:
			if ev.Token != nil {
				tokenText.WriteString(ev.Token.Text)
				if outputFormat == "text" {
					fmt.Print(ev.Token.Text)
				}
			}
		case agent.EventToolStarted:
			if ev.ToolStart != nil {
				var rawInput any
				_ = json.Unmarshal([]byte(ev.ToolStart.Input), &rawInput)
				toolsCalled = append(toolsCalled, headlessToolRecord{
					Name:   ev.ToolStart.Name,
					Input:  rawInput,
					Status: "running",
				})
				if outputFormat == "text" {
					fmt.Fprintf(os.Stderr, "\n● %s %s\n", ev.ToolStart.Name, ev.ToolStart.Input)
				}
				if store != nil {
					_ = store.SaveToolCall(ctx, session.ToolCallRecord{
						ID:        ev.ToolStart.ID,
						Name:      ev.ToolStart.Name,
						Input:     ev.ToolStart.Input,
						Status:    "running",
						CreatedAt: time.Now(),
					})
				}
			}
		case agent.EventApprovalRequested:
			if ev.Approval != nil {
				d, reason := decideHeadlessApproval(ev.Approval.IsDangerous, ev.Approval.IsProtected, yesFlag, yoloFlag)
				if d == agent.DecisionDeny {
					hadPermissionDenial = true
					if outputFormat == "text" {
						fmt.Fprintf(os.Stderr, "  [denied: %s]\n", reason)
					}
				}
				_ = s.Respond(ev.Approval.ID, d)
			}
		case agent.EventToolFinished:
			if ev.ToolDone != nil {
				if len(toolsCalled) > 0 {
					toolsCalled[len(toolsCalled)-1].Status = string(ev.ToolDone.Status)
					toolsCalled[len(toolsCalled)-1].DurationMs = ev.ToolDone.DurationMs
					toolsCalled[len(toolsCalled)-1].Error = ev.ToolDone.Error
				}
				if ev.ToolDone.Status == agent.ToolDenied {
					hadPermissionDenial = true
				}
				if outputFormat == "text" {
					fmt.Fprintf(os.Stderr, "  ↳ %s (%dms)\n", ev.ToolDone.Status, ev.ToolDone.DurationMs)
				}
				if store != nil {
					toolOutput := ev.ToolDone.Result
					if ev.ToolDone.Error != "" {
						toolOutput = ev.ToolDone.Error
					}
					_ = store.SaveToolCall(ctx, session.ToolCallRecord{
						ID:         ev.ToolDone.ID,
						Status:     string(ev.ToolDone.Status),
						Output:     toolOutput,
						DurationMs: ev.ToolDone.DurationMs,
					})
				}
			}
		case agent.EventUsageUpdated:
			if ev.Usage != nil {
				cumulativeUsage = *ev.Usage
				turnCount++
				if store != nil {
					_ = store.SaveUsage(ctx, session.UsageRecord{
						SessionID:    sessionID,
						Model:        cfg.Model,
						InputTokens:  ev.Usage.InputTokens,
						OutputTokens: ev.Usage.OutputTokens,
						Cost:         ev.Usage.Cost,
						CreatedAt:    time.Now(),
					})
				}
			}
		case agent.EventError:
			finalErr = ev.Err
			break eventLoop
		case agent.EventDone:
			finalDone = ev.Done
			if store != nil && tokenText.Len() > 0 {
				asstMsgID := fmt.Sprintf("msg_%d_a", time.Now().UnixNano())
				_ = store.SaveMessage(ctx, session.MessageRecord{
					ID:        asstMsgID,
					SessionID: sessionID,
					Role:      "assistant",
					Content:   tokenText.String(),
					CreatedAt: time.Now(),
				})
			}
			break eventLoop
		}
	}

	if outputFormat == "json" {
		status := "success"
		errStr := ""
		if finalErr != nil {
			status = finalErr.Code
			errStr = finalErr.Message
		} else if hadPermissionDenial {
			status = "permission_denied"
			errStr = "one or more actions were denied by permission policy"
		} else if finalDone != nil && finalDone.Reason != "" {
			status = finalDone.Reason
		}

		outObj := headlessJSONOutput{
			SessionID: sessionID,
			Status:    status,
			Prompt:    prompt,
			Content:   tokenText.String(),
			Turns:     turnCount,
			Tools:     toolsCalled,
			Usage:     cumulativeUsage,
			Error:     errStr,
		}
		data, _ := json.MarshalIndent(outObj, "", "  ")
		fmt.Println(string(data))
	}

	// Exit code resolution per SPEC §3.7
	if finalErr != nil {
		if finalErr.Code == "max_turns" {
			return ExitCodeError{Code: 3, Err: fmt.Errorf("turn limit reached: %s", finalErr.Message)}
		}
		if finalErr.Code == "permission_denied" || hadPermissionDenial {
			return ExitCodeError{Code: 4, Err: fmt.Errorf("permission denied: %s", finalErr.Message)}
		}
		return ExitCodeError{Code: 1, Err: fmt.Errorf("error [%s]: %s", finalErr.Code, finalErr.Message)}
	}

	if hadPermissionDenial {
		return ExitCodeError{Code: 4, Err: errors.New("permission denied")}
	}

	return nil
}

func runInteractive(ctx context.Context) error {
	cfg, err := config.Load(config.LoadOptions{
		WorkspaceRoot: rootFlag,
		Provider:      providerFlag,
		Model:         modelFlag,
		BaseURL:       baseURLFlag,
		Shell:         shellFlag,
		Theme:         themeFlag,
		SystemPrompt:  systemPromptFlag,
	})
	if err != nil {
		return ExitCodeError{Code: 2, Err: fmt.Errorf("loading config: %w", err)}
	}
	for _, w := range cfg.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}

	if demoFlag && rootFlag == "." {
		if demoDir, err := setupDemoWorkspace(); err == nil {
			cfg.WorkspaceRoot = demoDir
		}
	}
	systemPrompt := finalizeSystemPrompt(cfg.WorkspaceRoot, cfg.SystemPrompt, cfg.Shell)
	if observeSystemPrompt != nil {
		observeSystemPrompt("interactive", systemPrompt)
	}
	if stopAfterSystemPrompt {
		return nil
	}

	var store *session.Store
	if !noSessionFlag {
		if s, err := session.NewStore(""); err == nil {
			store = s
			defer func() { _ = store.Close() }()
		}
	}

	var resumedData *session.ResumedSessionData
	var sessionID string
	var sessionHistory []llm.Message

	if resumeFlag != "" {
		if store == nil {
			return ExitCodeError{Code: 1, Err: errors.New("cannot resume session: session storage unavailable")}
		}
		targetID := resumeFlag
		if targetID == "last" {
			list, err := store.ListSessions(ctx, session.CanonicalWorkspace(cfg.WorkspaceRoot), false)
			if err != nil || len(list) == 0 {
				fmt.Fprintf(os.Stderr, "No previous sessions found in workspace %s. Starting new session.\n", cfg.WorkspaceRoot)
			} else {
				targetID = list[0].ID
			}
		}
		if targetID != "last" {
			data, err := session.LoadResumedSession(ctx, store, targetID)
			if err != nil {
				return ExitCodeError{Code: 1, Err: fmt.Errorf("loading session %s: %w", targetID, err)}
			}
			explicitRoot := rootFlag != "" && rootFlag != "."
			if err := session.ValidateWorkspaceBinding(data.Session.Workspace, cfg.WorkspaceRoot, data.Session.ID); err != nil && !explicitRoot {
				return ExitCodeError{Code: 1, Err: err}
			}
			resumedData = data
			sessionID = data.Session.ID
			sessionHistory = data.LLMMessages
			if providerFlag == "" {
				cfg.Provider = data.Session.Provider
			}
			if modelFlag == "" {
				cfg.Model = data.Session.Model
			}
		}
	}

	if sessionID == "" {
		sessionID = session.GenerateSessionID()
		if store != nil {
			_ = store.CreateSession(ctx, session.SessionRecord{
				ID:        sessionID,
				Workspace: session.CanonicalWorkspace(cfg.WorkspaceRoot),
				Provider:  cfg.Provider,
				Model:     cfg.Model,
				Title:     "Interactive session",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			})
		}
	}

	var provider llm.Provider
	var toolRegistry *tools.Registry
	var providerName, modelName string
	var needsWizard bool

	if demoFlag {
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
		toolRegistry = tools.DemoTools(cfg.WorkspaceRoot, demoSpeedFlag)
	} else {
		providerName = cfg.Provider
		modelName = cfg.Model
		credMgr := credentials.NewManager(nil)
		cred := credMgr.Resolve(cfg.Provider)
		if cred.Source == credentials.SourceNone && cfg.Provider != "ollama" && cfg.Provider != "lmstudio" {
			needsWizard = true
		}
		p, err := llm.NewProvider(llm.ProviderConfig{
			Provider: cfg.Provider,
			Model:    cfg.Model,
			BaseURL:  cfg.BaseURL,
			APIKey:   cred.Key,
		})
		if err != nil {
			return ExitCodeError{Code: 2, Err: err}
		}
		provider = p
		sh := tools.DetectShell(cfg.Shell)
		toolRegistry = tools.DefaultTools(cfg.WorkspaceRoot, sh)
	}

	permMode := permission.ModeDefault
	if yoloFlag {
		permMode = permission.ModeYOLO
	} else if yesFlag {
		permMode = permission.ModeAcceptEdits
	} else if cfg.PermissionMode != "" {
		permMode = permission.Mode(cfg.PermissionMode)
	}

	chkMgr, _ := checkpoint.New(cfg.WorkspaceRoot)
	var checkpointer agent.Checkpointer
	if chkMgr != nil {
		checkpointer = chkMgr.AsCheckpointer()
	}

	ps, err := loadEffectivePolicy(cfg.WorkspaceRoot)
	if err != nil {
		if errors.Is(err, policy.ErrInvalidPolicy) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(2)
		}
	}
	if ps != nil && len(ps.Warnings) > 0 {
		for _, w := range ps.Warnings {
			fmt.Fprintf(os.Stderr, "%s\n", w)
		}
	}

	auditPath := ""
	auditEnabled := true
	auditRequired := false
	auditMaxMB := 50
	if cfg.Audit != nil {
		if cfg.Audit.Enabled != nil {
			auditEnabled = *cfg.Audit.Enabled
		}
		if cfg.Audit.Path != "" {
			auditPath = cfg.Audit.Path
		}
		if cfg.Audit.MaxMB > 0 {
			auditMaxMB = cfg.Audit.MaxMB
		}
		auditRequired = cfg.Audit.Required
	}
	if auditPath == "" {
		if def, err := defaultAuditPath(cfg.WorkspaceRoot); err == nil {
			auditPath = def
		}
	}
	auditLogger := audit.New(audit.Config{
		Enabled:  auditEnabled,
		Path:     auditPath,
		MaxMB:    auditMaxMB,
		Required: auditRequired,
		Root:     cfg.WorkspaceRoot,
	})

	s := agent.NewLocal(agent.Config{
		ID:           sessionID,
		History:      sessionHistory,
		Provider:     provider,
		Tools:        toolRegistry,
		Policy:       permission.Policy{Mode: permMode},
		PolicySet:    ps,
		AuditLogger:  auditLogger,
		System:       systemPrompt,
		Checkpointer: checkpointer,
	})
	defer s.Close()

	th, err := tui.LoadTheme(cfg.Theme)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v (falling back to dark)\n", err)
		th = tui.DarkTheme()
	}
	m := tui.New(s, th)
	m.SetSecurityNotices(cfg.Warnings)
	m.SetProviderInfo(providerName, modelName)
	m.SetBaseURL(cfg.BaseURL)
	m.SetWorkspaceRoot(cfg.WorkspaceRoot)
	m.SetSessionStore(store, noSessionFlag)
	m.SetVersion(version)
	m.SetGitBranch(tui.DetectGitBranch(cfg.WorkspaceRoot))
	if resumedData != nil {
		m.RestoreSession(resumedData)
	}
	if needsWizard {
		m.SetWizard(tui.NewWizardState())
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return ExitCodeError{Code: 1, Err: fmt.Errorf("running TUI: %w", err)}
	}
	return nil
}

// finalizeSystemPrompt is what both CLI entry points send to the model.
// The built security prompt is always the base. A user-set system_prompt
// is appended after it and never replaces it (F-006).
func finalizeSystemPrompt(workspace, userPrompt, shell string) string {
	sh := tools.DetectShell(shell)
	return agent.ComposeSystemPrompt(workspace, userPrompt, sh)
}

func main() {
	cmd := newRootCmd()
	if err := cmd.Execute(); err != nil {
		var exitErr ExitCodeError
		if errors.As(err, &exitErr) {
			if exitErr.Err != nil && exitErr.Code != 0 {
				fmt.Fprintf(os.Stderr, "Error: %v\n", exitErr.Err)
			}
			os.Exit(exitErr.Code)
		}
		os.Exit(2)
	}
}
