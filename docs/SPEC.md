# SPEC: Forge, a Terminal AI Coding Agent

> **Status:** Draft v0.1
> **Working name:** `forge` (placeholder, rename freely)
> **Language:** Go (core, TUI) + Wails/React (desktop)

---

## 1. Overview

Forge is a provider-agnostic AI coding agent that runs in the terminal and as a desktop application. It reads and edits code in a local repository, runs commands, and iterates on tasks under a user-controlled permission model. Every change is reversible through automatic git checkpoints.

**One engine, three clients:** a TUI, a desktop app, and a headless CLI mode all sit on the same agent core.

### 1.1 Goals

- A genuinely useful daily-driver coding agent, not a demo.
- Beautiful, fast terminal UI and a polished desktop UI sharing one design language.
- Provider-agnostic: Anthropic, OpenAI, Gemini, and local models (Ollama / OpenAI-compatible endpoints).
- Safety by default: reviewable diffs, permission rules, sandboxed execution, instant undo.
- Single static binary, trivial install, cross-platform (macOS, Linux, Windows).
- Measurable quality via a built-in evaluation harness.

### 1.2 Non-Goals (v1)

- Being an IDE or a text editor.
- Cloud-hosted execution or a SaaS backend.
- Multi-user collaboration.
- Training or fine-tuning models.
- IDE extensions (planned post-v1, enabled by the daemon architecture).

### 1.3 Differentiators

1. **Provider-agnostic** with per-session cost tracking and budget limits.
2. **Checkpoint and undo:** every edit is a git-backed checkpoint with one-command rollback.
3. **Evals included:** published pass rates per model on a task suite.
4. **Dual frontends** from one core (terminal and desktop).

---

## 2. Users and Use Cases

| Persona                               | Need                                                                |
| ------------------------------------- | ------------------------------------------------------------------- |
| Terminal-native developer             | Fast agent in the shell, scriptable, keyboard-driven                |
| GUI-preferring developer              | Same capability with visual diff review, file tree, session history |
| CI / automation                       | Headless runs with structured JSON output                           |
| Cost-conscious / privacy-focused user | Local models, budget caps, no telemetry by default                  |

**Core use cases:** explain a codebase, implement a feature, fix a failing test, refactor across files, write tests, review a diff, answer questions about a repo.

---

## 3. Functional Requirements

Priority: **P0** = MVP, **P1** = v1.0, **P2** = post-v1.

### 3.1 Agent Loop

| ID  | Requirement                                                                                      | Pri |
| --- | ------------------------------------------------------------------------------------------------ | --- |
| A-1 | Stream model responses token by token                                                            | P0  |
| A-2 | Execute tool calls requested by the model and return results                                     | P0  |
| A-3 | Run independent tool calls concurrently                                                          | P1  |
| A-4 | Support cancellation at any point (Ctrl+C / Stop) via `context.Context`                          | P0  |
| A-5 | Detect and break runaway loops (max iterations, repeated identical calls)                        | P0  |
| A-6 | Enforce per-session token and cost budgets                                                       | P1  |
| A-7 | Compact context when approaching the model's window limit (truncate tool output, then summarize) | P1  |
| A-8 | Retry transient provider errors with exponential backoff                                         | P0  |
| A-9 | Subagents for parallel, isolated research tasks                                                  | P2  |

### 3.2 Tools

| Tool                | Description                                                  | Approval      | Pri |
| ------------------- | ------------------------------------------------------------ | ------------- | --- |
| `read_file`         | Read a file with optional line range                         | No            | P0  |
| `list_dir` / `glob` | List and match paths (respects `.gitignore`)                 | No            | P0  |
| `grep`              | Regex search across the repo                                 | No            | P0  |
| `edit_file`         | Exact-match string replacement; fails if match is not unique | Yes           | P0  |
| `write_file`        | Create or overwrite a file                                   | Yes           | P0  |
| `bash`              | Run a shell command with timeout and output cap              | Yes (by rule) | P0  |
| `fetch_url`         | Fetch a web page as text                                     | Yes           | P1  |
| `todo`              | Model-maintained task list shown in UI                       | No            | P1  |
| `repo_map`          | Symbol-level overview via tree-sitter                        | No            | P1  |
| MCP tools           | Tools exposed by configured MCP servers                      | By rule       | P1  |

**Tool requirements**

- Every tool declares a JSON schema, a description, and an approval policy.
- Tool errors are returned to the model as structured, actionable messages (for example, "old_string matched 3 times; add more context").
- Output is truncated to a configurable limit, with a marker telling the model how to fetch more.
- File paths are validated to stay within the workspace unless explicitly allowed.
- Bash runs in its own process group so the entire tree can be killed on cancel or timeout.

### 3.3 Permissions

| ID  | Requirement                                                                                      | Pri |
| --- | ------------------------------------------------------------------------------------------------ | --- |
| P-1 | Modes: `ask` (default), `accept-edits`, `plan` (read-only), `yolo` (no prompts, explicit opt-in) | P0  |
| P-2 | Approval prompt shows the exact action (diff for edits, command for bash)                        | P0  |
| P-3 | User choices: allow once (`[y]`), allow for session (`[a]`, never offered for dangerous commands), always allow with user policy persistence (`[A]`, preview card; saves a narrow rule and does not approve every later call of that tool), deny (`[n]`/`[d]`), deny with feedback (`[f]`, returned to model up to 500 chars), diff preview (`[e]`). Tab switches permission mode only when no approval card is open. In headless mode, unanswerable approvals are auto-denied. Safety-floor commands (dangerous commands, protected paths) are denied even with `--yolo`. Ordinary edits and commands require `--yes` or `--yolo`. | P1  |
| P-4 | Policy files: user policy (`~/.forge/policy.json`) and project policy (`<workspace>/.forge/policy.json`) with allow/deny/ask rules using patterns, e.g. `bash(go test:*)`, `edit(docs/**)`. Project policy allow rules are strictly ignored with a warning (untrusted repository defense). | P1  |
| P-5 | Deny rules always override allow rules across all modes including `yolo`                              | P1  |
| P-6 | Protected paths (`.git/`, `.env*`, `.ssh/`, `.aws/`, `id_rsa*`, `*.pem`, `*.key`, `*.pfx`, `*.p12`, `credentials`) always prompt for approval in `ask`, `accept-edits`, and `yolo` modes; reads of credential files prompt in all modes (including `plan`); denied in headless mode. | P1  |
| P-7 | Audit log of every tool call, decision, and result (JSONL, per-session SHA-256 hash chains, a state file that detects a truncated tail, `forge audit verify`). Tamper-evident against edits, deletions and truncation, but not against an attacker who can rewrite both the log and its state file. | P1  |

### 3.4 Sessions, Checkpoints, and Memory

| ID  | Requirement                                                                                           | Pri |
| --- | ----------------------------------------------------------------------------------------------------- | --- |
| S-1 | Persist sessions to SQLite (messages, tool calls, usage)                                              | P1  |
| S-2 | `--resume` / `/resume` to continue a prior session                                                    | P1  |
| S-3 | Git checkpoint before each edit batch (shadow ref or stash-like commit, never polluting user history) | P1  |
| S-4 | `/undo` and `/restore <checkpoint>` revert files to a checkpoint                                      | P1  |
| S-5 | Load project instructions from `AGENTS.md` (also reads `CLAUDE.md` if present)                        | P0  |
| S-6 | Layered config: defaults, then user config, then project config, then env vars, then flags            | P0  |
| S-7 | Works in non-git directories using a fallback file-snapshot mechanism                                 | P2  |

### 3.5 Providers

| ID  | Requirement                                                                          | Pri |
| --- | ------------------------------------------------------------------------------------ | --- |
| L-1 | A single `Provider` interface abstracting streaming, tool calls, and usage reporting | P0  |
| L-2 | Anthropic implementation                                                             | P0  |
| L-3 | OpenAI and OpenAI-compatible implementation                                          | P1  |
| L-4 | Gemini implementation                                                                | P1  |
| L-5 | Ollama / local models                                                                | P1  |
| L-6 | Model catalog with context window and pricing metadata, used for cost display        | P1  |
| L-7 | Mid-session model switching (`/model`)                                               | P1  |
| L-8 | Prompt caching where the provider supports it                                        | P2  |

### 3.6 Extensibility

| ID  | Requirement                                                                          | Pri |
| --- | ------------------------------------------------------------------------------------ | --- |
| E-1 | MCP client (stdio and HTTP transports)                                               | P1  |
| E-2 | Custom slash commands from markdown files (`.forge/commands/*.md`)                   | P1  |
| E-3 | Hooks: run user scripts on events (`pre_tool`, `post_tool`, `session_start`, `stop`) | P2  |
| E-4 | Plugin system                                                                        | P2  |

### 3.7 Headless Mode

- `forge run "prompt"` executes non-interactively.
- Flags: `--output json|text|stream-json`, `--permission-mode`, `--max-turns`, `--budget`.
- Exit codes distinguish success, agent failure, budget exceeded, and permission denied.
- Approvals that cannot be answered auto-deny unless policy allows.

---

## 4. Terminal UI (TUI) Requirements

Built with Bubble Tea, Lip Gloss, Bubbles, Glamour, and Chroma.

### 4.1 Layout

```
┌──────────────────────────────────────────────────────┐
│ conversation viewport (scrollable)                   │
│   user message                                       │
│   assistant markdown (streaming)                     │
│   ▸ Read  internal/agent/loop.go           0.02s ✓   │
│   ▾ Edit  internal/agent/loop.go           ⏳ awaiting │
│       - old line                                     │
│       + new line                                     │
├──────────────────────────────────────────────────────┤
│ > input (multiline, @file mentions, /commands)       │
├──────────────────────────────────────────────────────┤
│ model · tokens · $cost · branch · mode               │
└──────────────────────────────────────────────────────┘
```

### 4.2 Features

| ID   | Requirement                                                                                    | Pri |
| ---- | ---------------------------------------------------------------------------------------------- | --- |
| T-1  | Streaming markdown with syntax-highlighted code blocks                                         | P0  |
| T-2  | Collapsible tool-call cards with status, duration, and truncated output                        | P0  |
| T-3  | Inline diff view for edits (unified, optional side-by-side), with approve / reject keys        | P0  |
| T-4  | Multiline input with history navigation                                                        | P0  |
| T-5  | Status bar: model, token use, running cost, git branch, permission mode                        | P1  |
| T-6  | `/` command palette with fuzzy search                                                          | P1  |
| T-7  | `@` file mention autocomplete                                                                  | P1  |
| T-8  | Theme system with adaptive light/dark and built-in palettes (Catppuccin, Tokyo Night, Gruvbox) | P1  |
| T-9  | Graceful terminal resize, mouse wheel scrolling, and bracketed paste                           | P0  |
| T-10 | Spinner with a meaningful activity label                                                       | P0  |
| T-11 | Degrades gracefully with no color or limited terminals (`NO_COLOR`)                            | P1  |

### 4.3 Keybindings (defaults)

| Key                     | Action                                             |
| ----------------------- | -------------------------------------------------- |
| `Enter` / `Shift+Enter` | Send / newline                                     |
| `Esc`                   | Interrupt the running agent                        |
| `Ctrl+C` twice          | Quit                                               |
| `Tab`                   | Cycle permission mode                              |
| `y` / `n` / `a`         | Approve / deny / always allow (in approval prompt) |
| `Ctrl+O`                | Expand or collapse tool output                     |
| `Ctrl+R`                | Search history                                     |

All bindings are configurable.

---

## 5. Desktop Application Requirements

Built with Wails (Go backend) and React (TypeScript). It runs the same agent core in-process.

### 5.1 Features

| ID   | Requirement                                                                | Pri |
| ---- | -------------------------------------------------------------------------- | --- |
| D-1  | Workspace picker ("Open folder") and recent workspaces                     | P0  |
| D-2  | Chat view with streaming markdown, tool cards, and approval flow           | P0  |
| D-3  | Visual diff review panel (Monaco diff editor or react-diff-view)           | P1  |
| D-4  | Session history sidebar with search and resume                             | P1  |
| D-5  | File tree with changed-file indicators                                     | P1  |
| D-6  | Streaming command output via xterm.js                                      | P1  |
| D-7  | Multiple concurrent sessions in tabs                                       | P2  |
| D-8  | Drag-and-drop files into the prompt                                        | P1  |
| D-9  | Settings UI: providers, API keys (OS keychain), models, permissions, theme | P1  |
| D-10 | Checkpoint timeline with one-click restore                                 | P1  |
| D-11 | Auto-update                                                                | P2  |

### 5.2 Desktop-Specific Requirements

- Resolve the user's login-shell environment (`PATH`, etc.) at startup, because GUI launches do not inherit it.
- Coalesce token-delta events (every 16 to 50 ms) before emitting to the webview.
- Store API keys in the OS keychain, never in plaintext config.
- Share theme tokens with the TUI via a single JSON source of truth.
- Code signing and notarization for macOS; signed installers for Windows.

---

## 6. Architecture

### 6.1 Principles

1. **Core is UI-agnostic.** No Bubble Tea, Wails, or terminal types in `internal/agent`.
2. **Events out, decisions in.** The core emits typed events; frontends send back decisions (approvals, cancel).
3. **Everything is cancelable** through context propagation.
4. **Deterministic testing** through a mock provider that replays recorded transcripts.

### 6.2 Repository Layout

```
forge/
├── cmd/forge/               # CLI entrypoint: TUI, headless, subcommands
├── apps/desktop/            # Wails app (main.go, frontend/ React)
├── internal/
│   ├── agent/               # loop, context manager, loop guards
│   ├── llm/                 # Provider interface, implementations, catalog
│   ├── tools/               # tool registry and implementations
│   ├── permission/          # policy engine, modes, approval requests
│   ├── session/             # SQLite store, checkpoints, resume
│   ├── repomap/             # tree-sitter symbol index
│   ├── mcp/                 # MCP client
│   ├── config/              # layered config loading
│   ├── sandbox/             # execution sandboxing
│   ├── tui/                 # Bubble Tea models, views, theme
│   └── eval/                # evaluation harness
├── themes/                  # shared palette JSON
├── docs/
├── SPEC.md
└── go.mod                   # single module
```

A single Go module is required so `internal/` packages are importable by both `cmd/` and `apps/desktop/`.

### 6.3 Core Interfaces

```go
// Frontend-facing contract. TUI, desktop, and headless all use this.
type Session interface {
    Send(ctx context.Context, prompt string) error
    Events() <-chan Event
    Respond(approvalID string, d Decision) error
    Cancel()
    Usage() Usage
    Mode() permission.Mode
    SetMode(mode permission.Mode)
    Checkpointer() Checkpointer
    Close() error
}

// Checkpointer abstracts atomic workspace snapshotting and rollback.
type Checkpointer interface {
    List(ctx context.Context) ([]CheckpointInfo, error)
    Restore(ctx context.Context, id string) error
    Undo(ctx context.Context) error
}

// Provider abstraction.
type Provider interface {
    Name() string
    Models() []ModelInfo
    Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}

// Tool abstraction.
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Run(ctx context.Context, input json.RawMessage) (Result, error)
}
```

### 6.4 Event Schema

All events are plain, JSON-serializable structs (Wails generates TypeScript types from these).

| Event               | Payload                                  |
| ------------------- | ---------------------------------------- |
| `TokenDelta`        | text chunk                               |
| `ThinkingDelta`     | reasoning chunk (if provider exposes it) |
| `ToolStarted`       | id, name, input                          |
| `ToolOutputDelta`   | id, chunk (streaming bash output)        |
| `ToolFinished`      | id, result, duration, error              |
| `ApprovalRequested` | id, tool, input, diff (if edit)          |
| `CheckpointCreated` | id, files                                |
| `UsageUpdated`      | input/output tokens, cost                |
| `Error`             | code, message, retryable                 |
| `Done`              | stop reason                              |

### 6.5 Frontend Connection Modes

1. **In-process (v1):** frontends import `agent` directly.
2. **Daemon (post-v1):** a `forge serve` process exposes the same `Session` API over HTTP + SSE or JSON-RPC. The `Session` interface is written so a remote implementation is a drop-in replacement.

### 6.6 Data Model (SQLite)

| Table         | Key fields                                            |
| ------------- | ----------------------------------------------------- |
| `sessions`    | id, workspace, model, created_at, title               |
| `messages`    | id, session_id, role, content (JSON), created_at      |
| `tool_calls`  | id, message_id, name, input, output, status, duration |
| `checkpoints` | id, session_id, git_ref, files, created_at            |
| `usage`       | session_id, input_tokens, output_tokens, cost         |

---

## 7. Security Requirements

| ID    | Requirement                                                                                                                                                            |
| ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| SEC-1 | Treat all file contents, tool output, and fetched web content as untrusted (prompt-injection aware). Never auto-approve actions that originate from untrusted content. |
| SEC-2 | Default to `ask` mode. `yolo` mode requires an explicit flag and shows a persistent warning in the UI.                                                                 |
| SEC-3 | Block or require approval for destructive and networked commands (`rm -rf`, `curl                                                                                      | sh`, `git push --force`, etc.) via pattern rules. |
| SEC-4 | Optional sandbox for `bash`: `sandbox-exec` (macOS), `bubblewrap` (Linux), container fallback.                                                                         |
| SEC-5 | Secrets are redacted from logs and audit output.                                                                                                                       |
| SEC-6 | API keys live in the OS keychain or environment variables, never in project config.                                                                                    |
| SEC-7 | No telemetry by default. Any telemetry is opt-in, anonymous, and documented.                                                                                           |
| SEC-8 | File operations are confined to the workspace unless the user grants additional directories.                                                                           |

---

## 8. Non-Functional Requirements

| Category           | Target                                                                            |
| ------------------ | --------------------------------------------------------------------------------- |
| Startup time       | TUI interactive in under 150 ms (cold)                                            |
| Memory             | Under 150 MB idle for TUI                                                         |
| UI responsiveness  | No dropped frames during streaming; deltas coalesced                              |
| Binary             | Single static binary, under 40 MB                                                 |
| Platforms          | macOS (arm64, amd64), Linux (amd64, arm64), Windows (amd64)                       |
| Reliability        | Zero data loss on crash: sessions and checkpoints are written before side effects |
| Concurrency safety | `go test -race ./...` passes in CI                                                |
| Accessibility      | Respects `NO_COLOR`; desktop supports keyboard navigation and screen readers      |

---

## 9. Testing and Evaluation

### 9.1 Test Strategy

- **Unit tests** for every tool, the permission engine, config merging, and the edit matcher.
- **Deterministic loop tests** using a mock provider that replays recorded transcripts.
- **TUI tests** with Bubble Tea's `teatest` and golden-file snapshots.
- **Integration tests** running the agent against fixture repos in temp directories.
- **Race detection** (`-race`) and fuzzing for the edit and patch logic.
- **Desktop:** Playwright smoke tests against the frontend with a mocked Go bridge.

### 9.2 Evaluation Harness (`forge eval`)

- Task format: repo fixture, prompt, and a verification command (for example, `go test ./...`).
- Runs tasks in isolated temp workspaces, optionally in parallel.
- Reports pass rate, turns, tokens, cost, and wall time per model.
- Starts with 20 to 30 hand-written tasks; stretch goal is a SWE-bench-style subset.
- Results are published in the README and tracked per release to catch regressions.

---

## 10. Milestones

### Phase 0: Foundations (Week 1)

- [ ] Repo, module, CI (lint, test, race), and basic CLI skeleton
- [ ] `Session`, `Provider`, `Tool`, and `Event` types defined
- [ ] Mock provider and transcript replay
- [ ] **Spike:** a throwaway Wails window streaming one prompt through `Session`

### Phase 1: MVP (Weeks 2-3)

- [ ] Anthropic provider with streaming
- [ ] Tools: `read_file`, `list_dir`, `glob`, `grep`, `edit_file`, `write_file`, `bash`
- [ ] Agent loop with cancellation and loop guards
- [ ] Bubble Tea UI: streaming markdown, tool cards, diff approval, spinner
- [ ] `AGENTS.md` loading and layered config

**Exit criteria:** the agent can fix a failing Go test in a fixture repo end-to-end in the TUI.

### Phase 2: Real Agent (Weeks 4-6)

- [ ] SQLite sessions and `--resume`
- [ ] Permission policy engine, modes, and audit log
- [ ] Git checkpoints, `/undo`, `/restore`
- [ ] Context compaction and budgets
- [ ] Status bar, command palette, `@` mentions, themes
- [ ] Headless mode with JSON output

### Phase 3: Differentiators (Weeks 7-10)

- [ ] OpenAI, Gemini, and Ollama providers; model catalog and cost tracking
- [ ] Tree-sitter repo map
- [ ] MCP client
- [ ] Custom slash commands and hooks
- [ ] **Eval harness** with first published results

### Phase 4: Desktop App (Weeks 11-14)

- [ ] Wails + React shell, workspace picker, chat view
- [ ] Diff review panel, session sidebar, file tree, xterm output
- [ ] Settings UI with keychain storage
- [ ] Shared theme tokens with the TUI

### Phase 5: Release (Weeks 15-16)

- [ ] Sandboxed bash
- [ ] GoReleaser, Homebrew tap, winget/scoop, signed desktop installers
- [ ] Docs site, README with VHS-recorded GIFs, architecture doc
- [ ] v1.0.0 tag

---

## 11. Tech Stack (As-Built)

| Concern           | Choice                                                                                  | Notes / Rationale |
| ----------------- | --------------------------------------------------------------------------------------- | ----------------- |
| TUI Engine        | Charm v1 (`bubbletea v1.3.4`, `lipgloss v1.0.0`, `bubbles v0.20.0`, `glamour v0.8.0`)   | Verified stable, production-tested ANSI rendering across Windows, Linux, macOS |
| CLI / Config      | Cobra + standard library pure Go JSON config loader                                     | 5-tier layered config without heavy external dependencies (koanf/viper dropped) |
| LLM Adapters      | Raw HTTP + SSE streaming (`internal/llm`)                                               | Full control over token deltas, retries, SSE chunking, and cost tracking |
| Process Isolation | Windows Job Objects (`golang.org/x/sys/windows`) and POSIX process groups               | Guarantees clean process tree termination on interrupt or timeout |
| Checkpoints / Undo| Native Git plumbing with isolated shadow Git repository in `os.UserCacheDir()`          | 100% isolation from user repo; ~298ms snapshot on 10k files; atomic rollback |
| Unified Diff      | `github.com/pmezard/go-difflib`                                                         | Clean unified diff output for approval review cards |
| Path Matching     | `bmatcuk/doublestar/v4`, `sabhiram/go-gitignore`                                        | Fast, standard globbing and ignore file evaluation |
| Release Toolchain | GoReleaser v2, GitHub Actions CI                                                        | Multi-platform cross-compilation (6 targets: linux/darwin/windows amd64/arm64) |
| Demo Capture      | Charm VHS                                                                               | Deterministic terminal demo GIF recording via `--demo` |

---

## 12. Risks and Mitigations

| Risk                                     | Impact                     | Mitigation                                                                   |
| ---------------------------------------- | -------------------------- | ---------------------------------------------------------------------------- |
| Unreliable edits                         | Broken code, user distrust | Require unique matches, clear error messages, re-read after edit, fuzz tests |
| Prompt injection via repo or web content | Unwanted command execution | Untrusted-content policy, approval gates, sandbox, protected paths           |
| Provider API drift                       | Breakage                   | Thin adapter layer, contract tests, recorded fixtures                        |
| Context overflow                         | Failed or degraded runs    | Output truncation, compaction, repo map instead of whole-file reads          |
| Scope creep                              | Never ships                | Phase gates with explicit exit criteria; P2 items deferred                   |
| Desktop environment bugs (PATH, signing) | Poor first-run experience  | Login-shell env resolution; plan signing early                               |
| Windows shell differences                | Inconsistent behavior      | Decide shell strategy in Phase 0; CI on Windows                              |
| Runaway cost                             | Surprise bills             | Visible cost counter, hard budget cap, loop guards                           |

---

## 13. Success Metrics

- **Quality:** eval pass rate published and improving release over release.
- **Reliability:** no known data-loss bugs; checkpoint restore works in 100% of tested scenarios.
- **Performance:** meets targets in section 8.
- **Adoption (if open sourced):** GitHub stars, installs per week, issues filed by real users.
- **Resume value:** public repo with tagged releases, install instructions, README GIFs, architecture doc, and an eval report.

---

## 14. Open Questions & As-Built Decisions

1. **Project Name**: Kept as "forge".
2. **License**: Undecided (awaiting user decision; see Known Issues).
3. **Shell Strategy on Windows**: As-built uses native shell detection (`pwsh` > `powershell.exe` > `cmd.exe`) with user-configurable overrides (`--shell` flag, config file).
4. **Checkpoint Mechanism**: As-built uses an isolated shadow Git repository in `os.UserCacheDir()` with strict environment isolation, avoiding user commit log pollution.
5. **Headless Execution**: Canonical form `forge run "prompt"` with `--output text|json|stream-json`, `--permission-mode`, `--max-turns`.

---

## 15. Glossary

- **Agent loop:** the cycle of model call, tool execution, and result feedback until the task completes.
- **Checkpoint:** a git-backed snapshot of workspace files taken before edits.
- **Compaction:** reducing conversation size by truncating or summarizing old turns.
- **MCP:** Model Context Protocol, a standard for exposing external tools and data to LLMs.
- **Repo map:** a compact index of a codebase's symbols used to orient the model.
- **Headless mode:** non-interactive execution for scripts and CI.

---

## 16. Revision Log

| Revision | Date | Author | Description |
| :--- | :--- | :--- | :--- |
| **0.1** | 2026-11-10 | Lead Engineer | Initial design specification and contracts. |
| **0.2** | 2026-11-11 | Lead Engineer | Added prompt injection defenses, exact string replacement (`edit_file`), and tool approval card designs. |
| **0.3** | 2026-11-11 | Lead Engineer | Defined native shell strategy for Windows and POSIX process tree termination. |
| **0.4** | 2026-11-12 | Lead Engineer | Hardened release engineering: GoReleaser 6-target distribution, layered config, themes, and WCAG contrast rules. |
| **0.5** | 2026-11-12 | Lead Engineer | As-built updates: §6.3 real `Session` and `Checkpointer` interfaces, §3.3 P-3 approval choices and headless auto-deny, §3.3 P-6 protected paths, §11 tech stack dependencies, and shadow Git checkpoint architecture. |
