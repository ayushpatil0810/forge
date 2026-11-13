# LLM Providers Guide

Forge supports both hosted cloud providers (OpenAI, Anthropic, Google Gemini) and local, open-weight model runtimes (Ollama, LM Studio).

---

## Verification Status

| Provider | Verification Status | Notes |
|---|---|---|
| **OpenAI** | **Live-Verified** | Fully tested with real network calls (chat, tools, bash fix loop). |
| **Anthropic** | **Experimental** | Fully implemented and contract-tested; not live-verified against live API. |
| **Google Gemini** | **Experimental** | Fully implemented and contract-tested; not live-verified against live API. |
| **Ollama** | **Contract-Tested** | Local runtime via loopback; contract-tested with single-chunk tool calls and omitted usage handling. |
| **LM Studio** | **Contract-Tested** | Local runtime via loopback; contract-tested with OpenAI-compatible endpoint. |

---

## Credential Management

Forge provides a secure, unified credentials manager (`internal/credentials`) that securely stores and resolves API keys across providers without plain-text exposure in version control.

### Resolution Precedence

When connecting to a provider, Forge resolves the API key in the following strict order:

1. **Environment Variable**: Provider-specific environment variable:
   - `OPENAI_API_KEY`
   - `ANTHROPIC_API_KEY`
   - `GEMINI_API_KEY` (or `GOOGLE_API_KEY`)
2. **OS Keychain**: Secure credential store using the OS keyring (`zalando/go-keyring`):
   - Windows Credential Manager
   - macOS Keychain
   - Linux Secret Service (via D-Bus)
3. **Consented File (Linux Only)**: If the OS keyring is unavailable (such as headless Linux environments or CI containers), keys can be stored in `~/.forge/credentials.json` with strict POSIX mode `0600` permissions. This fallback strictly requires explicit consent (`--allow-plaintext-key-file` or interactive `[y/N]` prompt) with an on-screen disclosure that keys are stored unencrypted. It is never supported or created on Windows or macOS.
4. **None**: For local servers (`ollama`, `lmstudio`), no key is required.

> **Security Note:** Passing API keys via command-line flags (`--api-key`, `--openai-api-key`, etc.) is completely disabled to prevent credential leakage into process tables and shell history. Any flag containing `key` carrying a secret exits immediately with code 2 and a hint to use `forge auth login`.

### OS Keychain Implementation Facts & Limitations

- **macOS (`zalando/go-keyring`)**: On macOS, `go-keyring` invokes `/usr/bin/security add-generic-password -s <service> -a <user> -w <secret>`. Because the secret is passed via the command-line argument `-w <secret>`, it is briefly visible in the OS process table (`ps aux` / `sysctl`) to other processes running under the same user ID during the invocation window. This is a known upstream limitation of `go-keyring`.
- **Windows Credential Manager**: Windows Credential Manager (`CredWrite`) enforces a maximum credential blob size of 2,512 bytes (`CRED_MAX_CREDENTIAL_BLOB_SIZE`, historically 512 bytes). Standard API keys (typically 40–160 bytes) fit with ample headroom, but very large custom tokens or certificates exceeding 2,512 bytes will fail with Win32 error 1783 (`ERROR_INVALID_PARAMETER`).

### CLI Auth Commands

Manage stored credentials directly from the command line:

```bash
# Check status of all provider credentials (keys are safely masked)
forge auth status

# Interactive login to store a provider key into the OS Keychain
forge auth login openai
forge auth login anthropic
forge auth login gemini

# Login reading key directly from standard input (useful for scripts)
cat api_key.txt | forge auth login openai --with-key

# Remove a stored key from the keychain/file
forge auth logout openai
```

During `forge auth login`, Forge executes a non-destructive connection probe against the provider's API endpoint to verify the key before saving it.

---

## Provider Catalogs & Defaults

### 1. OpenAI (Live-Verified)
- **Default Model**: `gpt-4o`
- **Catalog**:
  - `gpt-4o` (128k context, $2.50 / $10.00 per M tokens)
  - `gpt-4o-mini` (128k context, $0.15 / $0.60 per M tokens)
  - `o1` (200k context, $15.00 / $60.00 per M tokens)
  - `o3-mini` (200k context, $1.10 / $4.40 per M tokens)
- **Documentation**: [OpenAI Model Documentation](https://platform.openai.com/docs/models)

### 2. Anthropic (Experimental)
- **Default Model**: `claude-sonnet-5-5`
- **Catalog**:
  - `claude-sonnet-5-5` (1M context, $2.00 / $10.00 per M tokens)
  - `claude-opus-5-5` (1M context, $4.00 / $20.00 per M tokens)
  - `claude-haiku-5-5` (1M context, $0.10 / $0.50 per M tokens)
  - `claude-fable-5-2` (1M context, $8.00 / $40.00 per M tokens)
- **Documentation**: [Anthropic Models Documentation](https://docs.anthropic.com/en/docs/about-claude/models)

### 3. Google Gemini (Experimental)
- **Default Model**: `gemini-2.5-pro`
- **Catalog**:
  - `gemini-2.5-pro` (1M context, $1.25 / $10.00 per M tokens)
  - `gemini-2.5-flash` (1M context, $0.30 / $2.50 per M tokens)
- **Documentation**: [Google AI Gemini Documentation](https://ai.google.dev/gemini-api/docs/models/gemini)

### 4. Ollama (Local)
- **Default Endpoint**: `http://127.0.0.1:11434`
- **Default Model**: `llama3.3`
- **Suggested Models**: `qwen2.5-coder:7b`, `deepseek-r1:7b`
- **Cost**: Free ($0.00), marked as `(local)` in TUI status bar.
- **Key**: None needed.

### 5. LM Studio (Local)
- **Default Endpoint**: `http://127.0.0.1:1234/v1`
- **Default Model**: `model`
- **Suggested Models**: `qwen2.5-coder`, `deepseek-coder`
- **Cost**: Free ($0.00), marked as `(local)` in TUI status bar.
- **Key**: None needed.

---

## Dynamic Switching in the TUI

Inside interactive mode, you can switch providers and models without restarting Forge:

- `/provider`: List supported providers and see the active one.
- `/provider <name>`: Switch active provider (e.g. `/provider ollama`, `/provider openai`). Automatically selects the default model for that provider.
- `/model`: View the active model, context window, and pricing.
- `/model <id>`: Switch model (e.g. `/model gpt-4o-mini`). Forge validates conversation token size against the model's context window, alerting if tokens exceed 80% and blocking the switch if tokens exceed 100%. Arbitrary custom models are accepted with cost indicated as `n/a`.
- `/auth`: Check keychain and authentication status for each provider.

---

## Security & Cleartext Warnings

When using custom endpoints (`--base-url` or `base_url` in config), Forge validates the protocol and host:
- Local loopback URLs (`http://127.0.0.1:*`, `http://localhost:*`) are permitted over cleartext HTTP.
- Non-loopback URLs over cleartext HTTP emit a prominent warning alerting to potential credential and payload interception risks.
- Project-level `.forge/config.json` files are explicitly forbidden from overriding `base_url`, `shell`, `yolo`, or credentials.
