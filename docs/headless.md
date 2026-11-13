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

---

## Flags Reference

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--output` | `string` | `"text"` | Output format: `text`, `json`, or `stream-json`. |
| `--permission-mode` | `string` | `"ask"` | Permission policy: `ask`, `accept-edits`, `plan`, or `yolo`. |
| `--max-turns` | `integer` | `40` | Hard cap on the number of turns / model roundtrips before stopping. |
| `--yes` | `bool` | `false` | Auto-approve non-dangerous file edits and ordinary shell commands. |
| `--yolo` | `bool` | `false` | Auto-approve all operations, including dangerous commands. |
| `--provider` | `string` | `"anthropic"` | LLM provider override (`anthropic`, `openai`, `gemini`, `mock`). |
| `--model` | `string` | *(flagship)* | Model ID override. |
| `--base-url` | `string` | `""` | Custom API base URL (e.g. for Ollama `http://localhost:11434/v1`). |
| `--root` | `string` | `"."` | Workspace root directory. |
| `--shell` | `string` | *(auto)* | Host shell executable (`pwsh`, `powershell.exe`, `bash`, `cmd.exe`). |

---

## Exit Codes

Forge guarantees strict, predictable exit codes so shell scripts and CI runners can react unambiguously:

| Exit Code | Classification | Trigger Condition |
| :---: | :--- | :--- |
| **`0`** | **Success** | Task completed cleanly; model finished and all approved tool calls succeeded. |
| **`1`** | **Runtime Failure** | LLM API network error, authentication failure, or unrecoverable runtime crash. |
| **`2`** | **Usage / Config Error** | Invalid flags, unknown command-line arguments, missing prompt, or invalid configuration. |
| **`3`** | **Turn Limit Reached** | Session halted because `max_turns` limit was hit (`code: "max_turns"`). |
| **`4`** | **Permission Denied** | An action required approval which was not granted, or plan mode blocked mutation. |

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
   - Destructive commands (`rm -rf /`, `dd`, `git push --force`) cannot be approved with `--yes` (requires `--yolo`).
3. **With `--yolo`**:
   All operations are auto-approved, with an audit log of executed actions.

