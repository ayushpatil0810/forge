package agent_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/checkpoint"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestSlice7_UndoExitCheck(t *testing.T) {
	runTest := func(t *testing.T, isGitRepo bool) {
		ws := t.TempDir()

		// Initial files
		f1 := filepath.Join(ws, "file1.txt")
		f2 := filepath.Join(ws, "file2.txt")
		f3 := filepath.Join(ws, "file3.txt")

		init1 := []byte("line 1\nline 2\n")
		init2 := []byte("alpha\nbeta\n")
		init3 := []byte("original 3\n")

		if err := os.WriteFile(f1, init1, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f2, init2, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f3, init3, 0644); err != nil {
			t.Fatal(err)
		}

		var userHEADBefore, userStatusBefore []byte
		if isGitRepo {
			// Initialize user git repo and commit baseline
			_ = exec.Command("git", "init", ws).Run()
			_ = exec.Command("git", "-C", ws, "config", "user.name", "User").Run()
			_ = exec.Command("git", "-C", ws, "config", "user.email", "user@example.com").Run()
			_ = exec.Command("git", "-C", ws, "add", ".").Run()
			_ = exec.Command("git", "-C", ws, "commit", "-m", "initial user commit").Run()

			userHEADBefore, _ = exec.Command("git", "-C", ws, "rev-parse", "HEAD").Output()
			userStatusBefore, _ = exec.Command("git", "-C", ws, "status", "--porcelain").Output()
		}

		// Initialize Checkpoint Manager for workspace
		mgr, err := checkpoint.New(ws)
		if err != nil {
			t.Fatalf("failed to create checkpoint manager: %v", err)
		}

		// Scripted mock provider:
		// Turn 1: edits f1, edits f2, creates new_file.txt, modifies f3 via bash
		createdFile := filepath.Join(ws, "new_file.txt")

		// Bash command that appends to file3.txt
		sh := tools.DetectShell("")
		var bashCmd string
		if sh.Type == tools.ShellPowerShell || sh.Type == tools.ShellCmd {
			bashCmd = "Add-Content -Path file3.txt -Value 'appended by bash'"
		} else {
			bashCmd = "echo 'appended by bash' >> file3.txt"
		}

		mockProvider := mock.New(
			mock.Turn{
				mock.Text("First reading the files before editing.\n"),
				mock.ToolUse("call_read_1", "read_file", map[string]any{
					"path": "file1.txt",
				}),
				mock.ToolUse("call_read_2", "read_file", map[string]any{
					"path": "file2.txt",
				}),
			},
			mock.Turn{
				mock.Text("Executing multi-file edits and bash modification.\n"),
				mock.ToolUse("call_edit_1", "edit_file", map[string]any{
					"path":       "file1.txt",
					"old_string": "line 1\n",
					"new_string": "mutated 1\n",
				}),
				mock.ToolUse("call_edit_2", "edit_file", map[string]any{
					"path":       "file2.txt",
					"old_string": "alpha\n",
					"new_string": "omega\n",
				}),
				mock.ToolUse("call_write_1", "write_file", map[string]any{
					"path":    "new_file.txt",
					"content": "created content\n",
				}),
				mock.ToolUse("call_bash_1", "bash", map[string]any{
					"command": bashCmd,
				}),
			},
			mock.Turn{
				mock.Text("Edits and bash execution complete."),
			},
		)

		toolReg := tools.DefaultTools(ws, sh)

		sess := agent.NewLocal(agent.Config{
			Provider:     mockProvider,
			Tools:        toolReg,
			Policy:       permission.Policy{Mode: permission.ModeAcceptEdits},
			Checkpointer: mgr.AsCheckpointer(),
		})
		defer sess.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := sess.Send(ctx, "perform mutations"); err != nil {
			t.Fatalf("session.Send failed: %v", err)
		}

		// Drain events until Done
		for ev := range sess.Events() {
			if ev.Type == agent.EventToolFinished && ev.ToolDone != nil {
				t.Logf("Tool %s finished: status=%s, err=%s, result=%s", ev.ToolDone.Name, ev.ToolDone.Status, ev.ToolDone.Error, ev.ToolDone.Result)
			}
			if ev.Type == agent.EventApprovalRequested {
				_ = sess.Respond(ev.Approval.ID, agent.DecisionAllow)
			}
			if ev.Type == agent.EventDone {
				break
			}
			if ev.Type == agent.EventError {
				msg := "unknown error"
				if ev.Err != nil {
					msg = ev.Err.Message
				}
				t.Fatalf("turn failed with error: %s", msg)
			}
		}

		// Verify workspace was mutated
		data1, _ := os.ReadFile(f1)
		data2, _ := os.ReadFile(f2)
		data3, _ := os.ReadFile(f3)
		if bytes.Equal(data1, init1) {
			t.Fatal("expected f1 to be mutated")
		}
		if bytes.Equal(data2, init2) {
			t.Fatal("expected f2 to be mutated")
		}
		if bytes.Equal(data3, init3) {
			t.Fatal("expected f3 to be mutated")
		}
		if _, err := os.Stat(createdFile); os.IsNotExist(err) {
			t.Fatal("expected createdFile to exist")
		}

		// Now simulate `/undo`:
		// Retrieve checkpoints and restore the checkpoint taken before the turn mutations
		chks, err := sess.Checkpointer().List(ctx, 10)
		if err != nil {
			t.Fatalf("failed to list checkpoints: %v", err)
		}
		if len(chks) == 0 {
			t.Fatal("expected at least one checkpoint to be recorded before mutations")
		}

		// Restore the earliest/baseline checkpoint taken before the turn's mutating tools
		targetCP := chks[len(chks)-1]
		if _, err := sess.Checkpointer().Restore(ctx, targetCP.ID); err != nil {
			t.Fatalf("undo restore failed: %v", err)
		}

		// 1. Assert workspace is restored byte-identical
		restored1, err := os.ReadFile(f1)
		if err != nil || !bytes.Equal(restored1, init1) {
			t.Fatalf("file1.txt not restored: got %q, want %q (err: %v)", restored1, init1, err)
		}

		restored2, err := os.ReadFile(f2)
		if err != nil || !bytes.Equal(restored2, init2) {
			t.Fatalf("file2.txt not restored: got %q, want %q (err: %v)", restored2, init2, err)
		}

		restored3, err := os.ReadFile(f3)
		if err != nil || !bytes.Equal(restored3, init3) {
			t.Fatalf("file3.txt not restored: got %q, want %q (err: %v)", restored3, init3, err)
		}

		// 2. Assert created file is completely removed
		if _, err := os.Stat(createdFile); !os.IsNotExist(err) {
			t.Fatalf("created file %q was not removed by undo!", createdFile)
		}

		// 3. If git repo, assert user repo HEAD, porcelain status, and refs are untouched
		if isGitRepo {
			userHEADAfter, _ := exec.Command("git", "-C", ws, "rev-parse", "HEAD").Output()
			userStatusAfter, _ := exec.Command("git", "-C", ws, "status", "--porcelain").Output()

			if string(userHEADBefore) != string(userHEADAfter) {
				t.Fatalf("user git HEAD changed! before %s, after %s", userHEADBefore, userHEADAfter)
			}
			if string(userStatusBefore) != string(userStatusAfter) {
				t.Fatalf("user git status changed! before %s, after %s", userStatusBefore, userStatusAfter)
			}
		}
	}

	t.Run("GitWorkspace", func(t *testing.T) {
		runTest(t, true)
	})

	t.Run("NonGitWorkspace", func(t *testing.T) {
		runTest(t, false)
	})
}
