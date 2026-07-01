# RFP: ask-llm-mcp

> Generated: 2026-07-02
> Status: Draft

## 1. Problem Statement

AI コーディングエージェント (Claude Code / Claude Desktop 等) が単一モデルの判断のみで作業を進めると、思考の偏り・盲点・誤った確信が発生する。別モデルへ相談したいが、シェル実行権限のない MCP クライアントからは既存 CLI 群を直接呼べない。既存の `ask-gemini-mcp` はこの相談チャネルを提供するが、バックエンドが Vertex AI Gemini に固定されており、クラウド課金とデータの外部送信が前提になる。

本ツール `ask-llm-mcp` は、**OpenAI API 互換エンドポイントへ質問・相談を投げて回答を返す汎用 MCP サーバー**を提供する。主眼はローカルで動作する **LM Studio** の OpenAI 互換 API の呼び出しであり、クラウド課金・データ外部送信なしにセカンドオピニオンを得られることを最大の価値とする。バックエンドを OpenAI 互換に一本化することで、ローカル LLM だけでなく各社クラウド LLM の互換エンドポイントも同一クライアントから扱える。

利用者は、設計判断や代替案検討の局面でローカル LLM の別視点を取り入れたい AI エージェント (主に MCP クライアントとして動作する Claude Code / Desktop) およびその利用者を想定する。

## 2. Functional Specification

### Commands / API Surface

MCP ツールを 1 個のみ公開する:

```
ask_llm(prompt: string) -> text | error
```

- 単一ツール・単一引数のシンプル設計 (`ask-gemini-mcp` の透過パイプ・ステートレスモデルを踏襲)
- 用途別ツール (review / discuss / factcheck 等) には分けない (YAGNI)
- 振る舞いの分岐は MCP クライアント側がプロンプトで表現する
- バックエンド/モデルの選択は**呼び出し引数ではなく config で固定**する (後述)

CLI 起動フラグ:

```
ask-llm-mcp [-c <config-path>]
```

- `-c` / `--config`: 使用する config ファイルのパスを指定する。**省略時は既定 config (`~/.config/ask-llm-mcp/config.toml`) をロード**する。
- この 1 フラグにより、**モデル/エンドポイントの異なる config を複数用意し、それぞれを別サーバーとして MCP クライアントに登録**できる (1 インスタンス = 1 モデル)。

複数登録の例 (Claude Code / Desktop):

```jsonc
{
  "mcpServers": {
    "ask-qwen":  { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/qwen.toml"] },
    "ask-gemma": { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/gemma.toml"] },
    "ask-gpt":   { "command": "ask-llm-mcp", "args": ["-c", "~/.config/ask-llm-mcp/gpt-oss.toml"] }
  }
}
```

各サーバーは自身の config が指すモデルに固定された `ask_llm` を公開する。省略時は既定 config が使われる。

### Input / Output

**Input** (MCP tool inputSchema):

```json
{
  "type": "object",
  "properties": {
    "prompt": {
      "type": "string",
      "description": "LLMへの相談・質問内容。コンテキスト・背景・現状の自分の考えを含めて自由形式で書く。LLMはこの会話の周辺文脈を見ないので、必要な情報はすべてpromptに含める。"
    }
  },
  "required": ["prompt"],
  "additionalProperties": false
}
```

- `additionalProperties: false` + サーバー側 strict decode (`DisallowUnknownFields`) で、キーの綴り間違いは `invalid_arguments` として即エラー化する (feedback_strict_json_decode.md)。

**Wire** (upstream への送信):

```
POST {base_url}/chat/completions
Content-Type: application/json
Authorization: Bearer <api_key>   # api_key 設定時のみ

{
  "model": "<config.model>",
  "messages": [
    { "role": "system", "content": "<config.system_prompt>" },   // system_prompt 設定時のみ
    { "role": "user",   "content": "<prompt>" }
  ],
  "temperature": <config.temperature>,   // 設定時のみ含める
  "max_tokens":  <config.max_tokens>,    // 設定時のみ含める
  "stream": false
}
```

**Output**:
- 成功時: assistant メッセージ本文 (`choices[0].message.content`) を `content: [{type: "text", text: "..."}]` で返却
- ローカル推論モデル (DeepSeek-R1 / Qwen QwQ 等) が返す `<think>...</think>` ブロックや `reasoning_content` フィールドは**除去して返す** (`ask-gemini-mcp` の Thought フィルタと同じ思想)
- 失敗時: 構造化エラー `{code, message, details}` を content text に出力 (feedback_structured_mcp_tool_errors.md 準拠)

メタデータ (トークン使用量等) は返さない。シンプル原則。

### Configuration

`~/.config/ask-llm-mcp/config.toml` を既定でロードし、`-c` フラグで任意のパスに切り替える。読み込み順序: 組み込みデフォルト → TOML ファイル → 環境変数。TOML は strict decode (未知キーはエラー)。

設定項目:

| キー | 必須 | デフォルト | 備考 |
|---|---|---|---|
| `[llm].base_url` | No | `http://localhost:1234/v1` | LM Studio 既定。末尾に `/chat/completions` を付けて叩く |
| `[llm].model` | Yes | — | LM Studio でロード中のモデル名 |
| `[llm].api_key` | No | `""` | 設定時のみ `Authorization: Bearer` を付与 |
| `[llm].request_timeout` | No | `180` | 秒。ローカルの重いモデル向けに長め |
| `[llm].system_prompt` | No | `""` | 設定時のみ system メッセージを前置 |
| `[llm].temperature` | No | 未設定=省略 | 設定時のみリクエストに含める |
| `[llm].max_tokens` | No | 未設定=省略 | 設定時のみリクエストに含める |
| `[log].level` | No | `info` | stderr のみ (stdout は MCP transport 専用) |

環境変数上書き (優先度: `ASK_LLM_*` > `OPENAI_*` フォールバック):

- `ASK_LLM_BASE_URL` / `ASK_LLM_MODEL` / `ASK_LLM_API_KEY` / `ASK_LLM_REQUEST_TIMEOUT` / `ASK_LLM_SYSTEM_PROMPT` / `ASK_LLM_TEMPERATURE` / `ASK_LLM_MAX_TOKENS` / `ASK_LLM_LOG_LEVEL`
- フォールバック: `OPENAI_BASE_URL` / `OPENAI_API_KEY` / `OPENAI_MODEL` (OpenAI 互換ツールの慣例に合わせる)

### External Dependencies

- **OpenAI API 互換エンドポイント** (`/v1/chat/completions`)。主眼はローカル LM Studio。各社クラウド LLM の互換エンドポイントも利用可
- **HTTP クライアント**: Go 標準ライブラリ `net/http` + `encoding/json` のみ (SDK なし)
- **MCP プロトコル**: stdio transport のみ
- **認証**: 任意の Bearer API key (ローカル LM Studio では不要)
- 参考実装: `llm-cli` (cli-series) / `mail-analyzer-local` (util-series) / `data-analyzer` (util-series) の OpenAI 互換クライアント

## 3. Design Decisions

### 実装言語: Go

- 単一バイナリ配布の容易性 (macOS notarize 含む既存パイプラインに乗る)
- `ask-gemini-mcp` / `data-toolbox-mcp` / `mcp-guardian` の MCP サーバー骨格を流用可能
- 組織内に OpenAI 互換 Go クライアントの参考実装が複数あり (llm-cli / mail-analyzer-local / data-analyzer)

### バックエンドの OpenAI 互換一本化

- 初期構想は「Gemini + OpenAI 互換の両対応」だったが、**OpenAI 互換に一本化**して確定
- これにより `google.golang.org/genai` および Google Cloud 依存ツリーを**丸ごと削除**でき、依存は `cobra` + `BurntSushi/toml` + `nlk` のみに縮小 (組織の「外部依存最小」哲学に整合)
- クラウド (Gemini / OpenAI / OpenRouter 等) が必要なら、各社の OpenAI 互換エンドポイント経由で同一クライアントから叩ける。Vertex ネイティブ認証は捨てる

### 流用する既存資産

- **ask-gemini-mcp** (util-series): MCP サーバーの実装骨格を直接 fork。`Asker` インターフェース (`Ask(ctx, prompt) (string, error)`)、`internal/{jsonrpc,transport,mcpserver,toolerr}`、`//go:build e2e` の dummy MCP client harness、Makefile/署名パイプライン。`internal/vertexai/` を `internal/openai/` (OpenAI 互換 HTTP クライアント) に置換する
- **data-toolbox-mcp** (util-series): MCP 骨格の源流 (feedback_data_toolbox_mcp_skeleton.md)
- **llm-cli / mail-analyzer-local / data-analyzer**: OpenAI 互換 `/chat/completions` 呼び出し・reasoning 除去のパターン
- **nlk** (Go): `backoff` (429/5xx/transport リトライ)。`guard` は透過パイプ方針のため不使用

### Asker インターフェースによる差し替え

ツール層は具象クライアントではなく `Asker` インターフェースに依存する。本プロジェクトでは `*openai.Client` がこれを実装する。`ask-gemini-mcp` からの唯一の本質的差分はこの実装の差し替えであり、ツール層・MCP 骨格はほぼ無改変で再利用する。

### Out of Scope (明示的にやらないこと)

- **マルチターン会話** — MCP クライアント側が会話履歴を持つので不要 (ステートレス徹底)
- **呼び出し引数でのモデル/エンドポイント切替** — config + `-c` フラグ + 複数登録で代替。ツール面は単一固定を維持
- **ストリーミング** (`stream: true`) — MCP ツールは単一文字列を返すため不要
- **画像・VLM 入力 / tool-calling / function calling** — 単純な Q&A 中継に専念
- **RAG / 外部知識検索** — gem-rag / lite-rag に任せる
- **会話履歴・ログ永続化** — ステートレス徹底
- **HTTP/SSE トランスポート** — リモート公開は YAGNI。必要になったら stdio↔HTTP ブリッジを別途検討
- **Gemini / Anthropic ネイティブ API** — OpenAI 互換エンドポイント経由のみ
- **プロンプトインジェクション対策** — 呼び出し側 (Claude) の責任。透過パイプとして振る舞う

## 4. Development Plan

### Phase 1: Core (最小動作)

- リポジトリ scaffold (CONVENTIONS.md 準拠、`_wip/ask-llm-mcp/` 配下)
- `ask-gemini-mcp` から MCP 骨格を fork (`internal/{jsonrpc,transport,mcpserver,toolerr}`)、Go モジュール初期化、`Makefile` (build → `dist/`)
- 設定ローダー (`~/.config/ask-llm-mcp/config.toml` + `-c` フラグ + env、strict decode)
- `internal/openai/` OpenAI 互換 HTTP クライアント (`/chat/completions`、非ストリーミング、Bearer 任意、reasoning 除去)
- `ask_llm` ツール実装 (`Asker` インターフェース経由)
- 構造化エラー (`{code, message, details}`、HTTP status → toolerr コード変換)
- 単体テスト (config ローダー、エラー変換、reasoning 除去、`httptest` によるモック互換サーバー)
- README.md / README.ja.md / AGENTS.md / CHANGELOG.md 初版

この時点で機能完結し、独立にレビュー可能。

### Phase 2: Robustness (堅牢化)

- `nlk/backoff` 統合 (429/5xx/transport のリトライ)
- タイムアウト (`request_timeout`) とコンテキストキャンセル (MCP クライアント切断 = stdin close で in-flight 呼び出しを打ち切り。feedback_mcp_no_protocol_cancel.md)
- 接続拒否 (LM Studio 未起動) を分かりやすいエラーメッセージに変換
- ロギング (stderr 経由、stdio MCP transport を汚さない)
- エッジケース処理 (空 prompt、空応答、推論モデルの各種 reasoning 形式)
- E2E テスト (`//go:build e2e` dummy 互換サーバー → エラーパス・タイムアウト)

この時点で堅牢化単独でレビュー可能。

### Phase 3: Release (リリース)

- 実 LM Studio でのドッグフード (feedback_e2e_before_release.md)
- ドキュメント仕上げ (MCP クライアント設定例、複数 config 運用レシピ、トラブルシュート)
- config.example.toml、docs/{en,ja}、LICENSE 名義確認
- `v0.1.0` リリース (9-step プロセス、5 プラットフォームビルド + darwin 署名/notarize)
- util-series サブモジュール追加、`check-org.sh` 確認
- org profile (`nlink-jp/.github/profile/README.md`) と nlink-web-site (EN/JA) 更新 (feedback_catalog_sync_two_surfaces.md)

### スケジュール

Phase 1 + Phase 2 を 1 セッションで完了させる。Phase 3 (リリース) は後日。`ask-gemini-mcp` の参考実装があるため実装規模は小さい想定。

## 5. Required API Scopes / Permissions

**None** (クラウド資格情報は不要)。

- 主眼のローカル LM Studio はローカルホストへの HTTP outbound のみ。OAuth / IAM ロール / API 有効化は不要
- クラウドの OpenAI 互換エンドポイントを利用する場合の Bearer API key は、ユーザーが config (`[llm].api_key` または `OPENAI_API_KEY`) で提供する値であり、本ツールが権限を要求・管理することはない
- ファイルシステム: config.toml 読み込みのみ
- ネットワーク: 設定された `base_url` への outbound のみ
- データ永続化: なし

## 6. Series Placement

**Series: util-series**

**Reason**:
- `ask-gemini-mcp` の直系一般化であり、実装骨格を共有する
- 組織内の MCP サーバー (`ask-gemini-mcp` / `data-toolbox-mcp` / `mcp-guardian`) はすべて util-series に集約されている
- 配布形態 (Go 単一バイナリ + macOS notarize) が util-series 標準と一致
- lite-series は「Local-first LLM interaction」がスコープで主眼 (ローカル LLM) には合致するが、MCP サーバーの先例と骨格共有がなく、一貫性の観点で util-series を優先した

## 7. External Platform Constraints

### LM Studio / OpenAI 互換 API 側

- **`model` の扱い**: LM Studio はリクエストの `model` を無視してロード中モデルを返す場合がある。厳密なモデル固定はサーバー側設定 (ロード中モデル) に依存する
- **`api_key` 任意**: ローカル LM Studio は認証不要。`api_key` 未設定時は `Authorization` ヘッダを付けない
- **推論モデルの出力形式**: DeepSeek-R1 / QwQ 等は `<think>...</think>` を content 内に、または `reasoning_content` フィールドに reasoning を出す。両形式を吸収して本文のみ返す
- **互換性の揺れ**: OpenAI 互換を名乗るエンドポイント間で微妙な差異がある。`/chat/completions` の最小サブセット (model / messages / temperature / max_tokens / stream:false) のみ使用して差異を最小化する
- **レート制限**: ローカルでは基本的になし。クラウド互換エンドポイント利用時のみ 429 が発生しうるため Phase 2 の `nlk/backoff` で対応

### MCP プロトコル側

- **stdout 保護**: stdout は JSON-RPC 専用。ログはすべて `log/slog` で stderr へ (`fmt.Println` 禁止)
- **キャンセル通知なし** (MCP 2024-11-05、feedback_mcp_no_protocol_cancel.md): クライアント切断 = stdin close を唯一の中断シグナルとして `context.Context` cancel を伝播し、in-flight 呼び出しを打ち切る
- **クライアント側 inputSchema 検証** (feedback_mcp_client_validates_input_schema.md): サーバー側でも defense-in-depth として検証
- **構造化エラー** (feedback_structured_mcp_tool_errors.md): Phase 1 で対応

---

## Discussion Log

### 命名 (2026-07-02)

- 候補: `ask-llm-mcp` / `ask-local-mcp` / `llm-mcp` / `ask-any-mcp`
- 最終決定: **`ask-llm-mcp`** (`ask-gemini-mcp` → `ask-llm-mcp` の直系一般化。バックエンド非依存を命名で表現)
- MCP ツール名は `ask_llm` (`ask` / `ask_local` を却下。複数登録時に判別しやすさと汎用性のバランス)

### バックエンド範囲 (2026-07-02)

- 初期構想「Gemini + OpenAI 互換の両対応」から **OpenAI 互換一本化**に変更。genai/GCP 依存ツリーを全削除でき、外部依存が最小化される利点を重視。クラウドは互換エンドポイント経由で利用可能なため実用上の損失は小さいと判断

### ツール面とバックエンド選択 (2026-07-02)

- **単一固定ツール `ask_llm(prompt)`** を採用 (vs 呼び出し毎 model 指定 / 名前付き profile ルーティング)。`ask-gemini-mcp` の透過パイプ・ステートレスモデルを踏襲
- モデル/エンドポイント切替は**起動フラグ `-c` + 複数 config + 複数 MCP 登録**で実現する方針を追加確定。ツール面を単純に保ちつつ、モデルごとに別サーバーとして提供できる。省略時は既定 config をロード

### 生成制御パラメータ (2026-07-02)

- `system_prompt` / `temperature` / `max_tokens` を config で公開すると決定 (vs prompt のみの最小構成)。セカンドオピニオン用途で推論の幅や役割付けを調整できる利便性を優先。未設定時はリクエストに含めずサーバー/モデルの既定に委ねる

### 配置先 (2026-07-02)

- util-series で確定。`ask-gemini-mcp` の骨格共有と、組織内 MCP サーバーの集約先である点が決め手。lite-series (Local-first LLM interaction) も主眼に合致するが、骨格共有と一貫性を優先して却下
