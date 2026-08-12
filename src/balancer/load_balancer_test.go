package balancer

import (
	"errors"
	"net/http"
	"strconv"
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
	if _meta.CandidateCount != 2 {
		t.Fatalf("candidate count = %d, want both providers", _meta.CandidateCount)
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
