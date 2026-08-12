# Load Balance Provider

[繁體中文](README.md) | [English](README.en.md) | **日本語** | [한국어](README.ko.md)

`LoadBalanceProvider` は MarsCloud SDK で構築された LLM プロキシサービスです。OpenAI 互換の Chat Completions API と Responses API を提供し、リクエストサイズ、処理負荷、タスク特性、Provider のリアルタイム負荷に基づいて、適切なバックエンド LLM Provider を選択します。

## 目的と機能

- **OpenAI 互換エンドポイント**：`POST /v1/chat/completions` と `POST /v1/responses` をサポートし、既存の OpenAI SDK や互換 Client から接続できます。
- **長時間タスク向け Prompt Cache アフィニティ**：`previous_response_id` または `prompt_cache_key` を使って同じ Responses 会話を元の Provider/Model に戻し、複数ターンのツール呼び出しや暗号化された reasoning 内容がアカウント切り替えによって失効することを防ぎます。
- **複数 Provider の管理**：`data/llm_proxy.json` で複数の OpenAI-compatible Provider、モデル能力、重み、コスト、同時実行上限を登録します。
- **インテリジェントルーティング**：入力 token の推定値、出力負荷、メッセージ数、タスク種別、モデル能力を使用してスコアリングします。
- **通常応答とストリーミング**：通常の HTTP response を転送し、`stream=true` の場合は SSE/Chunked ストリーミングを維持します。
- **ロードバランシング**：Provider の重み、モデル品質、コスト、active request、max concurrent に基づいて汎用的に評価します。
- **標準 MCP**：MCP `2025-11-25` Streamable HTTP エンドポイントを提供し、キー管理以外の照会と操作をツールとして公開します。

## ディレクトリ構成

- `src/cmd/loadbalanceprovider/main.go`：MarsService、HTTP API、LLM Proxy コンポーネントを初期化するサービスエントリポイント。
- `src/service/cloud_service.go`：サービスのライフサイクルとバックグラウンドの Provider 状態記録。
- `src/api/http_api.go`：`/v1/chat/completions`、`/v1/responses`、`/api/health`、`/api/providers` を含む REST API ルート。
- `src/api/mcp.go`：標準 MCP Streamable HTTP、JSON-RPC ライフサイクル、ツールカタログ、既存 API アダプター。
- `src/domain/types.go`：Chat Completion、Provider、Model、エラー形式などの共通型。
- `src/config/provider_config.go`：`data/llm_proxy.json` から LLM Proxy 設定を読み込み、既定値を適用します。
- `src/analyzer/request_analyzer.go`：リクエストサイズ、出力負荷、タスク種別、複雑度を推定します。
- `src/balancer/load_balancer.go`：Provider/Model 候補のフィルタリング、スコアリング、リアルタイム負荷の追跡。
- `src/proxy/client.go`：OpenAI-compatible Chat Completion の HTTP 転送とストリーミングプロキシ。
- `agent.properties`：MarsCloud サービス設定。
- `data/llm_proxy.json`：LLM Provider、モデル能力、ロードバランシング設定。

## API

### Chat Completions

```http
POST /v1/chat/completions
Content-Type: application/json
Authorization: Bearer <client-token>
```

リクエスト形式は OpenAI Chat Completions と互換です。サービスは `model`、`messages`、`stream`、`max_tokens` または `max_completion_tokens` を解析し、バックエンドモデルを選択して転送します。

```json
{
  "model": "auto",
  "messages": [
    {"role": "user", "content": "高可用性アーキテクチャを設計してください。"}
  ],
  "stream": false
}
```

### Responses API

```http
POST /v1/responses
Content-Type: application/json
Authorization: Bearer <client-token>
```

リクエスト形式は OpenAI Responses API と互換です。通常応答と `stream=true` の SSE ストリーミングに対応します。`model` には実際のモデル名または `AUTO` を指定でき、Provider の能力とリアルタイム負荷に応じてバックエンドを選択します。

```json
{
  "model": "AUTO",
  "input": "このプロジェクトを分析し、リファクタリング計画を提案してください。",
  "stream": true,
  "prompt_cache_key": "project-refactor-2026"
}
```

Responses API は、既存 response の取得、削除、キャンセル、入力項目の取得、input token の計算などの互換サブルートにも対応します。リクエストは response を作成した Provider に転送され、ルーティング情報は呼び出し元の API Key ごとに分離されます。

#### 長時間タスク向け Prompt Cache アフィニティ

Responses の長時間タスクには、複数ターンの推論、ツール呼び出し、暗号化された reasoning 内容が含まれることがあります。途中で Provider またはアカウントが切り替わると、上流サービスが以前の状態を認識できない場合があります。本サービスは会話アフィニティを自動的に維持します。

1. response の作成に成功すると、response ID、`prompt_cache_key`、Provider、Model、呼び出し元 ID の対応を記録します。
2. 後続リクエストに `previous_response_id` がある場合、元の Provider/Model に優先的に戻します。
3. Codex などの Client が `previous_response_id` を送信せず、同じ `prompt_cache_key` を継続利用する場合でも、元の Provider/Model に戻します。長時間の Agent タスクや複数ターンのツール呼び出しに適しています。
4. アフィニティ情報は API Key ごとに分離され、別の呼び出し元の response または Prompt Cache ルートを共有できません。
5. Provider が利用不能、割り当て量が他 Provider より大幅に低い、アフィニティの期限切れ、またはルートが削除された場合、Provider 間で移行できない `previous_response_id` と暗号化 reasoning を除去してから再度ロードバランシングし、タスク全体の失敗を回避します。

管理画面の **設定 > 詳細設定** では、次の値を調整できます。

- **会話アフィニティ TTL**：既定値は `30` 分、設定範囲は `1`～`10080` 分です。長時間タスクでは、想定される最大ステップ間隔より長く設定してください。
- **アフィニティ割り当て許容値**：既定値は `10` パーセントポイントです。元の Provider の割り当て量が他 Provider の平均をこの値より大きく下回る場合、アフィニティを解除できます。
- **Response ルート上限**：既定値は `2000` 件、設定範囲は `100`～`100000` 件です。上限を超えると古いルートから削除されます。

長時間タスクで既存の Prompt Cache を安定して再利用するには、タスク全体で同じ `prompt_cache_key` を使い続け、別のタスクには異なる安定した識別値を使用してください。

### ヘルスチェック

```http
GET /api/health
GET /api/providers
```

## MCP

サービスは単一の Streamable HTTP エンドポイントを提供します。

```text
http://<host>:<port>/mcp/
```

MCP は既定で有効で、プロトコルバージョンは `2025-11-25` です。`initialize`、`notifications/initialized`、`ping`、`tools/list`、`tools/call` をサポートします。サーバー主導の SSE 接続は維持しないため、仕様に従って `GET /mcp/` は `405 Method Not Allowed` を返し、各 JSON-RPC メッセージは個別の HTTP `POST` を使用します。

接続前に、管理画面の **キー管理 > MCP キー** で MCP 専用キーを発行し、そのキーで認証します。

```http
POST /mcp/ HTTP/1.1
Authorization: Bearer <api-key>
Content-Type: application/json
Accept: application/json, text/event-stream

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"example","version":"1.0.0"}}}
```

Chat API Key と一時的な Web ログインキーは MCP を呼び出せません。MCP キーも Chat、Responses、その他の一般 REST API を呼び出せません。MCP キーについては累積および月次利用統計を収集しません。

管理画面の **設定 > MCP** では、次の操作ができます。

- MCP の有効化または無効化。
- 読み取り専用モード。有効な場合、`tools/list` は状態を変更しない照会ツールだけを返します。
- 許可するブラウザー Origin の追加。`Origin` を送信しないネイティブ MCP Client と同一オリジンのブラウザーリクエストは追加不要です。
- 実際のエンドポイント、プロトコルバージョン、現在公開中のツールの確認。

ツールは既存の REST handler を唯一の実行元として使用します。サービス状態、モデル、Provider、ダッシュボードと利用量、一般／詳細／通知／MCP 設定、ベンチマーク、システム監視、システム更新、Chat Completions、Responses、マルチモーダルプロキシを対象とします。API Key の一覧、発行、変更、有効化、削除、ルートバインド、利用量照会は MCP では公開されません。

## Provider 設定

Provider は `data/llm_proxy.json` の `providers` で設定します。サンプル設定では `enabled` が `false` です。本番利用前に `true` に変更し、対応する API Key 環境変数を設定してください。

Provider と通知先 URL は `http` と `https` のみ許可します。link-local、unspecified、multicast アドレスは拒否し、内部モデルサービス向けに private CIDR と localhost/loopback は許可します。

```json
{
  "id": "primary-openai-compatible",
  "base_url": "https://api.openai.com",
  "api_key_env": "OPENAI_API_KEY",
  "chat_completions_path": "/v1/chat/completions",
  "enabled": true,
  "weight": 10,
  "max_concurrent": 32,
  "models": [
    {
      "name": "gpt-4.1-mini",
      "aliases": ["auto", "balanced"],
      "max_input_tokens": 1040000,
      "max_output_tokens": 32768,
      "capabilities": ["chat", "reasoning", "coding"],
      "cost_tier": 2,
      "quality_tier": 7
    }
  ]
}
```

## 選択戦略

現在の既定戦略は `random` です。

1. 無効、`base_url` 未設定、`max_concurrent` 超過、token 容量不足の候補を除外します。
2. 適格な候補を戦略レイヤーへ渡します。現在の `random` は、適格な Provider/Model からランダムに選択します。
3. 戦略レイヤーは selection meta を生成し、レスポンスヘッダーに `X-Proxy-Strategy`、`X-Proxy-Provider`、`X-Proxy-Model` などを追加します。
4. 将来のコスト、遅延、能力分類、健全性などの戦略向けに `weighted_score` 実装を保持しています。
5. Responses の後続リクエストが `previous_response_id` または `prompt_cache_key` のアフィニティルートに一致した場合、元の Provider/Model を優先します。ルート失効、Provider 利用不能、または割り当て差が許容値を超えた場合のみ、通常のロードバランシングにフォールバックします。

## ローカル開発

```bash
go mod tidy
go test ./...
go run ./src/cmd/loadbalanceprovider
```

構文チェックに成功したらサービスを起動できます。有効な Provider がない場合、`/v1/chat/completions` と `/v1/responses` は `service_unavailable` を返します。
