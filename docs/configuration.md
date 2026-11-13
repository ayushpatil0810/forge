# Forge Configuration Guide

Forge uses a layered configuration system (`S-6`) designed to give developers total control over their environment, projects, and CLI overrides.

---

## Configuration Precedence

Configuration values are resolved in the following priority order, from highest to lowest:

| Priority | Layer | Location / Mechanism | Description |
| :--- | :--- | :--- | :--- |
| **1 (Highest)** | **CLI Flags** | Command line flags (`--provider`, `--model`, etc.) | Explicit per-invocation overrides. |
| **2** | **Environment Variables** | `FORGE_*` environment variables | Session or system-level environment configuration. |
| **3** | **Project Configuration** | `<workspace>/.forge/config.json` | Project-specific defaults committed or shared in a repository. |
| **4** | **User Configuration** | `~/.forge/config.json` | Personal developer defaults across all workspaces. |
| **5 (Lowest)** | **Built-in Defaults** | Hardcoded defaults | Sensible out-of-the-box defaults for quick start. |

---

## Configuration Keys Reference

Every configuration option supported by Forge:

| Key | Type | Default | Environment Variable | CLI Flag | Description |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `provider` | `string` | `"anthropic"` | `FORGE_PROVIDER` | `--provider` | Default LLM provider (`anthropic`, `openai`, `gemini`, `mock`). |
| `model` | `string` | `"claude-sonnet-5-5"` | `FORGE_MODEL` | `--model` | Model identifier to use. Defaults to provider's flagship model. |
| `base_url` | `string` | `""` | `FORGE_BASE_URL` / `OPENAI_BASE_URL` | `--base-url` | Custom API base URL (e.g. for Ollama, vLLM, or OpenAI-compatible proxies). |
| `max_turns` | `integer` | `40` | `FORGE_MAX_TURNS` | `--max-turns` | Maximum number of turns in an agent turn loop before stopping. |
| `max_tokens` | `integer` | `4096` | `FORGE_MAX_TOKENS` | `--max-tokens` | Maximum token generation limit per model request. |
| `permission_mode` | `string` | `"ask"` | `FORGE_PERMISSION_MODE` | `--mode` | Default permission policy: `ask`, `accept-edits`, `plan`, or `yolo`. |
| `workspace_root` | `string` | `"."` | `FORGE_WORKSPACE_ROOT` | `--root` | Path to the workspace root directory. |
| `theme` | `string` | `"dark"` | `FORGE_THEME` | `--theme` | TUI color theme: `dark`, `light`, `catppuccin-mocha`, `tokyo-night`, or `auto`. |
| `system_prompt` | `string` | *(generated)* | `FORGE_SYSTEM_PROMPT` | `--system-prompt` | Custom system prompt override. If omitted, Forge generates one with OS and shell context. |
| `shell` | `string` | *(auto-detected)* | `FORGE_SHELL` | `--shell` | Host shell executable (`pwsh`, `powershell.exe`, `cmd.exe`, `bash`, `zsh`, `sh`). |
| `sessions` | `object` | `{}` | - | - | Session persistence and SQLite database settings. |
| `sessions.enabled` | `boolean` | `true` | `FORGE_SESSIONS_ENABLED` | `--no-session` | Controls whether sessions are persisted to SQLite database. |
| `sessions.retention_days` | `integer` | `30` | `FORGE_SESSIONS_RETENTION_DAYS` | - | Default retention period in days before sessions are pruned. |
| `sessions.database_path` | `string` | `"~/.forge/sessions.db"` | `FORGE_SESSIONS_DATABASE_PATH` | - | File path to the SQLite sessions database (mode 0600). |
| `providers` | `array` | `[]` | - | - | Array of named provider profiles with custom base URLs and default models. |
| `ignored_project_keys` | `object` | `{}` | - | - | Map of untrusted project configuration keys ignored due to security policy. |

---

## Inspecting Configuration

To view the effective resolved configuration for your current workspace with all sensitive credentials safely redacted:

```bash
forge config
```

Example output:
```json
{
  "provider": "anthropic",
  "model": "claude-sonnet-5-5",
  "max_turns": 40,
  "max_tokens": 4096,
  "permission_mode": "ask",
  "workspace_root": "D:\\Code\\forge",
  "theme": "dark",
  "system_prompt": "You are Forge, a careful and capable coding agent.",
  "shell": "powershell.exe"
}
```

---

## Local Models with Ollama & LM Studio

Forge natively supports local, open-weight models via `ollama` and `lmstudio` providers without needing API keys:

### Running with Native Local Providers

```bash
# Ollama (defaults to http://127.0.0.1:11434)
forge --provider ollama --model qwen2.5-coder:7b

# LM Studio (defaults to http://127.0.0.1:1234/v1)
forge --provider lmstudio --model model
```

For complete local runtime documentation, hardware sizing, and troubleshooting, see [docs/local-models.md](local-models.md) and [docs/providers.md](providers.md).

### Custom Base URLs & OpenAI-Compatible Proxies

Forge also supports arbitrary OpenAI-compatible proxies (such as vLLM or LocalAI) via custom `base_url`:

```bash
forge --provider openai --model custom-model --base-url http://localhost:8000/v1
```

> [!WARNING]
> When `base_url` points to a non-loopback HTTP address, Forge emits a warning alerting that prompts, diffs, and credentials may be sent in cleartext across the network.

---

## Security Requirements & Untrusted Workspace Configuration (SEC-6)

> [!CAUTION]
> **Workspace repositories are untrusted.**
> An attacker who convinces a developer to clone a repository and run Forge must not achieve arbitrary code execution, credential exfiltration, or safety prompt bypasses.

To prevent repository-level attacks, Forge strictly enforces an allowlist on `<workspace>/.forge/config.json`:

### Permitted Project Configuration Keys
- `model`: Model selection for the repository.
- `theme`: UI visual theme preference.
- `permission_mode`: Restricted strictly to `"ask"`, `"accept-edits"`, or `"plan"`.
- `max_turns` & `max_tokens`: Execution limits.
- `system_prompt`: Project instructions.
- `checkpoints`: Shadow git configuration.

### Forbidden Keys (Ignored with Visible Warning)
The following keys are **strictly forbidden** in `.forge/config.json` and will be discarded:
- `shell`: **Ignored** (arbitrary code execution vector; can only be set via user config or `--shell`).
- `base_url`: **Ignored** (credential exfiltration and SSRF vector; can only be set via user config or `--base-url`).
- `yolo` or `permission_mode: "yolo"`: **Ignored** (repository files cannot disable human confirmation prompts).
- Credentials (`*_api_key`, `token`, `secret`): **Ignored** (prevents committing secrets to git).

Any ignored keys are listed under `ignored_project_keys` in `forge config` and logged to `stderr` with a `SEC-6` security alert.
