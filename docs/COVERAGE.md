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
| `P-3` | User choices: all 5 variants (`y`, `a`, `A`, `n`/`d`, `f`), headless auto-deny | P1 | `DONE` | Slice 9a | `internal/agent/session.go` (5 `Decision` variants), `internal/agent/local.go`, `internal/tui/app.go` (`'a'`, `'A'`, `'f'`), `internal/tui/approval_card.go`; tested in `internal/tui/approval_teatest_test.go:TestTeatest_AlwaysAllowPreviewAndConfirm`, `TestTeatest_DenyWithFeedback`, `cmd/forge/policy_audit_exit_test.go` |
| `P-4` | Policy file with allow/deny/ask rules. A bash wildcard allow matches only a non-opaque command whose arguments are plain paths or flags from a built-in table (`go test`/`build`/`vet`, read-only `git status`/`diff`/`log`/`show`/`branch`). Other commands match only an exact saved string. This is not a sandbox | P1 | `DONE` | Slice 9a | `internal/policy/bash.go`, `docs/policy.md`; tested in `internal/policy/floor_bypass_test.go:TestSafeFlagAllowAndFloorBypasses`, `internal/policy/bash_test.go:FuzzBashSplitter` |
| `P-5` | Deny rules always override allow rules across all modes including yolo | P1 | `DONE` | Slice 9a | `internal/policy/eval.go`, `internal/policy/policy_test.go:TestPolicyEvaluationTable`, `cmd/forge/policy_audit_exit_test.go` |
| `P-6` | Protected paths always ask. One classifier (`tools.IsProtectedPath`; `policy.IsProtectedPath` delegates). `.git` is case-insensitive everywhere; other names are case-insensitive on Windows and macOS and case-sensitive on Linux. Names: `.git`, `.env` and `.env.*`, `.ssh`, `.aws`, `.gnupg`, `*.pem`, `*.key`, `*.pfx`, `*.p12`, `id_rsa*`, `id_ed25519*`, `credentials`, `.npmrc`, `.pypirc`, `.netrc`. Windows strips trailing dots and spaces before classification and expands an 8.3 short name when the volume has one. A hardlink or symlink is protected when its identity matches a protected name in the workspace root or the target's parent. `read_file`, `write_file`, and `edit_file` also treat a regular file with link count above 1 as protected. `read_file` asks in every mode including plan; headless denies on the floor. `grep`, `glob`, and `list_dir` skip matches and report the count | P1 | `DONE` | Slice 9a | `internal/tools/path.go`; tested in `internal/tools/protected_test.go:TestProtectedPathClassifierTable`, `internal/tools/alias_test.go:TestReadFile_AliasToCredentialIsProtected`, `internal/tools/hardlink_test.go:TestHardlinkOutsideWorkspaceIsProtected`, `internal/tools/win32_short_windows_test.go:TestShortNameResolvesToProtectedLongName` (skips when 8.3 names are disabled) |
| `P-7` | Audit log of every tool call, decision, and result. Tamper-evident against edits, deletions and truncation, but not against an attacker who can rewrite both the log and its state file. `write_file` and `edit_file` store a path, content hashes, and a line delta, not the body. Bash stores the redacted command capped at 2 KB. With `audit.required`, a failed write denies the tool call; otherwise a warning is shown once | P1 | `DONE` | Slice 9a | `internal/audit/`, `cmd/forge/audit_cmd.go`; tested in `internal/audit/logger_test.go`, `internal/audit/honesty_test.go:TestAuditTailTruncationFails`, `TestAuditRotationVerifies`, `TestAuditConcurrentProcesses`, `TestSummarizeOmitsBodiesAndCapsCommands` |

---

## 3. Sessions, Checkpoints, and Configuration (`S-*`)

| ID | Requirement | Pri | Status | Slice | Code & Test Evidence |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `S-1` | Persist sessions to SQLite | P1 | `DONE` | Slice 9 | `internal/session/` (`modernc.org/sqlite`, pure Go, 0600, WAL mode, migrations, secret redaction, crash safety), `cmd/forge/main.go`; tested in `internal/session/store_test.go`, `cmd/forge/session_cmd_test.go`, `cmd/forge/session_exit_test.go` |
| `S-2` | `--resume` / `/resume` to continue prior session | P1 | `DONE` | Slice 9 | `cmd/forge/main.go` (`--resume`), `internal/tui/app.go` (`/resume`, `/sessions`, fuzzy session picker), `internal/session/session.go` (`LoadResumedSession`, `RepairInterruptedToolCalls`); tested in `cmd/forge/session_exit_test.go:TestSlice9_SessionPersistenceAndResumeExitCheck`, `internal/tui/picker_test.go` |
| `S-3` | Git checkpoint before edit batch (shadow repo) | P1 | `DOING` | Slice 7 | `internal/checkpoint/checkpoint.go`, `internal/agent/local.go`; tested in `internal/checkpoint/checkpoint_test.go:TestRoundTripRestore`, `TestUserRepoUntouched` |
| `S-4` | `/undo` and `/restore <checkpoint>` revert files | P1 | `DOING` | Slice 7 | `internal/checkpoint/checkpoint.go` (`Undo`, `Restore`), `cmd/forge/main.go` (`forge undo`), `internal/tui/app.go`; tested in `internal/checkpoint/checkpoint_test.go` |
| `S-5` | Load project instructions from `AGENTS.md` / `CLAUDE.md` | P0 | `DONE` | Slice 3 | `internal/agent/prompt.go` appends the workspace-root file inside the built prompt as a project-guidance block that cannot change permissions. Tested in `cmd/forge/system_prompt_test.go:TestSystemPrompt_BothEntryPointsKeepSecurityBase` (fixture body in the labeled block) and `internal/agent/prompt_test.go` |
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
| `L-7` | Mid-session model switching (`/model <id>`, `/models`, `/settings`) | P1 | `DONE` | Slice 8 | Setup asks for a typed model ID and stores the key in the OS keychain. `/model` does not print a model menu. Known IDs still get a context-window check. Tested in `internal/tui/app_test.go` |
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
| `SEC-1` | Treat untrusted content safely (prompt injection) | P0 | `DONE` | Slice 3 | The built prompt (untrusted-data rule, environment, tool guidance) is always the base for interactive and `forge run`. A user `system_prompt` is appended after it and does not replace it. Tested in `cmd/forge/system_prompt_test.go:TestSystemPrompt_BothEntryPointsKeepSecurityBase` and `internal/agent/prompt_test.go`. This is instruction text, not a sandbox |
| `SEC-2` | Default `ask` mode; `yolo` requires explicit flag | P0 | `DONE` | Slice 1 & 6 | `internal/permission/permission.go`, `cmd/forge/main.go`; tested in `cmd/forge/main_test.go` |
| `SEC-3` | Safety floor for a fixed list (dangerous-command table, opaque shell, `find -exec`/`-delete`, `xargs`, `env` running a command, shells with `-c`/`-enc`, `eval`, `iex`, `git -c`/`--config-env`/`--upload-pack`/`alias.*`, `go -exec`/`-toolexec`/`-overlay`), including flags hidden in quotes. Not a complete denylist | P0 | `DONE` | Slice 5 | `internal/policy/bash.go`, `internal/permission/dangerous.go`; tested in `internal/policy/floor_bypass_test.go:TestSafeFlagAllowAndFloorBypasses`, `internal/permission/dangerous_test.go` |
| `SEC-4` | partial: process-group kill only, no sandbox. Windows creates the shell suspended and resumes it only after the job assignment; the job handle is closed on every exit. POSIX uses a process group and SIGKILL | P1 | `PARTIAL` | Slice 5 | `internal/tools/proc_windows.go`, `internal/tools/proc_posix.go`, `docs/architecture.md`; tested in `internal/tools/bash_test.go:TestBashTool_ProcessTreeGrandchildKill`, `internal/tools/handle_windows_test.go:TestJobHandleDoesNotGrow` |
| `SEC-5` | Secrets redacted from logs, model inputs, database, and audit output. Bash drops credential-like environment variables unless the user config lists them in `bash.env_allow` (project config cannot). Audit records do not store file bodies | P1 | `DONE` | Slice 9a | `internal/redact/`, `internal/tools/bash.go`, `internal/audit/summary.go`; tested in `internal/redact/redact_test.go`, `internal/tools/bash_test.go:TestBashTool_SanitizedEnvironment`, `internal/audit/honesty_test.go:TestSummarizeOmitsBodiesAndCapsCommands` |
| `SEC-6` | API keys live in env vars or keychain, never project config | P0 | `DONE` | Slice 6 & 8 | Project `.forge/config.json` cannot set credentials, `permission_mode`, `system_prompt`, `audit`, `sessions`, or `redaction`. The ignore warning is printed and shown once in the TUI. `OPENAI_BASE_URL` applies only to OpenAI-compatible providers; a non-loopback `http://` base URL is refused unless `--allow-insecure-base-url`. `FORGE_AUDIT_REQUIRED`, `FORGE_AUDIT_ENABLED`, and `FORGE_SESSIONS_ENABLED` are read from the environment. Audit failure behavior was not changed. Tested in `internal/config/config_test.go:TestHostileProjectConfig`, `internal/config/sections_test.go:TestAuditSessionsRedactionPrecedence`, `internal/config/baseurl_test.go`, `internal/tui/app_test.go:TestTUISecurityNoticeOnFirstScreen` |
| `SEC-7` | No telemetry by default | P0 | `DONE` | Slice 0 | Zero telemetry libraries in `go.mod` |
| `SEC-8` | File operations confined to workspace | P0 | `DONE` | Slice 3 & 4 | `read_file`, `write_file`, `edit_file`, `grep`, `glob`, and `list_dir` use `ResolveInRoot`, which resolves the nearest existing ancestor (Windows junctions and POSIX symlinks) before joining a missing suffix and rejects an ancestor outside the workspace. Tested in `internal/tools/confine_test.go:TestWriteFile_JunctionOrSymlinkDoesNotEscape` and `TestResolveInRoot_WindowsSpecialPaths` (`\\?\`, UNC, other drive). Does not cover bash |

---

## 7. Desktop Application (`D-*`) & Extensibility (`E-*`)

| ID | Requirement | Pri | Status | Slice | Notes |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `D-1`–`D-11` | Wails desktop application & UI | P0–P2 | `PARKED` | Phase 2 | Planned for Phase 2 after CLI and SQLite foundation are complete |
| `E-1` | MCP client (stdio & HTTP) | P1 | `PARKED` | Phase 2 | Planned for Phase 2 |
| `E-2` | Custom slash commands (`.forge/commands/*.md`) | P1 | `PARKED` | Phase 2 | Planned for Phase 2 |
| `E-3` | Hooks (`pre_tool`, `post_tool`, etc.) | P2 | `PARKED` | Phase 2 | Planned for Phase 2 |
| `E-4` | Plugin system | P2 | `PARKED` | Phase 2 | Planned for Phase 2 |

