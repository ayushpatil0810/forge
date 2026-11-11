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

// writeTool is a fake tool requiring approval for testing.
type writeTool struct {
	ran chan struct{}
}

func (writeTool) Name() string        { return "write_file" }
func (writeTool) Description() string { return "fake write" }
func (writeTool) Schema() json.RawMessage {
	return json.RawMessage(`{}`)
}
func (writeTool) NeedsApproval(json.RawMessage) tools.ApprovalRequest {
	return tools.ApprovalRequest{Description: "write something", Diff: "+line\n"}
}
func (w writeTool) Run(context.Context, json.RawMessage) (tools.Result, error) {
	if w.ran != nil {
		close(w.ran)
	}
	return tools.Result{Output: "written"}, nil
}

func newWriteTool() writeTool {
	return writeTool{ran: make(chan struct{})}
}

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
				t.Fatalf("got terminal %q (%+v) while waiting for %q", ev.Type, ev, want)
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

// Slice 1 exit check: a scripted transcript with two tool calls runs to Done in a test.
func TestScriptedTranscriptWithTwoToolCallsRunsToDone(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "one.txt"), []byte("first content\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "two.txt"), []byte("second content\n"), 0o644)

	p := mock.New(
		// Turn 1: model asks to read one.txt
		mock.Turn{
			mock.Text("Reading file 1. "),
			mock.ToolUse("call-1", "read_file", map[string]any{"path": "one.txt"}),
			mock.Usage(10, 5),
		},
		// Turn 2: model asks to read two.txt
		mock.Turn{
			mock.Text("Reading file 2. "),
			mock.ToolUse("call-2", "read_file", map[string]any{"path": "two.txt"}),
			mock.Usage(20, 10),
		},
		// Turn 3: model gives final answer
		mock.Turn{
			mock.Text("Both files have been read successfully."),
			mock.Usage(30, 15),
		},
	)

	s := agent.NewLocal(agent.Config{
		Provider: p,
		Tools:    tools.NewRegistry(tools.ReadFile{Root: dir}),
	})
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Send(ctx, "Read one.txt and two.txt"); err != nil {
		t.Fatalf("unexpected Send error: %v", err)
	}

	// Verify first tool execution
	t1Start := next(t, s, agent.EventToolStarted)
	if t1Start.ToolStart == nil || t1Start.ToolStart.ID != "call-1" {
		t.Fatalf("unexpected tool start 1: %+v", t1Start.ToolStart)
	}
	t1Fin := next(t, s, agent.EventToolFinished)
	if t1Fin.ToolDone == nil || t1Fin.ToolDone.Status != agent.ToolOK {
		t.Fatalf("tool 1 did not complete with ok: %+v", t1Fin.ToolDone)
	}

	// Verify second tool execution
	t2Start := next(t, s, agent.EventToolStarted)
	if t2Start.ToolStart == nil || t2Start.ToolStart.ID != "call-2" {
		t.Fatalf("unexpected tool start 2: %+v", t2Start.ToolStart)
	}
	t2Fin := next(t, s, agent.EventToolFinished)
	if t2Fin.ToolDone == nil || t2Fin.ToolDone.Status != agent.ToolOK {
		t.Fatalf("tool 2 did not complete with ok: %+v", t2Fin.ToolDone)
	}

	// Verify completion
	doneEv := next(t, s, agent.EventDone)
	if doneEv.Done == nil || doneEv.Done.Reason != "completed" {
		t.Fatalf("expected Done with reason completed, got %+v", doneEv.Done)
	}

	// Verify mock requests: 3 turns executed
	if len(p.Requests) != 3 {
		t.Fatalf("want 3 model requests, got %d", len(p.Requests))
	}
	// Verify tool result was fed back in request 2 and request 3
	req2Msgs := p.Requests[1].Messages
	if last := req2Msgs[len(req2Msgs)-1]; last.Role != llm.RoleTool || last.ToolCallID != "call-1" {
		t.Fatalf("expected tool result for call-1, got %+v", last)
	}
	req3Msgs := p.Requests[2].Messages
	if last := req3Msgs[len(req3Msgs)-1]; last.Role != llm.RoleTool || last.ToolCallID != "call-2" {
		t.Fatalf("expected tool result for call-2, got %+v", last)
	}

	// Verify cumulative usage
	u := s.Usage()
	if u.InputTokens != 60 || u.OutputTokens != 30 {
		t.Fatalf("expected 60 input tokens and 30 output tokens, got %+v", u)
	}
}

// Slice 1 exit check: cancellation test passes under context cancellation and s.Cancel.
func TestCancellationWithContext(t *testing.T) {
	p := mock.New(
		mock.Turn{
			mock.Text("Starting turn..."),
			mock.ToolUse("c1", "write_file", map[string]any{}),
		},
	)

	s := agent.NewLocal(agent.Config{
		Provider: p,
		Tools:    tools.NewRegistry(newWriteTool()),
	})
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Send(ctx, "start"); err != nil {
		t.Fatal(err)
	}

	next(t, s, agent.EventApprovalRequested)

	// Cancel via context
	cancel()

	doneEv := next(t, s, agent.EventDone)
	if doneEv.Done == nil || doneEv.Done.Reason != "cancelled" {
		t.Fatalf("expected cancelled reason, got %+v", doneEv.Done)
	}

	// Verify session can run subsequent turns after cancellation
	p2 := mock.New(mock.Turn{mock.Text("recovered")})
	s2 := agent.NewLocal(agent.Config{Provider: p2})
	defer s2.Close()
	if err := s2.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	next(t, s2, agent.EventDone)
}

func TestCancelWhileAwaitingApproval(t *testing.T) {
	p := mock.New(mock.Turn{mock.ToolUse("w1", "write_file", map[string]any{})})
	s := agent.NewLocal(agent.Config{
		Provider: p,
		Tools:    tools.NewRegistry(newWriteTool()),
	})
	defer s.Close()

	if err := s.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	next(t, s, agent.EventApprovalRequested)
	s.Cancel()

	done := next(t, s, agent.EventDone)
	if done.Done == nil || done.Done.Reason != "cancelled" {
		t.Fatalf("want reason cancelled, got %+v", done.Done)
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

			if err := s.Send(context.Background(), "write something"); err != nil {
				t.Fatal(err)
			}

			req := next(t, s, agent.EventApprovalRequested)
			if err := s.Respond(req.Approval.ID, tc.d); err != nil {
				t.Fatal(err)
			}
			fin := next(t, s, agent.EventToolFinished)
			if fin.ToolDone == nil || fin.ToolDone.Status != tc.status {
				t.Fatalf("want %s, got %+v", tc.status, fin.ToolDone)
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
	p := mock.New(
		mock.Turn{mock.ToolUse("w1", "write_file", map[string]any{})},
		mock.Turn{mock.Text("ok")},
	)
	s := agent.NewLocal(agent.Config{
		Provider: p,
		Tools:    tools.NewRegistry(newWriteTool()),
		Policy:   permission.Policy{Mode: permission.ModePlan},
	})
	defer s.Close()

	if err := s.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	fin := next(t, s, agent.EventToolFinished)
	if fin.ToolDone == nil || fin.ToolDone.Status != agent.ToolDenied {
		t.Fatalf("want denied, got %+v", fin.ToolDone)
	}
	next(t, s, agent.EventDone)
}

func TestMaxTurnsStopsRunawayLoops(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	turn1 := mock.Turn{mock.ToolUse("c1", "read_file", map[string]any{"path": "a.txt"})}
	turn2 := mock.Turn{mock.ToolUse("c2", "read_file", map[string]any{"path": "a.txt", "offset": 10})}
	turn3 := mock.Turn{mock.ToolUse("c3", "read_file", map[string]any{"path": "a.txt", "offset": 20})}
	turn4 := mock.Turn{mock.ToolUse("c4", "read_file", map[string]any{"path": "a.txt", "offset": 30})}

	s := agent.NewLocal(agent.Config{
		Provider: mock.New(turn1, turn2, turn3, turn4),
		Tools:    tools.NewRegistry(tools.ReadFile{Root: dir}),
		MaxTurns: 3,
	})
	defer s.Close()

	if err := s.Send(context.Background(), "loop"); err != nil {
		t.Fatal(err)
	}

	for ev := range s.Events() {
		if ev.Type == agent.EventError {
			if ev.Err == nil || ev.Err.Code != "max_turns" {
				t.Fatalf("expected max_turns error, got %+v", ev.Err)
			}
			return
		}
		if ev.Type == agent.EventDone {
			t.Fatal("expected MaxTurns error, got done")
		}
	}
}

func TestRepeatedIdenticalCallsLoopGuard(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	turn := mock.Turn{mock.ToolUse("c", "read_file", map[string]any{"path": "a.txt"})}

	s := agent.NewLocal(agent.Config{
		Provider:         mock.New(turn, turn, turn, turn),
		Tools:            tools.NewRegistry(tools.ReadFile{Root: dir}),
		MaxTurns:         10,
		MaxRepeatedCalls: 3,
	})
	defer s.Close()

	if err := s.Send(context.Background(), "loop identical"); err != nil {
		t.Fatal(err)
	}

	for ev := range s.Events() {
		if ev.Type == agent.EventError {
			if ev.Err == nil || ev.Err.Code != "runaway_loop" {
				t.Fatalf("expected runaway_loop error, got %+v", ev.Err)
			}
			return
		}
		if ev.Type == agent.EventDone {
			t.Fatal("expected runaway_loop error, got done")
		}
	}
}

func TestSendWhileBusyAndUnknownApproval(t *testing.T) {
	p := mock.New(mock.Turn{mock.ToolUse("w1", "write_file", map[string]any{})})
	s := agent.NewLocal(agent.Config{Provider: p, Tools: tools.NewRegistry(newWriteTool())})
	defer s.Close()

	if err := s.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	next(t, s, agent.EventApprovalRequested)
	if err := s.Send(context.Background(), "second"); err != agent.ErrBusy {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	if err := s.Respond("nope", agent.DecisionAllow); err != agent.ErrUnknownApproval {
		t.Fatalf("want ErrUnknownApproval, got %v", err)
	}
}

type fakeFailingBashTool struct{}

func (fakeFailingBashTool) Name() string        { return "bash" }
func (fakeFailingBashTool) Description() string { return "failing bash" }
func (fakeFailingBashTool) Schema() json.RawMessage {
	return json.RawMessage(`{}`)
}
func (fakeFailingBashTool) NeedsApproval(json.RawMessage) tools.ApprovalRequest {
	return tools.ApprovalRequest{}
}
func (fakeFailingBashTool) Run(context.Context, json.RawMessage) (tools.Result, error) {
	return tools.Result{Output: "FAIL: TestCalc failed", IsError: true}, nil
}

type fakeEditTool struct{}

func (fakeEditTool) Name() string        { return "edit_file" }
func (fakeEditTool) Description() string { return "fake edit" }
func (fakeEditTool) Schema() json.RawMessage {
	return json.RawMessage(`{}`)
}
func (fakeEditTool) NeedsApproval(json.RawMessage) tools.ApprovalRequest {
	return tools.ApprovalRequest{}
}
func (fakeEditTool) Run(context.Context, json.RawMessage) (tools.Result, error) {
	return tools.Result{Output: "edited"}, nil
}

func TestFixLoopStallGuard(t *testing.T) {
	p := mock.New(
		mock.Turn{mock.ToolUse("e1", "edit_file", map[string]any{"path": "a.go"})},
		mock.Turn{mock.ToolUse("b1", "bash", map[string]any{"command": "go test ./..."})},
		mock.Turn{mock.ToolUse("e2", "edit_file", map[string]any{"path": "a.go"})},
		mock.Turn{mock.ToolUse("b2", "bash", map[string]any{"command": "go test ./..."})},
		mock.Turn{mock.ToolUse("e3", "edit_file", map[string]any{"path": "a.go"})},
		mock.Turn{mock.ToolUse("b3", "bash", map[string]any{"command": "go test ./..."})},
	)
	s := agent.NewLocal(agent.Config{
		Provider: p,
		Tools:    tools.NewRegistry(fakeFailingBashTool{}, fakeEditTool{}),
	})
	defer s.Close()

	if err := s.Send(context.Background(), "fix the test"); err != nil {
		t.Fatal(err)
	}

	for ev := range s.Events() {
		if ev.Type == agent.EventError {
			if ev.Err == nil || ev.Err.Code != "fix_loop_stalled" {
				t.Fatalf("expected fix_loop_stalled code, got %+v", ev.Err)
			}
			return
		}
		if ev.Type == agent.EventDone {
			t.Fatal("expected fix_loop_stalled error, got EventDone")
		}
	}
}

func TestSessionModeSwitching(t *testing.T) {
	s := agent.NewLocal(agent.Config{
		Provider: mock.New(),
		Policy:   permission.Policy{Mode: permission.ModeDefault},
	})
	defer s.Close()

	if s.Mode() != permission.ModeDefault {
		t.Fatalf("expected initial mode ask, got %v", s.Mode())
	}
	s.SetMode(permission.ModeAcceptEdits)
	if s.Mode() != permission.ModeAcceptEdits {
		t.Fatalf("expected mode accept-edits, got %v", s.Mode())
	}
	s.SetMode(permission.ModePlan)
	if s.Mode() != permission.ModePlan {
		t.Fatalf("expected mode plan, got %v", s.Mode())
	}
}
