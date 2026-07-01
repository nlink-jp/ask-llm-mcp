# CLAUDE.md — ask-llm-mcp

**Organization rules (mandatory): https://github.com/nlink-jp/.github/blob/main/CONVENTIONS.md**

## Project overview

MCP stdio server that exposes a single tool `ask_llm(prompt: string)`
forwarding prompts to an OpenAI API-compatible chat-completions endpoint
and returning the response. Primary target: a local LM Studio server.
Use case: AI coding agents (Claude Code, Claude Desktop, …) consulting a
local model for a second opinion — especially in MCP clients without
shell access. Generalized from `ask-gemini-mcp` (shared MCP skeleton;
backend unified on OpenAI-compatible, no genai / GCP dependency).

## Non-negotiable rules

- **Tests are mandatory** — write them with the implementation
- **Never `go build` directly** — always `make build` (outputs to `dist/`)
- **Docs in sync** — update `README.md` and `README.ja.md` together
- **Small, typed commits** — `feat:`, `fix:`, `test:`, `chore:`, `docs:`, `refactor:`, `security:`
- **stdout is sacred** — the MCP transport runs over stdout; logs MUST go to stderr
- **Stateless** — no conversation history; every `ask_llm` call is independent

## Build & test

```sh
make build      # → dist/ask-llm-mcp (auto-codesigns on darwin if a
                # Developer ID Application identity is in the keychain)
make test       # go test ./...
make build-all  # cross-compile 5 platforms; darwin builds get codesigned
make test-e2e   # build + spawn binary + drive over stdio against an
                # in-process OpenAI-compatible dummy server (hermetic:
                # no network / credentials / LM Studio required)
make package    # build-all + zip with version suffix + notarize darwin
                # zips via NOTARY_PROFILE (default: nlink-jp-notary)
```

Both signing and notarization degrade gracefully: a missing keychain
identity / notary profile produces un-signed / un-notarized binaries with
a one-line warning.

## Configuration

Settings load order: built-in defaults → TOML file → env vars.

- **Config file**: `~/.config/ask-llm-mcp/config.toml` (or `-c` flag)
- **Env vars**: `ASK_LLM_*` (tool-specific) > `OPENAI_*` (generic fallback)

| Variable | Required | Default |
|----------|----------|---------|
| `ASK_LLM_MODEL` | Yes | — |
| `ASK_LLM_BASE_URL` | No | `http://localhost:1234/v1` |
| `ASK_LLM_API_KEY` | No | *(none)* |
| `ASK_LLM_REQUEST_TIMEOUT` | No | `180` (seconds) |
| `ASK_LLM_SYSTEM_PROMPT` | No | *(none)* |
| `ASK_LLM_TEMPERATURE` | No | *(omitted)* |
| `ASK_LLM_MAX_TOKENS` | No | *(omitted)* |
| `ASK_LLM_LOG_LEVEL` | No | `info` |

## Key dependencies

- Go stdlib `net/http` + `encoding/json` — OpenAI-compatible HTTP client
- `github.com/nlink-jp/nlk/backoff` — retry backoff
- `github.com/spf13/cobra` — CLI framework
- `github.com/BurntSushi/toml` — config file parsing

## Architecture

- `cmd/` — cobra root command (flat, no subcommand), wires stdio server
- `internal/config/` — TOML + env-var configuration (`[llm]` + `[log]`)
- `internal/jsonrpc/` — JSON-RPC 2.0 message types
- `internal/transport/` — line-delimited JSON stdio transport
- `internal/mcpserver/` — MCP protocol: initialize / tools/list / tools/call
- `internal/toolerr/` — structured `{code, message, details}` errors
- `internal/openai/` — OpenAI-compatible chat client (implements `Asker`)
- `internal/tools/` — MCP tool handlers (`ask_llm.go`)
- `e2e/` — `//go:build e2e` harness driving the binary over stdio

## Gotchas

- **stdout collisions**: anything written to stdout that is not a JSON-RPC
  message breaks the MCP client. Use `log/slog` to stderr only.
- **No retry for non-transient errors**: 4xx and context deadline/cancel
  return immediately; only 429 / 5xx / transport errors retry.
- **Reasoning stripping**: `<think>…</think>` / `<thinking>…</thinking>`
  removed from `message.content`; `reasoning_content` never read.
- **Context cancel**: closing stdin cancels the parent context and aborts
  in-flight calls (MCP 2024-11-05 has no protocol-level cancel notification).
- **One instance = one model**: model/endpoint is fixed by config; expose
  more backends by running several servers with different `-c` files.

## Design references

- [`docs/en/ask-llm-mcp-rfp.md`](docs/en/ask-llm-mcp-rfp.md) /
  [`docs/ja/ask-llm-mcp-rfp.ja.md`](docs/ja/ask-llm-mcp-rfp.ja.md)
  — approved design RFP; canonical source for scope decisions.
