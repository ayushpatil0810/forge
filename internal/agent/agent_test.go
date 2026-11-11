package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
)

// writeTool is a fake RiskWrite tool so approval flows can be tested.
type writeTool struct{ ran chan struct{} }

func (writeTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: "write_file", Description: "fake", Schema: json.RawMessage(`{}`)}
}
func (writeTool) Risk(json.RawMessage) tools.Risk { return tools.RiskWrite }
func (w writeTool) Run(context.Context, json.RawMessage) (tools.Result, error) {
	close(w.ran)
	return tools.Result{Output: "written"}, nil
}

func newWriteTool() writeTool { return writeTool{ran: make(chan struct{})} }

// next waits for an event matching want, failing the test on timeout.
func next(t *testing.T, s agent.Session, want agent.EventType) agent.Event {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == want {
				return ev
			}
			if ev.Terminal() && ev.Type != want {
				t.Fatalf("got terminal %q (%s) while waiting for %q", ev.Type, ev.Error, want)
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

func TestLoopRunsToolThenAnswers(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi there\n"), 0o644)

	p := mock.New(
		mock.Turn{mock.Text("Reading. "), mock.ToolUse("c1", "read_file", map[string]any{"path": "hello.txt"})},
		mock.Turn{mock.Text("It says hi.")},
	)
	s := agent.NewLocal(agent.Config{Provider: p, Tools: tools.NewRegistry(tools.ReadFile{Root: dir})})
	defer s.Close()

	if err := s.Send("what's in hello.txt?"); err != nil {
		t.Fatal(err)
	}
	fin := next(t, s, agent.EventToolFinished)
	if fin.Tool.Status != agent.ToolOK || fin.Tool.Risk != "read" {
		t.Fatalf("unexpected tool result: %+v", fin.Tool)
	}
	next(t, s, agent.EventDone)

	// The second model request must contain the tool result.
	if len(p.Requests) != 2 {
		t.Fatalf("want 2 model requests, got %d", len(p.Requests))
	}
	last := p.Requests[1].Messages
	if got := last[len(last)-1]; got.Role != llm.RoleTool || got.ToolCallID != "c1" {
		t.Fatalf("tool result not fed back: %+v", got)
	}
}

func TestApprovalAllowAndDeny(t *testing.T) {
	for _, tc := range []struct {
		name   string
		d      agent.Decision
		status agent.ToolStatus
	}{
		{"allow", agent.DecisionAllow, agent.ToolOK},
		{"deny", agent.DecisionDeny, agent.ToolDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wt := newWriteTool()
			p := mock.New(
				mock.Turn{mock.ToolUse("w1", "write_file", map[string]any{})},
				mock.Turn{mock.Text("ok")},
			)
			s := agent.NewLocal(agent.Config{Provider: p, Tools: tools.NewRegistry(wt)})
			defer s.Close()
			s.Send("write something")

			req := next(t, s, agent.EventApprovalRequested)
			if err := s.Respond(req.Approval.ID, tc.d); err != nil {
				t.Fatal(err)
			}
			if fin := next(t, s, agent.EventToolFinished); fin.Tool.Status != tc.status {
				t.Fatalf("want %s, got %s", tc.status, fin.Tool.Status)
			}
			next(t, s, agent.EventDone)

			ran := false
			select {
			case <-wt.ran:
				ran = true
			default:
			}
			if ran != (tc.d == agent.DecisionAllow) {
				t.Fatalf("tool ran=%v with decision %s", ran, tc.d)
			}
		})
	}
}

func TestPlanModeDeniesWritesWithoutAsking(t *testing.T) {
	p := mock.New(mock.Turn{mock.ToolUse("w1", "write_file", map[string]any{})}, mock.Turn{mock.Text("ok")})
	s := agent.NewLocal(agent.Config{
		Provider: p, Tools: tools.NewRegistry(newWriteTool()),
		Policy: permission.Policy{Mode: permission.ModePlan},
	})
	defer s.Close()
	s.Send("go")
	if fin := next(t, s, agent.EventToolFinished); fin.Tool.Status != agent.ToolDenied {
		t.Fatalf("want denied, got %s", fin.Tool.Status)
	}
	next(t, s, agent.EventDone)
}

func TestCancelWhileAwaitingApproval(t *testing.T) {
	p := mock.New(mock.Turn{mock.ToolUse("w1", "write_file", map[string]any{})})
	s := agent.NewLocal(agent.Config{Provider: p, Tools: tools.NewRegistry(newWriteTool())})
	defer s.Close()
	s.Send("go")
	next(t, s, agent.EventApprovalRequested)
	s.Cancel()
	next(t, s, agent.EventCancelled)

	// Session must be reusable after a cancel.
	deadline := time.Now().Add(time.Second)
	for s.Send("again") == agent.ErrBusy && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMaxTurnsStopsRunawayLoops(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	loop := mock.Turn{mock.ToolUse("c", "read_file", map[string]any{"path": "a.txt"})}
	s := agent.NewLocal(agent.Config{
		Provider: mock.New(loop, loop, loop, loop, loop),
		Tools:    tools.NewRegistry(tools.ReadFile{Root: dir}),
		MaxTurns: 3,
	})
	defer s.Close()
	s.Send("loop forever")
	for ev := range s.Events() {
		if ev.Type == agent.EventError {
			return
		}
		if ev.Type == agent.EventDone {
			t.Fatal("expected MaxTurns error, got done")
		}
	}
}

func TestSendWhileBusyAndUnknownApproval(t *testing.T) {
	p := mock.New(mock.Turn{mock.ToolUse("w1", "write_file", map[string]any{})})
	s := agent.NewLocal(agent.Config{Provider: p, Tools: tools.NewRegistry(newWriteTool())})
	defer s.Close()
	s.Send("go")
	next(t, s, agent.EventApprovalRequested)
	if err := s.Send("second"); err != agent.ErrBusy {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	if err := s.Respond("nope", agent.DecisionAllow); err != agent.ErrUnknownApproval {
		t.Fatalf("want ErrUnknownApproval, got %v", err)
	}
}
