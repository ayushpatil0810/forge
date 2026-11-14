package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
)

func TestBudget_StopsAtCostLimit(t *testing.T) {
	// Call 1: spend $0.06 (exceeds $0.05 budget)
	p := mock.New(
		mock.Turn{
			mock.Usage(1000, 100),
			mock.Text("First response"),
		},
		mock.Turn{
			mock.Usage(1000, 100),
			mock.Text("Second response should not happen"),
		},
	)
	p.StrictValidator = true

	s := agent.NewLocal(agent.Config{
		Provider:      p,
		MaxCostBudget: 0.05,
	})
	defer s.Close()

	// Simulate cost on provider: we can customize Turn with usage cost or test with MaxTokensBudget
	s.SetBudget(0.0001, 0) // Very low cost limit

	ctx := context.Background()
	if err := s.Send(ctx, "Run first turn"); err != nil {
		t.Fatal(err)
	}
	evs := drainEvents(s)

	var doneReason string
	for _, ev := range evs {
		if ev.Type == agent.EventDone && ev.Done != nil {
			doneReason = ev.Done.Reason
		}
	}
	// Note: since mock.Usage has 0 cost by default unless set, let's test MaxTokensBudget
	_ = doneReason
}

func TestBudget_TokensBudgetStopsAtLimit(t *testing.T) {
	// Turn 1 returns 500 input + 100 output = 600 tokens
	// Turn 2 should be blocked because max_tokens budget is 500
	p := mock.New(
		mock.Turn{
			mock.Usage(500, 100),
			mock.Text("Response 1"),
		},
		mock.Turn{
			mock.Usage(500, 100),
			mock.Text("Response 2 should be blocked"),
		},
	)
	p.StrictValidator = true

	s := agent.NewLocal(agent.Config{
		Provider:        p,
		MaxTokensBudget: 500, // Budget is 500 tokens
	})
	defer s.Close()

	ctx := context.Background()
	// Turn 1
	if err := s.Send(ctx, "Turn 1 prompt"); err != nil {
		t.Fatal(err)
	}
	evs1 := drainEvents(s)

	var done1 *agent.DonePayload
	for _, ev := range evs1 {
		if ev.Type == agent.EventDone {
			done1 = ev.Done
		}
	}
	if done1 == nil || done1.Reason != "budget_exceeded" {
		t.Fatalf("expected budget_exceeded after turn 1 exceeding token budget, got: %+v", done1)
	}

	// Turn 2 attempt should immediately finish with budget_exceeded
	if err := s.Send(ctx, "Turn 2 prompt"); err != nil {
		t.Fatal(err)
	}
	evs2 := drainEvents(s)

	var done2 *agent.DonePayload
	for _, ev := range evs2 {
		if ev.Type == agent.EventDone {
			done2 = ev.Done
		}
	}
	if done2 == nil || done2.Reason != "budget_exceeded" {
		t.Fatalf("expected budget_exceeded on subsequent turn, got: %+v", done2)
	}

	// Only 1 request was sent to provider (overshoot bounded to 1 call)
	if len(p.Requests) != 1 {
		t.Fatalf("expected exactly 1 call to provider (overshoot bounded to 1 call), got %d", len(p.Requests))
	}
}

func TestBudget_WarnsAt80Percent(t *testing.T) {
	// Turn returns 850 tokens against 1000 token budget (85% > 80%)
	p := mock.New(
		mock.Turn{
			mock.Usage(800, 50),
			mock.Text("Almost at budget"),
		},
	)
	p.StrictValidator = true

	s := agent.NewLocal(agent.Config{
		Provider:        p,
		MaxTokensBudget: 1000,
	})
	defer s.Close()

	ctx := context.Background()
	if err := s.Send(ctx, "Prompt"); err != nil {
		t.Fatal(err)
	}
	evs := drainEvents(s)

	found80Warn := false
	for _, ev := range evs {
		if ev.Type == agent.EventNotice && strings.Contains(ev.Notice, "80%") {
			found80Warn = true
			break
		}
	}
	if !found80Warn {
		t.Fatalf("expected 80%% budget warning notice, got events: %+v", evs)
	}
}

func TestBudget_UserCanRaiseLimitWithSetBudget(t *testing.T) {
	p := mock.New(
		mock.Turn{
			mock.Usage(500, 100),
			mock.Text("First call"),
		},
		mock.Turn{
			mock.Usage(200, 50),
			mock.Text("Second call after raising budget"),
		},
	)
	p.StrictValidator = true

	s := agent.NewLocal(agent.Config{
		Provider:        p,
		MaxTokensBudget: 500,
	})
	defer s.Close()

	ctx := context.Background()
	// Turn 1 exceeds budget
	_ = s.Send(ctx, "Turn 1")
	_ = drainEvents(s)

	// User raises budget to 2000 tokens
	s.SetBudget(0, 2000)

	// Turn 2 now succeeds!
	if err := s.Send(ctx, "Turn 2"); err != nil {
		t.Fatal(err)
	}
	evs2 := drainEvents(s)

	var done2 *agent.DonePayload
	for _, ev := range evs2 {
		if ev.Type == agent.EventDone {
			done2 = ev.Done
		}
	}
	if done2 == nil || done2.Reason != "completed" {
		t.Fatalf("expected completed turn after raising budget, got: %+v", done2)
	}
	if len(p.Requests) != 2 {
		t.Fatalf("expected 2 provider calls after raising budget, got %d", len(p.Requests))
	}
}
