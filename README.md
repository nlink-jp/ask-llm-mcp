# ask-llm-mcp

[日本語版](README.ja.md)

A Model Context Protocol (MCP) server that exposes a single tool,
`ask_llm(prompt)`, which forwards the prompt to an **OpenAI API-compatible**
chat-completions endpoint and returns the response.

The primary target is a locally running **LM Studio** server — get a
second opinion from a local model with no cloud billing and no data
leaving your machine. Because the backend speaks the OpenAI-compatible
protocol, the same server also works against cloud providers' compatible
endpoints.

Intended as a second-opinion channel for AI coding agents (Claude Code,
Claude Desktop, …) — especially useful in MCP clients without shell
access, where CLI tools cannot be invoked directly.

## Quick start

```sh
# Build
make build            # → dist/ask-llm-mcp

# 1. Start LM Studio, load a model, and start its local server
#    (Developer tab → Start Server). Default: http://localhost:1234

# 2. Configure
mkdir -p ~/.config/ask-llm-mcp
cp config.example.toml ~/.config/ask-llm-mcp/config.toml
$EDITOR ~/.config/ask-llm-mcp/config.toml   # set [llm].model
```

Register the server with your MCP client. For Claude Code:

```sh
claude mcp add ask-llm /path/to/dist/ask-llm-mcp
```

For Claude Desktop, add to its config:

```json
{
  "mcpServers": {
    "ask-llm": { "command": "/path/to/dist/ask-llm-mcp" }
  }
}
```

## Multiple models / endpoints

One server instance is bound to one model (whatever its config points
at). To offer several backends, create one config per model and register
each with its own `--config` file:

```json
{
  "mcpServers": {
    "ask-qwen":  { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/qwen.toml"] },
    "ask-gemma": { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/gemma.toml"] }
  }
}
```

With no `--config`, the default `~/.config/ask-llm-mcp/config.toml` is loaded.

## Configuration

Load order: built-in defaults → TOML file → env vars. Env
`ASK_LLM_*` wins over the generic `OPENAI_*` fallback.

| Key | Required | Default | Notes |
|-----|----------|---------|-------|
| `[llm].base_url` | No | `http://localhost:1234/v1` | LM Studio default; `/chat/completions` is appended |
| `[llm].model` | **Yes** | — | The model your endpoint serves |
| `[llm].api_key` | No | `""` | Sent as `Authorization: Bearer` only when set |
| `[llm].request_timeout` | No | `180` | Per-request timeout, seconds |
| `[llm].system_prompt` | No | `""` | Prepended as a system message when set |
| `[llm].temperature` | No | *(omitted)* | Included in the request only when set |
| `[llm].max_tokens` | No | *(omitted)* | Included in the request only when set |
| `[log].level` | No | `info` | `debug` \| `info` \| `warn` \| `error` (stderr only) |

Env var names: `ASK_LLM_BASE_URL`, `ASK_LLM_MODEL`, `ASK_LLM_API_KEY`,
`ASK_LLM_REQUEST_TIMEOUT`, `ASK_LLM_SYSTEM_PROMPT`, `ASK_LLM_TEMPERATURE`,
`ASK_LLM_MAX_TOKENS`, `ASK_LLM_LOG_LEVEL`. Fallbacks: `OPENAI_BASE_URL`,
`OPENAI_API_KEY`, `OPENAI_MODEL`.

## Tool

```
ask_llm(prompt: string) -> text
```

Stateless: each call is independent — include all needed context in the
prompt itself. The reply is the assistant message content. Reasoning
that local models emit inline as `<think>…</think>` / `<thinking>…</thinking>`
blocks (DeepSeek-R1, Qwen QwQ, …) is stripped; reasoning delivered in a
separate `reasoning_content` field is not surfaced.

Failures return a structured `{code, message, details}` error with
`isError: true`. Codes: `invalid_arguments`, `upstream_error`,
`upstream_timeout`, `internal_error`.

## Build & test

```sh
make build      # → dist/ask-llm-mcp (auto-codesigns on darwin if a
                # Developer ID Application identity is in the keychain)
make test       # go test ./...
make test-e2e   # build + spawn the binary and drive it over stdio
                # against an in-process OpenAI-compatible dummy server
                # (hermetic — no network / credentials / LM Studio needed)
make build-all  # cross-compile 5 platforms; darwin builds get codesigned
make package    # build-all + zip with version suffix + notarize darwin
```

## Troubleshooting

- **`model is required`** — set `[llm].model` (or `ASK_LLM_MODEL` /
  `OPENAI_MODEL`).
- **`upstream_error: … is the server running and reachable?`** — the
  endpoint at `base_url` refused the connection. Start LM Studio's local
  server (or check the URL/port).
- **`upstream_timeout`** — the model took longer than `request_timeout`.
  Raise it for large local models.
- **Nothing happens / client shows garbage** — logs must go to stderr;
  stdout is the MCP transport. Run the binary directly to see stderr logs.

## Design

See [`docs/en/ask-llm-mcp-rfp.md`](docs/en/ask-llm-mcp-rfp.md) /
[`docs/ja/ask-llm-mcp-rfp.ja.md`](docs/ja/ask-llm-mcp-rfp.ja.md) — the
approved design RFP and canonical source for scope decisions.

## License

MIT. See [LICENSE](LICENSE).
