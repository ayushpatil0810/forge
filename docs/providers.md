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

## First launch

Run `forge` with no arguments. When no hosted provider has a saved key, the terminal opens a setup screen:

1. Choose a provider.
2. Paste an API key. Hosted keys are saved in the OS keychain. Ollama and LM Studio skip this step.
3. Type the model ID you want. There is no list to pick from. Use the ID published by your provider, then press Enter.

The choice is saved in `~/.forge/config.json` as `provider` and `model`. The key is not written to that file.

| Command | What it does |
| :--- | :--- |
| `/settings` | Run setup again: provider, API key, and model ID. |
| `/models` | Switch provider and type a model ID. Skips the key when one is already saved. |
| `/model <id>` | Switch to that model ID without leaving the chat. |
| `/provider` | Show the supported providers and which one is active. |
| `/provider <name>` | Switch provider immediately and use that provider's built-in fallback model ID. Use `/model <id>` or `/models` to type a different ID. |
| `/auth` | Show whether each provider has a saved key. Keys are masked. |

Press Esc on the first setup step to enter the chat without saving a model. The next message opens setup again.

`forge run` and `forge <prompt>` do not open this screen. They use the saved provider, model, and key. A missing key there exits with an error.

## Credential Management

Forge stores API keys in the OS keychain. The setup screen and `forge auth login` both write there. Keys are not stored in project config.

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

## Choosing a model

Type the model ID yourself. Forge does not show a list of models during setup, `/settings`, or `/models`.

Copy the ID from the provider that issued your key:

- OpenAI: [platform.openai.com/docs/models](https://platform.openai.com/docs/models)
- Anthropic: [docs.anthropic.com](https://docs.anthropic.com/en/docs/about-claude/models)
- Gemini: [ai.google.dev](https://ai.google.dev/gemini-api/docs/models/gemini)
- Ollama: the name you pulled, such as the tag printed by `ollama list`
- LM Studio: the ID shown by the local server

If the ID matches one Forge already knows, `/model <id>` checks the current conversation against that model's context window. A conversation over the window is refused, and one over 80% of the window shows a warning. Any other ID is accepted. Cost for an unknown ID is shown as `n/a`.

Until you save a choice, a headless run with no `--model` flag uses a built-in fallback: `claude-sonnet-5-5` for Anthropic, `gpt-4o` for OpenAI, `gemini-2.5-pro` for Gemini, `llama3.3` for Ollama, and `model` for LM Studio. The setup screen does not offer these as a menu.

### Local servers

| Provider | Endpoint Forge uses | API key |
| :--- | :--- | :--- |
| Ollama | `http://127.0.0.1:11434` | None |
| LM Studio | `http://127.0.0.1:1234/v1` | None |

Local turns show `(local)` in the status bar instead of a dollar cost.

---

## Security & Cleartext Warnings

When using custom endpoints (`--base-url` or `base_url` in config), Forge validates the protocol and host:
- Local loopback URLs (`http://127.0.0.1:*`, `http://localhost:*`) are permitted over cleartext HTTP.
- Non-loopback URLs over cleartext HTTP emit a prominent warning alerting to potential credential and payload interception risks.
- Project-level `.forge/config.json` files are explicitly forbidden from overriding `base_url`, `shell`, `yolo`, or credentials.
