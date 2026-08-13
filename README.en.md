# Load Balance Provider

[繁體中文](README.md) | **English** | [日本語](README.ja.md) | [한국어](README.ko.md)

`LoadBalanceProvider` is an LLM proxy service built with the MarsCloud SDK. It provides OpenAI-compatible Chat Completions and Responses APIs, and selects an appropriate backend LLM provider based on request size, workload, task characteristics, and real-time provider load.

## Goals

- **OpenAI-compatible endpoints**: Supports `POST /v1/chat/completions` and `POST /v1/responses`, allowing existing OpenAI SDKs and compatible clients to connect without changing their request style.
- **Prompt Cache affinity for long-running tasks**: Routes the same Responses conversation back to its original provider and model using `previous_response_id` or `prompt_cache_key`, preventing multi-turn tool calls and encrypted reasoning content from breaking after an account switch.
- **Multi-provider management**: Registers multiple OpenAI-compatible providers, model capabilities, weights, costs, and concurrency limits through `data/llm_proxy.json`.
- **Intelligent routing**: Uses estimated input tokens, expected output workload, message count, task type, and model capabilities for scored selection.
- **Regular and streaming responses**: Proxies standard HTTP responses and preserves SSE/chunked streaming when `stream=true`.
- **Load balancing**: Provides general-purpose evaluation based on provider weight, model quality, cost, active requests, and maximum concurrency.
- **Standard MCP support**: Provides an MCP `2025-11-25` Streamable HTTP endpoint and exposes queries and operations other than key management as tools.

## Project Structure

- `src/cmd/loadbalanceprovider/main.go`: Service entry point that initializes MarsService, the HTTP API, and LLM proxy components.
- `src/service/cloud_service.go`: Service lifecycle and background provider status recording.
- `src/api/http_api.go`: REST API routes, including `/v1/chat/completions`, `/v1/responses`, `/api/health`, and `/api/providers`.
- `src/api/mcp.go`: Standard MCP Streamable HTTP transport, JSON-RPC lifecycle, tool catalog, and adapters for existing APIs.
- `src/domain/types.go`: Shared types for Chat Completions, providers, models, and error responses.
- `src/config/provider_config.go`: Loads LLM proxy settings from `data/llm_proxy.json` and applies defaults.
- `src/analyzer/request_analyzer.go`: Estimates request size, output workload, task type, and complexity.
- `src/balancer/load_balancer.go`: Filters and scores provider/model candidates and tracks real-time load.
- `src/proxy/client.go`: Proxies OpenAI-compatible Chat Completions requests and streaming responses.
- `agent.properties`: MarsCloud service configuration.
- `data/llm_proxy.json`: LLM provider, model capability, and load-balancing configuration.

## API

### Chat Completions

```http
POST /v1/chat/completions
Content-Type: application/json
Authorization: Bearer <client-token>
```

The request format remains compatible with OpenAI Chat Completions. The service parses `model`, `messages`, `stream`, `max_tokens`, or `max_completion_tokens`, selects a backend model, and forwards the request.

```json
{
  "model": "auto",
  "messages": [
    {"role": "user", "content": "Help me design a highly available architecture."}
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

The request format remains compatible with the OpenAI Responses API. Both regular responses and SSE streaming with `stream=true` are supported. `model` may specify an actual model or use `AUTO`, allowing the service to select a backend based on provider capabilities and real-time load.

```json
{
  "model": "AUTO",
  "input": "Analyze this project and propose a refactoring plan.",
  "stream": true,
  "prompt_cache_key": "project-refactor-2026"
}
```

The Responses API also supports compatible subroutes for retrieving, deleting, or canceling an existing response, listing its input items, and calculating input tokens. The service forwards each request to the provider that created the response and isolates routing data by the caller's API key.

#### Prompt Cache Affinity for Long-Running Tasks

Long-running Responses tasks often contain multi-turn reasoning, tool calls, or encrypted reasoning content. Switching providers or accounts during the task may prevent the upstream service from recognizing earlier state. The service automatically maintains conversation affinity:

1. After a response is created successfully, the service records the mapping between the response ID, `prompt_cache_key`, provider, model, and caller identity.
2. A later request carrying `previous_response_id` is routed back to the original provider and model.
3. When clients such as Codex omit `previous_response_id` but continue using the same `prompt_cache_key`, the request is still routed to the original provider and model. This is suitable for long-running agent tasks and multi-turn tool calls.
4. Affinity data is isolated by API key, so one caller cannot reuse another caller's response or Prompt Cache route.
5. If the provider becomes unavailable, its quota falls significantly below its peers, the affinity expires, or the route is evicted, the service removes `previous_response_id` and encrypted reasoning content that cannot be transferred across providers, then performs load balancing again instead of failing the entire task.

The following values can be adjusted under **Settings > Advanced** in the management interface:

- **Conversation affinity TTL**: Defaults to `30` minutes and accepts values from `1` to `10080` minutes. For long-running tasks, set it longer than the maximum expected interval between steps.
- **Affinity quota tolerance**: Defaults to `10` percentage points. Affinity may be released when the original provider's quota falls below the peer average by more than this value.
- **Response route limit**: Defaults to `2000` entries and accepts values from `100` to `100000`. Older routes are evicted after the limit is reached.

To consistently reuse an existing Prompt Cache during a long-running task, the client should keep the same `prompt_cache_key` throughout that task and use a different stable identifier for each separate task.

### Health Checks

```http
GET /api/health
GET /api/providers
```

## MCP

The service provides one Streamable HTTP endpoint:

```text
http://<host>:<port>/mcp/
```

MCP is enabled by default and uses protocol version `2025-11-25`. It supports `initialize`, `notifications/initialized`, `ping`, `tools/list`, and `tools/call`. The endpoint does not maintain server-initiated SSE, so `GET /mcp/` returns `405 Method Not Allowed` as required; each JSON-RPC message uses a separate HTTP `POST`.

Authenticate with either an API key or a dedicated MCP key issued from **Key Management** in the management interface:

```http
POST /mcp/ HTTP/1.1
Authorization: Bearer <api-key>
Content-Type: application/json
Accept: application/json, text/event-stream

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"example","version":"1.0.0"}}}
```

API keys can call general REST APIs and MCP. Temporary web login keys cannot call MCP. MCP keys can only call MCP, cannot call Chat, Responses, or other general REST APIs, and do not collect accumulated or monthly usage statistics.

The management interface under **Settings > MCP** allows you to:

- Enable or disable MCP.
- Enable read-only mode, which limits `tools/list` to operations that do not change state.
- Add allowed browser origins. Native MCP clients without an `Origin` header and same-origin browser requests do not need to be added.
- View the effective endpoint, protocol version, and currently exposed tools.

Tools use the existing REST handlers as their single execution source. They cover service status, models, providers, dashboards and usage, general/advanced/notification/MCP settings, benchmarks, system monitoring, system updates, Chat Completions, Responses, and multimodal proxying. API key listing, issuance, modification, activation, deletion, route binding, and usage queries are never exposed through MCP.

## Provider Configuration

Providers are configured in the `providers` section of `data/llm_proxy.json`. The sample configuration sets `enabled` to `false`; change it to `true` and configure the corresponding API key environment variable before production use.

Provider and notification target URLs only allow `http` and `https`. Link-local, unspecified, and multicast addresses are blocked, while private CIDRs and localhost/loopback remain allowed for internal model services.

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

## Selection Strategy

The default strategy is currently `random`:

1. Exclude candidates that are disabled, have no `base_url`, exceed `max_concurrent`, or lack sufficient token capacity.
2. Pass qualified candidates to the strategy layer. The current `random` strategy selects a provider/model at random from the qualified set.
3. The strategy layer generates selection metadata. Response headers include `X-Proxy-Strategy`, `X-Proxy-Provider`, `X-Proxy-Model`, and related values.
4. A `weighted_score` strategy implementation is retained for future cost, latency, capability classification, health, and other policies.
5. When a later Responses request matches an affinity route through `previous_response_id` or `prompt_cache_key`, the original provider and model are preferred. The service falls back to normal load balancing only when the route expires, the provider is unavailable, or the quota difference exceeds the configured tolerance.

## Codex App Setup Tools

The `cmd/` directory provides platform-specific Codex setup tools:

| Platform / environment | Script |
| :--- | :--- |
| macOS Codex App | `cmd/marsCodexApp.sh` |
| Windows Codex App | `cmd/marsCodexApp.bat` |
| Ubuntu Codex CLI / desktop | `cmd/marsCodexApp_Linux_CLI.sh` |
| VS Code SSH Remote + Codex Extension | `cmd/marsCodexApp_Linux_VSC_Remote.sh` |

The tools can apply the Mars LLM source, restore the provider selection that was active before Mars (or native Codex defaults when none was saved), or refresh the Mars model catalog. Applying the source also configures `MARS_API_KEY`, the Responses provider, Codex image generation, and the Mars MCP `image_gen` tool.

Configuration is merged into the existing `config.toml` through a managed block. Existing `[projects."..."]` entries, project trust settings, feature flags, and unrelated sections are preserved. The generated `config.toml.mars-llm-proxy.bak` file is only an emergency backup; the normal restore operation does not overwrite the current configuration with it. Restore removes only Mars-managed provider, URL, catalog, MCP, and token-source settings. Keeping an existing backup instead of overwriting it does not stop the apply operation.

Existing `[model_providers.*]` and `[profiles.*]` definitions coexist with Mars and are never removed. Applying Mars saves the active top-level `model`, `model_provider`, `model_catalog_json`, and `profile`, switches the model/provider/catalog to Mars, and clears a potentially conflicting active profile. The previous values are stored in `config.toml.mars-llm-proxy.defaults` and restored when Mars is removed. If an older Mars installation has no state file, restore falls back to native Codex defaults instead of guessing another provider.

Documentation download URLs must use placeholders such as `https://example.com/downloads/marsCodexApp.sh`; the actual script distribution URL is intentionally not published. After applying, restoring, or refreshing, fully restart Codex App, the CLI, or the VS Code Extension Host so that environment variables, account state, and the model catalog are reloaded.

## Local Development

```bash
go mod tidy
go test ./...
go run ./src/cmd/loadbalanceprovider
```

The service can be started after syntax checks pass. If no provider is enabled, `/v1/chat/completions` and `/v1/responses` return `service_unavailable`.
