package agent_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/policy"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestAlwaysAllowDoesNotApproveOtherBash(t *testing.T) {
	dir := t.TempDir()
	set, err := policy.Load(filepath.Join(dir, "policy.json"), filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := mock.New(
		mock.Turn{mock.ToolUse("c1", "bash", map[string]any{"command": "echo alpha"})},
		mock.Turn{mock.ToolUse("c2", "bash", map[string]any{"command": "echo beta"})},
		mock.Turn{mock.Text("done")},
	)
	s := agent.NewLocal(agent.Config{
		Provider:  p,
		Tools:     tools.NewRegistry(tools.Bash{Root: dir, Shell: tools.DetectShell("")}),
		PolicySet: set,
	})
	defer s.Close()
	if err := s.Send(context.Background(), "run two commands"); err != nil {
		t.Fatal(err)
	}

	first := next(t, s, agent.EventApprovalRequested)
	if err := s.Respond(first.Approval.ID, agent.DecisionAlwaysAllow); err != nil {
		t.Fatal(err)
	}
	fin := next(t, s, agent.EventToolFinished)
	if fin.ToolDone == nil || fin.ToolDone.Status != agent.ToolOK {
		t.Fatalf("first command status: %+v", fin.ToolDone)
	}

	second := next(t, s, agent.EventApprovalRequested)
	if second.Approval == nil || second.Approval.Command != "echo beta" {
		t.Fatalf("expected a fresh approval for echo beta, got %+v", second.Approval)
	}
	_ = s.Respond(second.Approval.ID, agent.DecisionDeny)
	next(t, s, agent.EventDone)
}
