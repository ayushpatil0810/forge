# Local Models Guide (Ollama & LM Studio)

Forge provides native, zero-configuration support for running completely offline and locally using open-weight language models via Ollama and LM Studio.

---

## Why Local Models?

- **100% Privacy**: Your code, diffs, and prompts never leave your local machine or network.
- **Zero Cost**: No token fees or API rate limits.
- **Offline Capable**: Work on airplanes, trains, or in air-gapped secure enterprise environments.

---

## Hardware Requirements

Agentic workflows require multi-step reasoning, strict tool-calling precision, and reasonably large context windows (8k–32k tokens).

| Model Size | Recommended VRAM / Unified RAM | Recommended Models | Best For |
|---|---|---|---|
| **7B – 8B** | 8 GB – 12 GB | `qwen2.5-coder:7b`, `deepseek-r1:7b` | Laptops, quick edits, small workspaces |
| **14B** | 12 GB – 16 GB | `qwen2.5-coder:14b`, `deepseek-r1:14b` | Balanced daily driver, strong tool precision |
| **32B** | 24 GB – 32 GB | `qwen2.5-coder:32b` | Near frontier-model coding capability |
| **70B** | 48 GB – 64 GB | `llama3.3:70b` | Complex architecture, large context refactoring |

---

## Supported Runtimes

### 1. Ollama

Ollama is the simplest way to run local models on macOS, Linux, and Windows.

1. **Install and start Ollama**:
   Download from [ollama.com](https://ollama.com) and ensure the daemon is running.
2. **Pull a recommended model**:
   ```bash
   ollama pull qwen2.5-coder:7b
   ```
3. **Run Forge**:
   ```bash
   forge --provider ollama --model qwen2.5-coder:7b
   ```
   Or inside the interactive TUI:
   ```text
   /provider ollama
   /model qwen2.5-coder:7b
   ```

Forge automatically defaults the endpoint to `http://127.0.0.1:11434`.

### 2. LM Studio

LM Studio offers a visual interface for managing and running GGUF models.

1. **Download and install LM Studio** from [lmstudio.ai](https://lmstudio.ai).
2. **Download a model** (e.g., `Qwen2.5-Coder-7B-Instruct-GGUF`).
3. **Start the Local Server**:
   - Go to the **Local Server** tab (developer icon).
   - Load the model and click **Start Server**.
   - Note the server port (default is `1234`).
4. **Run Forge**:
   ```bash
   forge --provider lmstudio --model model
   ```
   Forge automatically connects to `http://127.0.0.1:1234/v1`.

---

## Runtime Handling & Quirks

Forge's LLM engine includes explicit handling for quirks common to local runtimes:

- **Cold-Start Latency**:
  When a model is first loaded into GPU memory, the first token may take 10–30 seconds. Forge displays a `"Loading model..."` status spinner instead of timing out, keeping the UI responsive.
- **Single-Chunk Tool Calls**:
  Unlike cloud providers that stream tool calls incrementally across many SSE chunks, Ollama and LM Studio often emit the entire JSON tool call in a single chunk. Forge buffers and parses these correctly.
- **Omitted Usage Metrics**:
  Many local server versions omit token usage objects in the stream. Forge gracefully handles missing usage structures without panic or failure.
- **Cost Display**:
  When using local providers, the TUI status bar displays `(local)` rather than a dollar cost, indicating that the turn incurs no monetary expense.
- **Base URL Normalization**:
  Forge accepts base URLs both with and without the `/v1` path component. Ollama configurations (`http://127.0.0.1:11434`) and LM Studio configurations (`http://127.0.0.1:1234/v1`) are automatically normalized to target `/v1/chat/completions`.

---

## Tool Calling Caveats & Model Sizing

Agentic execution requires models that reliably output syntactically valid JSON tool calls.
- **Model Capability**:
  Small (<7B) or un-tuned base models frequently fail to parse tool definitions or emit invalid arguments, causing HTTP 400 rejection errors.
- **Fallback to Plan Mode**:
  If your selected local model rejects tool calls or lacks tool-calling support, Forge detects this capability failure without entering a retry loop and suggests running in `plan` mode. In `plan` mode, the agent generates instructions and reasoning without executing local shell commands or file edits.
- **Recommended Tool-Capable Models**:
  - `qwen2.5-coder:7b` / `14b` / `32b` (exceptional tool accuracy and code synthesis)
  - `llama3.3:70b` (frontier-grade tool reasoning)

---

## Setting Context Window (`num_ctx`)

Agentic tasks with system prompts, file content insertions, and multi-turn tool outputs can easily exceed 4,000 to 8,000 tokens.

- **Ollama Default Limitation**:
  By default, Ollama instances initialize models with a restricted 2,048-token context window (`num_ctx 2048`) unless explicitly overridden. If your workspace turns exceed 2,048 tokens, Ollama will silently truncate conversation history or fail.
- **How to Increase Context Window in Ollama**:
  Create a custom `Modelfile`:
  ```text
  FROM qwen2.5-coder:7b
  PARAMETER num_ctx 32768
  ```
  Then build and run your model:
  ```bash
  ollama create qwen2.5-coder-32k -f Modelfile
  forge --provider ollama --model qwen2.5-coder-32k
  ```
- **How to Increase Context Window in LM Studio**:
  1. Open LM Studio and navigate to the **Developer / Local Server** tab.
  2. In the right-hand settings panel under **Context Length**, increase the value from the default (typically 2048 or 4096) to `16384` or `32768`.
  3. Reload the model into memory.
- Forge never guesses context limits: it warns users for local profiles when context length is server-bounded.

---

## Security & Remote Servers

- If your local model server is hosted on another machine on your local LAN (e.g., `http://192.168.1.50:11434`), pass `--base-url http://192.168.1.50:11434`.
- Because this transmits prompts and code unencrypted over the network, Forge will output a warning:
  `Warning: base URL "..." uses unencrypted HTTP on a non-loopback address. Prompts, code diffs, and credentials may be intercepted in transit.`
- For production or team setups, we recommend putting a reverse proxy with TLS (such as Caddy or Nginx) in front of remote Ollama/LM Studio servers.
