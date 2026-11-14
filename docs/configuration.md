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
| `model` | `string` | `"claude-sonnet-5-5"` | `FORGE_MODEL` | `--model` | Model ID sent to the provider. First launch asks you to type it. The default applies only when no model has been saved and `--model` / `FORGE_MODEL` are unset. |
| `base_url` | `string` | `""` | `FORGE_BASE_URL`; `OPENAI_BASE_URL` only for OpenAI-compatible providers | `--base-url`, `--allow-insecure-base-url` | Custom API base URL. `OPENAI_BASE_URL` applies only when the resolved provider is `openai`, `ollama`, `lmstudio`, or a profile whose kind is one of those. A non-loopback `http://` URL is refused unless `--allow-insecure-base-url` is set. Loopback `http://` is always allowed. |
| `max_turns` | `integer` | `40` | `FORGE_MAX_TURNS` | `--max-turns` | Maximum number of turns in an agent turn loop before stopping. |
| `max_tokens` | `integer` | `4096` | `FORGE_MAX_TOKENS` | `--max-tokens` | Maximum token generation limit per model request. |
| `permission_mode` | `string` | `"ask"` | `FORGE_PERMISSION_MODE` | `--mode` | Default permission policy: `ask`, `accept-edits`, `plan`, or `yolo`. |
| `workspace_root` | `string` | `"."` | `FORGE_WORKSPACE_ROOT` | `--root` | Path to the workspace root directory. |
| `theme` | `string` | `"dark"` | `FORGE_THEME` | `--theme` | TUI color theme: `dark`, `light`, `catppuccin-mocha`, `tokyo-night`, or `auto`. |
| `system_prompt` | `string` | `""` | `FORGE_SYSTEM_PROMPT` | `--system-prompt` | Additional instructions appended after the built security prompt. They never replace the base prompt (SEC-1, shell guidance, and the project-guidance block). |
| `shell` | `string` | *(auto-detected)* | `FORGE_SHELL` | `--shell` | Host shell executable (`pwsh`, `powershell.exe`, `cmd.exe`, `bash`, `zsh`, `sh`). |
| `bash.env_allow` | `string[]` | *(none)* | - | - | User-config-only names that bash may keep even when they look like credentials. Project config cannot set this. |
| `sessions` | `object` | `{}` | - | - | Session persistence and SQLite database settings. |
| `sessions.enabled` | `boolean` | `true` | `FORGE_SESSIONS_ENABLED` | `--no-session` | Controls whether sessions are persisted to SQLite database. |
| `sessions.retention_days` | `integer` | `30` | `FORGE_SESSIONS_RETENTION_DAYS` | - | Default retention period in days before sessions are pruned. |
| `sessions.database_path` | `string` | `"~/.forge/sessions.db"` | `FORGE_SESSIONS_DATABASE_PATH` | - | File path to the SQLite sessions database (mode 0600). |
| `audit` | `object` | `{}` | - | - | Audit log configuration (user config only). |
| `audit.enabled` | `boolean` | `true` | `FORGE_AUDIT_ENABLED` | - | Enables the JSONL audit log. It is tamper-evident against edits, deletions and truncation, but not against an attacker who can rewrite both the log and its state file. |
| `audit.path` | `string` | `"~/.forge/audit/<hash>.jsonl"` | `FORGE_AUDIT_PATH` | - | Path override for workspace audit log file. |
| `audit.max_mb` | `integer` | `50` | `FORGE_AUDIT_MAX_MB` | - | Maximum audit file size in megabytes before rotation. |
| `audit.required` | `boolean` | `false` | `FORGE_AUDIT_REQUIRED` | - | If true, fails closed (denies actions) when audit logging fails. |
| `redaction` | `object` | `{}` | - | - | Sensitive credentials redaction settings (user config only). |
| `redaction.tool_output` | `boolean` | `true` | `FORGE_REDACTION_TOOL_OUTPUT` | - | Automatically masks secrets and API keys in tool outputs. |
| `providers` | `array` | `[]` | - | - | Array of named provider profiles with custom base URLs and default models. |
| `ignored_project_keys` | `object` | `{}` | - | - | Map of untrusted project configuration keys ignored due to security policy. |

---

## First launch

`forge` with no arguments opens a setup screen when a hosted provider has no saved key. You choose a provider, paste an API key, and type a model ID. Forge writes `provider` and `model` to `~/.forge/config.json`. The key goes to the OS keychain, not this file.

`/settings` repeats that setup. `/models` switches the provider and asks for a model ID. `/model <id>` changes only the model ID.

`forge run` and `forge <prompt>` skip the setup screen. They use the saved provider and model, then the key resolution order in [docs/providers.md](providers.md). Set the provider's environment variable, or run interactive setup once, before a headless command.

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
> A non-loopback `http://` base URL is refused, because prompts and API keys would be sent in cleartext. Pass `--allow-insecure-base-url` to override. Loopback addresses (`localhost`, `127.0.0.1`, `::1`) are allowed. `OPENAI_BASE_URL` is not applied to Anthropic or Gemini.

---

## Security Requirements & Untrusted Workspace Configuration (SEC-6)

> [!CAUTION]
> **Workspace repositories are untrusted.**
> An attacker who convinces a developer to clone a repository and run Forge must not achieve arbitrary code execution, credential exfiltration, or safety prompt bypasses.

To prevent repository-level attacks, Forge strictly enforces an allowlist on `<workspace>/.forge/config.json`:

### Permitted Project Configuration Keys
- `model`: Model selection for the repository.
- `theme`: UI visual theme preference.
- `max_turns` & `max_tokens`: Execution limits.
- `checkpoints`: Shadow git configuration.

The built prompt (SEC-1 untrusted-data rule, operating environment, and tool guidance) is always the base of what the model receives. A `system_prompt` set in user config, `FORGE_SYSTEM_PROMPT`, or `--system-prompt` is appended after that base as additional instructions and never replaces it. `AGENTS.md` (or `CLAUDE.md`) from the workspace root is appended in a delimited block labeled as project-provided guidance that cannot change permissions, override safety rules, or authorize any action.

### Forbidden Keys (Ignored with Visible Warning)
The following keys are **strictly forbidden** in `.forge/config.json` and will be discarded:
- `shell`: **Ignored** (arbitrary code execution vector; can only be set via user config or `--shell`).
- `bash` and `bash.env_allow`: **Ignored** (the credential allow-list is user config only).
- `base_url`: **Ignored** (credential exfiltration and SSRF vector; can only be set via user config or `--base-url`).
- `permission_mode` (any value, including `ask`, `accept-edits`, `plan`, and `yolo`): **Ignored** (a cloned repository must not grant a permission mode).
- `system_prompt`: **Ignored** (a cloned repository must not set the system prompt).
- `yolo`: **Ignored** (repository files cannot disable human confirmation prompts).
- `audit`, `sessions`, and `redaction`: **Ignored** (a cloned repository cannot turn audit off, move the session database, or disable redaction).
- Credentials (`*_api_key`, `token`, `secret`): **Ignored** (prevents committing secrets to git).

Any ignored keys are listed under `ignored_project_keys` in `forge config`, logged to `stderr` with a `SEC-6` security alert, and shown once in the TUI on the first screen.
