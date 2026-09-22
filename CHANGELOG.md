# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **`make verify-release` now fails closed.** Its last block chained unzip, the
  packaged binary's `--version` and `spctl` with `&&` and ended the whole chain
  in `|| true`, so a zip that did not unpack or a binary that did not run exited
  0 and the upload proceeded. Each step is now judged on its own, the packaged
  binary's `--version` must contain the tag being released, and only the
  informational `spctl` line may be ignored. Matches the org template
  (CONVENTIONS.md §Code Signing → Verifying a release).
- **The Linux archives no longer carry macOS file metadata.** macOS `tar` wrote
  each bundled file's extended attributes (`com.apple.provenance`, and a Dropbox
  attribute where the tree is synced) into the `.tar.gz` twice: as AppleDouble
  `._` members, which GNU tar extracts as stray `._<name>` files beside the real
  ones, and as `LIBARCHIVE.xattr.*` / `SCHILY.xattr.*` pax headers, which it
  reports as unknown keywords. `make package` now archives with
  `COPYFILE_DISABLE=1 tar --no-xattrs`; each setting stops one of the two.
  Archives already published still carry them; the files themselves are
  unaffected.

### Internal

- `make verify-release` also judges each Linux archive: no AppleDouble or other
  macOS metadata members — listed with `--options 'tar:!mac-ext'`, because a
  plain macOS listing folds `._` members away — no extended attributes as pax
  headers, and exactly the canonical binary, `README.md` and `LICENSE`, compared
  in the C locale.
- The Linux-archive check in `make verify-release` reads each archive's pax
  headers with Python's `tarfile` instead of grepping the decompressed stream,
  which also matched file text that names the keywords (a bundled CHANGELOG,
  for one).

## [0.2.1] - 2026-09-21

### Added

- `TestEveryToolSchemaIsClosed` — the arch test organization ADR-021 §10
  requires: every registered tool's input schema must set
  `additionalProperties: false`. `ask_llm` already set it, so no schema
  changed; the test is what keeps it set and what covers the next tool.
- `tools.Registry` — one list pairing each tool descriptor with its handler.
  `cmd` now registers from it and the arch test walks it, so the two cannot
  disagree about which tools exist, and a tool added there needs no change in
  either caller.

## [0.2.0] - 2026-07-12

### Removed

- **darwin/amd64 (Intel) pre-built binary.** macOS releases now ship
  **arm64 only**, per the org-wide policy (darwin is Apple-Silicon only; no
  universal binaries). Intel Mac users can build from source.

### Changed

- **Linux release archives are now `.tar.gz`** (darwin/windows remain `.zip`),
  per `nlink-jp/.github` CONVENTIONS.md §Release Archive Standard.
- **`LICENSE` is now bundled** in every release archive alongside `README.md`.
- **darwin code-signature identifier** is now the canonical `ask-llm-mcp`.

### Fixed

- Notarization surfaces the real `notarytool` error (e.g. an expired Apple
  Developer agreement / HTTP 403) instead of a misleading "profile not found".

No change to the binary's behaviour — a packaging / build-config release.

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
