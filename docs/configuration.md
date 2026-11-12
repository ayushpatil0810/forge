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
| `max_turns` | `integer` | `40` | `FORGE_MAX_TURNS` | `--max-turns` | Maximum number of turns in an agent turn loop before stopping. |
| `max_tokens` | `integer` | `4096` | `FORGE_MAX_TOKENS` | `--max-tokens` | Maximum token generation limit per model request. |
| `permission_mode` | `string` | `"ask"` | `FORGE_PERMISSION_MODE` | `--mode` | Default permission policy: `ask`, `accept-edits`, `plan`, or `yolo`. |
| `workspace_root` | `string` | `"."` | `FORGE_WORKSPACE_ROOT` | `--root` | Path to the workspace root directory. |
| `theme` | `string` | `"dark"` | `FORGE_THEME` | `--theme` | TUI color theme: `dark`, `light`, `catppuccin-mocha`, `tokyo-night`, or `auto`. |
| `system_prompt` | `string` | *(generated)* | `FORGE_SYSTEM_PROMPT` | `--system-prompt` | Custom system prompt override. If omitted, Forge generates one with OS and shell context. |
| `shell` | `string` | *(auto-detected)* | `FORGE_SHELL` | `--shell` | Host shell executable (`pwsh`, `powershell.exe`, `cmd.exe`, `bash`, `zsh`, `sh`). |

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

## Security Requirements (SEC-6)

> [!CAUTION]
> **API Keys must NEVER be placed in project configuration files (`<workspace>/.forge/config.json`).**
> Project configurations are frequently checked into version control repositories. Committing API keys or secrets compromises credentials.

Forge enforces SEC-6 by:
1. Storing and reading API keys exclusively from environment variables:
   - Anthropic: `ANTHROPIC_API_KEY`
   - OpenAI: `OPENAI_API_KEY`
   - Gemini / Google: `GEMINI_API_KEY` or `GOOGLE_API_KEY`
2. Emitting an explicit security warning whenever an API key is detected in a project-level configuration file.
3. Redacting all secrets in error messages, logs, TUI cards, and `forge config` outputs.
