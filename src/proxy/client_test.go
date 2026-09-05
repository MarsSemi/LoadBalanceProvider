package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
func TestRewriteTextCompletionRequestBuildsPrompt(t *testing.T) {
	_body := []byte(`{"provider_id":"local","model":"AUTO","stream":true,"messages":[{"role":"system","content":"Rules"},{"role":"user","content":"Hello"}],"benchmark_text_completion_fallback":true}`)

	_rewritten, _err := rewriteTextCompletionRequest(_body, "local-model", true)

	if _err != nil {
		t.Fatalf("rewrite text completion request failed: %v", _err)
	}
	_rewrittenText := string(_rewritten)
	for _, _want := range []string{`"model":"local-model"`, `"prompt":"SYSTEM: Rules\nUSER: Hello\nASSISTANT:"`, `"stream":true`} {
		if !contains(_rewrittenText, _want) {
			t.Fatalf("rewritten body %s does not contain %s", _rewrittenText, _want)
		}
	}
	for _, _blocked := range []string{"provider_id", "messages", "benchmark_text_completion_fallback"} {
		if contains(_rewrittenText, _blocked) {
			t.Fatalf("rewritten body %s should not contain %s", _rewrittenText, _blocked)
		}
	}
}

// -------------------------------------------------------------------------------------
func TestRewriteRequestModelConvertsAttachmentsToImageParts(t *testing.T) {
	_body := []byte(`{"provider_id":"local","model":"AUTO","messages":[{"role":"user","content":"請讀圖"}],"attachments":[{"name":"sample.png","mime_type":"image/png","media_type":"image","file_data":"iVBORw0KGgo="}]}`)

	_rewritten, _, _, _err := rewriteRequestModel(_body, "vision-model", false)

	if _err != nil {
		t.Fatalf("rewrite request failed: %v", _err)
	}
	var _payload map[string]interface{}
	if _err := json.Unmarshal(_rewritten, &_payload); _err != nil {
		t.Fatalf("decode rewritten request: %v", _err)
	}
	if _, _ok := _payload["attachments"]; _ok {
		t.Fatalf("attachments should not be forwarded directly: %s", string(_rewritten))
	}
	_messages := _payload["messages"].([]interface{})
	_content := _messages[0].(map[string]interface{})["content"].([]interface{})
	if len(_content) != 2 {
		t.Fatalf("expected text and image parts, got %#v", _content)
	}
	_image := _content[1].(map[string]interface{})
	_imageURL := _image["image_url"].(map[string]interface{})
	if _image["type"] != "image_url" || _imageURL["url"] != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("unexpected image part: %#v", _image)
	}
}

// -------------------------------------------------------------------------------------
func TestRewriteRequestModelConvertsCamelCaseAttachmentsToImageParts(t *testing.T) {
	_body := []byte(`{"provider_id":"local","model":"AUTO","messages":[{"role":"user","content":"請讀圖"}],"attachments":[{"name":"sample.jpg","mimeType":"image/jpeg","mediaType":"image","fileData":"YWJj"}]}`)

	_rewritten, _, _, _err := rewriteRequestModel(_body, "vision-model", false)

	if _err != nil {
		t.Fatalf("rewrite request failed: %v", _err)
	}
	var _payload map[string]interface{}
	if _err := json.Unmarshal(_rewritten, &_payload); _err != nil {
		t.Fatalf("decode rewritten request: %v", _err)
	}
	_messages := _payload["messages"].([]interface{})
	_content := _messages[0].(map[string]interface{})["content"].([]interface{})
	_image := _content[1].(map[string]interface{})
	_imageURL := _image["image_url"].(map[string]interface{})
	if _imageURL["url"] != "data:image/jpeg;base64,YWJj" {
		t.Fatalf("unexpected image url: %#v", _imageURL)
	}
}

// -------------------------------------------------------------------------------------
func TestRewriteRequestModelConvertsDataURLAttachmentWithoutMetadataToImagePart(t *testing.T) {
	_body := []byte(`{"provider_id":"local","model":"AUTO","messages":[{"role":"user","content":"請讀圖"}],"attachments":[{"content":"data:image/png;base64,iVBORw0KGgo="}]}`)

	_rewritten, _, _, _err := rewriteRequestModel(_body, "vision-model", false)

	if _err != nil {
		t.Fatalf("rewrite request failed: %v", _err)
	}
	var _payload map[string]interface{}
	if _err := json.Unmarshal(_rewritten, &_payload); _err != nil {
		t.Fatalf("decode rewritten request: %v", _err)
	}
	if _, _ok := _payload["attachments"]; _ok {
		t.Fatalf("attachments should not be forwarded directly: %s", string(_rewritten))
	}
	_messages := _payload["messages"].([]interface{})
	_content := _messages[0].(map[string]interface{})["content"].([]interface{})
	_image := _content[1].(map[string]interface{})
	_imageURL := _image["image_url"].(map[string]interface{})
	if _image["type"] != "image_url" || _imageURL["url"] != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("unexpected image part: %#v", _image)
	}
}

// -------------------------------------------------------------------------------------
func TestRewriteRequestModelDoesNotDuplicateExistingImageParts(t *testing.T) {
	_body := []byte(`{"provider_id":"local","model":"AUTO","messages":[{"role":"user","content":[{"type":"text","text":"請讀圖"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo=","detail":"auto"}}]}],"attachments":[{"name":"sample.png","mime_type":"image/png","media_type":"image","file_data":"iVBORw0KGgo="}]}`)

	_rewritten, _, _, _err := rewriteRequestModel(_body, "vision-model", false)

	if _err != nil {
		t.Fatalf("rewrite request failed: %v", _err)
	}
	var _payload map[string]interface{}
	if _err := json.Unmarshal(_rewritten, &_payload); _err != nil {
		t.Fatalf("decode rewritten request: %v", _err)
	}
	if _, _ok := _payload["attachments"]; _ok {
		t.Fatalf("attachments should not be forwarded directly: %s", string(_rewritten))
	}
	_messages := _payload["messages"].([]interface{})
	_content := _messages[0].(map[string]interface{})["content"].([]interface{})
	if len(_content) != 2 {
		t.Fatalf("expected text and one image part, got %#v", _content)
	}
}

// -------------------------------------------------------------------------------------
func TestRewriteRequestModelAppendsAttachmentWhenExistingImagePartIsPlaceholder(t *testing.T) {
	_body := []byte(`{"provider_id":"local","model":"AUTO","messages":[{"role":"user","content":[{"type":"text","text":"請讀圖"},{"type":"image_url","image_url":{"url":"attachment://image-1","detail":"auto"}}]}],"attachments":[{"id":"image-1","content":"data:image/png;base64,iVBORw0KGgo="}]}`)

	_rewritten, _, _, _err := rewriteRequestModel(_body, "vision-model", false)

	if _err != nil {
		t.Fatalf("rewrite request failed: %v", _err)
	}
	var _payload map[string]interface{}
	if _err := json.Unmarshal(_rewritten, &_payload); _err != nil {
		t.Fatalf("decode rewritten request: %v", _err)
	}
	if _, _ok := _payload["attachments"]; _ok {
		t.Fatalf("attachments should not be forwarded directly: %s", string(_rewritten))
	}
	_messages := _payload["messages"].([]interface{})
	_content := _messages[0].(map[string]interface{})["content"].([]interface{})
	if len(_content) != 3 {
		t.Fatalf("expected text, placeholder and attachment image parts, got %#v", _content)
	}
	_image := _content[2].(map[string]interface{})
	_imageURL := _image["image_url"].(map[string]interface{})
	if _imageURL["url"] != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("unexpected appended image url: %#v", _imageURL)
	}
}

// -------------------------------------------------------------------------------------
func TestBuildCodexResponsesRequestKeepsImageURLParts(t *testing.T) {
	_request := &domain.ChatCompletionRequest{
		Model: "codex",
		Messages: []domain.ChatMessage{
			{Role: "user", Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "請讀圖"},
				map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,iVBORw0KGgo=", "detail": "auto"}},
			}},
		},
	}

	_payload := buildCodexResponsesRequest(_request, "gpt-5-codex", &domain.LLMProviderConfig{Kind: "openai-codex"})

	if len(_payload.Input) != 1 {
		t.Fatalf("expected one message, got %#v", _payload.Input)
	}
	_parts := _payload.Input[0].Content
	if len(_parts) != 2 {
		t.Fatalf("expected text and image parts, got %#v", _parts)
	}
	if _parts[0].Type != "input_text" || _parts[0].Text != "請讀圖" {
		t.Fatalf("unexpected text part: %#v", _parts[0])
	}
	if _parts[1].Type != "input_image" || _parts[1].ImageURL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("unexpected image part: %#v", _parts[1])
	}
	if !strings.Contains(_payload.Instructions, "attached media") {
		t.Fatalf("expected media boundary instruction, got %q", _payload.Instructions)
	}
}

// -------------------------------------------------------------------------------------
func TestTextCompletionURLFromChatEndpoint(t *testing.T) {
	_url := textCompletionURL(testProviderConfig("https://example.test", "/v1/chat/completions"))
	if _url != "https://example.test/v1/completions" {
		t.Fatalf("text completion url = %s", _url)
	}
}

// -------------------------------------------------------------------------------------
func TestBuildCodexResponsesRequestUsesOutputTextForAssistantHistory(t *testing.T) {
	_request := &domain.ChatCompletionRequest{
		Model: "codex",
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "請檢查服務"},
			{Role: "assistant", Content: "上一輪檢查完成"},
			{Role: "user", Content: "請重新檢查"},
		},
	}

	_payload := buildCodexResponsesRequest(_request, "gpt-5-codex", &domain.LLMProviderConfig{Kind: "openai-codex"})

	if len(_payload.Input) != 3 {
		t.Fatalf("expected three messages, got %#v", _payload.Input)
	}
	_assistant := _payload.Input[1]
	if _assistant.Role != "assistant" {
		t.Fatalf("expected assistant history at index 1, got %#v", _assistant)
	}
	if len(_assistant.Content) != 1 || _assistant.Content[0].Type != "output_text" || _assistant.Content[0].Text != "上一輪檢查完成" {
		t.Fatalf("assistant history must use output_text, got %#v", _assistant.Content)
	}
}

// -------------------------------------------------------------------------------------
func TestBuildCodexResponsesRequestUsesProviderReasoningEffort(t *testing.T) {
	_request := &domain.ChatCompletionRequest{
		Model:    "codex",
		Messages: []domain.ChatMessage{{Role: "user", Content: "請分析"}},
	}

	_defaultPayload := buildCodexResponsesRequest(_request, "gpt-5.5", &domain.LLMProviderConfig{Kind: "openai-codex"})
	if _defaultPayload.Reasoning == nil || _defaultPayload.Reasoning.Effort != "high" {
		t.Fatalf("default reasoning = %#v, want high", _defaultPayload.Reasoning)
	}

	_lowPayload := buildCodexResponsesRequest(_request, "gpt-5.5", &domain.LLMProviderConfig{Kind: "openai-codex", ReasoningEffort: "low"})
	if _lowPayload.Reasoning == nil || _lowPayload.Reasoning.Effort != "low" {
		t.Fatalf("configured reasoning = %#v, want low", _lowPayload.Reasoning)
	}
}

// -------------------------------------------------------------------------------------
func TestBuildCodexResponsesRequestPreservesClientReasoningEffort(t *testing.T) {
	_request := &domain.ChatCompletionRequest{
		Model:           "codex",
		Messages:        []domain.ChatMessage{{Role: "user", Content: "請分析"}},
		ReasoningEffort: "minimal",
	}

	_payload := buildCodexResponsesRequest(_request, "gpt-5.5", &domain.LLMProviderConfig{Kind: "openai-codex", ReasoningEffort: "high"})
	if _payload.Reasoning == nil || _payload.Reasoning.Effort != "minimal" {
		t.Fatalf("client reasoning_effort should be preserved, got %#v", _payload.Reasoning)
	}
}

// -------------------------------------------------------------------------------------
func TestBuildCodexResponsesRequestPreservesClientReasoningObject(t *testing.T) {
	_request := &domain.ChatCompletionRequest{
		Model:     "codex",
		Messages:  []domain.ChatMessage{{Role: "user", Content: "請分析"}},
		Reasoning: map[string]interface{}{"effort": "low"},
	}

	_payload := buildCodexResponsesRequest(_request, "gpt-5.5", &domain.LLMProviderConfig{Kind: "openai-codex", ReasoningEffort: "high"})
	if _payload.Reasoning == nil || _payload.Reasoning.Effort != "low" {
		t.Fatalf("client reasoning object should be preserved, got %#v", _payload.Reasoning)
	}
}

// -------------------------------------------------------------------------------------
func TestBuildCodexResponsesRequestDefaultsInvalidClientReasoningEffortToHigh(t *testing.T) {
	_request := &domain.ChatCompletionRequest{
		Model:           "codex",
		Messages:        []domain.ChatMessage{{Role: "user", Content: "請分析"}},
		ReasoningEffort: "fast",
	}

	_payload := buildCodexResponsesRequest(_request, "gpt-5.5", &domain.LLMProviderConfig{Kind: "openai-codex", ReasoningEffort: "low"})
	if _payload.Reasoning == nil || _payload.Reasoning.Effort != "high" {
		t.Fatalf("invalid client reasoning_effort should default to high, got %#v", _payload.Reasoning)
	}
}

// -------------------------------------------------------------------------------------
func TestBuildCodexResponsesRequestNormalizesModelIDForChatGPTCodexBackend(t *testing.T) {
	_request := &domain.ChatCompletionRequest{
		Model:    "codex",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
	}

	_payload := buildCodexResponsesRequest(_request, "GPT-5.5", &domain.LLMProviderConfig{Kind: "openai-codex"})
	if _payload.Model != "gpt-5.5" {
		t.Fatalf("codex upstream model = %q, want gpt-5.5", _payload.Model)
	}
}

// -------------------------------------------------------------------------------------
func TestBuildCodexResponsesRequestKeepsClientPromptCacheKey(t *testing.T) {
	_request := &domain.ChatCompletionRequest{
		Model:          "AUTO",
		Messages:       []domain.ChatMessage{{Role: "user", Content: "hi"}},
		PromptCacheKey: "lbp-session-key",
	}

	_payload := buildCodexResponsesRequest(_request, "gpt-5.5", &domain.LLMProviderConfig{Kind: "openai-codex"})
	if _payload.PromptCache != "lbp-session-key" {
		t.Fatalf("prompt_cache_key = %q, want lbp-session-key", _payload.PromptCache)
	}
}

// -------------------------------------------------------------------------------------
func TestProviderUsageProbeModelNameNormalizesCodexModelID(t *testing.T) {
	_provider := &domain.LLMProviderConfig{
		Kind: "openai-codex",
		Models: []domain.LLMModelConfig{
			{Name: "GPT-5.5"},
		},
	}

	if _got := providerUsageProbeModelName(_provider); _got != "gpt-5.5" {
		t.Fatalf("codex usage probe model = %q, want gpt-5.5", _got)
	}
}

// -------------------------------------------------------------------------------------
func TestProviderUsageProbeModelNamePreservesNonCodexModelCase(t *testing.T) {
	_provider := &domain.LLMProviderConfig{
		Kind: "custom",
		Models: []domain.LLMModelConfig{
			{Name: "MyLocalModel"},
		},
	}

	if _got := providerUsageProbeModelName(_provider); _got != "MyLocalModel" {
		t.Fatalf("non-codex usage probe model = %q, want MyLocalModel", _got)
	}
}

// -------------------------------------------------------------------------------------
func TestApplyCodexReasoningEffortToResponsesRoutePreservesExistingReasoning(t *testing.T) {
	_route := ResponsesProxyRoute{Method: http.MethodPost, Path: "/v1/responses"}
	_body := []byte(`{"model":"gpt-5.5","reasoning":{"effort":"minimal"},"input":"hi"}`)

	_rewritten, _err := applyCodexReasoningEffortToResponsesRoute(_route, _body, "application/json", &domain.LLMProviderConfig{Kind: "openai-codex", ReasoningEffort: "high"})
	if _err != nil {
		t.Fatal(_err)
	}
	if !strings.Contains(string(_rewritten), `"minimal"`) {
		t.Fatalf("existing reasoning should be preserved, got %s", string(_rewritten))
	}
	if !strings.Contains(string(_rewritten), `"summary":"auto"`) || !strings.Contains(string(_rewritten), `"reasoning.encrypted_content"`) {
		t.Fatalf("Codex reasoning metadata should be added, got %s", string(_rewritten))
	}
}

// -------------------------------------------------------------------------------------
func TestRewriteResponsesCompactRequestUsesSelectedModel(t *testing.T) {
	_route := ResponsesProxyRoute{Method: http.MethodPost, Path: "/v1/responses/compact"}
	_body, _stream, _contentType, _err := rewriteResponsesRouteRequestBody(_route, []byte(`{"model":"AUTO","input":"hi"}`), "application/json", "gpt-5.5", false)
	if _err != nil {
		t.Fatal(_err)
	}
	if _stream || _contentType != "application/json" {
		t.Fatalf("compact rewrite stream/content-type = %v/%q", _stream, _contentType)
	}
	if !strings.Contains(string(_body), `"model":"gpt-5.5"`) {
		t.Fatalf("compact request did not use selected model: %s", string(_body))
	}
}

// -------------------------------------------------------------------------------------
func TestApplyCodexReasoningEffortToResponsesRouteAddsProviderDefault(t *testing.T) {
	_route := ResponsesProxyRoute{Method: http.MethodPost, Path: "/v1/responses"}
	_body := []byte(`{"model":"gpt-5.5","input":"hi"}`)

	_rewritten, _err := applyCodexReasoningEffortToResponsesRoute(_route, _body, "application/json", &domain.LLMProviderConfig{Kind: "openai-codex", ReasoningEffort: "medium"})
	if _err != nil {
		t.Fatal(_err)
	}
	if !strings.Contains(string(_rewritten), `"reasoning"`) || !strings.Contains(string(_rewritten), `"medium"`) {
		t.Fatalf("provider reasoning should be added, got %s", string(_rewritten))
	}
}

// -------------------------------------------------------------------------------------
func TestCopyProviderPassthroughHeadersKeepsOpenAIHeadersOnly(t *testing.T) {
	_src := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_src.Header.Set("OpenAI-Beta", "responses=experimental")
	_src.Header.Set("OpenAI-Organization", "org_123")
	_src.Header.Set("OpenAI-Project", "proj_123")
	_src.Header.Set("Idempotency-Key", "idem_123")
	_src.Header.Set("Session_id", "session_123")
	_src.Header.Set("Conversation_id", "conversation_123")
	_src.Header.Set("Originator", "Codex Desktop")
	_src.Header.Set("User-Agent", "Codex Desktop/26.707")
	_src.Header.Set("Accept-Language", "zh-TW")
	_src.Header.Set("Authorization", "Bearer local-key")
	_target := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)

	copyProviderPassthroughHeaders(_src, _target, "")

	for _, _name := range []string{"OpenAI-Beta", "OpenAI-Organization", "OpenAI-Project", "Idempotency-Key", "Session_id", "Conversation_id", "Originator", "User-Agent", "Accept-Language"} {
		if _target.Header.Get(_name) == "" {
			t.Fatalf("expected %s to be copied", _name)
		}
	}
	if _target.Header.Get("Authorization") != "" {
		t.Fatalf("Authorization should not be copied, got %q", _target.Header.Get("Authorization"))
	}
}

// -------------------------------------------------------------------------------------
func TestApplyCodexUpstreamHeadersPreservesClientIdentity(t *testing.T) {
	_src := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_src.Header.Set("User-Agent", "Codex Desktop/26.707")
	_src.Header.Set("Originator", "Codex Desktop")
	_target := httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)

	applyCodexUpstreamHeaders(_src, _target, true)

	if _got := _target.Header.Get("User-Agent"); _got != "Codex Desktop/26.707" {
		t.Fatalf("User-Agent = %q, want client identity", _got)
	}
	if _got := _target.Header.Get("Originator"); _got != "Codex Desktop" {
		t.Fatalf("Originator = %q, want client identity", _got)
	}
}

// -------------------------------------------------------------------------------------
func TestApplyCodexUpstreamHeadersUsesCompatibleDefaults(t *testing.T) {
	_target := httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)

	applyCodexUpstreamHeaders(nil, _target, true)

	if _got := _target.Header.Get("User-Agent"); _got != defaultCodexUpstreamUserAgent {
		t.Fatalf("User-Agent = %q, want %q", _got, defaultCodexUpstreamUserAgent)
	}
	if _got := _target.Header.Get("Originator"); _got != defaultCodexUpstreamOriginator {
		t.Fatalf("Originator = %q, want %q", _got, defaultCodexUpstreamOriginator)
	}
}

// -------------------------------------------------------------------------------------
func TestApplyCodexUpstreamHeadersUsesDefaultsForInternalBenchmark(t *testing.T) {
	_src := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	_src.Header.Set("User-Agent", "Mozilla/5.0")
	_src.Header.Set("Originator", "browser")
	_target := httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)

	applyCodexUpstreamHeaders(_src, _target, false)

	if _got := _target.Header.Get("User-Agent"); _got != defaultCodexUpstreamUserAgent {
		t.Fatalf("User-Agent = %q, want benchmark default %q", _got, defaultCodexUpstreamUserAgent)
	}
	if _got := _target.Header.Get("Originator"); _got != defaultCodexUpstreamOriginator {
		t.Fatalf("Originator = %q, want benchmark default %q", _got, defaultCodexUpstreamOriginator)
	}
}

// -------------------------------------------------------------------------------------
func TestCopyProviderPassthroughHeadersAppliesDefaultOpenAIBeta(t *testing.T) {
	_target := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)

	copyProviderPassthroughHeaders(nil, _target, "responses=experimental")

	if _got := _target.Header.Get("OpenAI-Beta"); _got != "responses=experimental" {
		t.Fatalf("OpenAI-Beta = %q, want responses=experimental", _got)
	}
}

// -------------------------------------------------------------------------------------
func TestStreamDataMetricsCountsReasoningAndContent(t *testing.T) {
	_line := `data: {"choices":[{"delta":{"reasoning_content":"推理步驟","content":"正式回答","text":"!"}}]}`

	_metrics := streamDataMetrics(_line)

	if !_metrics.ContentSeen {
		t.Fatal("expected stream chunk to be counted as visible content")
	}
	_expectedTokens := estimateTokens("推理步驟正式回答!")
	if _metrics.streamedOutputTokens() != _expectedTokens {
		t.Fatalf("streamed output tokens = %d, want %d", _metrics.streamedOutputTokens(), _expectedTokens)
	}
}

// -------------------------------------------------------------------------------------
func TestClientDeliveryPrefersExactCompletionTokens(t *testing.T) {
	_metrics := ChatMetrics{
		CompletionTokens:   120,
		StreamedHanChars:   90,
		FirstResponseMS:    1000,
		ClientFirstWriteMS: 1000,
		ClientLastWriteMS:  3000,
		ClientContentItems: 10,
		TotalResponseMS:    3000,
	}

	_metrics.finalizeClientDelivery()

	if _metrics.ClientDeliveryTPS != 60 {
		t.Fatalf("client delivery tps = %.1f, want 60.0", _metrics.ClientDeliveryTPS)
	}
}

// -------------------------------------------------------------------------------------
// 上游把整段回應一次沖出時，「首次寫出→末次寫出」只有幾毫秒。時間窗自 TTFT 起算後，
// 這種情況會自然得到正確速率，不需要再切換成另一套公式。
func TestClientDeliveryUsesTTFTAnchoredWindowForBufferedBurst(t *testing.T) {
	_metrics := ChatMetrics{
		CompletionTokens:   60,
		StreamedHanChars:   90,
		ClientFirstWriteMS: 2990,
		ClientLastWriteMS:  3000,
		ClientContentItems: 8,
		TotalResponseMS:    3000,
		FirstResponseMS:    1000,
		GenerationDuration: 2000,
	}

	_metrics.finalizeClientDelivery()

	if _metrics.ClientDeliveryTPS != 30 {
		t.Fatalf("buffered delivery tps = %.1f, want 30.0", _metrics.ClientDeliveryTPS)
	}
}

// -------------------------------------------------------------------------------------
// 舊算法用「首次寫出→末次寫出」當分母，N 個 chunk 只有 N−1 個間隔卻計入全部 token，
// 會讓輸出速度反超生成速度（實際看到 243 生成 / 530 輸出）。
func TestClientDeliveryNeverExceedsGenerationRate(t *testing.T) {
	_metrics := ChatMetrics{
		CompletionTokens:   100,
		FirstResponseMS:    1000,
		ClientFirstWriteMS: 2800,
		ClientLastWriteMS:  3000,
		ClientContentItems: 20,
		TotalResponseMS:    3000,
	}

	_metrics.finalizeClientDelivery()

	_generationRate := _metrics.generationRate()
	if _generationRate <= 0 {
		t.Fatal("generation rate should be positive")
	}
	if _metrics.ClientDeliveryTPS > _generationRate {
		t.Fatalf("delivery tps %.1f must not exceed generation rate %.1f", _metrics.ClientDeliveryTPS, _generationRate)
	}
}

// -------------------------------------------------------------------------------------
// 樣本不足時不得寫入數值，維持既有 EWMA 而不是塞一個跳動的值。
func TestClientDeliverySkipsUndersizedSamples(t *testing.T) {
	_fewChunks := ChatMetrics{
		CompletionTokens: 100, FirstResponseMS: 1000, ClientFirstWriteMS: 1000,
		ClientLastWriteMS: 3000, ClientContentItems: 3, TotalResponseMS: 3000,
	}
	_fewChunks.finalizeClientDelivery()
	if _fewChunks.ClientDeliveryTPS != 0 {
		t.Fatalf("too few chunks should not report a rate, got %.1f", _fewChunks.ClientDeliveryTPS)
	}

	_shortWindow := ChatMetrics{
		CompletionTokens: 100, FirstResponseMS: 1000, ClientFirstWriteMS: 1000,
		ClientLastWriteMS: 1100, ClientContentItems: 20, TotalResponseMS: 1100,
	}
	_shortWindow.finalizeClientDelivery()
	if _shortWindow.ClientDeliveryTPS != 0 {
		t.Fatalf("too short a window should not report a rate, got %.1f", _shortWindow.ClientDeliveryTPS)
	}
}

// -------------------------------------------------------------------------------------
func TestTokenGenerationSpeedPrefersDurationOverProviderTPS(t *testing.T) {
	_metrics := ChatMetrics{
		CompletionTokens:   120,
		GenerationDuration: 3000,
		GenerationTPS:      999,
	}

	if _speed := _metrics.TokenGenerationSpeed(0); _speed != 40 {
		t.Fatalf("generation speed = %.1f, want 40.0", _speed)
	}
}

// -------------------------------------------------------------------------------------
func TestTokenGenerationSpeedFallsBackFromBufferedBurst(t *testing.T) {
	_metrics := ChatMetrics{
		CompletionTokens:   120,
		GenerationDuration: 10,
	}

	if _speed := _metrics.TokenGenerationSpeed(3 * time.Second); _speed != 40 {
		t.Fatalf("buffered generation speed = %.1f, want 40.0", _speed)
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesStreamMetricsCountsOutputAndReasoningDeltas(t *testing.T) {
	_metrics := streamEventMetrics("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"正式回答\"}\n\n")
	_metrics.merge(streamEventMetrics("event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"推理摘要\"}\n\n"))

	if !_metrics.ContentSeen {
		t.Fatal("expected Responses text deltas to be treated as streamed content")
	}
	if _got, _want := _metrics.streamedOutputTokens(), estimateTokens("正式回答推理摘要"); _got != _want {
		t.Fatalf("streamed Responses tokens = %d, want %d", _got, _want)
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesCompletedMetricsReadsNestedUsageWithoutContentEvent(t *testing.T) {
	_event := `event: response.completed
data: {"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":42,"total_tokens":52}}}

`
	_metrics := streamEventMetrics(_event)

	if _metrics.CompletionTokens != 42 {
		t.Fatalf("nested completion tokens = %d, want 42", _metrics.CompletionTokens)
	}
	if _metrics.ContentSeen {
		t.Fatal("response.completed usage must not be treated as a content delta")
	}
}

// -------------------------------------------------------------------------------------
func TestResponseMetricsReadsLlamaCppTimings(t *testing.T) {
	_payload := []byte(`{
		"choices":[{"message":{"content":"hello"}}],
		"usage":{"completion_tokens":128,"prompt_tokens":1024},
		"timings":{
			"prompt_n":1024,
			"prompt_ms":550.01,
			"prompt_per_second":1861.78,
			"predicted_n":128,
			"predicted_ms":3209.63,
			"predicted_per_second":39.88
		}
	}`)

	_metrics := responseMetrics(_payload)
	if _metrics.CompletionTokens != 128 {
		t.Fatalf("completion tokens = %d, want 128", _metrics.CompletionTokens)
	}
	if _metrics.GenerationDuration != 3209.63 {
		t.Fatalf("generation duration = %.2f, want 3209.63", _metrics.GenerationDuration)
	}
	if _metrics.GenerationTPS != 39.88 {
		t.Fatalf("generation TPS = %.2f, want 39.88", _metrics.GenerationTPS)
	}
	if _speed := _metrics.TokenGenerationSpeed(10 * time.Second); math.Abs(_speed-39.88) > 0.01 {
		t.Fatalf("normalized generation speed = %.2f, want 39.88", _speed)
	}
}

// -------------------------------------------------------------------------------------
func TestStreamDataMetricsReadsStandaloneLlamaCppTimings(t *testing.T) {
	_line := `data: {"timings":{"predicted_n":128,"predicted_ms":3209.63,"predicted_per_second":39.88}}`

	_metrics := streamDataMetrics(_line)
	if _metrics.CompletionTokens != 128 {
		t.Fatalf("completion tokens = %d, want 128", _metrics.CompletionTokens)
	}
	if _metrics.GenerationDuration != 3209.63 {
		t.Fatalf("generation duration = %.2f, want 3209.63", _metrics.GenerationDuration)
	}
	if _metrics.GenerationTPS != 39.88 {
		t.Fatalf("generation TPS = %.2f, want 39.88", _metrics.GenerationTPS)
	}
}

// -------------------------------------------------------------------------------------
func TestUsageCompletionTokensDoesNotDoubleCountReasoning(t *testing.T) {
	_payload := map[string]interface{}{
		"usage": map[string]interface{}{
			"output_tokens": float64(100),
			"output_tokens_details": map[string]interface{}{
				"reasoning_tokens": float64(40),
			},
		},
	}
	if _tokens := usageCompletionTokens(_payload); _tokens != 100 {
		t.Fatalf("completion tokens = %d, want 100", _tokens)
	}
}

// -------------------------------------------------------------------------------------
func TestUsageCompletionTokensFallsBackToReasoningOnlyCount(t *testing.T) {
	_payload := map[string]interface{}{
		"usage": map[string]interface{}{
			"output_tokens_details": map[string]interface{}{
				"reasoning_tokens": float64(40),
			},
		},
	}
	if _tokens := usageCompletionTokens(_payload); _tokens != 40 {
		t.Fatalf("completion tokens = %d, want reasoning fallback 40", _tokens)
	}
}

// -------------------------------------------------------------------------------------
func TestStreamCopyStopsOnFinishReasonWithoutProviderEOF(t *testing.T) {
	reader, writer := io.Pipe()
	recorder := httptest.NewRecorder()
	done := make(chan error, 1)

	go func() {
		_, err := streamCopy(recorder, reader, time.Now(), false)
		done <- err
	}()

	_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("streamCopy returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("streamCopy should finish after finish_reason without waiting for provider EOF")
	}
	_ = writer.Close()

	if !strings.Contains(recorder.Body.String(), "data: [DONE]") {
		t.Fatalf("expected [DONE], got %q", recorder.Body.String())
	}
}

// -------------------------------------------------------------------------------------
func TestStreamCopyReadsUsageTimingsAfterFinishReason(t *testing.T) {
	reader, writer := io.Pipe()
	recorder := httptest.NewRecorder()
	type copyResult struct {
		metrics ChatMetrics
		err     error
	}
	done := make(chan copyResult, 1)

	go func() {
		metrics, err := streamCopy(recorder, reader, time.Now(), true)
		done <- copyResult{metrics: metrics, err: err}
	}()

	stream := `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"completion_tokens":128},"timings":{"predicted_n":128,"predicted_ms":3209.63,"predicted_per_second":39.88}}

data: [DONE]

`
	_, _ = writer.Write([]byte(stream))
	_ = writer.Close()

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("streamCopy returned error: %v", result.err)
		}
		if result.metrics.CompletionTokens != 128 {
			t.Fatalf("completion tokens = %d, want 128", result.metrics.CompletionTokens)
		}
		if result.metrics.GenerationDuration != 3209.63 {
			t.Fatalf("generation duration = %.2f, want 3209.63", result.metrics.GenerationDuration)
		}
		if result.metrics.GenerationTPS != 39.88 {
			t.Fatalf("generation TPS = %.2f, want 39.88", result.metrics.GenerationTPS)
		}
	case <-time.After(time.Second):
		t.Fatal("streamCopy should finish after the trailing usage and [DONE] events")
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `"predicted_per_second":39.88`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected timings and [DONE] to be forwarded, got %q", body)
	}
}

// -------------------------------------------------------------------------------------
func TestCodexStreamStopsOnCompletedWithoutProviderEOF(t *testing.T) {
	reader, writer := io.Pipe()
	recorder := httptest.NewRecorder()
	done := make(chan error, 1)

	go func() {
		_, err := streamCodexResponsesAsChat(recorder, reader, "gpt-5", time.Now())
		done <- err
	}()

	completed := `event: response.completed
data: {"type":"response.completed","response":{"model":"gpt-5","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}

`
	_, _ = writer.Write([]byte(completed))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("streamCodexResponsesAsChat returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("codex stream should finish after response.completed without waiting for provider EOF")
	}
	_ = writer.Close()

	body := recorder.Body.String()
	if !strings.Contains(body, `"finish_reason":"stop"`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected finish chunk and [DONE], got %q", body)
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesRawStreamStopsOnCompletedWithoutProviderEOF(t *testing.T) {
	reader, writer := io.Pipe()
	recorder := httptest.NewRecorder()
	done := make(chan error, 1)

	go func() {
		_, err := streamCopyWithResponseRecorder(recorder, reader, time.Now(), true, nil, nil, nil, nil)
		done <- err
	}()

	completed := `event: response.completed
data: {"type":"response.completed","response":{"id":"resp_test","model":"gpt-5"}}

`
	_, _ = writer.Write([]byte(completed))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("streamCopyWithResponseRecorder returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("raw responses stream should finish after response.completed without waiting for provider EOF")
	}
	_ = writer.Close()

	body := recorder.Body.String()
	if !strings.Contains(body, "event: response.completed") {
		t.Fatalf("expected response.completed to be forwarded, got %q", body)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("raw responses stream should not inject chat [DONE], got %q", body)
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesRawStreamBuildsLocalOutputSnapshotFromDeltas(t *testing.T) {
	reader, writer := io.Pipe()
	recorder := httptest.NewRecorder()
	client := NewClient()
	done := make(chan error, 1)

	go func() {
		_, err := streamCopyWithResponseRecorder(recorder, reader, time.Now(), true, func(responseID string, response map[string]interface{}) {
			client.RecordResponseSnapshot(responseID, "provider-1", "gpt-5.5", []interface{}{"hi"}, response)
		}, nil, nil, nil)
		done <- err
	}()

	stream := `event: response.created
data: {"type":"response.created","response":{"id":"resp_snapshot","object":"response","model":"gpt-5.5","output":[]}}

event: response.output_item.added
data: {"type":"response.output_item.added","response_id":"resp_snapshot","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","response_id":"resp_snapshot","output_index":0,"content_index":0,"delta":"main.html"}

event: response.output_text.done
data: {"type":"response.output_text.done","response_id":"resp_snapshot","output_index":0,"content_index":0,"text":"main.html","annotations":[{"type":"url_citation","url":"http://127.0.0.1:10081/main.html"}]}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_snapshot","object":"response","model":"gpt-5.5","output":[]}}

`
	_, _ = writer.Write([]byte(stream))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("streamCopyWithResponseRecorder returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("raw responses stream should finish")
	}
	_ = writer.Close()

	snapshot, ok := client.LookupResponseRoute("resp_snapshot")
	if !ok {
		t.Fatal("expected local response snapshot")
	}
	output := responsesItemsSlice(snapshot.Response["output"])
	if len(output) != 1 {
		t.Fatalf("snapshot output count = %d, want 1: %#v", len(output), snapshot.Response)
	}
	item, ok := output[0].(map[string]interface{})
	if !ok {
		t.Fatalf("snapshot output item has unexpected type: %#v", output[0])
	}
	content := responsesItemsSlice(item["content"])
	if len(content) != 1 {
		t.Fatalf("snapshot content count = %d, want 1: %#v", len(content), item)
	}
	part, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("snapshot content has unexpected type: %#v", content[0])
	}
	if part["text"] != "main.html" {
		t.Fatalf("snapshot text = %#v, want main.html", part["text"])
	}
	if len(responsesItemsSlice(part["annotations"])) != 1 {
		t.Fatalf("snapshot annotations missing: %#v", part)
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesRawStreamReportsCapacityFailure(t *testing.T) {
	reader, writer := io.Pipe()
	recorder := httptest.NewRecorder()
	done := make(chan error, 1)

	go func() {
		_, err := streamCopyWithResponseRecorder(recorder, reader, time.Now(), true, nil, nil, nil, nil)
		done <- err
	}()

	failed := `event: response.failed
data: {"type":"response.failed","response":{"id":"resp_test","error":{"message":"Selected model is at capacity. Please try a different model."}}}

`
	_, _ = writer.Write([]byte(failed))
	var err error
	select {
	case err = <-done:
	case <-time.After(time.Second):
		t.Fatal("raw responses stream should finish after response.failed without waiting for provider EOF")
	}
	_ = writer.Close()

	if err == nil {
		t.Fatal("expected response.failed to return a provider stream error")
	}
	var streamErr *ProviderStreamError
	if !errors.As(err, &streamErr) {
		t.Fatalf("expected ProviderStreamError, got %T: %v", err, err)
	}
	if !streamErr.RetryableCapacity || !streamErr.ResponseForwarded {
		t.Fatalf("unexpected stream error flags: %+v", streamErr)
	}
	if !strings.Contains(recorder.Body.String(), "Selected model is at capacity") {
		t.Fatalf("expected original SSE error to be forwarded, got %q", recorder.Body.String())
	}
}

// -------------------------------------------------------------------------------------
func TestProviderStreamHeartbeatResetsIdleTimeout(t *testing.T) {
	_reader, _writer := io.Pipe()
	_idleReader := newStreamIdleTimeoutReader(_reader, 60*time.Millisecond)
	defer _idleReader.Stop()

	_events := newStreamEventQueue()
	_result := make(chan streamResult, 1)
	_done := make(chan struct{})
	go readProviderStream(_idleReader, time.Now(), true, _events, _result, _done)

	for _index := 0; _index < 4; _index++ {
		_, _ = _writer.Write([]byte("event: ping\ndata: {\"type\":\"ping\"}\n\n"))
		time.Sleep(25 * time.Millisecond)
	}
	_, _ = _writer.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"model\":\"gpt-5.5\"}}\n\n"))

	select {
	case _streamResult := <-_result:
		if _streamResult.Err != nil {
			t.Fatalf("heartbeat should keep provider stream alive: %v", _streamResult.Err)
		}
		if !_streamResult.DoneSeen {
			t.Fatal("expected response.completed after heartbeat events")
		}
	case <-time.After(time.Second):
		t.Fatal("provider stream did not finish")
	}
	_ = _writer.Close()
}

// -------------------------------------------------------------------------------------
func TestDownstreamHeartbeatKeepsSilentStreamOpen(t *testing.T) {
	_reader, _writer := io.Pipe()
	_recorder := httptest.NewRecorder()
	_done := make(chan error, 1)

	go func() {
		_, _err := streamCopyWithResponseRecorderHeartbeat(_recorder, _reader, time.Now(), true, nil, 10*time.Millisecond, nil, nil, nil)
		_done <- _err
	}()

	time.Sleep(35 * time.Millisecond)
	_, _ = _writer.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"model\":\"gpt-5.5\"}}\n\n"))
	_ = _writer.Close()

	select {
	case _err := <-_done:
		if _err != nil {
			t.Fatalf("silent stream heartbeat returned error: %v", _err)
		}
	case <-time.After(time.Second):
		t.Fatal("silent stream did not finish")
	}
	_body := _recorder.Body.String()
	if !strings.Contains(_body, ": keep-alive\n\n") {
		t.Fatalf("downstream heartbeat missing from %q", _body)
	}
	if !strings.Contains(_body, "response.completed") {
		t.Fatalf("provider event missing after heartbeat: %q", _body)
	}
}

func TestResponsesHeartbeatEmitsRealEventNotComment(t *testing.T) {
	_reader, _writer := io.Pipe()
	_recorder := httptest.NewRecorder()
	_done := make(chan error, 1)

	go func() {
		_, _err := streamCopyWithResponseRecorderHeartbeat(_recorder, _reader, time.Now(), true, nil, 10*time.Millisecond, responsesStreamHeartbeat(), nil, responsesStreamFailureTerminal)
		_done <- _err
	}()

	time.Sleep(35 * time.Millisecond)
	_, _ = _writer.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"model\":\"gpt-5.5\"}}\n\n"))
	_ = _writer.Close()

	select {
	case _err := <-_done:
		if _err != nil {
			t.Fatalf("responses heartbeat returned error: %v", _err)
		}
	case <-time.After(time.Second):
		t.Fatal("responses heartbeat stream did not finish")
	}

	_body := _recorder.Body.String()
	// Codex's eventsource parser discards SSE comments, so the keep-alive must be a real
	// dispatched event (has a data: field) to reset the client's idle timer.
	if !strings.Contains(_body, "event: response.ping\ndata: {") {
		t.Fatalf("responses heartbeat should emit a real ping event, got %q", _body)
	}
	if strings.Contains(_body, ": keep-alive\n\n") {
		t.Fatalf("responses heartbeat must not fall back to an SSE comment: %q", _body)
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesTransportFailureAfterContentEmitsErrorTerminal(t *testing.T) {
	_reader, _writer := io.Pipe()
	_recorder := httptest.NewRecorder()
	_done := make(chan error, 1)

	go func() {
		_, _err := streamCopyWithResponseRecorderHeartbeat(_recorder, _reader, time.Now(), true, nil, 0, responsesStreamHeartbeat(), responsesRefusalTerminal, responsesStreamFailureTerminal)
		_done <- _err
	}()

	_, _ = _writer.Write([]byte("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\",\"model\":\"gpt-5.5\"}}\n\n"))
	_, _ = _writer.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_test\",\"delta\":\"partial output\"}\n\n"))
	_upstreamErr := errors.New("stream ID 373; INTERNAL_ERROR; received from peer")
	_ = _writer.CloseWithError(_upstreamErr)

	select {
	case _err := <-_done:
		if _err == nil || !ResponseAlreadyForwarded(_err) {
			t.Fatalf("transport failure after content should return a forwarded stream error: %v", _err)
		}
	case <-time.After(time.Second):
		t.Fatal("transport failure stream did not terminate")
	}

	_body := _recorder.Body.String()
	if !strings.Contains(_body, "event: error") || !strings.Contains(_body, "INTERNAL_ERROR") {
		t.Fatalf("expected the upstream transport error to reach the client, got %q", _body)
	}
	if strings.Contains(_body, "data: [DONE]") {
		t.Fatalf("Responses transport failure must not use the Chat API terminator: %q", _body)
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesMissingTerminalAfterContentEmitsErrorTerminal(t *testing.T) {
	_reader, _writer := io.Pipe()
	_recorder := httptest.NewRecorder()
	_done := make(chan error, 1)

	go func() {
		_, _err := streamCopyWithResponseRecorderHeartbeat(_recorder, _reader, time.Now(), true, nil, 0, responsesStreamHeartbeat(), responsesRefusalTerminal, responsesStreamFailureTerminal)
		_done <- _err
	}()

	_, _ = _writer.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_test\",\"delta\":\"partial output\"}\n\n"))
	_ = _writer.Close()

	select {
	case _err := <-_done:
		if _err == nil || !ResponseAlreadyForwarded(_err) {
			t.Fatalf("missing terminal after content should return a forwarded stream error: %v", _err)
		}
	case <-time.After(time.Second):
		t.Fatal("missing-terminal stream did not terminate")
	}

	_body := _recorder.Body.String()
	if !strings.Contains(_body, "event: error") || !strings.Contains(_body, "closed before a Responses terminal event") {
		t.Fatalf("expected a deterministic Responses error terminal, got %q", _body)
	}
	if strings.Contains(_body, "data: [DONE]") {
		t.Fatalf("Responses premature EOF must not use the Chat API terminator: %q", _body)
	}
}

func TestResponsesEarlyFailureConvertedToGracefulCompletion(t *testing.T) {
	_reader, _writer := io.Pipe()
	_recorder := httptest.NewRecorder()
	_done := make(chan error, 1)

	go func() {
		_, _err := streamCopyWithResponseRecorderHeartbeat(_recorder, _reader, time.Now(), true, nil, 0, responsesStreamHeartbeat(), responsesRefusalTerminal, responsesStreamFailureTerminal)
		_done <- _err
	}()

	// Upstream refuses before any content (OpenAI content-policy style).
	_, _ = _writer.Write([]byte("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"This content was flagged for possible cybersecurity risk.\"}}}\n\n"))
	_ = _writer.Close()

	select {
	case _err := <-_done:
		if _err == nil || !IsUpstreamRequestRejected(_err) || !ResponseAlreadyForwarded(_err) {
			t.Fatalf("early rejection should be forwarded without penalizing provider: %v", _err)
		}
	case <-time.After(time.Second):
		t.Fatal("refusal conversion did not finish")
	}

	_body := _recorder.Body.String()
	if !strings.Contains(_body, "event: response.created") {
		t.Fatalf("expected a complete Responses sequence, got %q", _body)
	}
	if !strings.Contains(_body, "event: response.completed") {
		t.Fatalf("expected a graceful response.completed, got %q", _body)
	}
	if !strings.Contains(_body, "flagged for possible cybersecurity risk") {
		t.Fatalf("expected the upstream reason to be surfaced, got %q", _body)
	}
	if strings.Contains(_body, "response.failed") {
		t.Fatalf("raw response.failed must NOT be forwarded (client would spin), got %q", _body)
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesUntypedErrorConvertedToGracefulCompletion(t *testing.T) {
	_reader, _writer := io.Pipe()
	_recorder := httptest.NewRecorder()
	_done := make(chan error, 1)

	go func() {
		_, _err := streamCopyWithResponseRecorderHeartbeat(_recorder, _reader, time.Now(), true, nil, 0, responsesStreamHeartbeat(), responsesRefusalTerminal, responsesStreamFailureTerminal)
		_done <- _err
	}()

	_, _ = _writer.Write([]byte("data: {\"error\":{\"message\":\"Request blocked by content policy.\"}}\n\n"))

	select {
	case _err := <-_done:
		if _err == nil || !IsUpstreamRequestRejected(_err) {
			t.Fatalf("untyped rejection should be surfaced as a handled rejection: %v", _err)
		}
	case <-time.After(time.Second):
		t.Fatal("untyped rejection conversion did not finish")
	}
	_ = _writer.Close()

	_body := _recorder.Body.String()
	if !strings.Contains(_body, "response.completed") || !strings.Contains(_body, "Request blocked by content policy") {
		t.Fatalf("untyped rejection was not delivered to the client: %q", _body)
	}
	if strings.Contains(_body, "data: [DONE]") {
		t.Fatalf("Responses rejection must not use the Chat API terminator: %q", _body)
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesCapacityFailureRemainsRetryableBeforeFirstToken(t *testing.T) {
	_reader, _writer := io.Pipe()
	_recorder := httptest.NewRecorder()
	_done := make(chan error, 1)

	go func() {
		_, _err := streamCopyWithResponseRecorderHeartbeat(_recorder, _reader, time.Now(), true, nil, 0, responsesStreamHeartbeat(), responsesRefusalTerminal, responsesStreamFailureTerminal)
		_done <- _err
	}()

	_, _ = _writer.Write([]byte("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"Selected model is at capacity. Please try again later.\"}}}\n\n"))

	select {
	case _err := <-_done:
		if _err == nil || !IsRetryableCapacityError(_err) {
			t.Fatalf("capacity failure should remain retryable: %v", _err)
		}
		if ResponseAlreadyForwarded(_err) {
			t.Fatalf("capacity failure must remain replaceable before the first token: %v", _err)
		}
	case <-time.After(time.Second):
		t.Fatal("capacity failure did not terminate promptly")
	}
	_ = _writer.Close()

	if _body := _recorder.Body.String(); _body != "" {
		t.Fatalf("capacity failure should not be written before provider failover, got %q", _body)
	}
}

// -------------------------------------------------------------------------------------
func TestStreamEventActivityTypeIncludesHeartbeat(t *testing.T) {
	_eventType, _ok := streamEventActivityType("event: heartbeat\ndata: {\"type\":\"heartbeat\"}\n\n")
	if !_ok || _eventType != "heartbeat" {
		t.Fatalf("heartbeat activity = %q, %t; want heartbeat, true", _eventType, _ok)
	}

	if _eventType, _ok := streamEventActivityType(": comment only\n\n"); !_ok || _eventType != "comment" {
		t.Fatalf("comment-only activity = %q, %t; want comment, true", _eventType, _ok)
	}
}

// -------------------------------------------------------------------------------------
func contains(_text string, _pattern string) bool {
	return strings.Contains(_text, _pattern)
}

// -------------------------------------------------------------------------------------
func testProviderConfig(_baseURL string, _chatPath string) *domain.LLMProviderConfig {
	return &domain.LLMProviderConfig{BaseURL: _baseURL, ChatCompletionsPath: _chatPath}
}

// -------------------------------------------------------------------------------------
func TestStreamDataMetricsCountsNestedReasoningAndText(t *testing.T) {
	_line := `data: {"choices":[{"delta":{"reasoning":{"text":"think"},"content":[{"type":"text","text":"answer"}]}}]}`

	_metrics := streamDataMetrics(_line)

	if !_metrics.ContentSeen {
		t.Fatal("expected nested reasoning and content to be counted as visible content")
	}
	_expectedTokens := estimateTokens("thinkanswer")
	if _metrics.streamedOutputTokens() != _expectedTokens {
		t.Fatalf("streamed output tokens = %d, want %d", _metrics.streamedOutputTokens(), _expectedTokens)
	}
}

// -------------------------------------------------------------------------------------
// 綁定數只計 prompt-cache 命名空間：response id 每輪都會新增一筆，計進去會失真。
func TestPromptCacheRouteCountsOnlyCountsConversations(t *testing.T) {
	_client := NewClient()
	_client.RecordPromptCacheRoute(PromptCacheRouteID("conv-1"), "provider-a", "m", "owner")
	_client.RecordPromptCacheRoute(PromptCacheRouteID("conv-2"), "provider-a", "m", "owner")
	_client.RecordPromptCacheRoute(PromptCacheRouteID("conv-3"), "provider-b", "m", "owner")
	// 同一段對話重複記錄不應重複計數。
	_client.RecordPromptCacheRoute(PromptCacheRouteID("conv-1"), "provider-a", "m", "owner")
	// 一般 response id 不算綁定。
	_client.RecordResponseRoute("resp_1", "provider-a", "m")
	_client.RecordResponseRoute("resp_2", "provider-a", "m")

	_counts := _client.PromptCacheRouteCounts()
	if _counts["provider-a"] != 2 {
		t.Fatalf("provider-a bound = %d, want 2", _counts["provider-a"])
	}
	if _counts["provider-b"] != 1 {
		t.Fatalf("provider-b bound = %d, want 1", _counts["provider-b"])
	}
	if _counts["provider-c"] != 0 {
		t.Fatalf("unknown provider bound = %d, want 0", _counts["provider-c"])
	}
}

// -------------------------------------------------------------------------------------
// 上游的一般性暫時錯誤（訊息本身就寫著可重試）應歸類為可重試，
// 但內容政策拒絕必須維持不可重試 —— 換帳號重試也只會再被擋一次。
func TestRetryableUpstreamFailureClassification(t *testing.T) {
	_retryable := []string{
		"An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID abc.",
		"internal server error",
		"Please try again later",
	}
	for _, _text := range _retryable {
		if !providerErrorTextIsRetryableUpstreamFailure(_text) {
			t.Fatalf("should be retryable: %q", _text)
		}
	}

	_notRetryable := []string{
		"This content was flagged for possible cybersecurity risk.",
		"request blocked by content policy",
		"",
	}
	for _, _text := range _notRetryable {
		if providerErrorTextIsRetryableUpstreamFailure(_text) {
			t.Fatalf("must not be retryable: %q", _text)
		}
	}
}

// -------------------------------------------------------------------------------------
// 上游思考很久、最後一次沖出整段回應時，牆鐘窗會極短（此例 45ms/108 tokens ≈ 2400 tok/s）。
// 那量到的是 socket flush 而非生成速率，必須退回整段請求時間，
// 且不得因為 provider 有自報時間就跳過這個保護。
func TestGenerationSpeedIgnoresBufferedBurstEvenWithProviderTiming(t *testing.T) {
	_metrics := ChatMetrics{
		CompletionTokens: 108,
		FirstResponseMS:  5000,
		TotalResponseMS:  5045,
		ProviderTiming:   true,
		GenerationTPS:    2400,
	}

	_speed := _metrics.TokenGenerationSpeed(5045 * time.Millisecond)
	if _speed > 100 {
		t.Fatalf("generation speed = %.1f tok/s, want the end-to-end rate (~21), not the flush burst", _speed)
	}
	_expected := 108.0 / 5.045
	if math.Abs(_speed-_expected) > 0.5 {
		t.Fatalf("generation speed = %.2f, want ≈ %.2f", _speed, _expected)
	}
}

// -------------------------------------------------------------------------------------
// 只發工具呼叫的 turn，「給人看的文字」必須是 0 —— 這是分辨
// 「模型在做事」與「模型只是在轉包工作」的依據，且完全不依賴工具名稱。
func TestStreamPartsSeparateProseFromToolCalls(t *testing.T) {
	_parse := func(_raw string) streamTextParts {
		var _payload map[string]interface{}
		if _err := json.Unmarshal([]byte(_raw), &_payload); _err != nil {
			t.Fatalf("bad fixture: %v", _err)
		}
		return streamResponseParts(_payload)
	}

	_tool := _parse(`{"type":"response.function_call_arguments.delta","delta":"{\"command\":\"ls -la\"}"}`)
	if _tool.Prose != "" || _tool.Reasoning != "" || _tool.Tool == "" {
		t.Fatalf("tool call must not count as prose: %#v", _tool)
	}

	_text := _parse(`{"type":"response.output_text.delta","delta":"答案是這個"}`)
	if _text.Prose == "" || _text.Tool != "" {
		t.Fatalf("output text must count as prose: %#v", _text)
	}

	_reasoning := _parse(`{"type":"response.reasoning_summary_text.delta","delta":"considering options"}`)
	if _reasoning.Reasoning == "" || _reasoning.Prose != "" {
		t.Fatalf("reasoning must not count as prose: %#v", _reasoning)
	}
}

// -------------------------------------------------------------------------------------
// 三段相加必須等於原本 streamResponseText 的結果，否則既有的
// 字元數與 token 估算會跟著跑掉。
func TestStreamPartsPreserveTotalText(t *testing.T) {
	for _, _raw := range []string{
		`{"type":"response.output_text.delta","delta":"hello"}`,
		`{"type":"response.function_call_arguments.delta","delta":"{\"a\":1}"}`,
		`{"choices":[{"delta":{"content":"hi","reasoning_content":"because"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"id":"c1","function":{"arguments":"{}"}}]}}]}`,
	} {
		var _payload map[string]interface{}
		if _err := json.Unmarshal([]byte(_raw), &_payload); _err != nil {
			t.Fatalf("bad fixture: %v", _err)
		}
		_parts := streamResponseParts(_payload)
		if _combined := _parts.Prose + _parts.Reasoning + _parts.Tool; _combined != streamResponseText(_payload) {
			t.Fatalf("parts must recombine into the original text: %q != %q", _combined, streamResponseText(_payload))
		}
	}
}

// -------------------------------------------------------------------------------------
// 工具呼叫計數用字尾比對，上游新增型別時不必跟著改，也不依賴工具名稱。
func TestToolCallCountIsNameAgnostic(t *testing.T) {
	for _, _itemType := range []string{"function_call", "local_shell_call", "custom_tool_call", "mcp_call", "some_future_call"} {
		var _payload map[string]interface{}
		_raw := `{"type":"response.output_item.added","item":{"type":"` + _itemType + `"}}`
		if _err := json.Unmarshal([]byte(_raw), &_payload); _err != nil {
			t.Fatalf("bad fixture: %v", _err)
		}
		if _parts := streamResponseParts(_payload); _parts.ToolCalls != 1 {
			t.Fatalf("%s should count as one tool call, got %d", _itemType, _parts.ToolCalls)
		}
	}

	// 非 _call 結尾的項目不能被算進去。
	var _message map[string]interface{}
	_ = json.Unmarshal([]byte(`{"type":"response.output_item.added","item":{"type":"message"}}`), &_message)
	if _parts := streamResponseParts(_message); _parts.ToolCalls != 0 {
		t.Fatalf("message items must not count as tool calls: %d", _parts.ToolCalls)
	}
}

// -------------------------------------------------------------------------------------
// 非串流回應原本完全沒有分類資料，導致文字比恆為 0，
// 和「真的沒有產出文字」分不開。這裡釘住三種形狀都要拆得出來。
func TestNonStreamingResponsesCarryProseSplit(t *testing.T) {
	_chat := responseMetrics([]byte(`{
		"choices":[{"message":{"content":"這是回答","reasoning_content":"想一下",
			"tool_calls":[{"id":"c1","function":{"arguments":"{\"path\":\"/tmp\"}"}}]}}],
		"usage":{"completion_tokens":120}
	}`))
	if _chat.ProseTokens() <= 0 || _chat.ReasoningTokens() <= 0 || _chat.ToolTokens() <= 0 {
		t.Fatalf("chat completions split missing: %+v", _chat)
	}
	if _chat.ToolCallCount != 1 {
		t.Fatalf("chat tool call count = %d", _chat.ToolCallCount)
	}

	// Responses API：output 陣列依項目型別分流
	_responses := responseMetrics([]byte(`{
		"output":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"checking the tree"}]},
			{"type":"local_shell_call","arguments":"{\"command\":\"ls -la\"}"},
			{"type":"message","content":[{"type":"output_text","text":"目錄列出來了"}]}
		],
		"usage":{"output_tokens":90}
	}`))
	if _responses.ProseTokens() <= 0 || _responses.ReasoningTokens() <= 0 || _responses.ToolTokens() <= 0 {
		t.Fatalf("responses split missing: %+v", _responses)
	}
	if _responses.ToolCallCount != 1 {
		t.Fatalf("responses tool call count = %d", _responses.ToolCallCount)
	}

	// 純工具呼叫：文字必須是 0，但整體仍有分類資料
	_toolOnly := responseMetrics([]byte(`{
		"output":[{"type":"local_shell_call","arguments":"{\"command\":\"ls\"}"}],
		"usage":{"output_tokens":20}
	}`))
	if _toolOnly.ProseTokens() != 0 {
		t.Fatalf("tool-only turn must have no prose: %+v", _toolOnly)
	}
	if _toolOnly.ToolTokens() <= 0 {
		t.Fatalf("tool-only turn should still record tool tokens: %+v", _toolOnly)
	}
}

// -------------------------------------------------------------------------------------
// 推理量必須用上游回報的精確值。字元估算取代不了它：
// provider 多半不串流推理內容，就算開了摘要，摘要也比實際推理量小一個數量級。
func TestReasoningTokensPreferReportedValue(t *testing.T) {
	_reported := responseMetrics([]byte(`{
		"output":[{"type":"local_shell_call","arguments":"{\"command\":\"ls\"}"}],
		"usage":{"output_tokens":1589,"output_tokens_details":{"reasoning_tokens":1400}}
	}`))
	if _reported.ReasoningTokens() != 1400 {
		t.Fatalf("reported reasoning should win: %d", _reported.ReasoningTokens())
	}
	// 沒有任何推理文字被串流，字元估算會是 0 —— 這正是不能拿它當分母的理由。
	if estimateTokensFromCounts(_reported.ReasoningHanChars, _reported.ReasoningOtherChars) != 0 {
		t.Fatalf("no reasoning text was streamed in this fixture")
	}

	// 上游沒回報時才退回字元估算
	_estimated := responseMetrics([]byte(`{
		"output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking it through"}]}],
		"usage":{"output_tokens":40}
	}`))
	if _estimated.ReasoningTokens() <= 0 {
		t.Fatalf("should fall back to the character estimate: %+v", _estimated)
	}
}

// -------------------------------------------------------------------------------------
// usage 在串流裡通常只出現一次且是總量，合併時相加會重複計算。
func TestReportedReasoningMergesByMaxNotSum(t *testing.T) {
	_total := ChatMetrics{}
	_total.merge(ChatMetrics{ReportedReasoningTokens: 0})
	_total.merge(ChatMetrics{ReportedReasoningTokens: 1400})
	_total.merge(ChatMetrics{ReportedReasoningTokens: 1400})
	if _total.ReportedReasoningTokens != 1400 {
		t.Fatalf("reasoning tokens should not accumulate across events: %d", _total.ReportedReasoningTokens)
	}
}

// -------------------------------------------------------------------------------------
// 上游想很久、最後把整段一次沖出來時，生成窗量到的是 socket flush 而不是生成速率。
// 毫秒下限擋不住這種情況（500ms 的窗會通過），必須改看事件數。
func TestGenerationSpeedIgnoresBurstFlushWindow(t *testing.T) {
	_burst := ChatMetrics{
		CompletionTokens:      280,
		FirstResponseMS:       12000,
		TotalResponseMS:       12500,
		ProviderContentEvents: 2,
	}
	_speed := _burst.TokenGenerationSpeed(12500 * time.Millisecond)
	if _speed > 30 {
		t.Fatalf("burst flush must not report a generation rate: %.1f tok/s", _speed)
	}

	// 逐字串流的窗是可信的，不能被這個守衛壓掉。
	_steady := ChatMetrics{
		CompletionTokens:      280,
		FirstResponseMS:       800,
		TotalResponseMS:       12500,
		ProviderContentEvents: 140,
	}
	if _speed := _steady.TokenGenerationSpeed(12500 * time.Millisecond); _speed < 20 || _speed > 30 {
		t.Fatalf("steady streaming should keep its own window: %.1f tok/s", _speed)
	}

	// 非串流回應沒有事件計數，維持原本行為。
	_nonStreaming := ChatMetrics{CompletionTokens: 280, FirstResponseMS: 800, TotalResponseMS: 12500}
	if _speed := _nonStreaming.TokenGenerationSpeed(12500 * time.Millisecond); _speed <= 0 {
		t.Fatalf("non-streaming responses must still report a speed: %.1f", _speed)
	}
}

// -------------------------------------------------------------------------------------
// 工具呼叫參數是輸出的一部分，帶內容的事件要計數 ——
// 首字時間與生成窗都依賴它。
func TestToolCallDeltaCountsAsContentEvent(t *testing.T) {
	_metrics := streamDataMetrics(`data: {"choices":[{"delta":{"tool_calls":[{"id":"c1","function":{"arguments":"{\"path\":\"/tmp\"}"}}]}}]}`)
	if !_metrics.ContentSeen {
		t.Fatalf("tool call arguments are output and must count as content")
	}
	if _metrics.ProviderContentEvents != 1 {
		t.Fatalf("content event should be counted once, got %d", _metrics.ProviderContentEvents)
	}

	_empty := streamDataMetrics(`data: {"choices":[{"delta":{}}]}`)
	if _empty.ProviderContentEvents != 0 {
		t.Fatalf("empty delta must not count: %d", _empty.ProviderContentEvents)
	}
}

// -------------------------------------------------------------------------------------
// 上游沒送 terminal 就關掉串流時，先前 _err 是 nil，等於把截斷的串流回報成成功：
// 呼叫端不會換 provider 重試，provider 健康度也被記成成功。
func TestTerminallessStreamIsReportedAsFailure(t *testing.T) {
	_copy := func(_body string) (ChatMetrics, error) {
		return streamCopyWithProviderIdleTimeout(
			httptest.NewRecorder(), io.NopCloser(strings.NewReader(_body)),
			time.Now(), false, nil, nil, nil, nil, nil,
		)
	}

	// 一個字都沒送出去：可重試，換 provider 對使用者無痕。
	_, _empty := _copy("")
	if _empty == nil {
		t.Fatalf("an empty stream must not be reported as success")
	}
	if !IsTruncatedStreamError(_empty) {
		t.Fatalf("should be flagged as truncated: %v", _empty)
	}
	if ResponseAlreadyForwarded(_empty) {
		t.Fatalf("nothing was forwarded, so a retry must still be possible")
	}

	// 已經送出內容：仍是失敗（要計入 provider 健康度），但不能再重試。
	_, _partial := _copy("data: {\"choices\":[{\"delta\":{\"content\":\"半句話\"}}]}\n\n")
	if _partial == nil {
		t.Fatalf("a truncated stream must not be reported as success")
	}
	if !ResponseAlreadyForwarded(_partial) {
		t.Fatalf("content already reached the client; retrying would duplicate it")
	}

	// 正常結束不受影響。
	_, _complete := _copy("data: {\"choices\":[{\"delta\":{\"content\":\"完整\"}}]}\n\ndata: [DONE]\n\n")
	if _complete != nil {
		t.Fatalf("a properly terminated stream must stay successful: %v", _complete)
	}
}
