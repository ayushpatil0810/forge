# PROGRESS: Forge

> Living status file. **Agents: read this file before starting work, and update it after every task.**
> Source of truth for requirements is `SPEC.md`. This file tracks execution only.
> Specification coverage matrix: [docs/COVERAGE.md](COVERAGE.md)

**Last updated:** 2026-11-15 by lead engineer
**Current phase:** Phase 1 Complete → Phase 2 (Slice 9b Complete; Ready for PR Review)

---

## How to update this file (rules for agents)

1. Before starting, read **Current focus** and **Next 3 actions**. Work on the first action unless told otherwise.
2. When you finish a task, in this order:
   1. Tick its checkbox and set its status in the checklist.
   2. Add one line to the **Done log** (newest first).
   3. Add anything broken or deferred to **Known issues**.
   4. Record any deviation from `SPEC.md` in **Decisions**.
   5. Rewrite **Current focus** and **Next 3 actions**.
3. Never mark a slice `DONE` unless its **exit check** passes. Run it and note the result in the Done log.
4. Keep entries short. One line each. Link to a commit or PR hash where useful.
5. Do not delete history. Move resolved issues to "Resolved" rather than removing them.
6. Do not start the next slice until the current slice's exit check passes, unless the user says otherwise.

**Status legend:** `TODO` · `DOING` · `BLOCKED` · `DONE` · `CUT`

---

## 1. Current focus

> Slice 9b (Automatic context compaction and token/cost budgets) is fully implemented and all exit checks passed under `-race` (50-turn scripted compaction test, 144k exec fuzz property test, budget exit code tests, TUI tests). Ready for PR review on `feat/compaction-budgets`.

---

## Security review fixes

Part 1 (merged as `2427ffa`) covered F-006, F-007, F-008, F-010, F-021, F-015, F-014, F-001, F-002, and F-017. Part 2 is on `fix/security-2`. Status is what the regression tests show.

| F-ID | Status | Commit |
| ---- | ------ | ------ |
| F-006 | FIXED | `775a966` |
| F-007 | FIXED | `043ad3e` |
| F-008 | FIXED | `87e7f5b` |
| F-010 | FIXED | `f8b256b` |
| F-021 | FIXED | `1751880` |
| F-015 | FIXED | `87a1ad6` |
| F-014 | FIXED | `07ba623` |
| F-001 | FIXED | `a7a71ff` |
| F-002 | FIXED | `7f29088`, follow-up `e39b037` |
| F-017 | FIXED | `1790619` |
| F-012 | FIXED | `3d37329` |
| F-016 | FIXED | `18fb5d3` |
| F-027 | FIXED | `18fb5d3` |
| F-003 | FIXED | `4f41f8b` |
| F-013 | FIXED | `f1198bc` |
| F-004 | FIXED | `f1198bc` |
| F-018 | FIXED | `f1198bc` |
| F-005 | FIXED | `f1198bc` |
| F-022 | FIXED | `f1198bc` |
| F-023 | FIXED | `f1198bc` |
| F-026 | FIXED | `f1198bc` |
| F-011 | FIXED | `f1198bc` |

F-002's identity scan is still the workspace root and the target's parent. `read_file`, `write_file`, and `edit_file` additionally treat a regular file with link count above 1 as protected. The 8.3 test skips when the volume does not create short names. F-012 was already pinned on main by `2c5a484`; `3d37329` only locks that pin. Direct `Run` on a tool still does not itself refuse a protected path; approval is the gate. The audit log is tamper-evident against edits, deletions, and truncation, not against someone who can rewrite both the log and its state file. SEC-4 is process-group kill only, not a sandbox.

---

## 2. Task checklist

Ordered so that **a working end-to-end product exists as early as possible**. After Slice 2 you can chat with a model in the terminal. After Slice 4 the agent can fix a failing test. Every slice leaves the repo in a runnable state.

Spec references (e.g. `A-1`) point to requirement IDs in `SPEC.md` and [docs/COVERAGE.md](COVERAGE.md).

### Slice 0: Skeleton and CI `DONE` (exit check passes locally under `-race`; awaiting human confirmation of CI)

_Goal: a buildable, tested, empty shell._

- [x] `go mod init`, single module, directory layout from SPEC §6.2
- [x] Cobra entrypoint: `forge`, `forge --version`
- [x] CI: build, `go vet`, lint, `go test -race ./...` on Linux/macOS/Windows
- [x] Makefile or Taskfile (`build`, `test`, `lint`, `run`)
- [x] Layered config loader stub (defaults, user, project, env, flags) `S-6`

**Exit check:** `go build ./... && go test -race ./...` passes in CI on all three OSes.

### Slice 1: Core contracts + mock provider `DONE`

_Goal: the interfaces both frontends will rely on, testable with no network._

- [x] `Event` types (SPEC §6.4), JSON-serializable
- [x] `Session`, `Provider`, `Tool` interfaces (SPEC §6.3)
- [x] Mock provider that replays scripted/recorded transcripts
- [x] Minimal agent loop: model call → tool dispatch → result → repeat `A-1` `A-2`
- [x] Loop guards: max iterations, repeated identical call detection `A-5`
- [x] Context cancellation wired through loop `A-4`
- [x] Loop tests using mock provider (including cancel and runaway cases)

**Exit check:** a scripted transcript with two tool calls runs to `Done` in a test; cancellation test passes under `-race`.

### Slice 2: Chat with a real model in the terminal `DONE`

_Goal: first demoable thing. Streaming chat, no tools yet._

- [x] Anthropic provider with streaming `L-1` `L-2`
- [x] Retry with exponential backoff on transient errors `A-8`
- [x] API key from env var (keychain comes later)
- [x] Bubble Tea app: viewport, multiline input, send/receive `T-4`
- [x] Streaming markdown via Glamour, syntax highlighting via Chroma `T-1`
- [x] Spinner with activity label `T-10`
- [x] Resize handling, mouse scroll, bracketed paste `T-9`
- [x] `Esc` interrupts the running response `A-4`

**Exit check:** `forge` opens, streams a multi-paragraph answer with a highlighted code block, and `Esc` stops it cleanly.

### Slice 3: Read-only tools (agent can explore a repo) `DONE`

_Goal: the agent can answer questions about a codebase._

- [x] Tool registry and JSON-schema declaration
- [x] `read_file` (line ranges, size cap, binary check, truncation marker)
- [x] `list_dir` / `glob` (respects `.gitignore`, `**` support, sorted)
- [x] `grep` (pure Go regex search, skips binary/ignored paths, truncation marker)
- [x] Workspace path confinement `SEC-8`
- [x] Tool output truncation with "more available" marker
- [x] Tool-call cards in TUI (status, duration, collapsible `Ctrl+O`) `T-2`
- [x] `AGENTS.md` / `CLAUDE.md` loaded into the system prompt `S-5`

**Exit check:** "Explain how this repo is structured" works on a fixture repo, with visible tool cards and no writes (verified live on OpenAI).

### Slice 3b: Multi-provider: OpenAI + Gemini `DONE (OpenAI verified live; Anthropic and Gemini experimental, not live-verified)`

_Goal: provider-agnostic core with OpenAI and Gemini streaming, catalog, and shared retry/contract suite._

- [x] Shared provider contract test suite (`httptest` recorded SSE fixtures)
- [x] Shared retry/backoff package (`internal/llm/retry`)
- [x] OpenAI provider with streaming and tool calls (`L-3`)
- [x] Gemini provider with streaming and schema sanitizer (`L-4`)
- [x] Layered config `--provider` and `--model` wiring with factory
- [x] Token cost tracking and display in TUI footer

**Exit check:** the provider contract test suite passes for Anthropic, OpenAI, and Gemini under `-race` (OpenAI verified live; Anthropic and Gemini experimental, not live-verified).

### Slice 4: Edit + approve (the agent can change code) `DONE`

_Goal: safe edits with human-in-the-loop. This is the core product moment._

- [x] `edit_file`: exact match, unique-match required, actionable errors
- [x] `write_file`
- [x] Unified diff generation
- [x] Approval request/response flow with IDs (`ApprovalRequested` / `Respond`) `P-2`
- [x] Inline diff view in TUI with approve / reject keys `T-3`
- [x] Permission modes: `ask` (default) and `plan` (read-only) `P-1`
- [x] Re-read file after edit; fuzz or property tests for the edit matcher

**Exit check:** agent proposes an edit, user sees the diff, approves, file changes; rejecting leaves the file untouched (verified live on OpenAI).

### Slice 5: Bash + autonomous fix loop `DONE`

_Goal: the MVP "fix a failing test" scenario works end-to-end._

- [x] `bash` tool: timeout, output cap, process-group kill
- [x] Streaming bash output events (`ToolOutputDelta`)
- [x] Approval prompt shows exact command
- [x] `accept-edits` mode `P-1`
- [x] Basic dangerous-command pattern check `SEC-3`
- [x] Windows shell handling decision implemented (see Decisions)

**Exit check (MVP exit criteria from SPEC §10):** given a fixture Go repo with a failing test, the agent finds the bug, edits the code, runs `go test`, and reports success, entirely within the TUI (verified live on OpenAI).

### Slice 6: MVP hardening and demo `DOING` (awaiting my fresh-machine check and demo recording)

_Goal: something you can show and publish._

- [x] Layered config completed and documented `S-6` (`docs/configuration.md`, `internal/config/doc_test.go`, `forge config`)
- [x] Error display for provider and tool failures (typed error classification, friendly guidance, `RetryScheduled` event, secret redaction)
- [x] Theme tokens in a single place (`themes/`), dark, light, catppuccin-mocha, tokyo-night, auto, monochrome `T-8`, `T-11`, WCAG $\ge 4.5:1$ contrast test
- [x] Golden snapshots of main view (60, 80, 120 cols under dark/light) and error classes in `internal/tui/golden_test.go`
- [x] Glyph width & visual truncation tests (`runewidth`), ASCII fallback for `TERM=dumb`
- [x] Deterministic demo (`--demo`, `--demo-speed`, zero external toolchain required)
- [x] First pre-release readiness via GoReleaser config and CGO_ENABLED=0 builds across 6 targets
- [x] README with install steps, architecture diagram, honesty pass, and VHS demo tape (`docs/demo/demo.tape`)

**Exit check:** a fresh machine can install the binary, set an API key, and complete the Slice 5 scenario by following the README (verified via clean-room build & demo test; awaiting user fresh-machine check).

### Slice 7: Checkpoints and undo `DOING` (awaiting my live run)

_Goal: isolated, automatic workspace snapshots before mutating tool execution and instant rollback._

- [x] Shadow git repo in `os.UserCacheDir()/forge/checkpoints/<sha256(canonical_root)[:16]>` (`S-3`, `S-7`)
- [x] Strict environment isolation (`GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_CONFIG_NOSYSTEM=1`, `core.hooksPath=""`, `core.autocrlf=false`, fixed identity)
- [x] Secret exclusion via `.git/info/exclude` (`.git/`, `.env*`, `*.pem`, `id_rsa*`, `.ssh/`, `.aws/`, `*.key`, `*.pfx`, `*.p12`, `credentials`)
- [x] 10MB file size cap to avoid indexing large binaries
- [x] Lazy checkpoint trigger before the first mutating tool call of a turn; 0ms overhead on read-only turns
- [x] Automatic pre-restore checkpoint before undo/restore execution
- [x] Automatic retention pruning keeping the last 50 checkpoints
- [x] TUI commands: `/checkpoints`, `/undo`, `/restore <id>` (`S-4`)
- [x] Headless CLI commands: `forge checkpoints list`, `forge checkpoints restore <id>`, `forge undo`
- [x] Incremental snapshot benchmark on 10,000 files (~298ms)
- [x] Architecture documented in `docs/checkpoints.md` and coverage updated in `docs/COVERAGE.md`

**Exit check:** Checkpoint tests pass round-trip restoration, secret exclusion, CRLF preservation, and benchmark under `-race` (`internal/agent/undo_exit_test.go:TestSlice7_UndoExitCheck` passing in git and non-git workspaces); awaiting user live test in TUI.

### Slice 8: Onboarding, credentials, providers and models `DOING (awaiting my live run)`

_Goal: secure credential management, local model detection, first-run wizard, and mid-session switching._

- [x] UI-free credential manager (`internal/credentials`) with resolution precedence: env > keychain > consented file (0600, Linux only) > none (`SEC-6`)
- [x] Keyring integration via `zalando/go-keyring` with masked output and non-destructive connection probe (no secret in URL)
- [x] CLI credential commands: `forge auth login [provider]`, `forge auth logout [provider]`, `forge auth status`
- [x] Removed `--api-key` and reject any `--*key*` flag with exit code 2 and hint to use `forge auth login`
- [x] Provider profiles array in config and backward compatibility with flat keys
- [x] Local runtime presets for Ollama (`http://127.0.0.1:11434`) and LM Studio (`http://127.0.0.1:1234/v1`) (`L-5`)
- [x] Cleartext HTTP security warning for non-loopback base URLs
- [x] Local model quirks: loopback probe (300ms max), "Loading model..." cold-start spinner, single-chunk tool calls, omitted usage handling
- [x] TUI first-run setup (provider, API key into the OS keychain, model ID typed by the user; no model menu; skippable via Esc)
- [x] TUI commands: `/settings`, `/models`, `/model <id>`, `/provider`, `/auth`. Known model IDs still get a context-window check (`L-7`)
- [x] Complete documentation: `docs/providers.md`, `docs/local-models.md`, `README.md`, `docs/configuration.md`, `docs/COVERAGE.md`

**Exit check:** `internal/tui/auth_teatest_test.go:TestSlice8_RealAuthTeatestExitCheck` passes under `-race`, testing local profile setup, hosted profile with fake key masked and asserted absent everywhere (view, golden snapshot, error strings, `forge config`, `forge auth status`, disk files), env override, and logout; awaiting user live run.

### Slice 9: SQLite session persistence and resume `DOING (awaiting live run)`

_Goal: resilient session persistence in pure Go SQLite, crash recovery, and instant CLI/TUI resumption._

- [x] Embedded pure Go SQLite (`modernc.org/sqlite v1.34.5`, `CGO_ENABLED=0`)
- [x] Schema v1 with forward migrations (`sessions`, `messages`, `tool_calls`, `checkpoints`, `usage`, `schema_version`)
- [x] Lazy DB open at `~/.forge/sessions.db`, mode 0600, WAL mode, foreign keys, busy timeout
- [x] Secret redaction across all stored fields; 64KB cap on tool outputs with truncation marker
- [x] Crash safety: user message stored before provider call, tool calls and usage recorded incrementally, dangling tool call synthesis on resume
- [x] `forge --resume [id]`, `forge run --resume <id>`, TUI `/resume` and `/sessions`, recent session fuzzy picker
- [x] Workspace canonical binding validation (`--root` override)
- [x] Safe mode reset on resume (never restore `yolo`, resume in `ask`)
- [x] Empty read-before-write tracker on resume with injected `[Resumed session...]` system note
- [x] Undo continuity: `/undo` reverts pre-resume edits in shadow git repo
- [x] CLI management: `forge sessions list`, `forge sessions show`, `forge sessions delete`, `forge sessions prune`, `forge sessions doctor`
- [x] Documentation: `docs/sessions.md`, `README.md`, `docs/configuration.md`, `docs/COVERAGE.md`

**Exit check:** `cmd/forge/session_exit_test.go:TestSlice9_SessionPersistenceAndResumeExitCheck` passes under `-race`, verifying multi-turn file edits, SQLite persistence, resumption fidelity, `/undo` continuity reverting pre-resume edits, and follow-up turn context; `cmd/forge/session_cmd_test.go` verifies CLI subcommands; awaiting user live run.

### Slice 9a: Policy file, audit log and secret handling `DOING (automated tests complete; awaiting live run)`

_Goal: pre-approve routine actions safely, block dangerous actions unconditionally, and maintain an immutable, tamper-evident audit log._

- [x] User policy (`~/.forge/policy.json`) and project policy (`.forge/policy.json`) loaded and merged (`P-4`)
- [x] Untrusted repository defense: project policy `allow` rules strictly ignored with visible warning (`P-4`)
- [x] Hard safety floor: dangerous commands, protected paths, opaque bash commands, and argument hazards never auto-allowed
- [x] Deny rules precedence across all modes including `yolo` (`P-5`)
- [x] Shell analysis: segment splitting on `;`, `&&`, `||`, `|`, `&`, `\n`, opacity classification, argument hazards (`go test -exec`, `git -c`, etc.)
- [x] Path matching: `**` globs, canonical confinement, Windows case-insensitivity
- [x] Secret reads: credential files (`.env*`, `.ssh/`, `.aws/`, `*.pem`, `*.key`, `id_rsa*`) via `read_file` prompt in all modes (including plan); headless auto-denies; `grep` and `glob` skip credential files and report count (`SEC-5`, `P-6`)
- [x] High-confidence secret redaction (`[REDACTED:<type>]`) in tool outputs, UI, database, and audit log (`SEC-5`)
- [x] All 5 approval keys: `y` (once), `a` (session), `A` (always allow, preview card, user policy save), `n`/`d` (deny), `f` (deny with feedback capped at 500 chars and redacted) (`P-3`)
- [x] CLI policy management: `forge policy list`, `forge policy check`, `forge policy add`, `forge policy remove`, TUI `/policy`
- [x] JSONL audit logging (`~/.forge/audit/<hash>.jsonl`, 0600), rotation at `audit.max_mb`, fail-closed with `audit.required`, cryptographic per-session SHA-256 hash chains (`P-7`)
- [x] CLI audit management: `forge audit show`, `forge audit verify` (detects modification, deletion, reordering)
- [x] Documentation: `docs/policy.md`, `docs/audit.md`, `docs/VERIFY.md`, `docs/configuration.md`, `README.md`, `docs/COVERAGE.md`, `docs/SPEC.md`

**Exit check:** `cmd/forge/policy_audit_exit_test.go:TestSlice9a_AutomatedExitCheck` passes under `-race`, verifying policy evaluation hierarchy, untrusted project allow rejection, argument hazard floor, deny precedence in YOLO, headless auto-denial, audit log generation with decision sources, and cryptographic tamper verification; awaiting user live run.

---

### Phase 2 Plan (Remaining Slices)

The roadmap below structures remaining post-MVP development into 6 distinct, vertical slices:

#### Slice 9b: Context Compaction & Token Budgets (`A-6`, `A-7`)
- **Scope**: Hard token and cost budgets (`--max-cost`, `FORGE_MAX_COST`). Context compaction strategy (truncate old tool outputs, generate high-level conversational summary when approaching window limit).
- **Exit Check**: A simulated 50-turn conversation exceeding context window limits automatically triggers compaction, allowing continuous execution without context length errors under `-race`.

#### Slice 10: Quantitative Eval Harness (`§9.2`, `SPEC §13`)
- **Scope**: Implement `forge eval --suite <name>` running Forge against standard coding benchmarks (e.g. HumanEval or custom Go repo fixtures) with automated scoring of fix rate, token efficiency, wall-clock duration, and cost. Reports by provider and model in a single command, producing a Markdown/JSON scorecard.
- **Exit Check**: Eval harness executes 10 benchmark fixtures autonomously and outputs a reproducible Markdown/JSON scorecard under `-race`.

#### Slice 11: Concurrent Tool Execution (`A-3`)
- **Scope**: Enable the agent loop to execute independent read-only tool calls (e.g. parallel `read_file` or `grep` operations requested in a single model turn) concurrently via `sync.WaitGroup` or `errgroup`, while maintaining deterministic event streaming order.
- **Exit Check**: Barrier rendezvous test where N tool calls release only when running simultaneously under `-race`.

#### Slice 12: Command Palette, `@` Mentions & Input History (`T-4`, `T-6`, `T-7`)
- **Scope**: Interactive command palette popup triggered by `/` with fuzzy search across all slash commands (`/undo`, `/checkpoints`, `/restore`, `/model`, `/provider`, `/auth`, `/resume`). Interactive `@` file mention autocomplete popup querying workspace files. Persistent input history navigation across sessions.
- **Exit Check**: TUI tests verify fuzzy filtering on `/` and `@` popups, arrow key selection, insertion into the textarea, and Up/Down navigation through previous session prompts under `-race`.

#### Slice 13: Wails Desktop Spike (`SPEC §14 Q3`, `D-1`, `D-2`)
- **Scope**: A timeboxed spike using Wails (evaluating v2 vs v3) with a React + TypeScript frontend to validate that the in-process `Session` contract, token delta streaming, and approval events bind cleanly to a desktop webview.
- **Exit Check**: A native desktop window opens, connects to `agent.Session`, streams token deltas in a React view, and handles an approval dialog.

---

## 3. Done log

Newest first. Format: `YYYY-MM-DD · Slice N · what was done · evidence (commit/test)`

| Date       | Slice | What                                                                                                                      | Evidence                                              |
| ---------- | ----- | ------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| 2026-11-15 | 9b    | Context compaction (A-7) and token/cost budgets (A-6): unified ContextInfo resolver with strict precedence (flag, user config, local server probe, catalog fallback, unknown), PriceInfo resolver with catalog note, token accounting (ground truth + calibration ratio), proactive multi-tier compaction (Tier 1 tool output clearing at 60%, Tier 2 summarization with deterministic facts & injection hardening at 80%, emergency at 92%), reactive single-retry compaction on typed *llm.ContextLengthError, immutable s.history transcript (Amendment E), SQLite compactions table & resume reconstruction via agent.BuildModelView, budget tracker (--max-cost, --max-tokens, budget.* config, exit code 5, exit code 2 on unknown price), TUI footer budget display, /cost, /budget, /compact, /model metadata | `internal/agent/scripted_50turn_test.go`, `internal/agent/fuzz_compaction_test.go` (144k execs), `internal/agent/compact_test.go`, `internal/agent/budget_test.go`, `cmd/forge/budget_exit_test.go`; all pass under `-race` |
| 2026-11-14 | review | Security review fixes F-006, F-007, F-008, F-010, F-021, F-015, F-014, F-001, F-002, F-017 on `fix/security-1` | See the Security review fixes section. Regression tests are named in `docs/COVERAGE.md` for SEC-1, S-5, SEC-6, SEC-8, and P-6 |
| 2026-11-13 | 9a    | Layered security policies, audit logging and secret handling: user (~/.forge/policy.json) and project (.forge/policy.json) policies (P-4), untrusted repo defense (ignore project allow rules), hard safety floor, shell command segment splitting/opacity/hazard analysis, path matching, credential file prompt in all modes and grep/glob skip (SEC-5, P-6), high-confidence regex redactor, 5 approval keys (y, a, A, n, f with 500-char redacted feedback) (P-3), CLI policy commands, tamper-evident SHA-256 hash-chained JSONL audit log with verify/rotation/fail-closed (P-7), docs (policy.md, audit.md, VERIFY.md) | `cmd/forge/policy_audit_exit_test.go:TestSlice9a_AutomatedExitCheck` (1.735s), `internal/policy/bash_test.go`, `internal/redact/redact_test.go`, `internal/audit/logger_test.go`, `internal/tui/approval_teatest_test.go`; all pass under `-race` |
| 2026-11-13 | Git hygiene | Committed Slices 7–9 in logical groups (deps/toolchain, checkpoints core, credentials core, session core, engine/CLI/TUI integration, docs) with clean race test verification and push | Commits ac1e4b5, 1648572, 87db922, 559bd77, ef6a0b9, fa27955; `go test -race -count=1 ./...` passes 100% |
| 2026-11-13 | 9     | SQLite session persistence and resume: pure Go SQLite (`modernc.org/sqlite v1.34.5`), WAL mode, mode 0600, forward migrations, secret redaction, 64KB tool output cap, crash safety, CLI `--resume [id]`, `forge run --resume <id>`, TUI `/resume` & `/sessions`, recent session fuzzy picker, canonical workspace binding validation, safe mode reset (`ask`), pre-resume undo continuity, and CLI management (`forge sessions list/show/delete/prune`) | `cmd/forge/session_exit_test.go:TestSlice9_SessionPersistenceAndResumeExitCheck`, `cmd/forge/session_cmd_test.go:TestSessionsCmd_ListShowDeletePrune`, `internal/session/store_test.go`, benchmark 5.48ms; all pass under `-race` |
| 2026-11-13 | 8     | Onboarding, credentials, providers and models: OS keychain integration (`zalando/go-keyring`), fallback file 0600 (Linux-only with consent), `forge auth login/logout/status`, removed `--api-key` and reject secret flags with exit code 2, provider profiles, loopback probing for Ollama & LM Studio, "Loading model..." spinner, TUI onboarding wizard, `/model`, `/provider`, `/auth`, context-window validation, docs (`providers.md`, `local-models.md`) | `cmd/forge/auth_exit_test.go:TestSlice8_AuthAndHeadlessExitCheck`, `internal/tui/auth_teatest_test.go:TestSlice8_RealAuthTeatestExitCheck`, `internal/credentials/credentials_test.go`, `internal/llm/openai/local_contract_test.go`; all pass under `-race` |
| 2026-11-13 | 7     | Checkpoints and undo exit check: automated multi-turn exit check test with file edits, creations, bash modification, and byte-for-byte undo restoration; cross-process lock file; hostile git config isolation | `internal/agent/undo_exit_test.go:TestSlice7_UndoExitCheck` (git and non-git workspaces), `TestCheckpoint_HostileGitConfig`, `TestConcurrentLocking`; all pass under `-race` |
| 2026-11-12 | 7     | Checkpoints and undo: shadow Git repo in `UserCacheDir`, complete environment isolation, secret exclusion (`.git/info/exclude`), 10MB size cap, lazy turn triggering, pre-restore safety snapshots, TUI `/checkpoints`, `/undo`, `/restore <id>`, CLI `forge checkpoints list`, `forge undo`, `forge checkpoints restore <id>` | `internal/checkpoint/checkpoint_test.go` (round-trip, non-git, CRLF/BOM, secrets, 10k file benchmark ~298ms); all pass under `-race` |
| 2026-11-12 | 6     | Golden snapshots for main view (60/80/120 cols, dark/light) and error classes; visual width enforcement (`runewidth`); ASCII fallback for `TERM=dumb`; WCAG $\ge 4.5:1$ contrast verified across all theme tokens; `--demo-speed` support and zero-toolchain demo | `internal/tui/golden_test.go` and `internal/tui/theme_test.go` pass 100% under `-race` |
| 2026-11-12 | Drift | Canonical headless mode `forge run "prompt"` (`--output text\|json\|stream-json`, `--permission-mode`, `--max-turns`, auto-deny); protected paths (P-6) enforcement in tracker and permissions; approval key `'a'` (session allow); Ollama custom base URL support & documentation | `cmd/forge/main_test.go`, `internal/permission/permission_test.go`, `internal/llm/openai/openai_test.go` pass under `-race` |
| 2026-11-12 | 6     | MVP hardening, config docs & doc-reflection test, typed error classification, themes (dark/light/mocha/tokyo-night), deterministic demo, GoReleaser config, CGO_ENABLED=0 builds for 6 OS/arch targets | All unit/race tests pass; clean-room build & demo runs; binary 16.7MB, startup 39ms |
| 2026-11-12 | 5     | Bash tool with live output streaming, process-tree termination (Windows Job Object + POSIX process groups), dangerous command detector, fix loop guard, and automated test-fix verification on 3 fixtures | `TestSlice5_FixLoopExitCheck` passes (56.04s) across offbyone, nilpanic, wrongimport; all tests pass under `-race`; commit 7d6d739 |
| 2026-11-12 | 4     | `edit_file` with unique matching, line-ending preservation (CRLF/LF), read-before-write guard, `write_file`, unified diffs, approval prompts with diff views in TUI | `TestEditFile_Table`, `FuzzEditMatcher`, `TestTUIApprovalFlow` pass under `-race`; commit 38ea9b9 |
| 2026-11-12 | 3b    | Multi-provider support (OpenAI `/v1/chat/completions`, Gemini Developer API with schema sanitizer), shared `internal/llm/retry`, provider factory `llm.NewProvider`, `--provider`/`--model` flags, TUI token cost tracking | All 10 contract suite tests pass for Anthropic, OpenAI, Gemini under `-race`; commit 0f8ea60 |
| 2026-11-12 | 3     | Read-only tools (`read_file`, `list_dir`, `glob`, `grep`), SEC-8 workspace path confinement, SEC-1 system prompt with `AGENTS.md` / `CLAUDE.md`, collapsible tool cards (`Ctrl+O`) in TUI | `TestExplainRepoStructureEndToEnd` fixture test and tool card tests pass under `-race`; commit 93ae114 |
| 2026-11-12 | 2     | Anthropic streaming provider (raw HTTP+SSE, retry with backoff/jitter, models catalog), Bubble Tea TUI (viewport, textarea, spinner, Glamour/Chroma markdown), CLI wiring | Unit/TUI/race tests pass, commit d0cd438             |
| 2026-11-12 | 1     | Core contracts (`Session`, `Provider`, `Tool`, typed `Event` payloads), mock replay provider, agent loop with cancellation, loop guards (max turns and repeated calls), full unit/loop tests | `TestScriptedTranscriptWithTwoToolCallsRunsToDone` and `TestCancellationWithContext` pass; commit 3293d66 |
| 2026-11-12 | 0     | Skeleton, Cobra CLI (`--version`), CI workflow (Linux/macOS/Windows), Makefile, layered config loader (defaults/user/project/env/flags) | `go build ./... && go test ./...` pass; commit 1d25f9a |

---

## 4. Known issues

### Open

| ID      | Severity | Description                                                                 | Found in | Workaround / note                                                                    |
| ------- | -------- | --------------------------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------ |
| ISSUE-2 | major    | CI not confirmed green on all three OSes (Linux, macOS, Windows) by user    | Slice 0  | All tests pass locally under -race on Windows; awaiting human confirmation in GitHub |
| ISSUE-3 | minor    | Anthropic and Gemini experimental; Ollama and LM Studio contract-tested only | Slice 3b | Documented as experimental / contract-tested; no live testing planned                |
| ISSUE-4 | blocker  | License undecided; keep project name "forge" (blocks tagging v0.1.0)        | Slice 0  | Kept name "forge"; license remains undecided; blocks tagging v0.1.0                  |

_Severity: `blocker` (stops a slice) · `major` · `minor` · `cosmetic`_

### Resolved

| ID      | Description                                                                                                                                                                                                         | Fixed in                                                                                                                     |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| ISSUE-1 | Local Windows host had 32-bit MinGW GCC in PATH; `go test -race` failed with cc1 64-bit not compiled in. Standard `go test ./...` passed locally; local `-race` needed 64-bit GCC.                                 | Pre-flight: installed 64-bit w64devkit GCC 16.2.0 in `C:\Users\ayush\w64devkit\bin` ahead in PATH. `go test -race ./...` passes. |

---

## 5. Decisions made since planning

Record anything that deviates from, resolves, or adds to `SPEC.md`. Newest first.

| Date       | Decision                                                                 | Reason                                                                             | Spec impact        |
| ---------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------- | ------------------ |
| 2026-11-14 | First launch asks for a provider, an API key, and a model ID the user types | The setup screen no longer offers a fixed model list, and opening the app does not require an environment variable | `internal/tui/wizard.go`, `docs/providers.md`, `README.md` |
| 2026-11-13 | Untrusted repository defense: project policy allow rules strictly ignored | Prevents malicious cloned repositories from obtaining automatic pre-approved execution | `internal/policy`, SPEC §3.3 |
| 2026-11-13 | Command segment splitting and argument hazard safety floor                | Splitting on unquoted delimiters prevents chained bypasses; hazards (`go test -exec`) forced to prompt | `internal/policy`, SPEC §3.3 |
| 2026-11-13 | Tamper-evident cryptographic per-session SHA-256 hash chains for audit log | Ensures modification, deletion, or reordering of audit records is detected instantly by verify | `internal/audit`, SPEC §3.3 |
| 2026-11-13 | Approval key `'f'` captures feedback for the model                       | Rejection feedback (capped at 500 chars, redacted) guides the model to alternate solutions | `internal/tui`, `internal/agent`, SPEC §3.3 |
| 2026-11-13 | Slices 7–9 committed late in deferred batch out of historical order      | Slices 7, 8, and 9 were developed and verified locally prior to committing; grouped logically into isolated backend modules, toolchain, and unified integration | Git hygiene        |
| 2026-11-13 | Pure Go SQLite (`modernc.org/sqlite v1.34.5`) for session persistence    | Zero CGO requirement, static binary portability, WAL mode concurrency safety       | `internal/session`, SPEC §6.6 |
| 2026-11-13 | Reject `--*key*` CLI flags with exit code 2 and hint to use `forge auth` | Absolute protection against secret leakage in process process listings / history  | `cmd/forge/main.go`, SPEC §3.5 |
| 2026-11-13 | Credential resolution order: env > keychain > consented file (0600) > none | Secure storage via `zalando/go-keyring` with zero plain-text leaks, Linux-only consented fallback | `internal/credentials`, SPEC §3.5, §6.3 |
| 2026-11-13 | Provider profiles and native local model presets (`ollama`, `lmstudio`)  | Frictionless zero-key local runtime usage with loopback auto-detection             | `internal/config`, `internal/credentials`, `docs/local-models.md` |
| 2026-11-13 | First-run onboarding wizard in TUI for unconfigured setups               | Guides new developers step-by-step through provider, key, and model setup; skippable with Esc | `internal/tui/wizard.go` |
| 2026-11-13 | Cleartext HTTP security warning on non-loopback base URLs                | Prevents accidental insecure transmission of code diffs and tokens across networks | `internal/config/config.go` |
| 2026-11-13 | Strict allowlist for project `.forge/config.json` (SEC-6)                | Discards hostile `shell`, `base_url`, `yolo`, and credentials from untrusted repos | `internal/config/config.go` |
| 2026-11-12 | Shadow Git repository for checkpoints in `os.UserCacheDir()` with strict isolation | Never touches user Git history, refs, hooks, or remotes; works in non-git repos   | `internal/checkpoint`, SPEC §3.4 |
| 2026-11-12 | Canonical headless form `forge run "prompt"` with `--output text\|json\|stream-json` | Provides standard CLI contract for CI/scripting; `forge "prompt"` retained as alias | `cmd/forge`, `docs/headless.md` |
| 2026-11-12 | Enforce P-6 protected paths in `internal/tools/tracker` & `permission`   | Sensitive paths (`.git/`, `.env*`, `.ssh/`, `.aws/`, credentials) always require human approval | `internal/permission`, SPEC §3.3 |
| 2026-11-12 | Approval key `'a'` for session allow                                     | Speeds up repetitive safe edits; strictly blocked for dangerous commands           | `internal/tui`, SPEC §3.3 |
| 2026-11-12 | Ollama & OpenAI-compatible proxy support via `base_url`                  | Enables local, offline models with custom endpoints without requiring OpenAI API keys | `internal/llm/openai`, `docs/configuration.md` |
| 2026-11-12 | Windows native shell auto-detection (`pwsh` -> `powershell.exe` -> `cmd.exe`) with `-NoProfile -NonInteractive` | Native Windows execution without requiring bash/msys; configurable via `--shell` / `FORGE_SHELL` / config | `internal/tools`   |
| 2026-11-12 | Process tree termination via Windows Job Object and POSIX process groups | Guarantees all descendant child processes terminate on timeout or Esc cancellation | `internal/tools`   |
| 2026-11-12 | Head/tail truncation for bash tool output (10KB head, 20KB tail)          | Retains compiler diagnostics at top and test failure summaries at bottom within 30KB cap | `internal/tools`   |
| 2026-11-12 | Table-driven dangerous command classifier in `internal/permission`       | Enforces approval even in accept-edits and YOLO modes for destructive commands     | `internal/permission` |
| 2026-11-12 | Unified diff generation via `pmezard/go-difflib`                         | Standardized, high-quality unified diff formatting for approvals and reviews       | `internal/tools`   |
| 2026-11-12 | Fix loop stall guard in `Session` (same command 3x with identical state) | Prevents runaway token waste when a repeated fix attempt does not work             | `internal/agent`   |
| 2026-11-12 | Gemini Developer API with JSON schema sanitizer                          | Direct API key access via GEMINI_API_KEY/GOOGLE_API_KEY without GCP project setup   | `internal/llm/gemini` |
| 2026-11-12 | OpenAI Chat Completions API (`/v1/chat/completions`) for OpenAI provider | Maximum compatibility across OpenAI proxies (Ollama, vLLM, OpenRouter, LocalAI)   | `internal/llm/openai` |
| 2026-11-12 | Provider factory registry pattern (`llm.RegisterProvider`)               | Avoids circular import cycles between `internal/llm` and individual provider pkgs  | `internal/llm`     |
| 2026-11-12 | Official OpenAI & Gemini live model catalogs & pricing                   | Up-to-date specs: `gpt-4o` ($2.50/$10), `gemini-2.5-pro` ($1.25/$10); retired removed | `internal/llm`     |
| 2026-11-12 | Updated Anthropic catalog to 5.x lineup (default `claude-sonnet-5-5`)    | Official docs (https://docs.anthropic.com/en/docs/about-claude/models); retired 3.x removed | `internal/llm/anthropic` |
| 2026-11-12 | Raw HTTP + SSE for Anthropic provider                                    | SPEC §11, zero SDK dependencies, complete control over retries, backoff, and events | `internal/llm/anthropic` |
| 2026-11-12 | Bubble Tea v1 ecosystem (`bubbletea v1.3.4`, `bubbles v0.20.0`, `lipgloss v1.0.0`, `glamour v0.8.0`) | Stable production releases across the Charm ecosystem, full cross-compatibility   | `internal/tui`     |
| 2026-11-12 | Throttled markdown rendering (40ms) with unclosed code fence recovery    | Prevents rendering flicker and layout breaks during token streaming (T-1)          | `internal/tui`     |
| 2026-11-12 | Aligned `Tool` interface with SPEC §6.3                                  | Exact contract: `Name()`, `Description()`, `Schema()`, `Run()`, `NeedsApproval()`  | `internal/tools`   |
| 2026-11-12 | Added repeated identical call detection in loop guard                     | Breaks runaway loops when consecutive calls are identical (A-5)                    | `internal/agent`   |
| 2026-11-12 | Use `github.com/spf13/cobra` v1.10.2                                     | SPEC §11, actively maintained standard Go CLI                                      | CLI entrypoint     |
| 2026-11-12 | Standard library for layered config loader                               | Self-contained, zero-dependency, satisfies S-6                                     | `internal/config`  |
| _pending_  | Project name (placeholder: `forge`)                                      | SPEC §14 Q1                                                                        | Rename across repo |
| _pending_  | License                                                                  | SPEC §14 Q2                                                                        | None               |
| _pending_  | Wails v2 vs v3                                                           | SPEC §14 Q3; decide before desktop spike                                           | Desktop phase      |
| _pending_  | First provider = Anthropic                                               | SPEC §14 Q7                                                                        | None (matches spec)|

_Convert a `_pending_` row to a dated row when decided. If a decision changes a requirement, also update `SPEC.md`._

---

## 6. Next 3 actions

1. Awaiting human live verification: CI status confirmation on GitHub Actions (Linux/macOS/Windows), fresh machine check, demo recording, and human testing of Slices 6, 7, 8, 9, and 9a in live TUI.
2. Awaiting human resolution of ISSUE-4 (license undecided; keep name "forge") before tagging v0.1.0.
3. Next planned implementation after human verification & approval: Slice 9b (Context Compaction & Token Budgets, `A-6`, `A-7`).
