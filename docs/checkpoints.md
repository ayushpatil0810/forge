# Checkpoints & Undo Architecture

Forge provides an isolated, deterministic checkpoint and rollback system (`S-3`, `S-4`, `S-7`). It automatically records snapshots of workspace files before agent mutations occur, allowing developers to review turn histories and instantly roll back changes without altering the project's real Git commit log.

---

## 1. Architectural Design & Shadow Git

To guarantee that Forge never pollutes or alters user-facing Git commit history, tags, branches, hooks, or remotes, Forge maintains a completely isolated **shadow Git repository** stored in the OS-specific user cache directory:

```
[OS UserCacheDir]
└── forge/
    └── checkpoints/
        └── <sha256(canonical_workspace_root)[:16]>/
            ├── HEAD
            ├── config
            ├── objects/
            ├── refs/
            └── info/exclude
```

### Complete Environment Isolation

Every Git plumbing command invoked by the checkpointer explicitly isolates its environment:

- `GIT_DIR`: Absolute path to `<cacheDir>/forge/checkpoints/<hash>`.
- `GIT_WORK_TREE`: Absolute canonical path to the workspace root.
- `GIT_INDEX_FILE`: `<GIT_DIR>/index`.
- `GIT_CONFIG_NOSYSTEM=1`: Disables system-wide Git configuration.
- `GIT_CONFIG_GLOBAL=NUL` (or `/dev/null` on Unix): Points global config to the OS null device to ignore `~/.gitconfig` (requires Git $\ge 2.32$).
- Cross-process file locking: `<cacheDir>/forge/checkpoints/<hash>.lock` serializes concurrent operations with a 10s timeout, eliminating race conditions or index corruption when multiple Forge processes access the same workspace.
- Explicit configuration overrides (guarantees isolation across all Git versions, including Git $< 2.32$):
  - `-c core.hooksPath=""`: Completely disables pre-commit, post-checkout, and fsmonitor hooks.
  - `-c core.autocrlf=false`: Guarantees byte-for-byte CRLF/LF and BOM preservation across Windows, Linux, and macOS.
  - `-c commit.gpgsign=false`: Disables GPG signing regardless of global config.
  - `-c commit.template=""`: Disables commit templates.
  - `-c user.useConfigOnly=false`: Prevents missing identity aborts.
  - `-c user.name=Forge -c user.email=forge@localhost`: Enforces immutable checkpoint committer identity.
- Fixed Identity: `GIT_AUTHOR_NAME="Forge"`, `GIT_AUTHOR_EMAIL="forge@localhost"`, `GIT_COMMITTER_NAME="Forge"`, `GIT_COMMITTER_EMAIL="forge@localhost"`.

### Minimum Git Version & Graceful Degradation
- **Recommended**: Git $\ge 2.32$ (introduced `GIT_CONFIG_GLOBAL` support).
- **Graceful Degradation**: On hosts with Git $< 2.32$ where `GIT_CONFIG_GLOBAL` is ignored, the explicit `-c` flags, `GIT_CONFIG_NOSYSTEM=1`, and explicit environment variables ensure hostile global configurations (failing hooks, `gpgsign=true`, missing user identity) never break shadow checkpoint operations. Verified in `internal/checkpoint/hostile_config_test.go`.

---

## 2. Secrets & File Size Exclusions

### Secret Exclusion (`info/exclude`)

When a shadow repository is initialized, Forge populates its `.git/info/exclude` file with strict exclusion rules so sensitive tokens and keys are never indexed:

```
.git/
.env*
*.pem
id_rsa*
.ssh/
.aws/
*.key
*.pfx
*.p12
credentials
```

### File Size Limit (10MB Cap)

To avoid performance degradation when workspaces contain large binaries, build outputs, or datasets, Forge skips files exceeding 10MB during snapshot collection.

---

## 3. Lazy Triggering & Lifecycle

1. **Lazy Snapshots**: Checkpoints are not created on every agent turn. Instead, Forge activates the checkpoint manager lazily **immediately before the first mutating tool executes** (`write_file`, `edit_file`, or mutating `bash` commands). Turns consisting solely of read operations (`read_file`, `list_dir`, `grep`, `glob`) create zero checkpoints and incur zero overhead.
2. **Pre-Restore Safety Checkpoint**: When restoring to an earlier checkpoint (via `/undo` or `/restore <id>`), Forge first takes a snapshot of the current workspace state before checking out files. This ensures that no unsaved developer edits are permanently lost.
3. **Retention & Pruning**: Forge retains the last 50 checkpoints per workspace, automatically pruning older commits to prevent unbounded disk growth.

---

## 4. User Interfaces & Commands

### Interactive TUI Commands

Within the Bubble Tea terminal interface:

- `/checkpoints`: Displays a table of available checkpoints (checkpoint ID, timestamp, and commit description).
- `/undo`: Reverts the workspace to the checkpoint taken before the most recent mutating agent turn.
- `/restore <id>`: Reverts the workspace to a specific historical checkpoint ID.

### CLI Headless Commands

From shell scripts, CI pipelines, or standard terminal prompts:

```bash
# List all checkpoints for current workspace
forge checkpoints list

# Undo the last mutating turn
forge undo

# Restore a specific checkpoint by ID
forge checkpoints restore <id>
```

---

## 5. Performance Benchmarks

Forge uses native Git plumbing (`git add -A`, `git write-tree`, `git commit-tree`, `git update-ref`, `git read-tree -u --reset`) which leverages the OS page cache and Git's native tree diffing:

- **10,000 files incremental snapshot benchmark**: ~298ms on standard NVMe developer machines.
- **Subsequent clean turns**: 0ms (lazy triggering avoids unnecessary snapshots).
- **Rollback execution**: <1.5s end-to-end.

