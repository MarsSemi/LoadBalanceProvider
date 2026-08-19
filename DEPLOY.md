# 部署手冊

本文件說明 `LoadBalanceProvider` 的部署與設定方式。

## 設定檔

服務啟動時會讀取專案根目錄的 `agent.properties`、`data/llm_proxy.json` 與 `data/advanced_settings.json`。`agent.properties` 保留 MarsCloud 基本設定，LLM Provider 與負載平衡設定集中放在 `data/llm_proxy.json`；進階路由、輸出比分級與低推理降級設定保存在 `data/advanced_settings.json`。

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

### 低推理降級

管理介面的「設定 > 進階 > 低推理降級」使用每支 API 金鑰最近 `15` 分鐘的密度與完成輸出進行評估。預設關閉；預設條件為跨啟用 Provider 的當日平均配額消耗達 `18%`、金鑰頻率 `≥8 req/min`、推理比 `<10%`，且至少有 `5` 筆上游回報推理量的完成樣本。成立後套用品質等級上限 `4`，預設維持 `10` 分鐘。

需注意：這個機制設定的是候選模型品質上限，不會覆寫明確指定模型或金鑰強制 Provider。若沒有符合上限的候選，負載平衡器會 fail-open 使用較高等級模型。降級狀態只保存在記憶體，設定更新或服務重啟會清除。

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
GET /api/api-keys/density?window=15m
```

`/api/providers` 可查看目前各 Provider 的 active request、成功次數、失敗次數與模型設定摘要。

`/api/api-keys/density` 是管理端即時監看端點，需使用有效的 Web 登入 Session；一般 API 金鑰與 MCP 金鑰不能存取。`window` 可使用秒數或 `1m`、`5m`、`15m`、`30m`、`1h`，最大觀察範圍為一小時。回應除了請求頻率與複雜度外，也包含 `prompt_tokens`、`quality_tier_avg`、輸出比 `output_ratio`／`output_ratio_median`、正文比 `prose_ratio`／`prose_ratio_median`／`prose_samples`、推理量 `reasoning_tokens`／`reasoning_ratio`、工具呼叫與輪次、續接與重複任務，以及 `yield_low`／`yield_mid`／`yield_high` 分布和目前套用的 `yield_thresholds`。輸出比以實際完成輸出 Token ÷ 估算輸入 Token 計算，預設分級門檻為 `≤2%`、`>2% 且 ≤20%`、`>20%`，可從管理介面的進階設定調整。舊版 `os_tool_ratio`、`tool_type_counts` 與 OS 工具分類已移除。最近逐筆樣本僅保存在記憶體，服務重啟後會重新累積；月次永久統計不受影響。
