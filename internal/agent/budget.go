package agent

import (
	"fmt"
	"sync"
)

// BudgetTracker enforces per-session run token and cost budgets (SPEC A-6).
// Spend scope is since this run started.
type BudgetTracker struct {
	mu                  sync.Mutex
	maxCost             float64
	maxTokens           int
	runInputTokens      int
	runOutputTokens     int
	runCompactionTokens int
	runCost             float64
	warned80            bool
}

// NewBudgetTracker initializes a BudgetTracker with the given limits.
func NewBudgetTracker(maxCost float64, maxTokens int) *BudgetTracker {
	return &BudgetTracker{
		maxCost:   maxCost,
		maxTokens: maxTokens,
	}
}

// SetBudget dynamically updates the budget limits for this session (e.g. via /budget).
func (b *BudgetTracker) SetBudget(maxCost float64, maxTokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if maxCost > 0 {
		b.maxCost = maxCost
	}
	if maxTokens > 0 {
		b.maxTokens = maxTokens
	}
	// Reset 80% warning if budget was raised above current spend
	if b.maxCost > 0 && b.runCost < 0.80*b.maxCost {
		b.warned80 = false
	}
	if b.maxTokens > 0 && (b.runInputTokens+b.runOutputTokens+b.runCompactionTokens) < int(0.80*float64(b.maxTokens)) {
		b.warned80 = false
	}
}

// CheckPreCall checks whether the budget has already been reached before issuing a call.
// Returns an error if the budget is exhausted.
func (b *BudgetTracker) CheckPreCall() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.maxCost > 0 && b.runCost >= b.maxCost {
		return fmt.Errorf("budget exceeded: run cost $%.4f reached maximum allowed $%.2f", b.runCost, b.maxCost)
	}

	totalTokens := b.runInputTokens + b.runOutputTokens + b.runCompactionTokens
	if b.maxTokens > 0 && totalTokens >= b.maxTokens {
		return fmt.Errorf("budget exceeded: run tokens %d reached maximum allowed %d", totalTokens, b.maxTokens)
	}
	return nil
}

// RecordUsage updates run spend after a response or compaction call and checks for 80% warning.
// Returns (exceeded bool, warnMsg string).
func (b *BudgetTracker) RecordUsage(inputTokens, outputTokens int, cost float64, isCompaction bool) (bool, string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if isCompaction {
		b.runCompactionTokens += (inputTokens + outputTokens)
	} else {
		b.runInputTokens += inputTokens
		b.runOutputTokens += outputTokens
	}
	b.runCost += cost

	totalTokens := b.runInputTokens + b.runOutputTokens + b.runCompactionTokens

	var warnMsg string
	if !b.warned80 {
		cost80 := b.maxCost > 0 && b.runCost >= 0.80*b.maxCost
		tok80 := b.maxTokens > 0 && float64(totalTokens) >= 0.80*float64(b.maxTokens)
		if cost80 || tok80 {
			b.warned80 = true
			if cost80 {
				warnMsg = fmt.Sprintf("Warning: spend reached 80%% of cost budget ($%.4f / $%.2f)", b.runCost, b.maxCost)
			} else {
				warnMsg = fmt.Sprintf("Warning: token usage reached 80%% of token budget (%d / %d tokens)", totalTokens, b.maxTokens)
			}
		}
	}

	exceeded := false
	if b.maxCost > 0 && b.runCost >= b.maxCost {
		exceeded = true
	}
	if b.maxTokens > 0 && totalTokens >= b.maxTokens {
		exceeded = true
	}

	return exceeded, warnMsg
}

// Snapshot returns the current run budget state.
func (b *BudgetTracker) Snapshot() (runCost float64, maxCost float64, runTokens int, maxTokens int, compactionTokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	totalTokens := b.runInputTokens + b.runOutputTokens + b.runCompactionTokens
	return b.runCost, b.maxCost, totalTokens, b.maxTokens, b.runCompactionTokens
}
