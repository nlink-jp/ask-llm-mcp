# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-02

### Added

- Initial implementation: MCP stdio server exposing a single tool
  `ask_llm(prompt)` that forwards prompts to an OpenAI API-compatible
  chat-completions endpoint (primary target: local LM Studio) and returns
  the response.
- Configuration via `~/.config/ask-llm-mcp/config.toml` (or `-c`) with
  env-var overrides (`ASK_LLM_*` > `OPENAI_*`): `base_url`, `model`,
  `api_key`, `request_timeout`, `system_prompt`, `temperature`,
  `max_tokens`, log level. Strict TOML decode.
- OpenAI-compatible client: optional Bearer auth, per-request timeout,
  retry with exponential backoff on 429 / 5xx / transport errors, and
  structured `{code, message, details}` tool errors.
- Reasoning stripping: inline `<think>…</think>` / `<thinking>…</thinking>`
  blocks are removed from the response; out-of-band `reasoning_content`
  is not surfaced.
- Hermetic e2e harness (`//go:build e2e`) driving the binary over stdio
  against an in-process OpenAI-compatible dummy server.
- Generalized from `ask-gemini-mcp`: OpenAI-compatible backend only; the
  genai / Google Cloud dependency tree is removed.

[0.1.0]: https://github.com/nlink-jp/ask-llm-mcp/releases/tag/v0.1.0
