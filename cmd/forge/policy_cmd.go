package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/policy"
)

func newPolicyCmd() *cobra.Command {
	var rootDir string

	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect and manage security policies (SPEC §3.3 P-3, P-4, P-5)",
	}
	cmd.PersistentFlags().StringVar(&rootDir, "root", ".", "workspace root directory")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List effective policy rules across user and project configurations",
		RunE: func(c *cobra.Command, _ []string) error {
			ps, err := loadEffectivePolicy(rootDir)
			if err != nil {
				if errors.Is(err, policy.ErrInvalidPolicy) {
					c.PrintErrf("Error: %v\n", err)
					os.Exit(2)
				}
				return err
			}

			if len(ps.Warnings) > 0 {
				c.PrintErrln("Security Warnings:")
				for _, w := range ps.Warnings {
					c.PrintErrf("  %s\n", w)
				}
				c.PrintErrln()
			}

			c.Println("Effective Policy Rules:")
			c.Println()

			c.Println("User Rules (~/.forge/policy.json):")
			if len(ps.UserRules) == 0 {
				c.Println("  (none)")
			} else {
				for _, r := range ps.UserRules {
					c.Printf("  • [%s] %s\n", r.Action, r.Raw)
				}
			}
			c.Println()

			c.Printf("Project Rules (%s/.forge/policy.json):\n", rootDir)
			if len(ps.ProjectRules) == 0 {
				c.Println("  (none)")
			} else {
				for _, r := range ps.ProjectRules {
					c.Printf("  • [%s] %s\n", r.Action, r.Raw)
				}
			}
			return nil
		},
	}

	checkCmd := &cobra.Command{
		Use:   "check <tool> <input>",
		Short: "Explain policy evaluation for a specific tool and action",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			toolName := args[0]
			inputVal := args[1]

			ps, err := loadEffectivePolicy(rootDir)
			if err != nil {
				if errors.Is(err, policy.ErrInvalidPolicy) {
					c.PrintErrf("Error: %v\n", err)
					os.Exit(2)
				}
				return err
			}

			req := policy.EvalRequest{
				ToolName: toolName,
				Mode:     permission.ModeAsk,
			}
			if toolName == "bash" {
				req.Command = inputVal
			} else {
				req.Path = inputVal
			}

			res := policy.Evaluate(req, ps)

			var verdictStr string
			switch res.Verdict {
			case permission.Allow:
				verdictStr = "ALLOW"
			case permission.Ask:
				verdictStr = "ASK"
			case permission.Deny:
				verdictStr = "DENY"
			}

			c.Printf("Verdict:  %s\n", verdictStr)
			c.Printf("Source:   %s\n", res.Source)
			c.Printf("Reason:   %s\n", res.Reason)

			if res.BashAnalysis != nil {
				c.Println("\nBash Analysis:")
				c.Printf("  Segments:  %d (%v)\n", len(res.BashAnalysis.Segments), res.BashAnalysis.Segments)
				c.Printf("  Is Opaque: %v", res.BashAnalysis.IsOpaque)
				if res.BashAnalysis.IsOpaque {
					c.Printf(" (%s)", res.BashAnalysis.OpaqueReason)
				}
				c.Println()
				c.Printf("  Is Hazard: %v", res.BashAnalysis.IsHazard)
				if res.BashAnalysis.IsHazard {
					c.Printf(" (%s)", res.BashAnalysis.HazardReason)
				}
				c.Println()
			}
			return nil
		},
	}

	var isProject bool
	addCmd := &cobra.Command{
		Use:   "add <allow|deny|ask> <rule>",
		Short: "Add a rule to policy (defaults to user scope; use --project for project scope)",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			actionStr := strings.ToLower(args[0])
			ruleStr := args[1]

			var action policy.Action
			switch actionStr {
			case "allow":
				action = policy.ActionAllow
			case "deny":
				action = policy.ActionDeny
			case "ask":
				action = policy.ActionAsk
			default:
				c.PrintErrf("invalid action %q (must be allow, deny, or ask)\n", actionStr)
				os.Exit(2)
			}

			scope := policy.ScopeUser
			if isProject {
				scope = policy.ScopeProject
				if action == policy.ActionAllow {
					c.PrintErrln("Error: project policy cannot contain allow rules (untrusted repository defense)")
					os.Exit(2)
				}
			}

			ps, err := loadEffectivePolicy(rootDir)
			if err != nil {
				if errors.Is(err, policy.ErrInvalidPolicy) {
					c.PrintErrf("Error: %v\n", err)
					os.Exit(2)
				}
				return err
			}

			if err := ps.AddRule(scope, action, ruleStr); err != nil {
				if errors.Is(err, policy.ErrInvalidPolicy) {
					c.PrintErrf("Error: %v\n", err)
					os.Exit(2)
				}
				return err
			}

			c.Printf("Successfully added %s rule to %s policy: %s\n", action, scope, ruleStr)
			return nil
		},
	}
	addCmd.Flags().BoolVar(&isProject, "project", false, "save rule to project policy (.forge/policy.json) instead of user policy")

	removeCmd := &cobra.Command{
		Use:   "remove <rule>",
		Short: "Remove a rule from user or project policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ruleStr := args[0]
			scope := policy.ScopeUser
			if isProject {
				scope = policy.ScopeProject
			}

			ps, err := loadEffectivePolicy(rootDir)
			if err != nil {
				return err
			}

			if err := ps.RemoveRule(scope, ruleStr); err != nil {
				return err
			}

			c.Printf("Successfully removed rule %q from %s policy\n", ruleStr, scope)
			return nil
		},
	}
	removeCmd.Flags().BoolVar(&isProject, "project", false, "remove from project policy instead of user policy")

	cmd.AddCommand(listCmd, checkCmd, addCmd, removeCmd)
	return cmd
}

func loadEffectivePolicy(workspaceRoot string) (*policy.PolicySet, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	userPolicyPath := filepath.Join(home, ".forge", "policy.json")
	projPolicyPath := filepath.Join(workspaceRoot, ".forge", "policy.json")

	return policy.Load(userPolicyPath, projPolicyPath)
}
