# Load Balance Provider

**繁體中文** | [English](README.en.md) | [日本語](README.ja.md) | [한국어](README.ko.md)

`LoadBalanceProvider` 是一個以 MarsCloud SDK 建立的 LLM Proxy 服務。服務提供 OpenAI Chat Completions 與 Responses 相容 API，並依照請求內容大小、工作量、工作性質與 Provider 即時負載，選擇合適的後端 LLM Provider 處理。

## 功能目標

- **OpenAI 相容入口**：支援 `POST /v1/chat/completions` 與 `POST /v1/responses`，保留既有 OpenAI SDK 或相容 Client 的接入方式。
- **長任務 Prompt Cache 黏著**：以 `previous_response_id` 或 `prompt_cache_key` 將同一段 Responses 對話導回原 Provider/Model，避免多輪工具呼叫與加密推理內容因切換帳號而失效。
- **多 Provider 管理**：透過 `data/llm_proxy.json` 登錄多個 OpenAI-compatible Provider、模型能力、權重、成本與併發上限。
- **智慧路由**：依照輸入 token 估算、輸出需求、訊息數、任務類型與模型能力進行分數式選擇。
- **一般與串流支援**：非 streaming 以一般 HTTP response 轉回，`stream=true` 時維持 SSE/Chunked 轉送。
- **負載平衡**：以 provider 權重、模型品質、成本、目前 active request 與 max concurrent 做通用評分。
- **標準 MCP**：提供 MCP `2025-11-25` Streamable HTTP 端點，將金鑰管理以外的查詢與操作公開為工具。

## 目錄結構

- `src/cmd/loadbalanceprovider/main.go`：服務進入點，初始化 MarsService、HTTP API 與 LLM Proxy 元件。
- `src/service/cloud_service.go`：服務生命週期與背景 Provider 狀態記錄。
- `src/api/http_api.go`：REST API 路由，包含 `/v1/chat/completions`、`/v1/responses`、`/api/health`、`/api/providers`。
- `src/api/mcp.go`：標準 MCP Streamable HTTP、JSON-RPC 生命週期、工具目錄與既有 API 轉接。
- `src/domain/types.go`：Chat Completion、Provider、Model、錯誤格式等共用型別。
- `src/config/provider_config.go`：讀取 `data/llm_proxy.json` 的 LLM Proxy 設定並套用預設值。
- `src/analyzer/request_analyzer.go`：估算請求大小、輸出工作量、任務類型與複雜度。
- `src/balancer/load_balancer.go`：候選 Provider/Model 過濾、評分與即時負載統計。
- `src/proxy/client.go`：OpenAI-compatible Chat Completion HTTP 轉發與串流代理。
- `agent.properties`：MarsCloud 服務設定。
- `data/llm_proxy.json`：LLM Provider、模型能力與負載平衡設定。

## API

### Chat Completions

```http
POST /v1/chat/completions
Content-Type: application/json
Authorization: Bearer <client-token>
```

請求格式維持 OpenAI Chat Completions 相容。服務會解析 `model`、`messages`、`stream`、`max_tokens` 或 `max_completion_tokens`，選擇後端模型後轉發。

```json
{
  "model": "auto",
  "messages": [
    {"role": "user", "content": "請幫我規劃一個高可用架構"}
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

請求格式維持 OpenAI Responses API 相容，支援一般回應與 `stream=true` 的 SSE 串流轉送。`model` 可指定實際模型或使用 `AUTO`，由服務依 Provider 能力與即時負載選擇後端。

```json
{
  "model": "AUTO",
  "input": "請分析此專案並提出重構計畫",
  "stream": true,
  "prompt_cache_key": "project-refactor-2026"
}
```

Responses API 亦支援既有 response 的查詢、刪除、取消、輸入項目與 input token 計算等相容子路由；服務會將請求轉送至建立該 response 的 Provider，並以呼叫端 API Key 隔離路由資料。

#### 長任務 Prompt Cache 黏著

Responses 長任務通常包含多輪推理、工具呼叫或加密 reasoning 內容，過程中若切換 Provider 或帳號，上游可能無法識別先前狀態。服務會自動建立對話黏著關係：

1. 回應建立成功後，記錄 response ID、`prompt_cache_key`、Provider、Model 與呼叫端身分的對應。
2. 後續請求帶有 `previous_response_id` 時，優先導回原 Provider/Model。
3. Codex 等 Client 未送出 `previous_response_id`、但沿用相同 `prompt_cache_key` 時，仍會導回原 Provider/Model，適合長時間 Agent 任務與多輪工具呼叫。
4. 黏著資料依 API Key 隔離，不同呼叫端無法共用其他使用者的 response 或 Prompt Cache 路由。
5. Provider 不可用、配額顯著低於其他 Provider、黏著逾時或路由被淘汰時，服務會移除無法跨 Provider 使用的 `previous_response_id` 與加密 reasoning，再重新執行負載平衡，避免整個任務直接失敗。

管理介面的「設定 > 進階」可調整以下參數：

- **對話黏著 TTL**：預設 `30` 分鐘，可設定 `1` 至 `10080` 分鐘；長任務應依最長步驟間隔適度提高。
- **黏著配額容忍值**：預設 `10` 個百分點；原 Provider 配額低於同儕平均超過此值時，允許解除黏著並重新選擇。
- **Response 路由上限**：預設 `2000` 筆，可設定 `100` 至 `100000` 筆；超過上限時會淘汰較舊的路由。

若要讓同一個長任務穩定命中既有 Prompt Cache，Client 應在整段任務期間持續使用相同的 `prompt_cache_key`，不同任務則使用不同且穩定的識別值。

### 健康檢查

```http
GET /api/health
GET /api/providers
```

## MCP

服務提供單一 Streamable HTTP 端點：

```text
http://<host>:<port>/mcp/
```

MCP 預設啟用，協定版本為 `2025-11-25`，支援 `initialize`、`notifications/initialized`、`ping`、`tools/list` 與 `tools/call`。端點不維持伺服器主動 SSE，因此 `GET /mcp/` 依規格回傳 `405 Method Not Allowed`；每個 JSON-RPC 訊息使用獨立的 HTTP `POST`。

連線可使用管理介面「金鑰管理」核發的 API 金鑰或 MCP 專用金鑰驗證：

```http
POST /mcp/ HTTP/1.1
Authorization: Bearer <api-key>
Content-Type: application/json
Accept: application/json, text/event-stream

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"example","version":"1.0.0"}}}
```

API Key 可呼叫一般 REST API 與 MCP；Web 登入暫時金鑰不能呼叫 MCP。MCP 金鑰只能呼叫 MCP，不能呼叫 Chat、Responses 或其他一般 REST API，亦不收集累計或每月使用統計。

管理介面的「設定 > MCP」可調整：

- 啟用或停用 MCP。
- 唯讀模式；開啟時，`tools/list` 只提供不改變狀態的查詢工具。
- 額外允許的瀏覽器 Origin；未帶 `Origin` 的原生 MCP Client 與同源瀏覽器請求不需加入清單。
- 檢視實際端點、協定版本與目前公開工具。

工具以既有 REST handler 為唯一執行來源，涵蓋服務狀態、模型、Provider、儀表板與用量、一般／進階／通知／MCP 設定、基準測試、系統監控、系統更新、Chat Completions、Responses 與多模態代理。API Key 的列出、核發、修改、啟停、刪除、路由綁定與用量查詢固定不會透過 MCP 公開。

## Provider 設定

Provider 以 `data/llm_proxy.json` 的 `providers` 設定。`enabled` 預設範例為 `false`，正式使用前需改為 `true` 並設定對應 API key 環境變數。
Provider 與通知目標 URL 只允許 `http`/`https`，並阻擋 link-local、unspecified、multicast 等位址；private CIDR 與 localhost/loopback 暫時允許供內網模型服務使用。

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

## 選擇策略

目前預設採 `random`：

1. 先排除停用、未設定 `base_url`、超過 `max_concurrent`、token 容量不足的候選。
2. 將候選交給策略層處理，目前 `random` 會從合格 Provider/Model 中隨機挑選。
3. 策略層會產生 selection meta，回應 Header 會帶出 `X-Proxy-Strategy`、`X-Proxy-Provider`、`X-Proxy-Model` 等資訊。
4. 保留 `weighted_score` 策略實作，後續可加入成本、延遲、能力分類、健康度等策略。
5. Responses 後續請求若命中 `previous_response_id` 或 `prompt_cache_key` 黏著路由，會優先使用原 Provider/Model；僅在路由失效、Provider 不可用或配額差距超過容忍值時才降級回一般負載平衡。

## Codex App 設定工具

下列項目是依執行環境區分的 Codex 設定工具範例名稱。這些腳本不會封裝於部署 ZIP，應透過另行管理的發佈管道提供：

| 平台／環境 | 腳本 |
| :--- | :--- |
| macOS Codex App | `marsCodexApp.sh` |
| Windows Codex App | `marsCodexApp.bat` |
| Ubuntu Codex CLI／桌面環境 | `marsCodexApp_Linux_CLI.sh` |
| VS Code SSH Remote + Codex Extension | `marsCodexApp_Linux_VSC_Remote.sh` |

工具提供三項操作：

1. **套用 Mars LLM 來源**：下載 Mars Codex model catalog、設定 `MARS_API_KEY`、加入 Mars Responses Provider，並設定 `image_generation` 與 MCP `image_gen` 工具。
2. **恢復 Codex 原始設定**：移除 Mars 管理的 Provider、URL、model catalog 與 Token 來源；若套用前已有作用中的 Provider／Profile，會還原原值，否則回到 Codex 官方登入及原生 Provider 流程。
3. **更新 Mars 模型列表**：重新下載 model catalog，並同步補齊 Provider 驗證、影像生成與 MCP 設定。

腳本會以受管區塊合併 `config.toml`，不會覆蓋整份設定。既有的 `[projects."..."]`、專案信任資訊、功能旗標及其他非 Mars 區段會保留。套用前仍會建立：

```text
config.toml.mars-llm-proxy.bak
```

`.bak` 只作緊急人工復原用途。選擇「恢復 Codex 原始設定」時不會拿 `.bak` 覆蓋現有檔案；已有備份時，即使選擇不覆蓋備份，套用流程也會繼續。

若原本已設定其他 Provider，該 Provider 的 `[model_providers.*]`、`[profiles.*]` 及相關設定會繼續保留並與 Mars 共存。套用時會保存頂層的作用中 `model`、`model_provider`、`model_catalog_json` 與 `profile`；接著將 model／Provider／catalog 切換到 Mars，並清除可能衝突的作用中 profile。原值會保存於：

```text
config.toml.mars-llm-proxy.defaults
```

執行恢復時會先移除 Mars 管理區段，再從此狀態檔還原原本的作用中 Provider／Profile，最後刪除狀態檔。若舊版已經套用 Mars、但沒有狀態檔，恢復時則不猜測其他 Provider，而是回到 Codex 原生預設。

腳本產生的主要設定如下，實際 model catalog 路徑會依使用者目錄動態決定：

```toml
# BEGIN Mars LLM Proxy managed settings
model = "AUTO"
model_catalog_json = "<codex-home>/mars-model-catalog.json"
model_provider = "mars-llm-proxy"
# END Mars LLM Proxy managed settings

[model_providers.mars-llm-proxy]
name = "Mars"
base_url = "https://proxy.example.com/v1"
env_key = "MARS_API_KEY"
wire_api = "responses"
requires_openai_auth = true

[features]
image_generation = true

[mcp_servers.mars-llm-proxy]
url = "https://proxy.example.com/mcp/"
bearer_token_env_var = "MARS_API_KEY"
enabled_tools = ["image_gen"]
tool_timeout_sec = 600
```

完成套用、恢復或更新後，必須完整關閉並重新啟動 Codex App、CLI 或 VS Code Extension Host，讓環境變數、帳號資訊與模型目錄重新載入。

## 本地開發

```bash
go mod tidy
go test ./...
go run ./src/cmd/loadbalanceprovider
```

語法檢查通過後即可啟動。若未啟用任何 Provider，`/v1/chat/completions` 與 `/v1/responses` 會回傳 `service_unavailable`。
