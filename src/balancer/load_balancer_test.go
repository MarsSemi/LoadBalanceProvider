package balancer

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
func TestAllCapacityUnavailableProvidersReturnTemporaryOverload(t *testing.T) {
	_balancer := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers: []domain.LLMProviderConfig{
			testSessionProvider("provider-a"),
			testSessionProvider("provider-b"),
		},
	})
	for _, _provider := range _balancer.Providers {
		_provider.MarkTemporaryUnavailable(time.Millisecond, 30*time.Second)
		if _provider.CircuitOpen(time.Now()) {
			t.Fatal("temporary capacity exhaustion must not open the failure circuit")
		}
		if _provider.ConsecutiveFailures != 0 {
			t.Fatalf("temporary capacity exhaustion changed consecutive failures to %d", _provider.ConsecutiveFailures)
		}
	}

	_, _, _, _, _err := _balancer.Select(&domain.ChatCompletionRequest{
		Model:    "AUTO",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hello"}},
	})
	var _selectionErr *NoAvailableProviderError
	if !errors.As(_err, &_selectionErr) || !_selectionErr.TemporaryOverload {
		t.Fatalf("error = %#v, want temporary overload", _err)
	}
	if _seconds := SelectionRetryAfterSeconds(_err); _seconds < 1 || _seconds > 30 {
		t.Fatalf("Retry-After = %d, want 1..30", _seconds)
	}
}

// -------------------------------------------------------------------------------------
func TestUnavailableProviderKeepsGenericErrorWhenFailureIsNotCapacity(t *testing.T) {
	_balancer := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{testSessionProvider("provider-a")},
	})
	for _idx := 0; _idx < 3; _idx++ {
		_balancer.Providers[0].MarkFailure(time.Millisecond)
	}

	_, _, _, _, _err := _balancer.Select(&domain.ChatCompletionRequest{
		Model:    "AUTO",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if _err == nil || _err.Error() != "no available provider can handle this request" {
		t.Fatalf("error = %#v, want generic unavailable provider", _err)
	}
	if _seconds := SelectionRetryAfterSeconds(_err); _seconds != 0 {
		t.Fatalf("Retry-After = %d, want 0", _seconds)
	}
}

// -------------------------------------------------------------------------------------
func TestExplicitUnknownModelKeepsProviderCandidatesAndPassesThrough(t *testing.T) {
	_providerA := testSessionProvider("provider-a")
	_providerA.Kind = "openai-codex"
	_providerA.Type = "openai-codex"
	_providerB := testSessionProvider("provider-b")
	_providerB.Kind = "openai-codex"
	_providerB.Type = "openai-codex"
	_balancer := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{_providerA, _providerB},
	})
	_selected, _model, _, _meta, _err := _balancer.Select(&domain.ChatCompletionRequest{
		Model:    "gpt-5.6-luna",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if _err != nil {
		t.Fatal(_err)
	}
	if _selected == nil || _model == nil || _model.Name != "gpt-5.6-luna" {
		t.Fatalf("explicit model was not passed through: provider=%v model=%v", _selected, _model)
	}
	if _model.QualityTier != 4 {
		t.Fatalf("explicit Luna quality tier = %d, want 4", _model.QualityTier)
	}
	if _meta.CandidateCount != 2 {
		t.Fatalf("candidate count = %d, want both providers", _meta.CandidateCount)
	}
}

// -------------------------------------------------------------------------------------
func TestExplicitCodexVariantQualityTiers(t *testing.T) {
	_provider := testSessionProvider("provider-a")
	_provider.Kind = "openai-codex"
	_provider.Type = "openai-codex"

	_tests := []struct {
		model string
		want  int
	}{
		{model: "GPT-5.6-Sol", want: 8},
		{model: "gpt-5.6-terra", want: 6},
		{model: "gpt-5.6_luna", want: 4},
		{model: "gpt-5.5", want: 7},
	}
	for _, _test := range _tests {
		if _got := explicitModelQualityTier(&_provider, _test.model, 7); _got != _test.want {
			t.Errorf("explicitModelQualityTier(%q) = %d, want %d", _test.model, _got, _test.want)
		}
	}
}

// -------------------------------------------------------------------------------------
func TestExplicitUnknownModelStillRejectsNonCodexProvider(t *testing.T) {
	_balancer := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{testSessionProvider("provider-a")},
	})
	_, _, _, _, _err := _balancer.Select(&domain.ChatCompletionRequest{
		Model:    "gpt-5.6-luna",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if _err == nil {
		t.Fatal("non-Codex provider accepted an unconfigured explicit model")
	}
}

// -------------------------------------------------------------------------------------
func TestAutoStillUsesProviderDefaultModel(t *testing.T) {
	_balancer := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{testSessionProvider("provider-a")},
	})
	_, _model, _, _, _err := _balancer.Select(&domain.ChatCompletionRequest{
		Model:    "AUTO",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if _err != nil {
		t.Fatal(_err)
	}
	if _model == nil || _model.Name != "gpt-test" {
		t.Fatalf("AUTO selected model = %#v, want provider default gpt-test", _model)
	}
}

// -------------------------------------------------------------------------------------
func testSessionProvider(_id string) domain.LLMProviderConfig {
	return domain.LLMProviderConfig{
		ID:            _id,
		Name:          _id,
		Kind:          "openai",
		Type:          "openai",
		BaseURL:       "https://example.com",
		Enabled:       true,
		MaxConcurrent: 4,
		Models: []domain.LLMModelConfig{{
			Name:            "gpt-test",
			Aliases:         []string{"AUTO"},
			MaxInputTokens:  1048576,
			MaxOutputTokens: 262144,
			Capabilities:    []string{"chat", "reasoning"},
		}},
	}
}

// -------------------------------------------------------------------------------------
func TestResponsesRequirementDoesNotFallbackToChatCapability(t *testing.T) {
	_chatOnly := domain.LLMModelConfig{Name: "chat-only", Capabilities: []string{"chat"}}
	if modelSatisfiesHardRequirements(&_chatOnly, []string{"responses"}) {
		t.Fatal("chat-only model should not satisfy responses requirement")
	}

	_responses := domain.LLMModelConfig{Name: "responses", Capabilities: []string{"responses"}}
	if !modelSatisfiesHardRequirements(&_responses, []string{"responses"}) {
		t.Fatal("responses model should satisfy responses requirement")
	}
}

// -------------------------------------------------------------------------------------
func TestModelHardRequirements(t *testing.T) {
	_model := domain.LLMModelConfig{
		Name:            "multimodal",
		MaxInputTokens:  1048576,
		MaxOutputTokens: 262144,
		Capabilities:    []string{"vision", "tools", "json_mode", "responses"},
	}
	for _, _requirement := range []string{"vision", "tools", "json_mode", "responses", "long_context"} {
		if !modelSatisfiesHardRequirements(&_model, []string{_requirement}) {
			t.Fatalf("model should satisfy %s", _requirement)
		}
	}
	if modelSatisfiesHardRequirements(&_model, []string{"audio_generation"}) {
		t.Fatal("model without audio generation capability was accepted")
	}
}

// -------------------------------------------------------------------------------------
func TestProviderRuntimeRecordsRateLimitUsagePercent(t *testing.T) {
	_runtime := &ProviderRuntime{}
	_headers := http.Header{}
	_headers.Set("X-RateLimit-Limit-Requests", "100")
	_headers.Set("X-RateLimit-Remaining-Requests", "25")
	_headers.Set("X-RateLimit-Limit-Tokens", "1,000")
	_headers.Set("X-RateLimit-Remaining-Tokens", "400")

	_runtime.RecordUsageHeaders(_headers)
	_snapshot := _runtime.UsageSnapshot()

	if _snapshot.RequestUsagePercent != 75 || _snapshot.RequestRemainingPercent != 25 {
		t.Fatalf("request usage = %.1f / %.1f", _snapshot.RequestUsagePercent, _snapshot.RequestRemainingPercent)
	}
	if _snapshot.TokenUsagePercent != 60 || _snapshot.TokenRemainingPercent != 40 {
		t.Fatalf("token usage = %.1f / %.1f", _snapshot.TokenUsagePercent, _snapshot.TokenRemainingPercent)
	}
	if _snapshot.OverallUsagePercent() != 75 || _snapshot.OverallRemainingPercent() != 25 {
		t.Fatalf("overall usage = %.1f / %.1f", _snapshot.OverallUsagePercent(), _snapshot.OverallRemainingPercent())
	}
}

// -------------------------------------------------------------------------------------
func TestProviderRuntimeRecordsCodexUsagePercent(t *testing.T) {
	_runtime := &ProviderRuntime{}
	_headers := http.Header{}
	_headers.Set("X-Codex-Primary-Used-Percent", "20.5")
	_headers.Set("X-Codex-Secondary-Used-Percent", "0")

	_runtime.RecordUsageHeaders(_headers)
	_snapshot := _runtime.UsageSnapshot()

	if _snapshot.CodexPrimaryUsedPercent != 20.5 || _snapshot.CodexPrimaryRemainPercent != 79.5 {
		t.Fatalf("primary usage = %.1f / %.1f", _snapshot.CodexPrimaryUsedPercent, _snapshot.CodexPrimaryRemainPercent)
	}
	if _snapshot.CodexSecondaryUsedPercent != 0 || _snapshot.CodexSecondaryRemainPercent != 100 {
		t.Fatalf("secondary usage = %.1f / %.1f", _snapshot.CodexSecondaryUsedPercent, _snapshot.CodexSecondaryRemainPercent)
	}
}

// -------------------------------------------------------------------------------------
func TestProviderRuntimeClearsLastReactionWhenCurrentRequestHasNoFirstToken(t *testing.T) {
	_runtime := &ProviderRuntime{}

	_runtime.MarkSuccessWithMetrics(10*time.Second, 0, 3000, 0, 0)
	_, _, _, _reactionEWMA, _lastReaction, _, _, _ := _runtime.MetricsSnapshot()
	if _lastReaction != 3000 {
		t.Fatalf("last reaction after measured request = %.0f, want 3000", _lastReaction)
	}
	if _reactionEWMA <= 0 {
		t.Fatalf("reaction EWMA after measured request = %.0f, want positive", _reactionEWMA)
	}

	_runtime.MarkSuccessWithMetrics(5*time.Second, 0, 0, 0, 0)
	_, _, _lastDuration, _reactionEWMA, _lastReaction, _, _, _ := _runtime.MetricsSnapshot()
	if _lastDuration != 5000 {
		t.Fatalf("last duration = %.0f, want 5000", _lastDuration)
	}
	if _lastReaction != 0 {
		t.Fatalf("last reaction after unmeasured request = %.0f, want 0", _lastReaction)
	}
	if _reactionEWMA <= 0 {
		t.Fatalf("reaction EWMA should be retained for historical scoring, got %.0f", _reactionEWMA)
	}
}

// -------------------------------------------------------------------------------------
func TestScoreCandidatePrefersLowerProviderUsage(t *testing.T) {
	_balancer := NewLoadBalancer(&domain.ProxyConfig{})
	_model := &domain.LLMModelConfig{Name: "model", Capabilities: []string{"chat"}, QualityTier: 5, CostTier: 1, MaxInputTokens: 8192, MaxOutputTokens: 2048}
	_profile := domain.RequestProfile{TaskType: "chat", EstimatedInputTokens: 200, RequestedOutputTokens: 200, ComplexityScore: 3}
	_lowUsage := testRuntimeWithUsage("low", 90)
	_highUsage := testRuntimeWithUsage("high", 5)

	_lowScore := _balancer.scoreCandidate(_lowUsage, _model, _profile, "AUTO")
	_highScore := _balancer.scoreCandidate(_highUsage, _model, _profile, "AUTO")

	if _lowScore <= _highScore {
		t.Fatalf("low usage score %.2f should be greater than high usage score %.2f", _lowScore, _highScore)
	}
}

// -------------------------------------------------------------------------------------
func TestProviderUsageExhaustedForSelection(t *testing.T) {
	_rateLimited := &ProviderRuntime{}
	_rateHeaders := http.Header{}
	_rateHeaders.Set("X-RateLimit-Limit-Requests", "100")
	_rateHeaders.Set("X-RateLimit-Remaining-Requests", "5")
	_rateLimited.RecordUsageHeaders(_rateHeaders)
	if !_rateLimited.UsageSnapshot().ExhaustedForSelection() {
		t.Fatal("provider with 5% request remaining should be excluded from selection")
	}

	_codexFiveHourLimited := &ProviderRuntime{}
	_codexFiveHourHeaders := http.Header{}
	_codexFiveHourHeaders.Set("X-Codex-Primary-Used-Percent", "95")
	_codexFiveHourLimited.RecordUsageHeaders(_codexFiveHourHeaders)
	if !_codexFiveHourLimited.UsageSnapshot().ExhaustedForSelection() {
		t.Fatal("provider with 5% codex primary remaining should be excluded from selection")
	}

	_codexSevenDayLimited := &ProviderRuntime{}
	_codexSevenDayHeaders := http.Header{}
	_codexSevenDayHeaders.Set("X-Codex-Secondary-Used-Percent", "95")
	_codexSevenDayLimited.RecordUsageHeaders(_codexSevenDayHeaders)
	if !_codexSevenDayLimited.UsageSnapshot().ExhaustedForSelection() {
		t.Fatal("provider with 5% codex secondary remaining should be excluded from selection")
	}

	_unknown := ProviderUsageSnapshot{}
	if _unknown.ExhaustedForSelection() {
		t.Fatal("provider without usage info should not be excluded from selection")
	}
}

// -------------------------------------------------------------------------------------
func TestCandidateModelsUsesRequestedAliasAsUpstreamModel(t *testing.T) {
	_provider := &domain.LLMProviderConfig{
		ID:      "local",
		Name:    "Local",
		Kind:    "llamacpp",
		Enabled: true,
		Models: []domain.LLMModelConfig{
			{
				Name:            "gemma-default.gguf",
				Aliases:         []string{"ggml-org/gemma-4-E4B-it-GGUF"},
				MaxInputTokens:  1024,
				MaxOutputTokens: 1024,
			},
		},
	}

	_models := candidateModels(_provider, "ggml-org/gemma-4-E4B-it-GGUF")
	if len(_models) != 1 {
		t.Fatalf("candidate model count = %d, want 1", len(_models))
	}
	if _models[0].Name != "ggml-org/gemma-4-E4B-it-GGUF" {
		t.Fatalf("candidate model name = %q, want requested alias", _models[0].Name)
	}
	if !_models[0].MatchName("gemma-default.gguf") {
		t.Fatalf("candidate alias should still match original configured model")
	}
}

// -------------------------------------------------------------------------------------
func testRuntimeWithUsage(_id string, _remainingRequests int) *ProviderRuntime {
	_runtime := &ProviderRuntime{
		Config: &domain.LLMProviderConfig{
			ID:            _id,
			Name:          _id,
			Enabled:       true,
			Weight:        10,
			Priority:      1,
			MaxConcurrent: 4,
		},
	}
	_headers := http.Header{}
	_headers.Set("X-RateLimit-Limit-Requests", "100")
	_headers.Set("X-RateLimit-Remaining-Requests", strconv.Itoa(_remainingRequests))
	_runtime.RecordUsageHeaders(_headers)
	return _runtime
}

// -------------------------------------------------------------------------------------
// 對話黏著的保險：被釘住的 provider 若可用量落後同儕平均超過容忍值，就該放棄黏著。
func TestQuotaBelowPeerAverageDetectsImbalance(t *testing.T) {
	_usage := func(_remainingPercent float64) ProviderUsageSnapshot {
		return ProviderUsageSnapshot{
			UpdatedAt:               time.Now(),
			LimitRequests:           "1000",
			RemainingRequests:       strconv.Itoa(int(_remainingPercent * 10)),
			RequestUsagePercent:     100 - _remainingPercent,
			RequestRemainingPercent: _remainingPercent,
		}
	}

	_balancer := NewLoadBalancer(&domain.ProxyConfig{Providers: []domain.LLMProviderConfig{
		{ID: "drained", Enabled: true},
		{ID: "fresh-1", Enabled: true},
		{ID: "fresh-2", Enabled: true},
	}})
	for _, _provider := range _balancer.ProvidersSnapshot() {
		switch _provider.Config.ID {
		case "drained":
			_provider.Usage = _usage(50)
		default:
			_provider.Usage = _usage(80)
		}
	}

	// drained 只剩 50%，同儕平均 80% → 落後 30 個百分點，超過容忍值 10。
	if !_balancer.QuotaBelowPeerAverage("drained", 10) {
		t.Fatal("drained provider should be reported as below peer average")
	}
	if _balancer.QuotaBelowPeerAverage("drained", 40) {
		t.Fatal("a 40 point tolerance should still accept the drained provider")
	}
	if _balancer.QuotaBelowPeerAverage("fresh-1", 10) {
		t.Fatal("a balanced provider must not be reported as imbalanced")
	}
	if _balancer.QuotaBelowPeerAverage("unknown-provider", 10) {
		t.Fatal("unknown provider must not trigger the escape hatch")
	}
}

// -------------------------------------------------------------------------------------
// 不同 provider 家族的配額不可互相比較，否則本機 llama.cpp 也會影響 Codex 帳號黏著。
func TestQuotaBelowPeerAverageIgnoresUnrelatedProviderFamilies(t *testing.T) {
	_usage := func(_remainingPercent float64) ProviderUsageSnapshot {
		return ProviderUsageSnapshot{
			UpdatedAt:               time.Now(),
			LimitRequests:           "1000",
			RemainingRequests:       strconv.Itoa(int(_remainingPercent * 10)),
			RequestUsagePercent:     100 - _remainingPercent,
			RequestRemainingPercent: _remainingPercent,
		}
	}
	_balancer := NewLoadBalancer(&domain.ProxyConfig{Providers: []domain.LLMProviderConfig{
		{ID: "codex-a", Kind: "openai-codex", Enabled: true},
		{ID: "codex-b", Kind: "openai-codex", Enabled: true},
		{ID: "local", Kind: "llama.cpp", Enabled: true},
	}})
	for _, _provider := range _balancer.ProvidersSnapshot() {
		switch _provider.Config.ID {
		case "codex-a", "codex-b":
			_provider.Usage = _usage(50)
		case "local":
			_provider.Usage = _usage(100)
		}
	}
	if _balancer.QuotaBelowPeerAverage("codex-a", 10) {
		t.Fatal("an unrelated provider family must not force Codex affinity to drop")
	}
}

// -------------------------------------------------------------------------------------
// 對話綁定上限：已達上限的 provider 不再接新對話，但已釘住的請求不受影響，
// 且在完全沒有其他候選時仍可動用（不能把請求逼成沒有 provider 可用）。
func TestConversationBindingCapAvoidsSaturatedProvider(t *testing.T) {
	_balancer := NewLoadBalancer(&domain.ProxyConfig{Providers: []domain.LLMProviderConfig{
		{ID: "busy", Enabled: true, BaseURL: "http://busy", MaxConcurrent: 16,
			Models: []domain.LLMModelConfig{{Name: "m", MaxInputTokens: 100000, MaxOutputTokens: 8192}}},
		{ID: "free", Enabled: true, BaseURL: "http://free", MaxConcurrent: 16,
			Models: []domain.LLMModelConfig{{Name: "m", MaxInputTokens: 100000, MaxOutputTokens: 8192}}},
	}})
	_balancer.SetConversationBindings(map[string]int{"busy": 4}, 4)

	_request := &domain.ChatCompletionRequest{Model: "AUTO", Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}}}
	for _idx := 0; _idx < 20; _idx++ {
		_target, _, _, _, _err := _balancer.Select(_request)
		if _err != nil {
			t.Fatalf("select failed: %v", _err)
		}
		if _target.Config.ID != "free" {
			t.Fatalf("saturated provider must not take new conversations, got %s", _target.Config.ID)
		}
	}

	// 明確指定已滿的 provider（對話黏著）仍必須選得到
	_pinned := &domain.ChatCompletionRequest{Model: "AUTO", ProviderID: "busy", Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}}}
	_target, _, _, _, _err := _balancer.Select(_pinned)
	if _err != nil || _target.Config.ID != "busy" {
		t.Fatalf("pinned request must still reach the saturated provider: target=%v err=%v", _target, _err)
	}

	// 全部都滿的時候不能變成「沒有可用 provider」
	_balancer.SetConversationBindings(map[string]int{"busy": 9, "free": 9}, 4)
	if _, _, _, _, _err := _balancer.Select(_request); _err != nil {
		t.Fatalf("cap must not starve selection when every provider is saturated: %v", _err)
	}
}

// -------------------------------------------------------------------------------------
func TestConversationBindingCapDisabledWhenLimitUnset(t *testing.T) {
	_balancer := NewLoadBalancer(&domain.ProxyConfig{Providers: []domain.LLMProviderConfig{
		{ID: "only", Enabled: true, BaseURL: "http://only", MaxConcurrent: 16,
			Models: []domain.LLMModelConfig{{Name: "m", MaxInputTokens: 100000, MaxOutputTokens: 8192}}},
	}})
	_balancer.SetConversationBindings(map[string]int{"only": 99}, 0)

	_request := &domain.ChatCompletionRequest{Model: "AUTO", Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}}}
	if _, _, _, _, _err := _balancer.Select(_request); _err != nil {
		t.Fatalf("limit 0 should disable the cap: %v", _err)
	}
}

// -------------------------------------------------------------------------------------
// 等級上限要真的排除高階模型，但在沒有任何合格模型時必須放行 ——
// 降級是政策，不該讓請求變成「無 provider 可用」。
func TestMaxQualityTierCapsSelection(t *testing.T) {
	_tiered := func(_id string, _tier int) domain.LLMProviderConfig {
		_provider := testSessionProvider(_id)
		_provider.Models[0].QualityTier = _tier
		_provider.Models[0].CostTier = _tier / 2
		return _provider
	}

	_both := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{_tiered("strong", 8), _tiered("cheap", 4)},
	})
	_req := &domain.ChatCompletionRequest{
		Messages:       []domain.ChatMessage{{Role: "user", Content: "hi"}},
		MaxQualityTier: 4,
	}
	for _i := 0; _i < 20; _i++ {
		_target, _model, _, _, _err := _both.Select(_req)
		if _err != nil {
			t.Fatalf("selection failed: %v", _err)
		}
		if _model.QualityTier > 4 {
			t.Fatalf("tier cap was ignored: provider=%s tier=%d", _target.Config.ID, _model.QualityTier)
		}
	}

	// 只剩高階模型時仍要能選出來，而不是回錯誤。
	_only := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{_tiered("strong", 8)},
	})
	_target, _model, _, _, _err := _only.Select(_req)
	if _err != nil {
		t.Fatalf("cap must degrade, not break: %v", _err)
	}
	if _target == nil || _model == nil || _model.QualityTier != 8 {
		t.Fatalf("expected the over-tier provider as a last resort, got %+v", _model)
	}
}

// -------------------------------------------------------------------------------------
// 可用量見底的 provider 不能被硬排除：多數帳號同時見底時，流量會全擠到少數
// 幾個上，併發一滿就選不到 provider，使用者看到的是斷線。
func TestExhaustedProvidersAreLastResortNotExcluded(t *testing.T) {
	_drain := func(_balancer *LoadBalancer, _id string) {
		for _, _provider := range _balancer.Providers {
			if _provider.Config.ID != _id {
				continue
			}
			_headers := http.Header{}
			_headers.Set("X-RateLimit-Limit-Requests", "1000")
			_headers.Set("X-RateLimit-Remaining-Requests", "20")
			_provider.RecordUsageHeaders(_headers)
		}
	}
	_req := &domain.ChatCompletionRequest{Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}}}

	// 還有健康的 provider 時，見底的那個不該被選到。
	_mixed := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{testSessionProvider("healthy"), testSessionProvider("drained")},
	})
	_drain(_mixed, "drained")
	for _i := 0; _i < 30; _i++ {
		_target, _, _, _, _err := _mixed.Select(_req)
		if _err != nil {
			t.Fatalf("selection failed: %v", _err)
		}
		if _target.Config.ID == "drained" {
			t.Fatalf("a drained provider must not be preferred while a healthy one exists")
		}
	}

	// 全部見底時仍要選得出來，而不是回錯誤讓客戶端斷線重連。
	_allDrained := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{testSessionProvider("drained")},
	})
	_drain(_allDrained, "drained")
	if _allDrained.Providers[0].UsageSnapshot().ExhaustedForSelection() != true {
		t.Fatalf("fixture should be exhausted: %+v", _allDrained.Providers[0].UsageSnapshot())
	}
	_target, _, _, _, _err := _allDrained.Select(_req)
	if _err != nil {
		t.Fatalf("exhausted providers must still be selectable as a last resort: %v", _err)
	}
	if _target == nil || _target.Config.ID != "drained" {
		t.Fatalf("expected the drained provider, got %+v", _target)
	}
}

// -------------------------------------------------------------------------------------
// 退讓過的選擇必須留下痕跡，否則「請求為什麼落在見底的 provider 上」事後追不出來。
func TestFallbackSelectionRecordsItsReason(t *testing.T) {
	_drain := func(_balancer *LoadBalancer, _id string) {
		for _, _provider := range _balancer.Providers {
			if _provider.Config.ID != _id {
				continue
			}
			_headers := http.Header{}
			_headers.Set("X-RateLimit-Limit-Requests", "1000")
			_headers.Set("X-RateLimit-Remaining-Requests", "20")
			_provider.RecordUsageHeaders(_headers)
		}
	}
	_req := &domain.ChatCompletionRequest{Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}}}

	// 正常選擇不得帶退讓訊息，否則訊號會被雜訊淹沒。
	_healthy := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{testSessionProvider("healthy")},
	})
	if _, _, _, _meta, _err := _healthy.Select(_req); _err != nil {
		t.Fatalf("selection failed: %v", _err)
	} else if strings.Contains(_meta.Reason, "fallback") {
		t.Fatalf("a normal selection must not be marked as a fallback: %q", _meta.Reason)
	}

	// 只剩見底的 provider：選得出來，而且要說明原因。
	_drained := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{testSessionProvider("drained")},
	})
	_drain(_drained, "drained")
	_, _, _, _meta, _err := _drained.Select(_req)
	if _err != nil {
		t.Fatalf("exhausted providers must remain selectable: %v", _err)
	}
	if !strings.Contains(_meta.Reason, "exhausted quota") {
		t.Fatalf("fallback reason missing from selection meta: %q", _meta.Reason)
	}
}

// -------------------------------------------------------------------------------------
// 配額見底不該把既有對話趕走：5% 是新對話的選擇保留量，不是「不能用」。
// 用門檻趕走對話的代價是每輪重建脈絡，那些重複工作反而讓配額掉得更快。
func TestExhaustedProviderStaysPinnable(t *testing.T) {
	_balancer := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{testSessionProvider("healthy"), testSessionProvider("drained")},
	})
	_drained := _balancer.Providers[1]
	_headers := http.Header{}
	_headers.Set("X-RateLimit-Limit-Requests", "1000")
	_headers.Set("X-RateLimit-Remaining-Requests", "20")
	_drained.RecordUsageHeaders(_headers)

	if !_drained.UsageSnapshot().ExhaustedForSelection() {
		t.Fatalf("fixture should be exhausted: %+v", _drained.UsageSnapshot())
	}
	if !_balancer.ProviderAvailableForSelection("drained") {
		t.Fatalf("low quota alone must not evict an existing conversation")
	}

	// 但「確定不能用」的狀態仍要放棄黏著 —— 那是證據而不是門檻。
	atomic.StoreInt64(&_drained.CircuitOpenUntil, time.Now().Add(time.Minute).UnixNano())
	if _balancer.ProviderAvailableForSelection("drained") {
		t.Fatalf("an open circuit must still drop the pin")
	}
}

// -------------------------------------------------------------------------------------
// 見底的 provider 仍然只在沒有其他候選時才會被選中（新對話的保留量照舊生效）。
func TestExhaustedProviderStillLastForNewConversations(t *testing.T) {
	_balancer := NewLoadBalancer(&domain.ProxyConfig{
		SelectionStrategy: "weighted_score",
		Providers:         []domain.LLMProviderConfig{testSessionProvider("healthy"), testSessionProvider("drained")},
	})
	_headers := http.Header{}
	_headers.Set("X-RateLimit-Limit-Requests", "1000")
	_headers.Set("X-RateLimit-Remaining-Requests", "20")
	_balancer.Providers[1].RecordUsageHeaders(_headers)

	_req := &domain.ChatCompletionRequest{Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}}}
	for _i := 0; _i < 30; _i++ {
		_target, _, _, _, _err := _balancer.Select(_req)
		if _err != nil {
			t.Fatalf("selection failed: %v", _err)
		}
		if _target.Config.ID == "drained" {
			t.Fatalf("new conversations must still avoid a drained provider")
		}
	}
}
