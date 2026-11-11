package agent_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func runGoTest(dir string) (int, string) {
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(out)
		}
		return 1, string(out)
	}
	return 0, string(out)
}

func TestSlice5_FixLoopExitCheck(t *testing.T) {
	fixtures := []struct {
		name       string
		relPath    string
		fixFile    string
		oldString  string
		newString  string
	}{
		{
			name:      "offbyone",
			relPath:   filepath.Join("..", "..", "testdata", "fixfail", "offbyone"),
			fixFile:   "calc.go",
			oldString: "for i := start; i < end; i++ {",
			newString: "for i := start; i <= end; i++ {",
		},
		{
			name:      "nilpanic",
			relPath:   filepath.Join("..", "..", "testdata", "fixfail", "nilpanic"),
			fixFile:   "store.go",
			oldString: "return &Cache{}",
			newString: "return &Cache{items: make(map[string]string)}",
		},
		{
			name:      "wrongimport",
			relPath:   filepath.Join("..", "..", "testdata", "fixfail", "wrongimport"),
			fixFile:   "format.go",
			oldString: `"string" // BUG: should be "strings"`,
			newString: `"strings"`,
		},
	}

	sh := tools.DetectShell("")

	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if err := copyDir(tc.relPath, tmpDir); err != nil {
				t.Fatalf("failed to copy fixture %s: %v", tc.name, err)
			}

			// Precondition: test must fail initially
			initialExit, out := runGoTest(tmpDir)
			if initialExit == 0 {
				t.Fatalf("fixture %s was supposed to fail initially, but passed:\n%s", tc.name, out)
			}

			// Scripted mock provider that replicates the agent fix loop:
			// 1. Run tests with bash -> fails
			// 2. Read the source file with read_file
			// 3. Apply the fix with edit_file
			// 4. Run tests again with bash -> passes
			// 5. Final summary
			provider := mock.New(
				mock.Turn{
					mock.Text("Running tests to observe failure...\n"),
					mock.ToolUse("call_bash_1", "bash", map[string]any{"command": "go test ./..."}),
				},
				mock.Turn{
					mock.Text("Inspecting source code...\n"),
					mock.ToolUse("call_read_1", "read_file", map[string]any{"path": tc.fixFile}),
				},
				mock.Turn{
					mock.Text("Applying bug fix...\n"),
					mock.ToolUse("call_edit_1", "edit_file", map[string]any{
						"path":       tc.fixFile,
						"old_string": tc.oldString,
						"new_string": tc.newString,
					}),
				},
				mock.Turn{
					mock.Text("Verifying fix with tests...\n"),
					mock.ToolUse("call_bash_2", "bash", map[string]any{"command": "go test ./..."}),
				},
				mock.Turn{
					mock.Text("The test suite now passes successfully! The bug has been resolved."),
				},
			)

			toolRegistry := tools.DefaultTools(tmpDir, sh)
			session := agent.NewLocal(agent.Config{
				Provider: provider,
				Tools:    toolRegistry,
				Policy:   permission.Policy{Mode: permission.ModeAcceptEdits},
				System:   agent.BuildSystemPrompt(tmpDir, sh),
			})
			defer session.Close()

			if err := session.Send(context.Background(), "Fix the failing tests in this package"); err != nil {
				t.Fatalf("session.Send failed: %v", err)
			}

			timeout := time.After(30 * time.Second)
			var completed bool
		EventLoop:
			for {
				select {
				case ev, ok := <-session.Events():
					if !ok {
						break EventLoop
					}
					if ev.Type == agent.EventApprovalRequested && ev.Approval != nil {
						// Auto-approve any pending execution requests
						_ = session.Respond(ev.Approval.ID, agent.DecisionAllow)
					}
					if ev.Type == agent.EventDone {
						completed = true
						break EventLoop
					}
					if ev.Type == agent.EventError {
						t.Fatalf("agent loop returned error: %+v", ev.Err)
					}
				case <-timeout:
					t.Fatalf("timed out waiting for fix loop to complete on %s", tc.name)
				}
			}

			if !completed {
				t.Fatalf("session did not complete cleanly on fixture %s", tc.name)
			}

			// Postcondition: go test in fixture must now pass!
			finalExit, finalOut := runGoTest(tmpDir)
			if finalExit != 0 {
				t.Fatalf("fixture %s still failing after fix loop (exit %d):\n%s", tc.name, finalExit, finalOut)
			}
		})
	}
}

