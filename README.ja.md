# ask-llm-mcp

[English](README.md)

単一のツール `ask_llm(prompt)` を公開する Model Context Protocol (MCP)
サーバー。プロンプトを **OpenAI API 互換**の chat-completions
エンドポイントへ転送し、応答を返します。

主眼はローカルで動作する **LM Studio** サーバーの呼び出しです。クラウド
課金やデータの外部送信なしに、ローカルモデルからセカンドオピニオンを
得られます。バックエンドが OpenAI 互換プロトコルなので、各社クラウドの
互換エンドポイントに対しても同じサーバーで動作します。

シェルアクセスのない MCP クライアント（Claude Code / Claude Desktop 等）
から、別モデルへ相談するためのチャネルとして使えます。

## クイックスタート

```sh
# ビルド
make build            # → dist/ask-llm-mcp

# 1. LM Studio を起動 → モデルをロード → ローカルサーバー開始
#    (Developer タブ → Start Server)。既定: http://localhost:1234

# 2. 設定
mkdir -p ~/.config/ask-llm-mcp
cp config.example.toml ~/.config/ask-llm-mcp/config.toml
$EDITOR ~/.config/ask-llm-mcp/config.toml   # [llm].model を設定
```

MCP クライアントに登録します。Claude Code の場合:

```sh
claude mcp add ask-llm /path/to/dist/ask-llm-mcp
```

Claude Desktop の場合は設定に追記:

```json
{
  "mcpServers": {
    "ask-llm": { "command": "/path/to/dist/ask-llm-mcp" }
  }
}
```

## 複数モデル / エンドポイント

1 インスタンス = 1 モデル（config が指すモデルに固定）です。複数の
バックエンドを提供するには、モデルごとに config を用意し、それぞれを
`--config` で別サーバーとして登録します:

```json
{
  "mcpServers": {
    "ask-qwen":  { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/qwen.toml"] },
    "ask-gemma": { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/gemma.toml"] }
  }
}
```

`--config` 省略時は既定の `~/.config/ask-llm-mcp/config.toml` を読み込みます。

## 設定

読み込み順序: 組み込みデフォルト → TOML ファイル → 環境変数。環境変数は
`ASK_LLM_*` が汎用フォールバック `OPENAI_*` より優先されます。

| キー | 必須 | 既定 | 備考 |
|------|------|------|------|
| `[llm].base_url` | いいえ | `http://localhost:1234/v1` | LM Studio 既定。`/chat/completions` を付与して叩く |
| `[llm].model` | **はい** | — | エンドポイントが提供するモデル名 |
| `[llm].api_key` | いいえ | `""` | 設定時のみ `Authorization: Bearer` を付与 |
| `[llm].request_timeout` | いいえ | `180` | リクエストごとのタイムアウト（秒） |
| `[llm].system_prompt` | いいえ | `""` | 設定時のみ system メッセージを前置 |
| `[llm].temperature` | いいえ | *(省略)* | 設定時のみリクエストに含める |
| `[llm].max_tokens` | いいえ | *(省略)* | 設定時のみリクエストに含める |
| `[log].level` | いいえ | `info` | `debug` \| `info` \| `warn` \| `error`（stderr のみ） |

環境変数: `ASK_LLM_BASE_URL` / `ASK_LLM_MODEL` / `ASK_LLM_API_KEY` /
`ASK_LLM_REQUEST_TIMEOUT` / `ASK_LLM_SYSTEM_PROMPT` /
`ASK_LLM_TEMPERATURE` / `ASK_LLM_MAX_TOKENS` / `ASK_LLM_LOG_LEVEL`。
フォールバック: `OPENAI_BASE_URL` / `OPENAI_API_KEY` / `OPENAI_MODEL`。

## ツール

```
ask_llm(prompt: string) -> text
```

ステートレス: 各呼び出しは独立です。必要な文脈はすべて prompt に含めて
ください。応答は assistant メッセージ本文です。ローカル推論モデル
（DeepSeek-R1 / Qwen QwQ 等）が本文中にインラインで出す
`<think>…</think>` / `<thinking>…</thinking>` は除去されます。別フィールド
`reasoning_content` の reasoning も返しません。

失敗時は構造化エラー `{code, message, details}`（`isError: true`）を
返します。コード: `invalid_arguments` / `upstream_error` /
`upstream_timeout` / `internal_error`。

## ビルド & テスト

```sh
make build      # → dist/ask-llm-mcp（darwin はキーチェーンに
                # Developer ID Application があれば自動 codesign）
make test       # go test ./...
make test-e2e   # ビルド後、バイナリを spawn し stdio 経由で駆動。
                # インプロセスの OpenAI 互換ダミーサーバに対して実行
                # （hermetic — ネットワーク/資格情報/LM Studio 不要）
make build-all  # 5 プラットフォームをクロスコンパイル（darwin は署名）
make package    # build-all + バージョン付き zip + darwin notarize
```

## トラブルシュート

- **`model is required`** — `[llm].model`（または `ASK_LLM_MODEL` /
  `OPENAI_MODEL`）を設定。
- **`upstream_error: … is the server running and reachable?`** —
  `base_url` への接続が拒否された。LM Studio のローカルサーバーを起動
  （または URL/ポートを確認）。
- **`upstream_timeout`** — モデルが `request_timeout` を超過。大きな
  ローカルモデルでは値を上げる。
- **反応がない / クライアントに変な出力** — ログは stderr へ出す必要が
  ある（stdout は MCP トランスポート専用）。バイナリを直接実行して
  stderr のログを確認。

## 設計

[`docs/ja/ask-llm-mcp-rfp.ja.md`](docs/ja/ask-llm-mcp-rfp.ja.md) /
[`docs/en/ask-llm-mcp-rfp.md`](docs/en/ask-llm-mcp-rfp.md) — 承認済みの
設計 RFP。スコープ判断の canonical source。

## ライセンス

MIT。[LICENSE](LICENSE) を参照。
