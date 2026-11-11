# PROGRESS: Forge

> Living status file. **Agents: read this file before starting work, and update it after every task.**
> Source of truth for requirements is `SPEC.md`. This file tracks execution only.

**Last updated:** 2026-11-12 by lead engineer
**Current phase:** Phase 0 (Foundations) → Phase 1 (MVP)

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

> Slice 3: Read-only tools (agent can explore a repo).

---

## 2. Task checklist

Ordered so that **a working end-to-end product exists as early as possible**. After Slice 2 you can chat with a model in the terminal. After Slice 4 the agent can fix a failing test. Every slice leaves the repo in a runnable state.

Spec references (e.g. `A-1`) point to requirement IDs in `SPEC.md`.

### Slice 0: Skeleton and CI `DONE`

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

### Slice 3: Read-only tools (agent can explore a repo) `TODO`

_Goal: the agent can answer questions about a codebase._

- [ ] Tool registry and JSON-schema declaration
- [ ] `read_file` (line ranges, size cap)
- [ ] `list_dir` / `glob` (respects `.gitignore`)
- [ ] `grep`
- [ ] Workspace path confinement `SEC-8`
- [ ] Tool output truncation with "more available" marker
- [ ] Tool-call cards in TUI (status, duration, collapsible) `T-2`
- [ ] `AGENTS.md` loaded into the system prompt `S-5`

**Exit check:** "Explain how this repo is structured" works on a fixture repo, with visible tool cards and no writes.

### Slice 4: Edit + approve (the agent can change code) `TODO`

_Goal: safe edits with human-in-the-loop. This is the core product moment._

- [ ] `edit_file`: exact match, unique-match required, actionable errors
- [ ] `write_file`
- [ ] Unified diff generation
- [ ] Approval request/response flow with IDs (`ApprovalRequested` / `Respond`) `P-2`
- [ ] Inline diff view in TUI with approve / reject keys `T-3`
- [ ] Permission modes: `ask` (default) and `plan` (read-only) `P-1`
- [ ] Re-read file after edit; fuzz or property tests for the edit matcher

**Exit check:** agent proposes an edit, user sees the diff, approves, file changes; rejecting leaves the file untouched.

### Slice 5: Bash + autonomous fix loop `TODO`

_Goal: the MVP "fix a failing test" scenario works end-to-end._

- [ ] `bash` tool: timeout, output cap, process-group kill
- [ ] Streaming bash output events (`ToolOutputDelta`)
- [ ] Approval prompt shows exact command
- [ ] `accept-edits` mode `P-1`
- [ ] Basic dangerous-command pattern check `SEC-3`
- [ ] Windows shell handling decision implemented (see Decisions)

**Exit check (MVP exit criteria from SPEC §10):** given a fixture Go repo with a failing test, the agent finds the bug, edits the code, runs `go test`, and reports success, entirely within the TUI.

### Slice 6: MVP hardening and demo `TODO`

_Goal: something you can show and publish._

- [ ] Layered config completed and documented `S-6`
- [ ] Error display for provider and tool failures
- [ ] Theme tokens in a single place (`themes/`), one dark and one light palette `T-8`
- [ ] README with install steps and a VHS-recorded demo GIF
- [ ] First tagged pre-release (`v0.1.0`) via GoReleaser snapshot

**Exit check:** a fresh machine can install the binary, set an API key, and complete the Slice 5 scenario by following the README.

### Parked (post-MVP, from SPEC Phase 2+)

Do not start until Slice 6 is `DONE`.

- [ ] Wails spike (recommended **during Phase 0/1 as a 1-2 day throwaway**, only to validate the `Session` contract)
- [ ] SQLite sessions and `--resume` `S-1` `S-2`
- [ ] Git checkpoints, `/undo` `S-3` `S-4`
- [ ] Permission policy file and audit log `P-4` `P-7`
- [ ] Context compaction and budgets `A-6` `A-7`
- [ ] Headless mode (`forge run`) `§3.7`
- [ ] Multi-provider (OpenAI, Gemini, Ollama) `L-3` `L-4` `L-5`
- [ ] Eval harness `§9.2`
- [ ] MCP client, repo map, custom slash commands
- [ ] Desktop app (Phase 4)

---

## 3. Done log

Newest first. Format: `YYYY-MM-DD · Slice N · what was done · evidence (commit/test)`

| Date       | Slice | What                                                                                                                      | Evidence                                              |
| ---------- | ----- | ------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| 2026-11-12 | 2     | Anthropic streaming provider (raw HTTP+SSE, retry with backoff/jitter, models catalog), Bubble Tea TUI (viewport, textarea, spinner, Glamour/Chroma markdown), CLI wiring | Unit/TUI/race tests pass, commit d0cd438             |
| 2026-11-12 | 1     | Core contracts (`Session`, `Provider`, `Tool`, typed `Event` payloads), mock replay provider, agent loop with cancellation, loop guards (max turns and repeated calls), full unit/loop tests | `TestScriptedTranscriptWithTwoToolCallsRunsToDone` and `TestCancellationWithContext` pass; commit 3293d66 |
| 2026-11-12 | 0     | Skeleton, Cobra CLI (`--version`), CI workflow (Linux/macOS/Windows), Makefile, layered config loader (defaults/user/project/env/flags) | `go build ./... && go test ./...` pass; commit 1d25f9a |

---

## 4. Known issues

### Open

| ID         | Severity | Description | Found in | Workaround / note |
| ---------- | -------- | ----------- | -------- | ----------------- |
| _none yet_ |          |             |          |                   |

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
| _pending_  | Windows shell strategy                                                   | SPEC §14 Q4; needed by Slice 5                                                     | `bash` tool        |
| _pending_  | First provider = Anthropic                                               | SPEC §14 Q7                                                                        | None (matches spec)|

_Convert a `_pending_` row to a dated row when decided. If a decision changes a requirement, also update `SPEC.md`._

---

## 6. Next 3 actions

1. Slice 3: implement Tool registry and JSON-schema declaration for read-only tools (`read_file`, `list_dir`, `glob`, `grep`).
2. Slice 3: implement workspace path confinement (`SEC-8`) and output truncation with "more available" marker.
3. Slice 3: build collapsible tool-call cards in the TUI (`T-2`) and load `AGENTS.md` into the system prompt (`S-5`).
