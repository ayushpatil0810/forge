// Command forge is the headless entrypoint. It proves the engine works with
// no UI at all; the Bubble Tea TUI and Wails app drive the same Session.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func main() {
	root := flag.String("root", ".", "workspace root")
	demo := flag.Bool("demo", false, "use a scripted mock provider (no API key needed)")
	yes := flag.Bool("yes", false, "auto-approve every action")
	flag.Parse()
	prompt := strings.Join(flag.Args(), " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: forge [--demo] [--root DIR] [--yes] <prompt>")
		os.Exit(2)
	}

	var provider llm.Provider
	if *demo {
		provider = mock.New(
			mock.Turn{mock.Text("Let me look at go.mod first.\n"), mock.ToolUse("demo1", "read_file", map[string]any{"path": "go.mod"})},
			mock.Turn{mock.Text("That's a Go module. The engine loop works end to end.\n")},
		)
	} else {
		fmt.Fprintln(os.Stderr, "no real provider wired up yet; run with --demo (next step: internal/llm/anthropic)")
		os.Exit(1)
	}

	s := agent.NewLocal(agent.Config{
		Provider: provider,
		Tools:    tools.NewRegistry(tools.ReadFile{Root: *root}),
		Policy:   permission.Policy{Mode: permission.ModeDefault},
		System:   "You are a careful coding agent.",
	})
	defer s.Close()
	if err := s.Send(prompt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	stdin := bufio.NewReader(os.Stdin)
	for ev := range s.Events() {
		switch ev.Type {
		case agent.EventTextDelta:
			fmt.Print(ev.Text)
		case agent.EventToolStarted:
			fmt.Printf("\n● %s %s\n", ev.Tool.Name, ev.Tool.Input)
		case agent.EventApprovalRequested:
			d := agent.DecisionAllow
			if !*yes {
				fmt.Printf("  allow %s? [y/N] ", ev.Approval.Tool.Name)
				line, _ := stdin.ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
					d = agent.DecisionDeny
				}
			}
			_ = s.Respond(ev.Approval.ID, d)
		case agent.EventToolFinished:
			fmt.Printf("  ↳ %s (%dms)\n", ev.Tool.Status, ev.Tool.DurationMs)
		case agent.EventError:
			fmt.Fprintln(os.Stderr, "error:", ev.Error)
			os.Exit(1)
		case agent.EventDone, agent.EventCancelled:
			return
		}
	}
}
