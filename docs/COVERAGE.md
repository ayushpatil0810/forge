# Specification Coverage Matrix

This document tracks implementation status, delivery slices, and automated test evidence for every requirement identified in [SPEC.md](SPEC.md).

---

## 1. Agent Loop & Core Architecture (`A-*`)

| ID | Requirement | Pri | Status | Slice | Code & Test Evidence |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `A-1` | Stream model responses token by token | P0 | `DONE` | Slice 1 & 2 | `internal/agent/local.go` (`s.emit(EventTokenDelta)`), `internal/llm/anthropic/anthropic.go`, `internal/llm/openai/openai.go`; tested in `internal/agent/agent_test.go:TestLocalSessionSendStream` |
| `A-2` | Execute tool calls requested by model & return results | P0 | `DONE` | Slice 1 & 3 | `internal/agent/local.go` (`runToolCalls`), `internal/tools/`; tested in `internal/agent/agent_test.go:TestSessionToolExecutionAndFeedback` |
| `A-3` | Run independent tool calls concurrently | P1 | `PLANNED` | Slice 11 | Planned for Slice 11; currently sequential to guarantee strict state ordering |
| `A-4` | Cancellation at any point via `context.Context` | P0 | `DONE` | Slice 1 | `internal/agent/local.go` (`s.Cancel()`, context propagation); tested in `internal/agent/agent_test.go:TestCancellationHonored` |
| `A-5` | Break runaway loops (max iterations, duplicate calls) | P0 | `DONE` | Slice 1 & 5 | `internal/agent/local.go` (`loopGuardThreshold = 10`), stall detection; tested in `internal/agent/agent_test.go:TestLoopGuardThreshold` |
| `A-6` | Enforce per-session token and cost budgets | P1 | `PLANNED` | Slice 9b | Planned for Slice 9b context compaction and token budgets |
| `A-7` | Compact context approaching model window limit | P1 | `PLANNED` | Slice 9b | Planned for Slice 9b context compaction and token budgets; tool output caps active in `internal/tools/` |
| `A-8` | Retry transient errors with exponential backoff | P0 | `DONE` | Slice 1 & 6 | `internal/agent/local.go` (`retryTransientWithBackoff`), `internal/agent/errors.go`; tested in `internal/agent/agent_test.go:TestRetryTransientErrorWithBackoff` |
| `A-9` | Subagents for parallel, isolated research tasks | P2 | `PARKED` | Phase 2 | Parked for Phase 2 |

---

## 2. Permissions & Safety Policy (`P-*`)

| ID | Requirement | Pri | Status | Slice | Code & Test Evidence |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `P-1` | Modes: `ask`, `accept-edits`, `plan`, `yolo` | P0 | `DONE` | Slice 1 | `internal/permission/permission.go` (`ModeAsk`, `ModeAcceptEdits`, `ModePlan`, `ModeYOLO`); tested in `internal/permission/permission_test.go` |
| `P-2` | Approval prompt shows exact action (diff / command) | P0 | `DONE` | Slice 4 & 5 | `internal/tui/approval_card.go` (`RenderApprovalCard`, unified diffs); tested in `internal/tui/app_test.go:TestTUIApprovalFlow` |
| `P-3` | User choices: allow once, allow session, deny, diff, auto-deny in headless | P1 | `DONE` | Slice 7 / Drift | `internal/tui/app.go` (`'a'` key, `'e'` key), `cmd/forge/main.go` (`decideHeadlessApproval`); tested in `internal/tui/app_test.go:TestTUIApprovalSessionAllow`, `cmd/forge/main_test.go:TestForgeRunHeadlessJSON` |
| `P-4` | Policy file with allow/deny/ask rules using patterns | P1 | `PLANNED` | Slice 9a | Planned for Slice 9a permission policy files |
| `P-5` | Deny rules always override allow rules | P1 | `DONE` | Slice 1 | `internal/permission/permission.go` (`Check` plan mode / protected paths override); tested in `internal/permission/permission_test.go` |
| `P-6` | Protected paths (`.git/`, `.env*`, `.ssh/`, credentials) always ask | P1 | `DONE` | Slice 7 / Drift | `internal/tools/tracker.go` (`IsProtectedPath`), `internal/permission/permission.go`; tested in `internal/permission/permission_test.go:TestProtectedPathsByModeTable` |
| `P-7` | Audit log of every tool call, decision, and result | P1 | `PLANNED` | Slice 9a | Planned for Slice 9a JSONL audit logging |

---

## 3. Sessions, Checkpoints, and Configuration (`S-*`)

| ID | Requirement | Pri | Status | Slice | Code & Test Evidence |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `S-1` | Persist sessions to SQLite | P1 | `DONE` | Slice 9 | `internal/session/` (`modernc.org/sqlite`, pure Go, 0600, WAL mode, migrations, secret redaction, crash safety), `cmd/forge/main.go`; tested in `internal/session/store_test.go`, `cmd/forge/session_cmd_test.go`, `cmd/forge/session_exit_test.go` |
| `S-2` | `--resume` / `/resume` to continue prior session | P1 | `DONE` | Slice 9 | `cmd/forge/main.go` (`--resume`), `internal/tui/app.go` (`/resume`, `/sessions`, fuzzy session picker), `internal/session/session.go` (`LoadResumedSession`, `RepairInterruptedToolCalls`); tested in `cmd/forge/session_exit_test.go:TestSlice9_SessionPersistenceAndResumeExitCheck`, `internal/tui/picker_test.go` |
| `S-3` | Git checkpoint before edit batch (shadow repo) | P1 | `DOING` | Slice 7 | `internal/checkpoint/checkpoint.go`, `internal/agent/local.go`; tested in `internal/checkpoint/checkpoint_test.go:TestRoundTripRestore`, `TestUserRepoUntouched` |
| `S-4` | `/undo` and `/restore <checkpoint>` revert files | P1 | `DOING` | Slice 7 | `internal/checkpoint/checkpoint.go` (`Undo`, `Restore`), `cmd/forge/main.go` (`forge undo`), `internal/tui/app.go`; tested in `internal/checkpoint/checkpoint_test.go` |
| `S-5` | Load project instructions from `AGENTS.md` / `CLAUDE.md` | P0 | `DONE` | Slice 3 | `internal/agent/prompt.go` (`LoadProjectInstructions`); tested in `internal/agent/agent_test.go` |
| `S-6` | Layered config (defaults, user, project, env, flags) | P0 | `DONE` | Slice 6 | `internal/config/config.go` (`Load`), precedence order; tested in `internal/config/config_test.go:Test5TierPrecedence` |
| `S-7` | Works in non-git directories via shadow checkpointing | P2 | `DOING` | Slice 7 | `internal/checkpoint/checkpoint.go`; tested in `internal/checkpoint/checkpoint_test.go:TestNonGitWorkspace` |

---

## 4. Providers & Catalog (`L-*`)

| ID | Requirement | Pri | Status | Slice | Code & Test Evidence |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `L-1` | Single `Provider` interface (streaming, tools, usage) | P0 | `DONE` | Slice 1 | `internal/llm/provider.go` (`Provider`, `StreamEvent`, `Usage`); tested in `internal/llm/mock/` |
| `L-2` | Anthropic implementation | P0 | `DONE` | Slice 2 & 3b | `internal/llm/anthropic/anthropic.go` (SSE streaming, tool use); contract tests in `anthropic_test.go` (experimental, not live-verified) |
| `L-3` | OpenAI and OpenAI-compatible implementation | P1 | `DONE` | Slice 3b | `internal/llm/openai/openai.go` (SSE streaming, tool calling); verified live and contract-tested in `openai_test.go` |
| `L-4` | Gemini implementation | P1 | `DONE` | Slice 3b | `internal/llm/gemini/gemini.go` (SSE streaming, function calling); contract-tested in `gemini_test.go` (experimental, not live-verified) |
| `L-5` | Ollama / local models via loopback & custom base URL | P1 | `DONE` | Slice 8 | `internal/credentials/`, `internal/llm/openai/local_contract_test.go`, `docs/local-models.md`, `docs/providers.md`; tested in `TestLocalOpenAIContract_Ollama` |
| `L-6` | Model catalog with context window & pricing metadata | P1 | `DONE` | Slice 2 & 3b | `internal/llm/anthropic/models.go`, `internal/llm/openai/models.go`, `internal/llm/gemini/models.go`; tested in unit tests |
| `L-7` | Mid-session model switching (`/model`, `/provider`) | P1 | `DONE` | Slice 8 | `internal/tui/app.go` (`/model`, `/provider`, `/auth`), context window validation; tested in `internal/tui/app_test.go` |
| `L-8` | Prompt caching support | P2 | `PARKED` | Phase 2 | Parked for Phase 2 |

---

## 5. Terminal User Interface (`T-*`)

| ID | Requirement | Pri | Status | Slice | Code & Test Evidence |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `T-1` | Streaming markdown with syntax-highlighted code blocks | P0 | `DONE` | Slice 2 | `internal/tui/markdown.go` (Glamour renderer); tested in `internal/tui/app_test.go:TestMarkdownRendererIncompleteFence` |
| `T-2` | Collapsible tool-call cards with duration and output | P0 | `DONE` | Slice 3 | `internal/tui/tool_card.go` (`Ctrl+O` toggle); tested in `internal/tui/tool_card_test.go` |
| `T-3` | Inline diff view for edits with approve/reject keys | P0 | `DONE` | Slice 4 | `internal/tui/approval_card.go` (`'e'` diff toggle, `'y'`/`'n'`/`'a'`); tested in `internal/tui/app_test.go` |
| `T-4` | Multiline input with history navigation | P0 | `DONE` | Slice 2 | `internal/tui/app.go` (`bubbles/textarea`, Alt+Enter newline); tested in `internal/tui/app_test.go` |
| `T-5` | Status bar: model, token use, cost, branch, mode | P1 | `DONE` | Slice 2 & 6 | `internal/tui/app.go` (`View()`); tested in `internal/tui/app_test.go:TestTUIWelcomeBannerAndFooter` |
| `T-6` | `/` command palette with fuzzy search | P1 | `PLANNED` | Slice 12 | Slash commands implemented (`/undo`, `/checkpoints`, `/restore`, `/model`, `/provider`, `/auth`); palette search planned for Slice 12 |
| `T-7` | `@` file mention autocomplete | P1 | `PLANNED` | Slice 12 | Planned for Slice 12 |
| `T-8` | Theme system with adaptive light/dark & palettes | P1 | `DONE` | Slice 6 | `themes/` (`dark`, `light`, `catppuccin-mocha`, `tokyo-night`), WCAG $\ge 4.5:1$ contrast; tested in `internal/tui/theme_test.go` |
| `T-9` | Graceful resize, scrolling, and bracketed paste | P0 | `DONE` | Slice 2 & 6 | `internal/tui/app.go` (`tea.WindowSizeMsg`); tested in `internal/tui/app_test.go:TestTUIResizeWithoutPanic`, `TestTUINarrowAndShortLayout` |
| `T-10` | Spinner with meaningful activity label | P0 | `DONE` | Slice 2 & 8 | `internal/tui/app.go` (`bubbles/spinner`, `activityLabel`, `"Loading model..."` for local runtimes); tested in `internal/tui/app_test.go` |
| `T-11` | Graceful degradation for `NO_COLOR` / `TERM=dumb` | P1 | `DONE` | Slice 6 & 7 | `internal/tui/app.go` (`isDumbTerminal()`, ASCII glyphs); tested in `internal/tui/golden_test.go:TestASCIIFallbackForDumbTerminal` |

---

## 6. Security, Isolation, & Sandbox (`SEC-*`)

| ID | Requirement | Pri | Status | Slice | Code & Test Evidence |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `SEC-1` | Treat untrusted content safely (prompt injection) | P0 | `DONE` | Slice 3 | `internal/agent/prompt.go` (untrusted content wrappers); tested in `internal/agent/agent_test.go` |
| `SEC-2` | Default `ask` mode; `yolo` requires explicit flag | P0 | `DONE` | Slice 1 & 6 | `internal/permission/permission.go`, `cmd/forge/main.go`; tested in `cmd/forge/main_test.go` |
| `SEC-3` | Intercept destructive commands (`rm -rf`, `curl\|sh`, etc.) | P0 | `DONE` | Slice 5 | `internal/tools/bash.go` (`isDangerousCommand`); tested in `internal/tools/bash_test.go:TestDangerousCommandTable` |
| `SEC-4` | Optional sandbox for bash (Job Object / process group) | P1 | `DONE` | Slice 5 | `internal/tools/process_windows.go` (Job Objects), `process_posix.go`; tested in `internal/tools/bash_test.go` |
| `SEC-5` | Secrets redacted from logs and audit output | P1 | `DONE` | Slice 6 | `internal/agent/prompt.go` (key redaction), `internal/credentials/credentials.go` (`MaskKey`) |
| `SEC-6` | API keys live in env vars or keychain, never project config | P0 | `DONE` | Slice 6 & 8 | `internal/credentials/credentials.go` (`zalando/go-keyring`), `internal/config/config.go` (strict allowlist, rejects project credentials); tested in `internal/config/config_test.go:TestHostileProjectConfig`, `cmd/forge/auth_exit_test.go:TestSlice8_AuthAndHeadlessExitCheck` |
| `SEC-7` | No telemetry by default | P0 | `DONE` | Slice 0 | Zero telemetry libraries in `go.mod` |
| `SEC-8` | File operations confined to workspace | P0 | `DONE` | Slice 3 & 4 | `internal/tools/tracker.go` (`IsWithinWorkspace`); tested in `internal/tools/tracker_test.go` |

---

## 7. Desktop Application (`D-*`) & Extensibility (`E-*`)

| ID | Requirement | Pri | Status | Slice | Notes |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `D-1`–`D-11` | Wails desktop application & UI | P0–P2 | `PARKED` | Phase 2 | Planned for Phase 2 after CLI and SQLite foundation are complete |
| `E-1` | MCP client (stdio & HTTP) | P1 | `PARKED` | Phase 2 | Planned for Phase 2 |
| `E-2` | Custom slash commands (`.forge/commands/*.md`) | P1 | `PARKED` | Phase 2 | Planned for Phase 2 |
| `E-3` | Hooks (`pre_tool`, `post_tool`, etc.) | P2 | `PARKED` | Phase 2 | Planned for Phase 2 |
| `E-4` | Plugin system | P2 | `PARKED` | Phase 2 | Planned for Phase 2 |

