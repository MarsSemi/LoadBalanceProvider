# 部署手冊

本文件說明 `LoadBalanceProvider` 的部署與設定方式。

## 設定檔

服務啟動時會讀取專案根目錄的 `agent.properties` 與 `data/llm_proxy.json`。`agent.properties` 保留 MarsCloud 基本設定，LLM Provider 與負載平衡設定集中放在 `data/llm_proxy.json`。

| 參數 | 說明 |
| :--- | :--- |
| `selection_strategy` | 負載平衡策略，目前預設 `random`，並保留 `weighted_score`。 |
| `retry_count` | 首 Token 前可自動排除失敗 Provider 並重新選路由的次數；已開始輸出後不切換。 |
| `providers[].id` | Provider 唯一識別碼。 |
| `providers[].base_url` | OpenAI-compatible Provider base URL。 |
| `providers[].api_key_env` | API key 的環境變數名稱。 |
| `providers[].chat_completions_path` | Chat Completions endpoint path。 |
| `providers[].enabled` | 是否啟用此 Provider。 |
| `providers[].weight` | 權重，數值越高越容易被選中。 |
| `providers[].priority` | 優先序調整，數值越高會降低分數。 |
| `providers[].max_concurrent` | 同時處理 request 上限。 |
| `models[].max_input_tokens` | 模型可接受的最大輸入 token。 |
| `models[].max_output_tokens` | 模型可接受的最大輸出 token。 |
| `models[].capabilities` | 模型適合的任務類型，例如 `chat`、`reasoning`、`coding`、`summarization`。 |
| `models[].cost_tier` | 成本級距，數值越高代表越昂貴。 |
| `models[].quality_tier` | 品質級距，數值越高代表越適合高複雜度任務。 |

## 部署步驟

1. 設定 Provider API key：

   ```bash
   export OPENAI_API_KEY="..."
   ```

2. 編輯 `data/llm_proxy.json`，將要使用的 Provider `enabled` 改為 `true`。

3. 整理依賴並確認語法：

   ```bash
   go mod tidy
   go test ./...
   ```

4. 編譯：

   ```bash
   go build -o LoadBalanceProvider ./src/cmd/loadbalanceprovider
   ```

5. 啟動：

   ```bash
   ./LoadBalanceProvider
   ```

## 維運端點

```http
GET /api/health
GET /api/providers
```

`/api/providers` 可查看目前各 Provider 的 active request、成功次數、失敗次數與模型設定摘要。
