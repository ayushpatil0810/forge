package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
)

const (
	defaultMaxTurns = 40
	maxToolOutput   = 30_000 // bytes returned to the model per tool call
)

// NewLocal returns an in-process Session.
func NewLocal(cfg Config) Session {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = defaultMaxTurns
	}
	if cfg.Tools == nil {
		cfg.Tools = tools.NewRegistry()
	}
	return &local{
		cfg:     cfg,
		events:  make(chan Event, 256),
		closed:  make(chan struct{}),
		pending: map[string]chan Decision{},
		always:  map[string]bool{},
	}
}

type local struct {
	cfg    Config
	events chan Event
	closed chan struct{}
	seq    atomic.Int64
	wg     sync.WaitGroup

	mu        sync.Mutex // guards the fields below
	running   bool
	isClosed  bool
	cancel    context.CancelFunc
	pending   map[string]chan Decision
	always    map[string]bool
	closeOnce sync.Once

	history []llm.Message // only touched by the single running turn
}

func (s *local) Events() <-chan Event { return s.events }

func (s *local) Send(prompt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed {
		return ErrClosed
	}
	if s.running {
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.running, s.cancel = true, cancel
	s.wg.Add(1)
	go s.runTurn(ctx, cancel, prompt)
	return nil
}

func (s *local) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *local) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.isClosed = true
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
		close(s.closed) // unblocks any emit stuck on a stalled consumer
		s.wg.Wait()
		close(s.events)
	})
}

func (s *local) Respond(id string, d Decision) error {
	s.mu.Lock()
	ch, ok := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if !ok {
		return ErrUnknownApproval
	}
	ch <- d // buffered(1): never blocks
	return nil
}

// emit delivers an event, giving up only if the session is closed.
func (s *local) emit(ev Event) {
	ev.Seq = s.seq.Add(1)
	select {
	case s.events <- ev:
	case <-s.closed:
	}
}

func (s *local) runTurn(ctx context.Context, cancel context.CancelFunc, prompt string) {
	defer s.wg.Done()
	final := s.loop(ctx, prompt)
	cancel()
	// Mark idle BEFORE the terminal event so a UI can Send as soon as it sees it.
	s.mu.Lock()
	s.running, s.cancel = false, nil
	s.mu.Unlock()
	s.emit(final)
}

// loop is the agent loop: model -> tools -> model ... until no tool calls.
func (s *local) loop(ctx context.Context, prompt string) Event {
	s.history = append(s.history, llm.Message{Role: llm.RoleUser, Text: prompt})

	for turn := 0; turn < s.cfg.MaxTurns; turn++ {
		msg, err := s.streamModel(ctx)
		if ctx.Err() != nil {
			return Event{Type: EventCancelled}
		}
		if err != nil {
			return Event{Type: EventError, Error: err.Error()}
		}
		s.history = append(s.history, msg)
		if len(msg.ToolCalls) == 0 {
			return Event{Type: EventDone}
		}
		// Always append one result per call, even when cancelled, or the
		// history becomes invalid for the next request.
		for _, call := range msg.ToolCalls {
			s.history = append(s.history, s.runToolCall(ctx, call))
		}
		if ctx.Err() != nil {
			return Event{Type: EventCancelled}
		}
		// TODO: context compaction goes here.
	}
	return Event{Type: EventError, Error: fmt.Sprintf("stopped after %d model calls (MaxTurns)", s.cfg.MaxTurns)}
}

func (s *local) streamModel(ctx context.Context) (llm.Message, error) {
	ch, err := s.cfg.Provider.Stream(ctx, llm.Request{
		System:    s.cfg.System,
		Messages:  s.history,
		Tools:     s.cfg.Tools.Specs(),
		MaxTokens: s.cfg.MaxTokens,
	})
	if err != nil {
		return llm.Message{}, err
	}
	msg := llm.Message{Role: llm.RoleAssistant}
	var text strings.Builder
	for ev := range ch {
		switch ev.Type {
		case llm.StreamText:
			text.WriteString(ev.Text)
			s.emit(Event{Type: EventTextDelta, Text: ev.Text})
		case llm.StreamToolCall:
			msg.ToolCalls = append(msg.ToolCalls, *ev.ToolCall)
		case llm.StreamUsage:
			s.emit(Event{Type: EventUsage, Usage: ev.Usage})
		case llm.StreamError:
			return msg, ev.Err
		}
	}
	msg.Text = text.String()
	return msg, nil
}

func (s *local) runToolCall(ctx context.Context, c llm.ToolCall) llm.Message {
	te := ToolEvent{ID: c.ID, Name: c.Name, Input: string(c.Input), Status: ToolPending}
	result := func(status ToolStatus, out string, start time.Time) llm.Message {
		te.Status, te.Output = status, out
		if !start.IsZero() {
			te.DurationMs = time.Since(start).Milliseconds()
		}
		s.emit(Event{Type: EventToolFinished, Tool: &te})
		return llm.Message{Role: llm.RoleTool, ToolCallID: c.ID, Text: out, IsError: status != ToolOK}
	}

	t, ok := s.cfg.Tools.Get(c.Name)
	if !ok {
		s.emit(Event{Type: EventToolStarted, Tool: &te})
		return result(ToolError, fmt.Sprintf("unknown tool %q", c.Name), time.Time{})
	}
	risk := t.Risk(c.Input)
	te.Risk = string(risk)
	s.emit(Event{Type: EventToolStarted, Tool: &te})

	if ctx.Err() != nil {
		return result(ToolCancelled, "cancelled by user", time.Time{})
	}

	verdict := s.cfg.Policy.Check(c.Name, risk)
	if verdict == permission.Ask && s.isAlwaysAllowed(c.Name) {
		verdict = permission.Allow
	}
	switch verdict {
	case permission.Deny:
		return result(ToolDenied, "denied by permission policy", time.Time{})
	case permission.Ask:
		d, err := s.awaitApproval(ctx, te)
		if err != nil {
			return result(ToolCancelled, "cancelled by user", time.Time{})
		}
		if d == DecisionDeny {
			return result(ToolDenied, "the user denied this action", time.Time{})
		}
		if d == DecisionAllowAlways {
			s.mu.Lock()
			s.always[c.Name] = true
			s.mu.Unlock()
		}
	}

	start := time.Now()
	res, err := t.Run(ctx, json.RawMessage(c.Input))
	if ctx.Err() != nil {
		return result(ToolCancelled, "cancelled by user", start)
	}
	if err != nil {
		return result(ToolError, err.Error(), start)
	}
	out := truncate(res.Output, maxToolOutput)
	if res.IsError {
		return result(ToolError, out, start)
	}
	return result(ToolOK, out, start)
}

func (s *local) isAlwaysAllowed(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.always[name]
}

func (s *local) awaitApproval(ctx context.Context, te ToolEvent) (Decision, error) {
	ch := make(chan Decision, 1)
	s.mu.Lock()
	s.pending[te.ID] = ch
	s.mu.Unlock()

	s.emit(Event{Type: EventApprovalRequested, Approval: &ApprovalRequest{ID: te.ID, Tool: te}})

	select {
	case d := <-ch:
		return d, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, te.ID)
		s.mu.Unlock()
		return DecisionDeny, ctx.Err()
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "") + fmt.Sprintf("\n... [truncated %d bytes]", len(s)-max)
}
