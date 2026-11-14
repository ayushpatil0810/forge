# Token & Cost Budgets (A-6)

Forge provides run-scoped token and monetary cost budgets to prevent runaway agent loops, accidental overspending, and unexpected API bills during automated and interactive workflows.

---

## Command-Line Flags

| Flag | Type | Description |
| :--- | :--- | :--- |
| `--max-cost <USD>` | `float` | Hard monetary spend ceiling for the session (e.g. `--max-cost 2.50`). |
| `--max-tokens <int>` | `integer` | Hard total token ceiling (input + output + compaction) for the session. |

---

## Configuration Reference

Budgets can be configured globally in `~/.config/forge/config.json` or locally in `.forge/config.json`:

```json
{
  "budget": {
    "max_cost": 5.00,
    "max_tokens": 100000
  }
}
```

---

## Unknown Pricing Safety Check (Exit Code 2)

If `--max-cost` is configured or passed via CLI, Forge requires model pricing to be known.
- If pricing is unknown (e.g., custom model or unsupported provider without saved `price_in` / `price_out`), Forge refuses to start and exits immediately with **Exit Code 2** (`usage_error`).
- **Fix**: Save model pricing using the setup screen, `/model`, or configure `models.<provider>/<model>.price_in` and `price_out` in `~/.config/forge/config.json`.

---

## Budget Enforcement Lifecycle & Overshoot Behavior

1. **Pre-Call Check**: Before every model call (main turn or summarization), Forge verifies that the current cumulative spend does not exceed the limit. If already exceeded, the call is blocked immediately.
2. **Turn Completion & Overshoot**: When an in-flight API call finishes and its usage exceeds the budget limit:
   - The response is received and recorded cleanly.
   - Any completed tool call results are processed so history remains valid and checkpoints are preserved.
   - Forge then halts the session with `budget_exceeded` before making any subsequent model calls.
   - *Why overshoot is permitted*: Halting mid-stream would drop in-flight tokens and leave orphaned tool calls that invalidate the session transcript.
3. **80% Warning**: When cumulative usage crosses 80% of the configured budget, Forge emits a warning notice in the TUI or event stream.
4. **Exit Code 5 in Headless Mode**: When running headlessly (`forge run`), hitting a budget causes Forge to terminate with **Exit Code 5** (`budget_exceeded`), outputting JSON metadata indicating token and cost totals.

---

## Interactive TUI Commands

- **/cost**: Displays a comprehensive breakdown of session spend:
  - Input tokens and cost
  - Output tokens and cost
  - Compaction tokens and cost
  - Total cumulative cost and remaining budget
  - Built-in pricing disclaimer note if pricing is derived from the static catalog.
- **/budget [amount]**: Dynamically inspects or raises the active budget without restarting Forge (e.g. `/budget 10.00` or `/budget 200k`). Every budget change is recorded to the tamper-evident audit log.
- **Status Footer**: Real-time budget progress is visible in the status bar:
  - Cost budget: `$2.50/$10.00`
  - Token budget: `45k/100k tok`
