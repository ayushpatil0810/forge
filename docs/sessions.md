# Session Persistence and Resumption

Forge provides resilient, automatic session persistence and resumption powered by an embedded SQLite database (`modernc.org/sqlite`, pure Go, CGO-free). Every interactive and headless session can be preserved, inspected, and resumed seamlessly across process restarts and provider changes.

---

## 1. Storage Architecture

- **Location**: `~/.forge/sessions.db` (customizable via `sessions.database_path` or environment).
- **Permissions**: Created with file mode `0600` (readable and writable only by the owner).
- **Engine**: Pure Go SQLite without CGO dependencies.
- **Concurrency & WAL**: Opened with `PRAGMA journal_mode = WAL`, `PRAGMA busy_timeout = 5000`, and `PRAGMA foreign_keys = ON` to support safe concurrent multi-process access and fast atomic transactions.
- **Lazy Initialization**: The database file and schema are opened lazily on first access.
- **Integrity Verification**: `PRAGMA quick_check` runs on startup to detect file corruption early and suggest recovery.

---

## 2. Schema and Migrations

Forge utilizes forward migrations versioned via the `schema_version` table. If the database schema is newer than the running binary supports, Forge refuses to touch it and suggests updating Forge.

```
┌─────────────────────────────────────────────────────────────┐
│                          sessions                           │
│  id, workspace, provider, model, title, created_at, updated │
└──────────────┬──────────────────────────────┬───────────────┘
               │                              │
               ▼                              ▼
┌──────────────────────────────┐ ┌────────────────────────────┐
│           messages           │ │           usage            │
│  id, session_id, role,       │ │  id, session_id, model,    │
│  content, tool_call_id, ...  │ │  input_tokens, cost, ...   │
└──────────────┬───────────────┘ └────────────────────────────┘
               │                              ▲
               ▼                              │
┌──────────────────────────────┐ ┌────────────┴───────────────┐
│          tool_calls          │ │        checkpoints         │
│  id, message_id, name,       │ │  id, session_id, turn,     │
│  input, output, status, ...  │ │  git_ref, files, ...       │
└──────────────────────────────┘ └────────────────────────────┘
```

### Table Definitions

1. **`sessions`**: Root session record containing canonical workspace directory, provider, model, title (auto-derived from first prompt, capped at 60 chars), creation timestamp, and last updated timestamp.
2. **`messages`**: Provider-neutral conversation turns (`user`, `assistant`, `tool`).
3. **`tool_calls`**: Structured tool invocations (`name`, `input` JSON, `output` capped at 64KB with truncation marker, execution `duration_ms`, `status`).
4. **`checkpoints`**: Snapshot metadata tying shadow git commit refs to session turns.
5. **`usage`**: Granular token usage (input/output tokens) and cost accounting per turn and model.

---

## 3. Crash Safety & Zero Data Loss

Per SPEC §8, Forge ensures zero data loss during unexpected crashes or abrupt process terminations:

1. **Pre-flight Write**: The user's prompt is persisted to SQLite *before* calling the LLM provider.
2. **Incremental Ingestion**: Tool calls, tool results, streamed tokens, and usage metrics are saved incrementally as events arrive.
3. **Dangling Call Synthesis**: If a process crashes or is killed mid-turn while a tool was executing, `LoadResumedSession` automatically synthesizes an `interrupted` tool response. This prevents LLM providers from rejecting the conversation history due to unmatched tool calls.

---

## 4. Secret Redaction

Forge's credential redactor runs on all text before it is written to the database. API keys (`sk-...`, `anthropic-...`, `AIza...`) and sensitive strings are replaced with `[REDACTED]` in message contents, tool inputs, and tool outputs.

---

## 5. Resuming Sessions

### CLI Resumption

- **Resume specific session**:
  ```bash
  forge --resume s_1730000000_abcdef
  ```
- **Resume most recent session in workspace**:
  ```bash
  forge --resume
  # or
  forge --resume last
  ```
- **Headless resumption**:
  ```bash
  forge run --resume s_1730000000_abcdef "Run the test suite again"
  ```
- **Ephemeral session (no persistence)**:
  ```bash
  forge --no-session
  ```

### TUI Resumption

Inside an active interactive session:
- `/resume`: Opens the interactive fuzzy session picker.
- `/resume <session_id>`: Immediately switches to and restores the specified session.
- `/sessions`: Alias for `/resume`.

### Restoration Semantics

1. **Viewport Reconstruction**: Previous user prompts, assistant answers, and collapsed tool cards are restored with full markdown rendering.
2. **Cumulative Metrics**: Input tokens, output tokens, and dollar costs are restored accurately.
3. **Workspace Binding**: By default, Forge refuses to resume a session belonging to a different canonical directory root. To override this, pass `--root <path>`.
4. **Permission Mode Reset**: Forge *never* restores `yolo` mode from a saved session. Resumed sessions always start in `ask` mode unless `--yolo` is explicitly passed on the current command line.
5. **Read-Before-Write Reset**: The tool tracker's read set is emptied upon resumption, and a system notice is injected into the model context:
   ```
   [Resumed session <id>. Files may have changed on disk since last session; always re-read files before editing.]
   ```
6. **Undo Continuity**: Checkpoint history in `.forge/checkpoints` remains intact. Running `/undo` or `/restore` in a resumed session continues to revert changes made prior to resumption.

---

## 6. CLI Management Commands

```bash
# List sessions for the current workspace
forge sessions list

# List all sessions across all workspaces
forge sessions list --all

# Show full metadata and usage stats for a session
forge sessions show <session_id>

# Delete a session and cascade delete its messages, tools, and usage
forge sessions delete <session_id>

# Prune sessions older than N days (default: 30)
forge sessions prune --older-than 14
```
