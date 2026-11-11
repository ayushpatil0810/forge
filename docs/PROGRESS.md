# PROGRESS: Forge

> Living status file. **Agents: read this file before starting work, and update it after every task.**
> Source of truth for requirements is `SPEC.md`. This file tracks execution only.

**Last updated:** YYYY-MM-DD by <agent/human>
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

> Slice 0: repo skeleton and CI green.

---

## 2. Task checklist

Ordered so that **a working end-to-end product exists as early as possible**. After Slice 2 you can chat with a model in the terminal. After Slice 4 the agent can fix a failing test. Every slice leaves the repo in a runnable state.

Spec references (e.g. `A-1`) point to requirement IDs in `SPEC.md`.

### Slice 0: Skeleton and CI `TODO`

_Goal: a buildable, tested, empty shell._

- [ ] `go mod init`, single module, directory layout from SPEC §6.2
- [ ] Cobra entrypoint: `forge`, `forge --version`
- [ ] CI: build, `go vet`, lint, `go test -race ./...` on Linux/macOS/Windows
- [ ] Makefile or Taskfile (`build`, `test`, `lint`, `run`)
- [ ] Layered config loader stub (defaults, user, project, env, flags) `S-6`

**Exit check:** `go build ./... && go test -race ./...` passes in CI on all three OSes.

### Slice 1: Core contracts + mock provider `TODO`

_Goal: the interfaces both frontends will rely on, testable with no network._

- [ ] `Event` types (SPEC §6.4), JSON-serializable
- [ ] `Session`, `Provider`, `Tool` interfaces (SPEC §6.3)
- [ ] Mock provider that replays scripted/recorded transcripts
- [ ] Minimal agent loop: model call → tool dispatch → result → repeat `A-1` `A-2`
- [ ] Loop guards: max iterations, repeated identical call detection `A-5`
- [ ] Context cancellation wired through loop `A-4`
- [ ] Loop tests using mock provider (including cancel and runaway cases)

**Exit check:** a scripted transcript with two tool calls runs to `Done` in a test; cancellation test passes under `-race`.

### Slice 2: Chat with a real model in the terminal `TODO`

_Goal: first demoable thing. Streaming chat, no tools yet._

- [ ] Anthropic provider with streaming `L-1` `L-2`
- [ ] Retry with exponential backoff on transient errors `A-8`
- [ ] API key from env var (keychain comes later)
- [ ] Bubble Tea app: viewport, multiline input, send/receive `T-4`
- [ ] Streaming markdown via Glamour, syntax highlighting via Chroma `T-1`
- [ ] Spinner with activity label `T-10`
- [ ] Resize handling, mouse scroll, bracketed paste `T-9`
- [ ] `Esc` interrupts the running response `A-4`

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

| Date       | Slice | What | Evidence |
| ---------- | ----- | ---- | -------- |
| _none yet_ |       |      |          |

---

## 4. Known issues

### Open

| ID         | Severity | Description | Found in | Workaround / note |
| ---------- | -------- | ----------- | -------- | ----------------- |
| _none yet_ |          |             |          |                   |

_Severity: `blocker` (stops a slice) · `major` · `minor` · `cosmetic`_

### Resolved

| ID         | Description | Fixed in |
| ---------- | ----------- | -------- |
| _none yet_ |             |          |

---

## 5. Decisions made since planning

Record anything that deviates from, resolves, or adds to `SPEC.md`. Newest first.

| Date      | Decision                            | Reason                                   | Spec impact         |
| --------- | ----------------------------------- | ---------------------------------------- | ------------------- |
| _pending_ | Project name (placeholder: `forge`) | SPEC §14 Q1                              | Rename across repo  |
| _pending_ | License                             | SPEC §14 Q2                              | None                |
| _pending_ | Wails v2 vs v3                      | SPEC §14 Q3; decide before desktop spike | Desktop phase       |
| _pending_ | Windows shell strategy              | SPEC §14 Q4; needed by Slice 5           | `bash` tool         |
| _pending_ | First provider = Anthropic          | SPEC §14 Q7                              | None (matches spec) |

_Convert a `_pending_` row to a dated row when decided. If a decision changes a requirement, also update `SPEC.md`._

---

## 6. Next 3 actions

1. Slice 0: run `go mod init`, create the directory layout from SPEC §6.2, and add the Cobra `forge --version` entrypoint.
2. Slice 0: set up CI (build, vet, lint, `go test -race`) on Linux, macOS, and Windows.
3. Slice 1: define the `Event`, `Session`, `Provider`, and `Tool` types with JSON tags.
