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
	if cfg.Tier1Threshold <= 0 {
		cfg.Tier1Threshold = 0.60
	}
	if cfg.Tier2Threshold <= 0 {
		cfg.Tier2Threshold = 0.80
	}
	if cfg.EmergencyThreshold <= 0 {
		cfg.EmergencyThreshold = 0.92
	}
	if cfg.KeepRecentTurns <= 0 {
		cfg.KeepRecentTurns = 6
	}
	if cfg.SummaryMaxTokens <= 0 {
		cfg.SummaryMaxTokens = 1500
	}
	if cfg.Tools == nil {
		cfg.Tools = tools.NewRegistry()
	}
	autoCompact := true
	if cfg.NoAutoCompact {
		autoCompact = false
	}
	id := cfg.ID
	if id == "" {
		id = fmt.Sprintf("s_%d", time.Now().UnixNano())
	}
	history := make([]llm.Message, len(cfg.History))
	copy(history, cfg.History)
	var seq atomic.Int64
	for i := range history {
		if history[i].ID == "" {
			history[i].ID = fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), seq.Add(1))
		}
	}
	compactions := make([]CompactionRecord, len(cfg.Compactions))
	copy(compactions, cfg.Compactions)

	return &local{
		id:              id,
		cfg:             cfg,
		autoCompact:     autoCompact,
		events:          make(chan Event, 256),
		closed:          make(chan struct{}),
		pending:         map[string]chan approvalResponse{},
		always:          map[string]bool{},
		history:         history,
		compactions:     compactions,
		calibratedRatio: 0.25,
		budget:          NewBudgetTracker(cfg.MaxCostBudget, cfg.MaxTokensBudget),
		contextUsage: ContextUsagePayload{
			Window: cfg.ContextWindow,
			Known:  cfg.ContextWindow > 0,
		},
		usage: UsageUpdatedPayload{
			InputTokens: cfg.InitialTokens,
			Cost:        cfg.InitialCost,
		},
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

	mu              sync.Mutex // guards the fields below
	mutatingMu      sync.Mutex // ensures bash, edit_file, write_file never run concurrently
	running         bool
	isClosed        bool
	cancel          context.CancelFunc
	pending         map[string]chan approvalResponse
	always          map[string]bool
	usage           UsageUpdatedPayload
	contextUsage    ContextUsagePayload
	calibratedRatio float64
	autoCompact     bool
	budget          *BudgetTracker
	budgetExceeded  bool
	compactionUsage UsageUpdatedPayload
	closeOnce       sync.Once

	history     []llm.Message      // full uncompacted transcript for the life of the session (Amendment E)
	compactions []CompactionRecord // records of all compactions applied to build model view

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

func (s *local) ContextUsage() ContextUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contextUsage
}

func (s *local) SetBudget(maxCost float64, maxTokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.budget != nil {
		s.budget.SetBudget(maxCost, maxTokens)
	}
	s.budgetExceeded = false
	if s.cfg.AuditLogger != nil {
		_ = s.cfg.AuditLogger.Log(context.Background(), s.id, "budget_raised", "user", fmt.Sprintf("max_cost: $%.2f, max_tokens: %d", maxCost, maxTokens), "allow", "user", nil, nil, "")
	}
}

func (s *local) History() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]llm.Message, len(s.history))
	for i, m := range s.history {
		cp[i] = cloneMessage(m)
	}
	return cp
}

func (s *local) ModelView() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return BuildModelView(s.history, s.compactions)
}

func (s *local) buildModelView() []llm.Message {
	return BuildModelView(s.history, s.compactions)
}

func (s *local) Compact(ctx context.Context, focusText ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	focus := ""
	if len(focusText) > 0 {
		focus = focusText[0]
	}
	return s.compactLocked(ctx, 2, "manual", focus)
}

func (s *local) compactLocked(ctx context.Context, requestedTier int, reason string, focusText string) error {
	currentView := BuildModelView(s.history, s.compactions)
	boundary := FindTurnBoundary(currentView, s.cfg.KeepRecentTurns)
	if boundary <= 0 {
		return nil
	}

	toolSpecs := s.cfg.Tools.Specs()
	charsBefore := EstimateChars(currentView, s.cfg.System, toolSpecs)
	tokensBefore := int(float64(charsBefore) * s.calibratedRatio)

	s.emit(Event{
		Type: EventCompactionStarted,
		CompactionStart: &CompactionStartedPayload{
			Tier:   requestedTier,
			Reason: reason,
		},
	})

	tracker := s.cfg.Tools.Tracker()
	targetTokens := 0
	if s.contextUsage.Window > 0 {
		targetTokens = int(s.cfg.Tier1Threshold * float64(s.contextUsage.Window))
	}

	t1Summary, clearedCount := IdentifyToolOutputsToClear(currentView, boundary, tracker, targetTokens, s.calibratedRatio, tokensBefore)
	if clearedCount > 0 {
		t1JSON, _ := json.Marshal(t1Summary)
		firstID := ""
		lastID := ""
		if len(currentView) > 0 {
			firstID = currentView[0].ID
		}
		if boundary > 0 && boundary <= len(currentView) {
			lastID = currentView[boundary-1].ID
		}
		rec := CompactionRecord{
			ID:             fmt.Sprintf("cmp_t1_%d", time.Now().UnixNano()),
			SessionID:      s.id,
			Tier:           1,
			Summary:        string(t1JSON),
			FirstMessageID: firstID,
			LastMessageID:  lastID,
			TokensBefore:   tokensBefore,
			TokensAfter:    tokensBefore,
			CreatedAt:      time.Now().UTC(),
		}
		s.compactions = append(s.compactions, rec)
		if s.cfg.SessionStore != nil {
			_ = s.cfg.SessionStore.SaveCompaction(ctx, rec)
		}
	}

	if requestedTier < 2 {
		viewAfter := BuildModelView(s.history, s.compactions)
		charsAfter := EstimateChars(viewAfter, s.cfg.System, toolSpecs)
		tokensAfter := int(float64(charsAfter) * s.calibratedRatio)
		s.contextUsage.Used = tokensAfter
		if s.contextUsage.Window > 0 {
			s.contextUsage.Percent = (tokensAfter * 100) / s.contextUsage.Window
		}
		s.emit(Event{
			Type: EventCompactionFinished,
			CompactionDone: &CompactionFinishedPayload{
				Tier:         1,
				TokensBefore: tokensBefore,
				TokensAfter:  tokensAfter,
				SummaryChars: 0,
				OK:           true,
			},
		})
		s.emit(Event{Type: EventContextUsageUpdated, ContextUsage: &s.contextUsage})
		return nil
	}

	// Tier 2: Summarization
	currentView = BuildModelView(s.history, s.compactions)
	boundary = FindTurnBoundary(currentView, s.cfg.KeepRecentTurns)
	if boundary <= 0 {
		return nil
	}

	summary, inTok, outTok, cost, err := GenerateSummary(ctx, s.cfg.Provider, "", currentView, boundary, s.cfg.SummaryMaxTokens, focusText, s.budget)
	if err != nil {
		s.emit(Event{
			Type:   EventNotice,
			Notice: fmt.Sprintf("Context summarization failed (%v); keeping cleared tool outputs only", err),
		})
		viewAfter := BuildModelView(s.history, s.compactions)
		charsAfter := EstimateChars(viewAfter, s.cfg.System, toolSpecs)
		tokensAfter := int(float64(charsAfter) * s.calibratedRatio)
		s.contextUsage.Used = tokensAfter
		if s.contextUsage.Window > 0 {
			s.contextUsage.Percent = (tokensAfter * 100) / s.contextUsage.Window
		}
		s.emit(Event{
			Type: EventCompactionFinished,
			CompactionDone: &CompactionFinishedPayload{
				Tier:         1,
				TokensBefore: tokensBefore,
				TokensAfter:  tokensAfter,
				SummaryChars: 0,
				OK:           false,
				Error:        err.Error(),
			},
		})
		s.emit(Event{Type: EventContextUsageUpdated, ContextUsage: &s.contextUsage})

		if s.contextUsage.Window > 0 && tokensAfter >= int(s.cfg.EmergencyThreshold*float64(s.contextUsage.Window)) {
			return fmt.Errorf("context length %d exceeds emergency threshold (92%%) and automatic summarization was unavailable: %w", tokensAfter, err)
		}
		return nil
	}

	s.compactionUsage.InputTokens += inTok
	s.compactionUsage.OutputTokens += outTok
	s.compactionUsage.Cost += cost
	s.usage.CompactionTokens += (inTok + outTok)
	s.usage.CompactionCost += cost
	s.usage.InputTokens += inTok
	s.usage.OutputTokens += outTok
	s.usage.Cost += cost

	firstID := ""
	lastID := ""
	if len(currentView) > 0 {
		firstID = currentView[0].ID
	}
	if boundary > 0 && boundary <= len(currentView) {
		lastID = currentView[boundary-1].ID
	}

	rec := CompactionRecord{
		ID:             fmt.Sprintf("cmp_t2_%d", time.Now().UnixNano()),
		SessionID:      s.id,
		Tier:           2,
		Summary:        summary,
		FirstMessageID: firstID,
		LastMessageID:  lastID,
		TokensBefore:   tokensBefore,
		TokensAfter:    0,
		CreatedAt:      time.Now().UTC(),
	}

	s.compactions = append(s.compactions, rec)

	viewAfter := BuildModelView(s.history, s.compactions)
	charsAfter := EstimateChars(viewAfter, s.cfg.System, toolSpecs)
	tokensAfter := int(float64(charsAfter) * s.calibratedRatio)
	rec.TokensAfter = tokensAfter

	if s.cfg.SessionStore != nil {
		_ = s.cfg.SessionStore.SaveCompaction(ctx, rec)
	}

	s.contextUsage.Used = tokensAfter
	if s.contextUsage.Window > 0 {
		s.contextUsage.Percent = (tokensAfter * 100) / s.contextUsage.Window
	}

	s.emit(Event{
		Type: EventCompactionFinished,
		CompactionDone: &CompactionFinishedPayload{
			Tier:         2,
			TokensBefore: tokensBefore,
			TokensAfter:  tokensAfter,
			SummaryChars: len(summary),
			OK:           true,
		},
	})
	s.emit(Event{Type: EventContextUsageUpdated, ContextUsage: &s.contextUsage})
	return nil
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
	s.mu.Lock()
	userMsg := llm.Message{
		ID:   fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), s.seq.Add(1)),
		Role: llm.RoleUser,
		Text: prompt,
	}
	s.history = append(s.history, userMsg)
	s.mu.Unlock()

	var lastToolCallsSig string
	var consecutiveRepeats int
	failingCmdCounts := make(map[string]int)
	var lastEditKey string
	var reactiveDone bool

	for turn := 0; turn < s.cfg.MaxTurns; turn++ {
		// Budget check before calling model
		s.mu.Lock()
		if s.budgetExceeded {
			s.mu.Unlock()
			return Event{Type: EventDone, Done: &DonePayload{Reason: "budget_exceeded"}}
		}
		if s.budget != nil {
			if err := s.budget.CheckPreCall(); err != nil {
				s.budgetExceeded = true
				s.mu.Unlock()
				return Event{Type: EventDone, Done: &DonePayload{Reason: "budget_exceeded"}}
			}
		}

		// Auto-compaction check before model call (Amendment E: operates on model view)
		toolSpecs := s.cfg.Tools.Specs()
		currentView := s.buildModelView()
		chars := EstimateChars(currentView, s.cfg.System, toolSpecs)
		estTokens := int(float64(chars) * s.calibratedRatio)
		s.contextUsage.Used = estTokens
		if s.contextUsage.Window > 0 {
			s.contextUsage.Percent = (estTokens * 100) / s.contextUsage.Window
		}

		if s.autoCompact && s.contextUsage.Window > 0 {
			if estTokens >= int(s.cfg.EmergencyThreshold*float64(s.contextUsage.Window)) {
				if err := s.compactLocked(ctx, 2, "emergency", ""); err != nil {
					s.mu.Unlock()
					return Event{
						Type: EventError,
						Err: &ErrorPayload{
							Code:     ErrCodeContextLength,
							Message:  err.Error(),
							Guidance: "Conversation length reached emergency threshold (92%) and automatic summarization was unavailable. Start a new session or switch models.",
						},
					}
				}
			} else if estTokens >= int(s.cfg.Tier2Threshold*float64(s.contextUsage.Window)) {
				_ = s.compactLocked(ctx, 2, "tier2_threshold", "")
			} else if estTokens >= int(s.cfg.Tier1Threshold*float64(s.contextUsage.Window)) {
				_ = s.compactLocked(ctx, 1, "tier1_threshold", "")
			}
		}
		modelView := s.buildModelView()
		s.mu.Unlock()

		msg, err := s.streamModel(ctx, modelView)
		if ctx.Err() != nil {
			return Event{Type: EventDone, Done: &DonePayload{Reason: "cancelled"}}
		}
		if err != nil {
			code, guidance := ClassifyError(err)
			// Reactive compaction on context length error: compact once and retry once! Never loop.
			if code == ErrCodeContextLength && !reactiveDone {
				reactiveDone = true
				s.emit(Event{Type: EventNotice, Notice: "Context length exceeded. Running reactive compaction and retrying..."})
				s.mu.Lock()
				compactErr := s.compactLocked(ctx, 2, "reactive", "")
				retriedView := s.buildModelView()
				s.mu.Unlock()
				if compactErr == nil {
					msg, err = s.streamModel(ctx, retriedView)
					if err != nil {
						code, guidance = ClassifyError(err)
					}
				}
			}
			if err != nil {
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
		}

		s.mu.Lock()
		if msg.ID == "" {
			msg.ID = fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), s.seq.Add(1))
		}
		s.history = append(s.history, msg)
		if s.budgetExceeded {
			s.mu.Unlock()
			return Event{Type: EventDone, Done: &DonePayload{Reason: "budget_exceeded"}}
		}
		s.mu.Unlock()

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
			if resMsg.ID == "" {
				resMsg.ID = fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), s.seq.Add(1))
			}
			s.mu.Lock()
			s.history = append(s.history, resMsg)
			s.mu.Unlock()

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

func (s *local) streamModel(ctx context.Context, messages []llm.Message) (llm.Message, error) {
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

	s.mu.Lock()
	if s.budget != nil {
		if err := s.budget.CheckPreCall(); err != nil {
			s.budgetExceeded = true
			s.mu.Unlock()
			return llm.Message{}, err
		}
	}
	s.mu.Unlock()

	ch, err := s.cfg.Provider.Stream(retryCtx, llm.Request{
		System:    s.cfg.System,
		Messages:  messages,
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

				// Calibrate token estimation ratio
				chars := EstimateChars(messages, s.cfg.System, s.cfg.Tools.Specs())
				if chars > 0 && ev.Usage.InputTokens > 0 {
					s.calibratedRatio = float64(ev.Usage.InputTokens) / float64(chars)
				}
				s.contextUsage.Used = ev.Usage.InputTokens
				if s.contextUsage.Window > 0 {
					s.contextUsage.Percent = (ev.Usage.InputTokens * 100) / s.contextUsage.Window
				}

				var warnNotice string
				if s.budget != nil {
					exceeded, warnMsg := s.budget.RecordUsage(ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.Cost, false)
					if exceeded {
						s.budgetExceeded = true
					}
					warnNotice = warnMsg
				}

				currentUsage := s.usage
				currentContextUsage := s.contextUsage
				s.mu.Unlock()

				s.emit(Event{Type: EventUsageUpdated, Usage: &currentUsage})
				s.emit(Event{Type: EventContextUsageUpdated, ContextUsage: &currentContextUsage})
				if warnNotice != "" {
					s.emit(Event{Type: EventNotice, Notice: warnNotice})
				}
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
