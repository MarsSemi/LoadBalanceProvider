package api

import (
	"testing"
	"time"

	"LoadBalanceProvider/src/config"
	"LoadBalanceProvider/src/domain"
	"LoadBalanceProvider/src/keyusage"
)

// -------------------------------------------------------------------------------------
func demotionSettings() domain.AdvancedSettingsConfig {
	_settings := config.DefaultAdvancedSettingsConfig()
	_settings.LowReasoningDemotionEnabled = true
	_settings.LowReasoningDemotionMinDailyUsagePercent = 18
	_settings.LowReasoningDemotionRequestsPerMin = 8
	_settings.LowReasoningDemotionReasoningPercent = 10
	_settings.LowReasoningDemotionTargetTier = 4
	_settings.LowReasoningDemotionMinutes = 10
	return _settings
}

// -------------------------------------------------------------------------------------
// quotaAt 產生一個「今日已用 n%」的讀取器。
func quotaAt(_percent float64) dailyUsageReader {
	return func() (float64, bool) { return _percent, true }
}

// -------------------------------------------------------------------------------------
// 兩個條件必須同時成立才降級。
func TestDemotionRequiresBothFrequencyAndLowReasoning(t *testing.T) {
	_base := keyusage.RequestDensity{PerMinute: 12, ReasoningRatio: 0.04, ReasoningSamples: 20}
	if !shouldDemoteForLowReasoning(_base, demotionSettings()) {
		t.Fatalf("high frequency with low reasoning should demote: %+v", _base)
	}

	_slow := _base
	_slow.PerMinute = 3
	if shouldDemoteForLowReasoning(_slow, demotionSettings()) {
		t.Fatalf("low frequency must not demote")
	}

	_thinking := _base
	_thinking.ReasoningRatio = 0.42
	if shouldDemoteForLowReasoning(_thinking, demotionSettings()) {
		t.Fatalf("a key that is genuinely reasoning must not be demoted")
	}
}

// -------------------------------------------------------------------------------------
// 這是最重要的守衛：不回報推理量的模型，推理比恆為 0。
// 少了它，所有非推理模型的高頻金鑰都會被無條件降級。
func TestDemotionSkipsKeysWithoutReasoningReports(t *testing.T) {
	_unreported := keyusage.RequestDensity{PerMinute: 40, ReasoningRatio: 0, ReasoningSamples: 0}
	if shouldDemoteForLowReasoning(_unreported, demotionSettings()) {
		t.Fatalf("no reasoning reports means 'cannot measure', not 'needs no reasoning'")
	}

	// 樣本太少同樣不判斷。
	_thin := keyusage.RequestDensity{PerMinute: 40, ReasoningRatio: 0, ReasoningSamples: demotionMinReasoningSamples - 1}
	if shouldDemoteForLowReasoning(_thin, demotionSettings()) {
		t.Fatalf("too few reasoning samples to judge: %+v", _thin)
	}

	// 有足夠回報、而且推理確實很低時才成立。
	_measured := keyusage.RequestDensity{PerMinute: 40, ReasoningRatio: 0, ReasoningSamples: demotionMinReasoningSamples}
	if !shouldDemoteForLowReasoning(_measured, demotionSettings()) {
		t.Fatalf("measured zero reasoning should demote: %+v", _measured)
	}
}

// -------------------------------------------------------------------------------------
// 關掉開關時完全不作用。
func TestDemotionDisabledBySwitch(t *testing.T) {
	_settings := demotionSettings()
	_settings.LowReasoningDemotionEnabled = false
	_tracker := &demotionTracker{states: map[string]demotionState{}, lastEval: map[string]time.Time{}}
	if _tier := _tracker.MaxQualityTierForKey("key-a", _settings, quotaAt(50)); _tier != 0 {
		t.Fatalf("switch off must yield no cap, got %d", _tier)
	}
}

// -------------------------------------------------------------------------------------
// 降級期間維持同一個上限，時間到才解除 —— 解除必須是時間驅動，
// 否則降級後的模型推理本來就少，指標永遠不會回升。
func TestDemotionHoldsThenExpires(t *testing.T) {
	_settings := demotionSettings()
	_tracker := &demotionTracker{
		states:   map[string]demotionState{"key-a": {Until: time.Now().Add(time.Minute), Tier: 4}},
		lastEval: map[string]time.Time{},
	}
	if _tier := _tracker.MaxQualityTierForKey("key-a", _settings, quotaAt(50)); _tier != 4 {
		t.Fatalf("active demotion should report its tier, got %d", _tier)
	}

	_tracker.states["key-a"] = demotionState{Until: time.Now().Add(-time.Minute), Tier: 4}
	if _tier := _tracker.MaxQualityTierForKey("key-a", _settings, quotaAt(50)); _tier != 0 {
		t.Fatalf("expired demotion must be released, got %d", _tier)
	}
	if _, _ok := _tracker.states["key-a"]; _ok {
		t.Fatalf("expired state should be cleared")
	}
}

// -------------------------------------------------------------------------------------
// 啟動條件：配額還充裕時完全不偵測。大量消耗本身不是問題。
func TestDemotionGatedByDailyQuotaUsage(t *testing.T) {
	_settings := demotionSettings()

	if dailyUsageGatePassed(_settings, quotaAt(17.9)) {
		t.Fatalf("below the gate must not start detecting")
	}
	if !dailyUsageGatePassed(_settings, quotaAt(18)) {
		t.Fatalf("reaching the gate should start detecting")
	}

	// 今天還沒有觀測時不能當成 0% —— 那是「還不知道」。
	_unknown := dailyUsageReader(func() (float64, bool) { return 0, false })
	if dailyUsageGatePassed(_settings, _unknown) {
		t.Fatalf("unknown usage must not pass the gate")
	}
	if dailyUsageGatePassed(_settings, nil) {
		t.Fatalf("a missing reader must not pass the gate")
	}

	// 門檻設 0 表示不設限，此時即使沒有觀測也照常偵測。
	_ungated := _settings
	_ungated.LowReasoningDemotionMinDailyUsagePercent = 0
	if !dailyUsageGatePassed(_ungated, nil) {
		t.Fatalf("a zero gate should disable the check entirely")
	}
}

// -------------------------------------------------------------------------------------
// 閘門未過時，即使頻率與推理都符合也不降級。
func TestDemotionSkippedWhileQuotaIsPlentiful(t *testing.T) {
	_settings := demotionSettings()
	_tracker := &demotionTracker{states: map[string]demotionState{}, lastEval: map[string]time.Time{}}
	if _tier := _tracker.MaxQualityTierForKey("key-a", _settings, quotaAt(5)); _tier != 0 {
		t.Fatalf("plentiful quota should suppress demotion, got %d", _tier)
	}
	if len(_tracker.states) != 0 {
		t.Fatalf("no demotion state should be recorded")
	}
}
