# RFP: ask-llm-mcp

> Generated: 2026-07-02
> Status: Draft

## 1. Problem Statement

When an AI coding agent (Claude Code / Claude Desktop, etc.) proceeds on the judgment of a single model, it is prone to bias, blind spots, and false confidence. Consulting a different model helps, but MCP clients without shell access cannot invoke the existing CLI tools directly. The existing `ask-gemini-mcp` provides such a consultation channel, but its backend is fixed to Vertex AI Gemini, which entails cloud billing and sending data off-machine.

`ask-llm-mcp` provides a **generic MCP server that forwards a prompt to an OpenAI API-compatible endpoint and returns the response**. The primary target is a locally running **LM Studio** OpenAI-compatible API — its greatest value is obtaining a second opinion with no cloud billing and no data leaving the machine. Unifying the backend on the OpenAI-compatible protocol means the same client can also reach the compatible endpoints of cloud LLM providers, not just local models.

The intended users are AI agents (primarily Claude Code / Desktop acting as MCP clients) — and their operators — who want a local LLM's alternative perspective during design decisions and when weighing alternatives.

## 2. Functional Specification

### Commands / API Surface

Exactly one MCP tool is exposed:

```
ask_llm(prompt: string) -> text | error
```

- Single tool, single argument — mirrors `ask-gemini-mcp`'s stateless transparent-pipe model
- No use-case-specific tools (review / discuss / factcheck, etc.) — YAGNI
- Behavioral branching is expressed by the MCP client through the prompt
- Backend / model selection is fixed **in config, not via call arguments** (see below)

CLI startup flags:

```
ask-llm-mcp [-c <config-path>]
```

- `-c` / `--config`: path to the config file to use. **When omitted, the default config (`~/.config/ask-llm-mcp/config.toml`) is loaded.**
- With this single flag, you can prepare multiple configs pointing at different models/endpoints and **register each as a separate server with the MCP client** (one instance = one model).

Example of registering several (Claude Code / Desktop):

```jsonc
{
  "mcpServers": {
    "ask-qwen":  { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/qwen.toml"] },
    "ask-gemma": { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/gemma.toml"] },
    "ask-gpt":   { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/gpt-oss.toml"] }
  }
}
```

Each server exposes an `ask_llm` fixed to the model its config points at. When omitted, the default config is used.

### Input / Output

**Input** (MCP tool inputSchema):

```json
{
  "type": "object",
  "properties": {
    "prompt": {
      "type": "string",
      "description": "The question or consultation to send to the LLM. Free-form; include context, background, and your current thinking. The LLM does not see the surrounding conversation, so include everything it needs in the prompt."
    }
  },
  "required": ["prompt"],
  "additionalProperties": false
}
```

- `additionalProperties: false` plus server-side strict decode (`DisallowUnknownFields`) surfaces a misspelled key as an `invalid_arguments` error rather than silently ignoring it (feedback_strict_json_decode.md).

**Wire** (sent upstream):

```
POST {base_url}/chat/completions
Content-Type: application/json
Authorization: Bearer <api_key>   # only when api_key is set

{
  "model": "<config.model>",
  "messages": [
    { "role": "system", "content": "<config.system_prompt>" },   // only when set
    { "role": "user",   "content": "<prompt>" }
  ],
  "temperature": <config.temperature>,   // included only when set
  "max_tokens":  <config.max_tokens>,    // included only when set
  "stream": false
}
```

**Output**:
- Success: the assistant message body (`choices[0].message.content`) returned as `content: [{type: "text", text: "..."}]`
- `<think>...</think>` blocks and the `reasoning_content` field emitted by local reasoning models (DeepSeek-R1 / Qwen QwQ, etc.) are **stripped** before returning (same philosophy as `ask-gemini-mcp`'s Thought filter)
- Failure: a structured error `{code, message, details}` written to the content text (per feedback_structured_mcp_tool_errors.md)

No metadata (token usage, etc.) is returned. Simplicity first.

### Configuration

Loads `~/.config/ask-llm-mcp/config.toml` by default; the `-c` flag switches to an arbitrary path. Load order: built-in defaults → TOML file → environment variables. TOML uses strict decode (unknown keys are errors).

| Key | Required | Default | Notes |
|---|---|---|---|
| `[llm].base_url` | No | `http://localhost:1234/v1` | LM Studio default; `/chat/completions` is appended |
| `[llm].model` | Yes | — | Model name loaded in LM Studio |
| `[llm].api_key` | No | `""` | `Authorization: Bearer` added only when set |
| `[llm].request_timeout` | No | `180` | Seconds; long, for heavy local models |
| `[llm].system_prompt` | No | `""` | System message prepended only when set |
| `[llm].temperature` | No | unset = omitted | Included in request only when set |
| `[llm].max_tokens` | No | unset = omitted | Included in request only when set |
| `[log].level` | No | `info` | stderr only (stdout is reserved for the MCP transport) |

Environment overrides (precedence: `ASK_LLM_*` > `OPENAI_*` fallback):

- `ASK_LLM_BASE_URL` / `ASK_LLM_MODEL` / `ASK_LLM_API_KEY` / `ASK_LLM_REQUEST_TIMEOUT` / `ASK_LLM_SYSTEM_PROMPT` / `ASK_LLM_TEMPERATURE` / `ASK_LLM_MAX_TOKENS` / `ASK_LLM_LOG_LEVEL`
- Fallback: `OPENAI_BASE_URL` / `OPENAI_API_KEY` / `OPENAI_MODEL` (matching the convention of OpenAI-compatible tooling)

### External Dependencies

- **OpenAI API-compatible endpoint** (`/v1/chat/completions`). Primary target: local LM Studio. Cloud LLM compatible endpoints also usable
- **HTTP client**: Go stdlib `net/http` + `encoding/json` only (no SDK)
- **MCP protocol**: stdio transport only
- **Auth**: optional Bearer API key (not needed for local LM Studio)
- Reference implementations: the OpenAI-compatible clients in `llm-cli` (cli-series) / `mail-analyzer-local` (util-series) / `data-analyzer` (util-series)

## 3. Design Decisions

### Language: Go

- Easy single-binary distribution (rides the existing macOS notarize pipeline)
- Reuses the MCP server skeleton of `ask-gemini-mcp` / `data-toolbox-mcp` / `mcp-guardian`
- Multiple in-org reference implementations of an OpenAI-compatible Go client exist (llm-cli / mail-analyzer-local / data-analyzer)

### Unify the backend on OpenAI compatibility

- The initial concept was "both Gemini and OpenAI-compatible"; finalized as **OpenAI-compatible only**
- This lets us **drop the entire `google.golang.org/genai` and Google Cloud dependency tree**, shrinking dependencies to just `cobra` + `BurntSushi/toml` + `nlk` (consistent with the org's minimal-dependency philosophy)
- Cloud providers (Gemini / OpenAI / OpenRouter, etc.) remain reachable via their OpenAI-compatible endpoints from the same client; Vertex native auth is dropped

### Reused existing assets

- **ask-gemini-mcp** (util-series): fork the MCP server skeleton directly — the `Asker` interface (`Ask(ctx, prompt) (string, error)`), `internal/{jsonrpc,transport,mcpserver,toolerr}`, the `//go:build e2e` dummy MCP client harness, and the Makefile/signing pipeline. Replace `internal/vertexai/` with `internal/openai/` (an OpenAI-compatible HTTP client)
- **data-toolbox-mcp** (util-series): origin of the MCP skeleton (feedback_data_toolbox_mcp_skeleton.md)
- **llm-cli / mail-analyzer-local / data-analyzer**: patterns for OpenAI-compatible `/chat/completions` calls and reasoning stripping
- **nlk** (Go): `backoff` (retry on 429/5xx/transport). `guard` is not used — transparent-pipe policy

### Swap via the Asker interface

The tool layer depends on the `Asker` interface, not a concrete client. Here `*openai.Client` implements it. The only essential difference from `ask-gemini-mcp` is swapping this implementation; the tool layer and MCP skeleton are reused almost unchanged.

### Out of Scope (explicitly excluded)

- **Multi-turn conversation** — the MCP client holds history; unnecessary (strictly stateless)
- **Model/endpoint switching via call arguments** — replaced by config + `-c` flag + multiple registrations. The tool surface stays a single fixed tool
- **Streaming** (`stream: true`) — the MCP tool returns a single string; unnecessary
- **Image / VLM input, tool-calling / function calling** — focus on plain Q&A relay
- **RAG / external knowledge retrieval** — delegated to gem-rag / lite-rag
- **Conversation history / log persistence** — strictly stateless
- **HTTP/SSE transport** — remote exposure is YAGNI; consider a stdio↔HTTP bridge later if needed
- **Gemini / Anthropic native APIs** — only via OpenAI-compatible endpoints
- **Prompt-injection defense** — the caller's (Claude's) responsibility; behaves as a transparent pipe

## 4. Development Plan

### Phase 1: Core (minimal working)

- Repo scaffold (per CONVENTIONS.md, under `_wip/ask-llm-mcp/`)
- Fork the MCP skeleton from `ask-gemini-mcp` (`internal/{jsonrpc,transport,mcpserver,toolerr}`), init the Go module, `Makefile` (build → `dist/`)
- Config loader (`~/.config/ask-llm-mcp/config.toml` + `-c` flag + env, strict decode)
- `internal/openai/` OpenAI-compatible HTTP client (`/chat/completions`, non-streaming, optional Bearer, reasoning stripping)
- `ask_llm` tool implementation (via the `Asker` interface)
- Structured errors (`{code, message, details}`, HTTP status → toolerr code mapping)
- Unit tests (config loader, error mapping, reasoning stripping, a mock compatible server via `httptest`)
- Initial README.md / README.ja.md / AGENTS.md / CHANGELOG.md

Feature-complete and independently reviewable at this point.

### Phase 2: Robustness

- Integrate `nlk/backoff` (retry on 429/5xx/transport)
- Timeout (`request_timeout`) and context cancellation (MCP client disconnect = stdin close aborts in-flight calls; feedback_mcp_no_protocol_cancel.md)
- Convert connection-refused (LM Studio not running) into a clear error message
- Logging (via stderr; never pollute the stdio MCP transport)
- Edge cases (empty prompt, empty response, the various reasoning formats of reasoning models)
- E2E tests (`//go:build e2e` dummy compatible server → error paths, timeout)

Robustness is independently reviewable at this point.

### Phase 3: Release

- Dogfood against real LM Studio (feedback_e2e_before_release.md)
- Documentation polish (MCP client setup examples, multi-config recipe, troubleshooting)
- config.example.toml, docs/{en,ja}, LICENSE attribution check
- `v0.1.0` release (9-step process, 5-platform build + darwin signing/notarize)
- Add util-series submodule, verify with `check-org.sh`
- Update org profile (`nlink-jp/.github/profile/README.md`) and nlink-web-site (EN/JA) (feedback_catalog_sync_two_surfaces.md)

### Schedule

Complete Phase 1 + Phase 2 in one session. Phase 3 (release) later. Implementation is expected to be small given the `ask-gemini-mcp` reference.

## 5. Required API Scopes / Permissions

**None** (no cloud credentials required).

- The primary local LM Studio target only makes HTTP outbound to localhost. No OAuth / IAM roles / API enablement needed
- A Bearer API key for a cloud OpenAI-compatible endpoint is a value the user supplies via config (`[llm].api_key` or `OPENAI_API_KEY`); the tool neither requests nor manages any permission
- Filesystem: config.toml read only
- Network: outbound to the configured `base_url` only
- Data persistence: none

## 6. Series Placement

**Series: util-series**

**Reason**:
- Direct generalization of `ask-gemini-mcp`, sharing its implementation skeleton
- All in-org MCP servers (`ask-gemini-mcp` / `data-toolbox-mcp` / `mcp-guardian`) are consolidated in util-series
- Distribution form (Go single binary + macOS notarize) matches the util-series standard
- lite-series ("Local-first LLM interaction") fits the primary target (local LLM) on that axis, but has no MCP-server precedent or shared skeleton; util-series is preferred for consistency

## 7. External Platform Constraints

### LM Studio / OpenAI-compatible API side

- **Handling of `model`**: LM Studio may ignore the request `model` and return the currently loaded model. Strict model pinning depends on the server-side (loaded model)
- **`api_key` optional**: local LM Studio needs no auth. When `api_key` is unset, no `Authorization` header is sent
- **Reasoning-model output**: DeepSeek-R1 / QwQ, etc. emit reasoning either inside `<think>...</think>` in content or in a `reasoning_content` field. Both forms are absorbed and only the body is returned
- **Compatibility drift**: OpenAI-compatible endpoints vary subtly. Use only the minimal `/chat/completions` subset (model / messages / temperature / max_tokens / stream:false) to minimize drift
- **Rate limits**: essentially none locally. Only cloud-compatible endpoints may return 429, handled by `nlk/backoff` in Phase 2

### MCP protocol side

- **Protect stdout**: stdout is JSON-RPC only. All logs go to stderr via `log/slog` (`fmt.Println` banned)
- **No cancel notification** (MCP 2024-11-05, feedback_mcp_no_protocol_cancel.md): client disconnect = stdin close is the only interrupt signal; propagate `context.Context` cancel to abort in-flight calls
- **Client-side inputSchema validation** (feedback_mcp_client_validates_input_schema.md): also validated server-side as defense-in-depth
- **Structured errors** (feedback_structured_mcp_tool_errors.md): addressed in Phase 1

---

## Discussion Log

### Naming (2026-07-02)

- Candidates: `ask-llm-mcp` / `ask-local-mcp` / `llm-mcp` / `ask-any-mcp`
- Final: **`ask-llm-mcp`** (a direct generalization of `ask-gemini-mcp`; backend-agnostic expressed in the name)
- MCP tool name is `ask_llm` (`ask` / `ask_local` rejected — balances distinguishability across multiple registrations with generality)

### Backend scope (2026-07-02)

- Changed from the initial "both Gemini and OpenAI-compatible" to **OpenAI-compatible only**. Prioritized the benefit of dropping the entire genai/GCP dependency tree and minimizing dependencies. Judged the practical loss small since cloud is reachable via compatible endpoints

### Tool surface & backend selection (2026-07-02)

- Adopted a **single fixed tool `ask_llm(prompt)`** (vs per-call model / named-profile routing). Mirrors `ask-gemini-mcp`'s transparent-pipe, stateless model
- Additionally decided to realize model/endpoint switching via the **`-c` startup flag + multiple configs + multiple MCP registrations**. Keeps the tool surface simple while offering each model as a separate server. Omitting `-c` loads the default config

### Generation-control parameters (2026-07-02)

- Decided to expose `system_prompt` / `temperature` / `max_tokens` in config (vs prompt-only minimal). Prioritized the convenience of tuning reasoning breadth and role assignment for the second-opinion use case. When unset, they are omitted from the request, deferring to server/model defaults

### Placement (2026-07-02)

- Finalized as util-series. Decisive factors: shared skeleton with `ask-gemini-mcp` and being the consolidation point for in-org MCP servers. lite-series (Local-first LLM interaction) also fits the primary target, but skeleton sharing and consistency won out
