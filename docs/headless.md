# Forge Headless Mode Guide (`forge run`)

Forge provides non-interactive headless execution (`SPEC §3.7`) for scripting, background jobs, automated test fix loops, and CI/CD pipelines.

---

## Command Syntax

The canonical command for headless execution is `forge run`:

```bash
forge run [flags] <prompt>
```

For developer convenience in interactive shells, passing a prompt directly to `forge` acts as a direct alias:

```bash
forge [flags] <prompt>
```

Headless commands do not open the setup screen. They use the provider and model saved by an earlier `forge` session, or `--provider` and `--model`. The API key is the same one the interactive app stored in the OS keychain, unless that provider's environment variable is set. A hosted provider with no saved key and no environment variable exits before any model call.

---

## Flags Reference

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--output` | `string` | `"text"` | Output format: `text`, `json`, or `stream-json`. |
| `--permission-mode` | `string` | `"ask"` | Permission policy: `ask`, `accept-edits`, `plan`, or `yolo`. |
| `--max-turns` | `integer` | `40` | Hard cap on the number of turns / model roundtrips before stopping. |
| `--yes` | `bool` | `false` | Auto-approve non-dangerous file edits and ordinary shell commands. |
| `--yolo` | `bool` | `false` | Auto-approve actions that are not on the safety floor. Dangerous commands and protected paths are still denied. |
| `--provider` | `string` | `"anthropic"` | LLM provider override (`anthropic`, `openai`, `gemini`, `mock`). |
| `--model` | `string` | Saved model, or the built-in fallback | Model ID sent to the provider. This is the ID you type, not a menu. |
| `--base-url` | `string` | `""` | Custom API base URL (e.g. for Ollama `http://localhost:11434/v1`). |
| `--root` | `string` | `"."` | Workspace root directory. |
| `--shell` | `string` | *(auto)* | Host shell executable (`pwsh`, `powershell.exe`, `bash`, `cmd.exe`). |
| `--context-window` | `integer` | *(auto)* | Override context window size in tokens for compaction triggers. |
| `--no-auto-compact` | `bool` | `false` | Disable automatic proactive context compaction triggers. |
| `--max-cost` | `float` | `0` | Hard cap on USD spend for the run (refuses to start with exit code 2 if model price is unknown). |
| `--max-tokens` | `integer` | `0` | Hard cap on total tokens (input + output + compaction) for the run. |

---

## Exit Codes

Forge guarantees strict, predictable exit codes so shell scripts and CI runners can react unambiguously:

| Exit Code | Classification | Trigger Condition |
| :---: | :--- | :--- |
| **`0`** | **Success** | Task completed cleanly; model finished and all approved tool calls succeeded. |
| **`1`** | **Runtime Failure** | LLM API network error, authentication failure, or unrecoverable runtime crash. |
| **`2`** | **Usage / Config Error** | Invalid flags, unknown command-line arguments, missing prompt, invalid configuration, or `--max-cost` used on a model with unknown pricing. |
| **`3`** | **Turn Limit Reached** | Session halted because `max_turns` limit was hit (`code: "max_turns"`). |
| **`4`** | **Permission Denied** | An action required approval which was not granted, or plan mode blocked mutation. |
| **`5`** | **Budget Exceeded** | Session halted because token or monetary cost budget was exhausted (`reason: "budget_exceeded"`). |

---

## Output Formats & Stream Routing

### 1. `text` (Default)
- **`stdout`**: Model text tokens stream incrementally as generated.
- **`stderr`**: Diagnostic lines, tool invocation headers (`● bash ...`), tool durations, and warnings.

Example:
```bash
forge run --output text "List files in src/ and summarize the architecture"
```

### 2. `json`
- **`stdout`**: Exactly one JSON document emitted upon task completion. Ideal for programmatic ingestion.
- **`stderr`**: Diagnostics, progress markers, and warnings.

Schema:
```json
{
  "status": "success",
  "prompt": "Fix failing tests in discount.go",
  "content": "All tests are passing! I have fixed the boundary condition.",
  "turns": 4,
  "tools": [
    {
      "name": "bash",
      "input": { "command": "go test ./..." },
      "status": "error",
      "duration_ms": 1250,
      "error": "FAIL: TestDiscount"
    },
    {
      "name": "read_file",
      "input": { "path": "discount.go" },
      "status": "ok",
      "duration_ms": 1
    },
    {
      "name": "edit_file",
      "input": {
        "path": "discount.go",
        "old_string": "if amount > 100 {",
        "new_string": "if amount >= 100 {"
      },
      "status": "ok",
      "duration_ms": 3
    }
  ],
  "usage": {
    "inputTokens": 2450,
    "outputTokens": 320,
    "cost": 0.0093
  }
}
```

*Note on `status` values:*
- `"success"`: Task completed normally (exit code 0).
- `"error"`: Runtime error or unexpected failure (exit code 1).
- `"max_turns"`: Halted due to turn cap limit (exit code 3).
- `"budget_exceeded"`: Halted due to token or monetary cost budget cap (exit code 5).

### 3. `stream-json`
- **`stdout`**: Line-delimited JSON (NDJSON) streaming every typed event (`TokenDelta`, `ToolStarted`, `ToolFinished`, `UsageUpdated`, `Done`) in real time.
- **`stderr`**: System diagnostics and configuration warnings.

---

## Headless Approval Policy

In non-interactive headless mode, stdin is not available to prompt a human. Forge handles approvals deterministically:

1. **Without `--yes` or `--yolo`**:
   Any action requiring human approval is automatically denied with a clear reason.
2. **With `--yes`**:
   File edits and non-dangerous commands are automatically allowed.
   - Protected paths (`.git/`, `.env*`, `.ssh/`, `.aws/`, private keys) cannot be auto-approved in headless mode (exit code 4).
   - Destructive commands (`rm -rf /`, `dd`, `git push --force`) and other safety-floor actions cannot be auto-approved in headless mode, with or without `--yolo`.
3. **With `--yolo`**:
   Actions that are not on the safety floor are auto-approved. Dangerous commands and protected paths are denied. Decisions are written to the audit log.

