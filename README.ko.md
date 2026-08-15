# Load Balance Provider

[繁體中文](README.md) | [English](README.en.md) | [日本語](README.ja.md) | **한국어**

`LoadBalanceProvider`는 MarsCloud SDK로 구축된 LLM 프록시 서비스입니다. OpenAI 호환 Chat Completions 및 Responses API를 제공하며, 요청 크기, 작업량, 작업 특성, Provider의 실시간 부하를 기준으로 적절한 백엔드 LLM Provider를 선택합니다.

## 주요 목표

- **OpenAI 호환 엔드포인트**: `POST /v1/chat/completions`와 `POST /v1/responses`를 지원하여 기존 OpenAI SDK 및 호환 Client를 그대로 연결할 수 있습니다.
- **장기 작업용 Prompt Cache 고정 라우팅**: `previous_response_id` 또는 `prompt_cache_key`를 사용해 동일한 Responses 대화를 원래 Provider/Model로 라우팅하여, 다중 턴 도구 호출과 암호화된 reasoning 내용이 계정 전환으로 인해 무효화되는 것을 방지합니다.
- **다중 Provider 관리**: `data/llm_proxy.json`을 통해 여러 OpenAI-compatible Provider, 모델 기능, 가중치, 비용, 동시 실행 한도를 등록합니다.
- **지능형 라우팅**: 입력 token 추정치, 예상 출력 작업량, 메시지 수, 작업 유형, 모델 기능을 기준으로 점수를 계산합니다.
- **일반 응답 및 스트리밍**: 일반 HTTP response를 전달하고, `stream=true`이면 SSE/Chunked 스트리밍을 유지합니다.
- **부하 분산**: Provider 가중치, 모델 품질, 비용, 현재 active request, max concurrent를 기준으로 범용 평가를 수행합니다.
- **표준 MCP 지원**: MCP `2025-11-25` Streamable HTTP 엔드포인트를 제공하고 키 관리를 제외한 조회와 작업을 도구로 공개합니다.

## 디렉터리 구조

- `src/cmd/loadbalanceprovider/main.go`: MarsService, HTTP API, LLM Proxy 구성 요소를 초기화하는 서비스 진입점입니다.
- `src/service/cloud_service.go`: 서비스 수명 주기와 백그라운드 Provider 상태 기록을 담당합니다.
- `src/api/http_api.go`: `/v1/chat/completions`, `/v1/responses`, `/api/health`, `/api/providers`를 포함하는 REST API 라우트입니다.
- `src/api/mcp.go`: 표준 MCP Streamable HTTP, JSON-RPC 수명 주기, 도구 카탈로그, 기존 API 어댑터입니다.
- `src/domain/types.go`: Chat Completion, Provider, Model, 오류 형식 등의 공통 타입입니다.
- `src/config/provider_config.go`: `data/llm_proxy.json`에서 LLM Proxy 설정을 읽고 기본값을 적용합니다.
- `src/analyzer/request_analyzer.go`: 요청 크기, 출력 작업량, 작업 유형, 복잡도를 추정합니다.
- `src/balancer/load_balancer.go`: Provider/Model 후보 필터링, 점수 계산, 실시간 부하 추적을 수행합니다.
- `src/proxy/client.go`: OpenAI-compatible Chat Completion HTTP 전달 및 스트리밍 프록시를 담당합니다.
- `agent.properties`: MarsCloud 서비스 설정입니다.
- `data/llm_proxy.json`: LLM Provider, 모델 기능, 부하 분산 설정입니다.

## API

### Chat Completions

```http
POST /v1/chat/completions
Content-Type: application/json
Authorization: Bearer <client-token>
```

요청 형식은 OpenAI Chat Completions와 호환됩니다. 서비스는 `model`, `messages`, `stream`, `max_tokens` 또는 `max_completion_tokens`를 분석하고 백엔드 모델을 선택해 요청을 전달합니다.

```json
{
  "model": "auto",
  "messages": [
    {"role": "user", "content": "고가용성 아키텍처를 설계해 주세요."}
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

요청 형식은 OpenAI Responses API와 호환됩니다. 일반 응답과 `stream=true` SSE 스트리밍을 지원합니다. `model`에는 실제 모델 또는 `AUTO`를 지정할 수 있으며, 서비스가 Provider 기능과 실시간 부하에 따라 백엔드를 선택합니다.

```json
{
  "model": "AUTO",
  "input": "이 프로젝트를 분석하고 리팩터링 계획을 제안해 주세요.",
  "stream": true,
  "prompt_cache_key": "project-refactor-2026"
}
```

Responses API는 기존 response 조회, 삭제, 취소, 입력 항목 조회, input token 계산 등의 호환 하위 라우트도 지원합니다. 서비스는 요청을 해당 response를 생성한 Provider로 전달하며, 호출자의 API Key를 기준으로 라우팅 데이터를 분리합니다.

#### 장기 작업용 Prompt Cache 고정 라우팅

Responses 장기 작업에는 다중 턴 추론, 도구 호출 또는 암호화된 reasoning 내용이 포함될 수 있습니다. 작업 중 Provider나 계정이 변경되면 업스트림 서비스가 이전 상태를 인식하지 못할 수 있습니다. 서비스는 다음과 같이 대화 고정 관계를 자동으로 유지합니다.

1. response 생성에 성공하면 response ID, `prompt_cache_key`, Provider, Model, 호출자 ID의 매핑을 기록합니다.
2. 후속 요청에 `previous_response_id`가 포함되면 원래 Provider/Model로 우선 라우팅합니다.
3. Codex 등의 Client가 `previous_response_id`를 보내지 않고 동일한 `prompt_cache_key`를 계속 사용하는 경우에도 원래 Provider/Model로 라우팅합니다. 장시간 Agent 작업과 다중 턴 도구 호출에 적합합니다.
4. 고정 라우팅 데이터는 API Key별로 분리되므로 다른 호출자의 response 또는 Prompt Cache 라우트를 공유할 수 없습니다.
5. Provider를 사용할 수 없거나, 할당량이 다른 Provider보다 크게 낮거나, 고정 라우팅이 만료되거나, 라우트가 제거된 경우 Provider 간에 이동할 수 없는 `previous_response_id`와 암호화된 reasoning 내용을 제거한 뒤 다시 부하 분산을 수행하여 전체 작업 실패를 방지합니다.

관리 화면의 **설정 > 고급 설정**에서 다음 값을 조정할 수 있습니다.

- **대화 고정 TTL**: 기본값은 `30`분이며 `1`~`10080`분으로 설정할 수 있습니다. 장기 작업에서는 예상되는 최대 단계 간격보다 길게 설정하십시오.
- **고정 라우팅 할당량 허용치**: 기본값은 `10`%p입니다. 원래 Provider의 할당량이 다른 Provider 평균보다 이 값 이상 낮아지면 고정 라우팅을 해제할 수 있습니다.
- **Response 라우트 한도**: 기본값은 `2000`개이며 `100`~`100000`개로 설정할 수 있습니다. 한도를 초과하면 오래된 라우트부터 제거됩니다.

장기 작업에서 기존 Prompt Cache를 안정적으로 재사용하려면 작업 전체에서 동일한 `prompt_cache_key`를 유지하고, 서로 다른 작업에는 각각 다른 안정적인 식별자를 사용하십시오.

### 상태 확인

```http
GET /api/health
GET /api/providers
```

## MCP

서비스는 하나의 Streamable HTTP 엔드포인트를 제공합니다.

```text
http://<host>:<port>/mcp/
```

MCP는 기본적으로 활성화되며 프로토콜 버전은 `2025-11-25`입니다. `initialize`, `notifications/initialized`, `ping`, `tools/list`, `tools/call`을 지원합니다. 엔드포인트는 서버 주도 SSE 연결을 유지하지 않으므로 규격에 따라 `GET /mcp/`는 `405 Method Not Allowed`를 반환하며, 각 JSON-RPC 메시지는 별도의 HTTP `POST`를 사용합니다.

관리 화면의 **키 관리**에서 발급한 API 키 또는 MCP 전용 키로 인증할 수 있습니다.

```http
POST /mcp/ HTTP/1.1
Authorization: Bearer <api-key>
Content-Type: application/json
Accept: application/json, text/event-stream

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"example","version":"1.0.0"}}}
```

API Key는 일반 REST API와 MCP를 호출할 수 있습니다. 임시 Web 로그인 키는 MCP를 호출할 수 없습니다. MCP 키는 MCP만 호출할 수 있고 Chat, Responses 또는 기타 일반 REST API는 호출할 수 없으며 누적 및 월별 사용 통계도 수집하지 않습니다.

관리 화면의 **설정 > MCP**에서 다음 항목을 조정할 수 있습니다.

- MCP 활성화 또는 비활성화.
- 읽기 전용 모드. 활성화하면 `tools/list`는 상태를 변경하지 않는 조회 도구만 제공합니다.
- 허용할 브라우저 Origin 추가. `Origin`을 보내지 않는 네이티브 MCP Client와 동일 출처 브라우저 요청은 추가할 필요가 없습니다.
- 실제 엔드포인트, 프로토콜 버전, 현재 공개된 도구 확인.

도구는 기존 REST handler를 유일한 실행 소스로 사용합니다. 서비스 상태, 모델, Provider, 대시보드와 사용량, 일반/고급/알림/MCP 설정, 벤치마크, 시스템 모니터링, 시스템 업데이트, Chat Completions, Responses, 멀티모달 프록시를 포함합니다. API Key 목록 조회, 발급, 변경, 활성화, 삭제, 라우트 바인딩, 사용량 조회는 MCP를 통해 공개되지 않습니다.

## Provider 설정

Provider는 `data/llm_proxy.json`의 `providers`에서 설정합니다. 샘플 설정의 `enabled` 기본값은 `false`이므로 실제 사용 전 `true`로 변경하고 해당 API Key 환경 변수를 설정해야 합니다.

Provider 및 알림 대상 URL은 `http`와 `https`만 허용합니다. link-local, unspecified, multicast 주소는 차단하며, 내부 모델 서비스용 private CIDR과 localhost/loopback은 허용합니다.

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

## 선택 전략

현재 기본 전략은 `random`입니다.

1. 비활성화, `base_url` 미설정, `max_concurrent` 초과, token 용량 부족 후보를 제외합니다.
2. 적합한 후보를 전략 계층으로 전달합니다. 현재 `random` 전략은 적합한 Provider/Model 중 하나를 무작위로 선택합니다.
3. 전략 계층은 selection meta를 생성하며, 응답 Header에 `X-Proxy-Strategy`, `X-Proxy-Provider`, `X-Proxy-Model` 등의 정보를 포함합니다.
4. 향후 비용, 지연 시간, 기능 분류, 상태 등의 정책을 추가할 수 있도록 `weighted_score` 전략 구현을 유지합니다.
5. Responses 후속 요청이 `previous_response_id` 또는 `prompt_cache_key` 고정 라우트와 일치하면 원래 Provider/Model을 우선 사용합니다. 라우트 만료, Provider 사용 불가 또는 할당량 차이가 허용치를 초과한 경우에만 일반 부하 분산으로 전환합니다.

## Codex 설정 통합

설정 통합 기능은 모델 소스, Provider, 모델 카탈로그 및 관련 확장 기능 관리를 지원합니다. 사용 가능한 기능은 실행 환경, 배포 방식 및 부여된 권한에 따라 달라질 수 있습니다.

변경 사항은 기존 `config.toml`에 증분 방식으로 병합되며 프로젝트 신뢰 정보, 기능 플래그 및 관련 없는 설정은 유지됩니다. 필요할 때 이전 Provider, Profile 및 모델 선택을 복원할 수 있도록 적용 전에 필요한 상태를 보존합니다.

설정 적용, 복원 또는 갱신 후에는 Codex App, CLI 또는 VS Code Extension Host를 완전히 다시 시작하여 변경된 설정과 계정 상태를 다시 불러오십시오.

## 로컬 개발

```bash
go mod tidy
go test ./...
go run ./src/cmd/loadbalanceprovider
```

구문 검사가 통과하면 서비스를 시작할 수 있습니다. 활성화된 Provider가 없으면 `/v1/chat/completions`와 `/v1/responses`는 `service_unavailable`을 반환합니다.
