package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ayushpatil0810/forge/internal/audit"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/retry"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/policy"
	"github.com/ayushpatil0810/forge/internal/redact"
	"github.com/ayushpatil0810/forge/internal/tools"
)

const (
	defaultMaxTurns         = 40
	defaultMaxRepeatedCalls = 3
	maxToolOutput           = 30_000 // bytes returned to the model per tool call
)

// NewLocal returns an in-process Session.
func NewLocal(cfg Config) Session {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = defaultMaxTurns
	}
	if cfg.MaxRepeatedCalls <= 0 {
		cfg.MaxRepeatedCalls = defaultMaxRepeatedCalls
	}
	if cfg.Tools == nil {
		cfg.Tools = tools.NewRegistry()
	}
	id := cfg.ID
	if id == "" {
		id = fmt.Sprintf("s_%d", time.Now().UnixNano())
	}
	return &local{
		id:      id,
		cfg:     cfg,
		events:  make(chan Event, 256),
		closed:  make(chan struct{}),
		pending: map[string]chan approvalResponse{},
		always:  map[string]bool{},
		history: cfg.History,
	}
}

type approvalResponse struct {
	decision Decision
	feedback string
}

type local struct {
	id     string
	cfg    Config
	events chan Event
	closed chan struct{}
	seq    atomic.Int64
	wg     sync.WaitGroup

	mu         sync.Mutex // guards the fields below
	mutatingMu sync.Mutex // ensures bash, edit_file, write_file never run concurrently
	running    bool
	isClosed   bool
	cancel     context.CancelFunc
	pending    map[string]chan approvalResponse
	always     map[string]bool
	usage      UsageUpdatedPayload
	closeOnce  sync.Once

	history []llm.Message // only touched by the single running turn

	checkpointTakenInTurn bool
	auditWarned           bool
}

func (s *local) ID() string { return s.id }

func (s *local) PolicySet() *policy.PolicySet { return s.cfg.PolicySet }

func (s *local) Events() <-chan Event { return s.events }

func (s *local) Send(ctx context.Context, prompt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed {
		return ErrClosed
	}
	if s.running {
		return ErrBusy
	}
	if ctx == nil {
		ctx = context.Background()
	}
	turnCtx, cancel := context.WithCancel(ctx)
	s.running, s.cancel = true, cancel
	s.wg.Add(1)
	go s.runTurn(turnCtx, cancel, prompt)
	return nil
}

func (s *local) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *local) Usage() Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage
}

func (s *local) Mode() permission.Mode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Policy.Mode
}

func (s *local) SetMode(m permission.Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Policy.Mode = m
}

func (s *local) Checkpointer() Checkpointer {
	return s.cfg.Checkpointer
}

func (s *local) SetProvider(p llm.Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Provider = p
}

func (s *local) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.isClosed = true
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
		close(s.closed) // unblocks any emit or await stuck on a stalled consumer
		s.wg.Wait()
		close(s.events)
	})
}

func (s *local) Respond(id string, d Decision, feedback ...string) error {
	s.mu.Lock()
	ch, ok := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if !ok {
		return ErrUnknownApproval
	}
	fb := ""
	if len(feedback) > 0 {
		fb = feedback[0]
	}
	ch <- approvalResponse{decision: d, feedback: fb} // buffered(1): never blocks
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
	s.mu.Lock()
	s.checkpointTakenInTurn = false
	s.mu.Unlock()
	final := s.loop(ctx, prompt)
	cancel()
	// Mark idle BEFORE the terminal event so a frontend can Send as soon as it sees it.
	s.mu.Lock()
	s.running, s.cancel = false, nil
	s.mu.Unlock()
	s.emit(final)
}

// loop is the agent loop: model -> tools -> model ... until no tool calls or stopped.
func (s *local) loop(ctx context.Context, prompt string) Event {
	s.history = append(s.history, llm.Message{Role: llm.RoleUser, Text: prompt})

	var lastToolCallsSig string
	var consecutiveRepeats int
	failingCmdCounts := make(map[string]int)
	var lastEditKey string

	for turn := 0; turn < s.cfg.MaxTurns; turn++ {
		msg, err := s.streamModel(ctx)
		if ctx.Err() != nil {
			return Event{Type: EventDone, Done: &DonePayload{Reason: "cancelled"}}
		}
		if err != nil {
			code, guidance := ClassifyError(err)
			return Event{
				Type: EventError,
				Err: &ErrorPayload{
					Code:      code,
					Message:   RedactSecrets(err.Error()),
					Guidance:  guidance,
					Retryable: code == ErrCodeRateLimit || code == ErrCodeNetwork,
				},
			}
		}
		s.history = append(s.history, msg)
		if len(msg.ToolCalls) == 0 {
			return Event{Type: EventDone, Done: &DonePayload{Reason: "completed"}}
		}

		// Loop guard A-5: detect repeated identical calls.
		var sig strings.Builder
		for _, tc := range msg.ToolCalls {
			sig.WriteString(tc.Name)
			sig.WriteString(":")
			sig.Write(tc.Input)
			sig.WriteString(";")
		}
		currentSig := sig.String()
		if currentSig == lastToolCallsSig {
			consecutiveRepeats++
			if consecutiveRepeats >= s.cfg.MaxRepeatedCalls {
				return Event{
					Type: EventError,
					Err: &ErrorPayload{
						Code:      "runaway_loop",
						Message:   fmt.Sprintf("detected runaway loop: %d consecutive identical calls (%s)", consecutiveRepeats, RedactSecrets(currentSig)),
						Guidance:  "Agent loop guard interrupted execution to prevent excessive API costs.",
						Retryable: false,
					},
				}
			}
		} else {
			lastToolCallsSig = currentSig
			consecutiveRepeats = 1
		}

		// Always append one result per call, even when cancelled, or the
		// history becomes invalid for the next request.
		for _, call := range msg.ToolCalls {
			resMsg := s.runToolCall(ctx, call)
			s.history = append(s.history, resMsg)

			// Fix loop stall guard: if bash fails 3 times with the exact same file content, abort
			if call.Name == "bash" && resMsg.IsError {
				var bArgs struct {
					Command string `json:"command"`
				}
				_ = json.Unmarshal(call.Input, &bArgs)
				key := bArgs.Command + "@" + lastEditKey
				failingCmdCounts[key]++
				if failingCmdCounts[key] >= 3 {
					return Event{
						Type: EventError,
						Err: &ErrorPayload{
							Code:      "fix_loop_stalled",
							Message:   fmt.Sprintf("detected stalled fix loop: command %q failed 3 times with identical file edits", bArgs.Command),
							Guidance:  "The fix loop stalled because repeated attempts failed to fix the error. Review the test failure and code manually.",
							Retryable: false,
						},
					}
				}
			}
			if (call.Name == "edit_file" || call.Name == "write_file") && !resMsg.IsError {
				lastEditKey = string(call.Input)
			}
		}
		if ctx.Err() != nil {
			return Event{Type: EventDone, Done: &DonePayload{Reason: "cancelled"}}
		}
	}
	return Event{
		Type: EventError,
		Err: &ErrorPayload{
			Code:      "max_turns",
			Message:   fmt.Sprintf("stopped after %d model calls (MaxTurns)", s.cfg.MaxTurns),
			Guidance:  "Reached maximum conversation turns limit. Adjust max_turns in configuration if more turns are needed.",
			Retryable: false,
		},
	}
}

func (s *local) streamModel(ctx context.Context) (llm.Message, error) {
	retryCtx := retry.ContextWithOnRetry(ctx, func(attempt, maxAttempts int, delay time.Duration, err error) {
		redactedErr := ""
		if err != nil {
			redactedErr = RedactSecrets(err.Error())
		}
		s.emit(Event{
			Type: EventRetryScheduled,
			Retry: &RetryScheduledPayload{
				Attempt:     attempt,
				MaxAttempts: maxAttempts,
				DelayMs:     delay.Milliseconds(),
				Error:       redactedErr,
			},
		})
	})

	ch, err := s.cfg.Provider.Stream(retryCtx, llm.Request{
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
			s.emit(Event{Type: EventTokenDelta, Token: &TokenDeltaPayload{Text: ev.Text}})
		case llm.StreamThinking:
			s.emit(Event{Type: EventThinkingDelta, Thinking: &ThinkingDeltaPayload{Thinking: ev.Text}})
		case llm.StreamToolCall:
			if ev.ToolCall != nil {
				msg.ToolCalls = append(msg.ToolCalls, *ev.ToolCall)
			}
		case llm.StreamUsage:
			if ev.Usage != nil {
				s.mu.Lock()
				s.usage.InputTokens += ev.Usage.InputTokens
				s.usage.OutputTokens += ev.Usage.OutputTokens
				s.usage.Cost += ev.Usage.Cost
				currentUsage := s.usage
				s.mu.Unlock()
				s.emit(Event{Type: EventUsageUpdated, Usage: &currentUsage})
			}
		case llm.StreamError:
			return msg, ev.Err
		}
	}
	msg.Text = text.String()
	return msg, nil
}

func (s *local) runToolCall(ctx context.Context, c llm.ToolCall) llm.Message {
	te := ToolFinishedPayload{ID: c.ID, Name: c.Name, Status: ToolPending}
	result := func(status ToolStatus, out string, errMsg string, start time.Time) llm.Message {
		te.Status, te.Result, te.Error = status, out, errMsg
		if !start.IsZero() {
			te.DurationMs = time.Since(start).Milliseconds()
		}
		s.emit(Event{Type: EventToolFinished, ToolDone: &te})
		return llm.Message{Role: llm.RoleTool, ToolCallID: c.ID, Text: out, IsError: status != ToolOK}
	}

	t, ok := s.cfg.Tools.Get(c.Name)
	if !ok {
		s.emit(Event{Type: EventToolStarted, ToolStart: &ToolStartedPayload{ID: c.ID, Name: c.Name, Input: string(c.Input)}})
		return result(ToolError, fmt.Sprintf("unknown tool %q", c.Name), fmt.Sprintf("unknown tool %q", c.Name), time.Time{})
	}

	s.emit(Event{Type: EventToolStarted, ToolStart: &ToolStartedPayload{ID: c.ID, Name: c.Name, Input: string(c.Input)}})

	if ctx.Err() != nil {
		return result(ToolCancelled, "cancelled by user", "cancelled by user", time.Time{})
	}

	appReq := t.NeedsApproval(c.Input)
	var evalRes policy.EvalResult
	if s.cfg.PolicySet != nil {
		evalRes = policy.Evaluate(policy.EvalRequest{
			ToolName:    c.Name,
			Input:       string(c.Input),
			Path:        appReq.Path,
			Command:     appReq.Command,
			IsProtected: appReq.IsProtected,
			IsDangerous: appReq.IsDangerous,
			Mode:        s.Mode(),
			IsHeadless:  s.cfg.IsHeadless,
			AutoApprove: s.cfg.AutoApprove,
		}, s.cfg.PolicySet)
	} else {
		verdict := s.cfg.Policy.Check(c.Name, appReq)
		evalRes = policy.EvalResult{
			Verdict: verdict,
			Source:  "mode",
		}
	}

	if evalRes.Verdict == permission.Ask && s.isAlwaysAllowed(c.Name) && !evalRes.IsFloor {
		evalRes.Verdict = permission.Allow
		evalRes.Source = "session"
	}

	switch evalRes.Verdict {
	case permission.Deny:
		if msg := s.writeAudit(ctx, "decision", c.Name, string(c.Input), "deny", evalRes.Source, nil, nil, ""); msg != "" {
			return result(ToolDenied, msg, msg, time.Time{})
		}
		return result(ToolDenied, "denied by permission policy", "denied by permission policy", time.Time{})
	case permission.Ask:
		if evalRes.IsFloor {
			appReq.IsDangerous = true
		}
		resp, err := s.awaitApproval(ctx, c.ID, c.Name, string(c.Input), appReq)
		if err != nil {
			return result(ToolCancelled, "cancelled by user", "cancelled by user", time.Time{})
		}
		switch resp.decision {
		case DecisionDeny:
			if msg := s.writeAudit(ctx, "decision", c.Name, string(c.Input), "deny", "user", nil, nil, ""); msg != "" {
				return result(ToolDenied, msg, msg, time.Time{})
			}
			return result(ToolDenied, "the user denied this action", "the user denied this action", time.Time{})
		case DecisionDenyWithFeedback:
			fb := redact.Redact(resp.feedback)
			if len(fb) > 500 {
				fb = fb[:500]
			}
			if msg := s.writeAudit(ctx, "decision", c.Name, string(c.Input), "deny", "user-feedback", nil, nil, ""); msg != "" {
				return result(ToolDenied, msg, msg, time.Time{})
			}
			msg := "the user denied this action: " + fb
			return result(ToolDenied, msg, msg, time.Time{})
		case DecisionAllowSession:
			s.mu.Lock()
			s.always[c.Name] = true
			s.mu.Unlock()
			if msg := s.writeAudit(ctx, "decision", c.Name, string(c.Input), "allow", "session", nil, nil, ""); msg != "" {
				return result(ToolDenied, msg, msg, time.Time{})
			}
		case DecisionAlwaysAllow:
			if s.cfg.PolicySet != nil {
				ruleStr := generateNarrowRule(c.Name, string(c.Input), appReq)
				_ = s.cfg.PolicySet.AddRule(policy.ScopeUser, policy.ActionAllow, ruleStr)
			}
			if msg := s.writeAudit(ctx, "decision", c.Name, string(c.Input), "allow", "user-saved", nil, nil, ""); msg != "" {
				return result(ToolDenied, msg, msg, time.Time{})
			}
		default: // DecisionAllow
			if msg := s.writeAudit(ctx, "decision", c.Name, string(c.Input), "allow", "user", nil, nil, ""); msg != "" {
				return result(ToolDenied, msg, msg, time.Time{})
			}
		}
	case permission.Allow:
		if msg := s.writeAudit(ctx, "decision", c.Name, string(c.Input), "allow", evalRes.Source, nil, nil, ""); msg != "" {
			return result(ToolDenied, msg, msg, time.Time{})
		}
	}

	if isMutatingTool(c.Name) {
		s.mutatingMu.Lock()
		defer s.mutatingMu.Unlock()

		if s.cfg.Checkpointer != nil {
			s.mu.Lock()
			needCheckpoint := !s.checkpointTakenInTurn
			if needCheckpoint {
				s.checkpointTakenInTurn = true
			}
			s.mu.Unlock()

			if needCheckpoint {
				info, err := s.cfg.Checkpointer.Create(ctx, fmt.Sprintf("Before %s in turn", c.Name))
				if err == nil {
					s.emit(Event{
						Type: EventCheckpointCreated,
						Checkpt: &CheckpointCreatedPayload{
							ID:    info.ID,
							Files: info.SkippedFiles,
						},
					})
				}
			}
		}
	}

	start := time.Now()
	streamer := redact.NewStream(func(chunk string) {
		s.emit(Event{
			Type: EventToolOutputDelta,
			ToolOut: &ToolOutputDeltaPayload{
				ID:    c.ID,
				Chunk: chunk,
			},
		})
	})
	streamCtx := tools.ContextWithStream(ctx, streamer.Write)
	res, err := t.Run(streamCtx, json.RawMessage(c.Input))
	streamer.Flush()
	if ctx.Err() != nil {
		return result(ToolCancelled, "cancelled by user", "cancelled by user", start)
	}
	if err != nil {
		return result(ToolError, err.Error(), err.Error(), start)
	}
	cleanOutput := redact.Redact(res.Output)
	out := truncate(cleanOutput, maxToolOutput)

	var exitCode *int
	if res.IsError {
		ec := 1
		exitCode = &ec
	} else {
		ec := 0
		exitCode = &ec
	}
	durMs := time.Since(start).Milliseconds()

	if msg := s.writeAudit(ctx, "tool_result", c.Name, string(c.Input), "", "", exitCode, &durMs, cleanOutput); msg != "" {
		return result(ToolError, msg, msg, start)
	}

	if res.IsError {
		return result(ToolError, out, out, start)
	}
	return result(ToolOK, out, "", start)
}

func generateNarrowRule(toolName, input string, req tools.ApprovalRequest) string {
	switch toolName {
	case "bash":
		if req.Command != "" {
			return fmt.Sprintf("bash(%s)", req.Command)
		}
		var args struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal([]byte(input), &args)
		return fmt.Sprintf("bash(%s)", args.Command)
	case "edit_file", "write_file":
		p := req.Path
		if p == "" {
			var args struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal([]byte(input), &args)
			p = args.Path
		}
		return fmt.Sprintf("edit(%s)", p)
	case "read_file":
		p := req.Path
		if p == "" {
			var args struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal([]byte(input), &args)
			p = args.Path
		}
		return fmt.Sprintf("read(%s)", p)
	default:
		return fmt.Sprintf("%s(*)", toolName)
	}
}

func isMutatingTool(name string) bool {
	return name == "bash" || name == "edit_file" || name == "write_file"
}

func (s *local) writeAudit(ctx context.Context, event, tool, input, decision, source string, exitCode *int, dur *int64, output string) string {
	if s.cfg.AuditLogger == nil {
		return ""
	}
	err := s.cfg.AuditLogger.Log(ctx, s.id, event, tool, audit.SummarizeAction(tool, input), decision, source, exitCode, dur, output)
	if err != nil && errors.Is(err, audit.ErrRequired) {
		return "audit log write failed and audit.required is true; this tool call was denied. " + err.Error()
	}
	if w := s.cfg.AuditLogger.Warning(); w != "" {
		s.mu.Lock()
		already := s.auditWarned
		s.auditWarned = true
		s.mu.Unlock()
		if !already {
			s.emit(Event{Type: EventNotice, Notice: w})
		}
	}
	return ""
}

func (s *local) isAlwaysAllowed(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.always[name]
}

func (s *local) awaitApproval(ctx context.Context, id, name, input string, req tools.ApprovalRequest) (approvalResponse, error) {
	ch := make(chan approvalResponse, 1)
	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()

	s.emit(Event{
		Type: EventApprovalRequested,
		Approval: &ApprovalRequestedPayload{
			ID:             id,
			Tool:           name,
			Input:          input,
			Description:    req.Description,
			Diff:           req.Diff,
			Path:           req.Path,
			Command:        req.Command,
			Cwd:            req.Cwd,
			TimeoutSeconds: req.TimeoutSeconds,
			IsDangerous:    req.IsDangerous,
			IsProtected:    req.IsProtected,
		},
	})

	select {
	case d := <-ch:
		return d, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return approvalResponse{decision: DecisionDeny}, ctx.Err()
	case <-s.closed:
		return approvalResponse{decision: DecisionDeny}, ErrClosed
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "") + fmt.Sprintf("\n... [truncated %d bytes]", len(s)-max)
}
