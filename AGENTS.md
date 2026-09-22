# AGENTS.md — ask-llm-mcp

## Project summary

MCP stdio server exposing a single tool `ask_llm(prompt: string)` that
forwards prompts to an **OpenAI API-compatible** chat-completions
endpoint and returns the response. Primary target: a local LM Studio
server. Use case: AI coding agents (Claude Code / Desktop) consulting a
local model for a second opinion, especially in MCP clients without
shell access.

Generalized from `ask-gemini-mcp`: the MCP skeleton is shared; the
backend is unified on the OpenAI-compatible protocol, so there is **no
genai / Google Cloud dependency**. Dependencies: `cobra`, `BurntSushi/toml`,
`nlink-jp/nlk` (backoff).

## Build & test

```sh
make build      # → dist/ask-llm-mcp (auto-codesign on darwin)
make test       # go test ./...
make test-e2e   # hermetic e2e: spawn binary + drive over stdio against
                # an in-process OpenAI-compatible dummy server
make build-all  # 5 platforms; darwin codesigned
make package    # build-all + zip + notarize darwin (NOTARY_PROFILE)
make verify-release  # gate: notarized, fresh, runs at this version, clean linux archives (run before upload)
```

Never `go build` directly — always `make build` (outputs to `dist/`).

## Structure

- `cmd/` — cobra root command (flat, no subcommand); wires the stdio server
- `internal/config/` — TOML + env-var config (`[llm]` + `[log]`); strict decode
- `internal/jsonrpc/` — JSON-RPC 2.0 message types
- `internal/transport/` — line-delimited JSON stdio transport
- `internal/mcpserver/` — MCP protocol: initialize / tools/list / tools/call
- `internal/toolerr/` — structured `{code, message, details}` errors
- `internal/openai/` — OpenAI-compatible HTTP client (implements `tools.Asker`)
- `internal/tools/` — MCP tool handlers (`ask_llm.go`)
- `e2e/` — `//go:build e2e` harness (dummy upstream server; hermetic)

The tool layer depends on the `Asker` interface (`Ask(ctx, prompt)
(string, error)`), so swapping backends means swapping the `internal/openai`
implementation only.

## Configuration

Order: defaults → TOML file → env. Default path
`~/.config/ask-llm-mcp/config.toml` (or `-c`). Env `ASK_LLM_*` >
`OPENAI_*` fallback. `[llm].model` is required. `temperature`/`max_tokens`
are pointers (nil = omitted from the request).

## Gotchas

- **stdout is sacred**: it carries JSON-RPC. All logs go to stderr via
  `log/slog`; `fmt.Println` is banned. Any stray stdout write breaks the
  MCP client.
- **No retry for non-transient errors**: 4xx (auth/schema) and context
  deadline/cancel return immediately; only 429 / 5xx / transport errors
  retry (via `nlk/backoff`).
- **Reasoning stripping**: `<think>…</think>` / `<thinking>…</thinking>`
  blocks are removed from `message.content`; `reasoning_content` is never
  read.
- **Context cancel**: closing stdin cancels the parent context and aborts
  in-flight calls (MCP 2024-11-05 has no protocol-level cancel).
- **e2e retry cost**: the retry backoff is 2s–30s in production; tests set
  `Client.backoffBase/backoffMax` to 1ms to stay fast.
- **Every tool schema is closed** (`additionalProperties: false`, organization
  ADR-021 §10), and both halves of the contract are real: the schema stops a
  mistyped argument at a validating client, the handler's
  `DisallowUnknownFields` stops it at the server. `ask_llm` already set the
  key; `TestEveryToolSchemaIsClosed` is what keeps it set and what covers the
  next tool.
- **`tools.Registry` is the single registration list.** `cmd` registers from
  it and the arch test walks it, so a tool added there is registered and
  asserted over without touching either caller. Do not go back to naming a
  tool constructor directly in `cmd` — a second list is a list that drifts,
  and the arch test would then be asserting over a set the server does not
  serve.
- **No `make check` / `make lint` target** — only `make test`. Nothing in this
  repo gates formatting or vet, so drift accumulates unnoticed. Run
  `go vet ./...` and `gofmt -l .` by hand before a release.

## Design references

- [`docs/en/ask-llm-mcp-rfp.md`](docs/en/ask-llm-mcp-rfp.md) /
  [`docs/ja/ask-llm-mcp-rfp.ja.md`](docs/ja/ask-llm-mcp-rfp.ja.md) —
  approved design RFP; canonical source for scope decisions.
