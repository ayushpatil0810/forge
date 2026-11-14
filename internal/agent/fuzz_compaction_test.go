package agent_test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
)

// generateRandomHistory generates a valid conversation history with random characteristics:
// - nTurns turns
// - 0 to 4 parallel tool calls per assistant message
// - tool results with error, empty text, or large text
func generateRandomHistory(r *rand.Rand, nTurns int) []llm.Message {
	var msgs []llm.Message
	msgSeq := 1

	for turn := 1; turn <= nTurns; turn++ {
		// User message
		msgs = append(msgs, llm.Message{
			ID:   fmt.Sprintf("msg_%d", msgSeq),
			Role: llm.RoleUser,
			Text: fmt.Sprintf("User prompt for turn %d (seed %d)", turn, r.Intn(1000)),
		})
		msgSeq++

		// Decide if this turn makes tool calls
		numCalls := r.Intn(4) // 0, 1, 2, or 3 parallel tool calls
		if numCalls > 0 {
			var tcs []llm.ToolCall
			var callIDs []string
			for c := 0; c < numCalls; c++ {
				callID := fmt.Sprintf("call_%d_%d", turn, c)
				callIDs = append(callIDs, callID)
				toolName := "read_file"
				if c%2 == 1 {
					toolName = "bash"
				}
				tcs = append(tcs, llm.ToolCall{
					ID:    callID,
					Name:  toolName,
					Input: []byte(fmt.Sprintf(`{"arg":"val_%d"}`, c)),
				})
			}

			// Assistant with tool calls
			msgs = append(msgs, llm.Message{
				ID:        fmt.Sprintf("msg_%d", msgSeq),
				Role:      llm.RoleAssistant,
				Text:      fmt.Sprintf("Calling %d tools in parallel", numCalls),
				ToolCalls: tcs,
			})
			msgSeq++

			// Tool results (MUST answer every tool call in order)
			for _, cid := range callIDs {
				isErr := r.Float64() < 0.25 // 25% error rate
				resultText := fmt.Sprintf("Result for %s", cid)
				outcome := r.Intn(4)
				switch outcome {
				case 0:
					resultText = "" // empty output
				case 1:
					resultText = "short"
				case 2:
					resultText = fmt.Sprintf("Error occurred: command %s failed", cid)
				case 3:
					resultText = fmt.Sprintf("Large payload data %d", r.Intn(100000))
				}

				msgs = append(msgs, llm.Message{
					ID:         fmt.Sprintf("msg_%d", msgSeq),
					Role:       llm.RoleTool,
					ToolCallID: cid,
					Text:       resultText,
					IsError:    isErr,
				})
				msgSeq++
			}
		}

		// Assistant response
		msgs = append(msgs, llm.Message{
			ID:   fmt.Sprintf("msg_%d", msgSeq),
			Role: llm.RoleAssistant,
			Text: fmt.Sprintf("Assistant completed turn %d", turn),
		})
		msgSeq++
	}

	return msgs
}

func TestCompactionRandomizedPropertyChecks(t *testing.T) {
	validator := mock.NewValidator()
	r := rand.New(rand.NewSource(42))

	// Run 100 property iterations with varying turn counts and compaction boundaries
	for iter := 0; iter < 100; iter++ {
		numTurns := 1 + r.Intn(30)
		history := generateRandomHistory(r, numTurns)

		// Pre-condition: original history must pass all validator rules
		if err := validator.ValidateAll(history); err != nil {
			t.Fatalf("iter %d: generated history is invalid: %v", iter, err)
		}

		// Test Tier 1 alone: clear some tool outputs
		t1Summary, _ := agent.IdentifyToolOutputsToClear(history, len(history), nil, 0, 0.25, 100000)
		t1Record := agent.CompactionRecord{
			ID:      fmt.Sprintf("cmp_t1_%d", iter),
			Tier:    1,
			Summary: fmt.Sprintf(`{"stubs":%s}`, fmt.Sprintf("%v", t1Summary.Stubs)),
		}

		// Test Tier 2: pick random boundary
		var compactions []agent.CompactionRecord
		if numTurns > 6 && r.Float64() < 0.8 {
			boundaryTurn := 1 + r.Intn(numTurns-6)
			// Find message ID of the turn boundary
			userIndices := []int{}
			for i, m := range history {
				if m.Role == llm.RoleUser {
					userIndices = append(userIndices, i)
				}
			}
			boundaryMsgIdx := userIndices[boundaryTurn]
			lastCoveredID := history[boundaryMsgIdx-1].ID
			firstCoveredID := history[0].ID

			compactions = append(compactions, agent.CompactionRecord{
				ID:             fmt.Sprintf("cmp_t2_%d", iter),
				Tier:           2,
				Summary:        fmt.Sprintf("Randomized summary for iter %d up to turn %d", iter, boundaryTurn),
				FirstMessageID: firstCoveredID,
				LastMessageID:  lastCoveredID,
				CreatedAt:      time.Now(),
			})
		}
		if r.Float64() < 0.5 {
			compactions = append(compactions, t1Record)
		}

		origLen := len(history)

		// Build model view
		view := agent.BuildModelView(history, compactions)

		// Property Assertion 1: Model view must validate against Anthropic, OpenAI, and Gemini rules
		if err := validator.ValidateAll(view); err != nil {
			t.Fatalf("iter %d (turns=%d, compactions=%d): compacted view failed validation: %v\nView length: %d\nHistory length: %d",
				iter, numTurns, len(compactions), err, len(view), len(history))
		}

		// Property Assertion 2: No orphan tool results
		toolCallsSeen := make(map[string]bool)
		for _, m := range view {
			switch m.Role {
			case llm.RoleAssistant:
				for _, tc := range m.ToolCalls {
					toolCallsSeen[tc.ID] = true
				}
			case llm.RoleTool:
				if !toolCallsSeen[m.ToolCallID] {
					t.Fatalf("iter %d: orphan tool result detected for ID %q", iter, m.ToolCallID)
				}
			}
		}

		// Property Assertion 3: History slice was never altered
		if len(history) != origLen {
			t.Fatalf("iter %d: history length modified: expected %d, got %d", iter, origLen, len(history))
		}
	}
}

func FuzzCompactionValidity(f *testing.F) {
	// Seed the fuzzer with diverse turn counts and keepTurn values
	f.Add(8, 6, int64(12345))
	f.Add(15, 6, int64(67890))
	f.Add(25, 4, int64(11223))
	f.Add(50, 10, int64(99887))

	validator := mock.NewValidator()

	f.Fuzz(func(t *testing.T, nTurns, keepTurns int, seed int64) {
		if nTurns < 1 || nTurns > 60 {
			return
		}
		if keepTurns < 1 || keepTurns > nTurns {
			return
		}

		r := rand.New(rand.NewSource(seed))
		history := generateRandomHistory(r, nTurns)

		boundary := agent.FindTurnBoundary(history, keepTurns)
		if boundary <= 0 {
			return
		}

		lastID := history[boundary-1].ID
		firstID := history[0].ID

		compactions := []agent.CompactionRecord{
			{
				ID:             "fuzz_cmp_t2",
				Tier:           2,
				Summary:        "Fuzz generated summary of prior conversation turns.",
				FirstMessageID: firstID,
				LastMessageID:  lastID,
			},
		}

		view := agent.BuildModelView(history, compactions)

		// Assert strict validity for all providers
		if err := validator.ValidateAll(view); err != nil {
			t.Fatalf("fuzz validation failure: %v (nTurns=%d, keepTurns=%d)", err, nTurns, keepTurns)
		}

		// Assert first message is user
		if len(view) > 0 && view[0].Role != llm.RoleUser {
			t.Fatalf("first message in compacted view must be user, got %s", view[0].Role)
		}
	})
}
