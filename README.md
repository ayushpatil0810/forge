# forge

A terminal-first AI coding agent in Go. One engine, three clients: TUI, desktop (Wails), headless.

```
cmd/forge/            headless entrypoint (TUI will join here)
apps/desktop/         Wails + React app (later)
internal/
  agent/              Session interface, events, the agent loop  <- the contract
  llm/                Provider interface + adapters (mock now; anthropic/openai/ollama next)
  tools/              Tool interface, registry, read_file
  permission/         allow / ask / deny policy
```

## Try it

    go test -race ./...
    go run ./cmd/forge --demo "what is this project?"

## Rules that keep it clean

- Frontends talk to the engine only through `agent.Session`. No UI types in `internal/agent`.
- `agent.Event` is flat and JSON-friendly so Wails generates TypeScript types from it.
- Every tool call gets exactly one result, even on cancel, so history stays valid.
- Hard `MaxTurns` cap guards against runaway tool loops.
