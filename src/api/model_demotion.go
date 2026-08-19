package api

import (
	"log"
	"strings"
	"sync"
	"time"

	"LoadBalanceProvider/src/domain"
	"LoadBalanceProvider/src/keyusage"
)

// -------------------------------------------------------------------------------------
// 低推理降級：請求頻率高、而模型幾乎沒有在推理時，暫時把該金鑰壓到低階模型。
//
// 依據是「模型自己的難度評估」—— 推理量是模型對任務有多難的判斷，
// 比任何我們自己寫的啟發式都準。推理少代表這個工作量不需要強模型。
const (
	// 評估用的觀察窗。刻意放到十五分鐘：短暫的密集操作要能自然過去，
	// 使用者有十五分鐘的緩衝把節奏降下來，只有持續維持高頻才會被降級。
	demotionEvalWindow = 15 * time.Minute
	// 同一支金鑰的重新評估間隔。每個請求都重算密度太貴，而降級本來就不需要即時。
	demotionEvalInterval = 10 * time.Second
	// 有回報推理的樣本數下限。樣本太少時的比例沒有意義，寧可不判斷。
	demotionMinReasoningSamples = 5
)

// -------------------------------------------------------------------------------------
type demotionState struct {
	Until time.Time
	Tier  int
}

// -------------------------------------------------------------------------------------
type demotionTracker struct {
	lock     sync.Mutex
	states   map[string]demotionState
	lastEval map[string]time.Time
}

var _defaultDemotionTracker = &demotionTracker{
	states:   map[string]demotionState{},
	lastEval: map[string]time.Time{},
}

// -------------------------------------------------------------------------------------
// MaxQualityTierForKey 回傳這支金鑰目前該套用的模型等級上限，0 表示不限。
//
// 解除降級是時間驅動而不是指標驅動：降級後的模型推理本來就少，
// 若用指標決定何時解除，指標永遠不會回升，金鑰會被永久留在低階模型上。
// 時間到就放回去重新量，才觀察得到「他現在是不是需要強模型了」。
func (_t *demotionTracker) MaxQualityTierForKey(_keyID string, _settings domain.AdvancedSettingsConfig, _dailyUsage dailyUsageReader) int {
	_keyID = strings.TrimSpace(_keyID)
	if _t == nil || _keyID == "" || !_settings.LowReasoningDemotionEnabled {
		return 0
	}

	_now := time.Now()

	_t.lock.Lock()
	if _state, _ok := _t.states[_keyID]; _ok {
		if _now.Before(_state.Until) {
			_tier := _state.Tier
			_t.lock.Unlock()
			return _tier
		}
		delete(_t.states, _keyID)
	}
	if _last, _ok := _t.lastEval[_keyID]; _ok && _now.Sub(_last) < demotionEvalInterval {
		_t.lock.Unlock()
		return 0
	}
	_t.lastEval[_keyID] = _now
	_t.lock.Unlock()

	// 啟動條件先看：配額還很充裕時完全不偵測。
	// 大量消耗本身不是問題 —— 只有在配額開始吃緊時，把資源用在雜事上才需要處理。
	if !dailyUsageGatePassed(_settings, _dailyUsage) {
		return 0
	}
	if !shouldDemoteForLowReasoning(keyusage.DefaultRecorder().RequestDensity(_keyID, demotionEvalWindow), _settings) {
		return 0
	}

	_tier := _settings.LowReasoningDemotionTargetTier
	_until := _now.Add(time.Duration(_settings.LowReasoningDemotionMinutes) * time.Minute)

	_t.lock.Lock()
	_t.states[_keyID] = demotionState{Until: _until, Tier: _tier}
	_t.lock.Unlock()

	log.Printf("low-reasoning demotion applied: key=%s tier<=%d until=%s", _keyID, _tier, _until.Format(time.RFC3339))
	return _tier
}

// -------------------------------------------------------------------------------------
// dailyUsageReader 回傳今日已消耗的配額百分比，以及「今天有沒有觀測資料」。
type dailyUsageReader func() (float64, bool)

// -------------------------------------------------------------------------------------
// dailyUsageGatePassed 判斷今日配額消耗是否已達啟動門檻。
//
// 沒有觀測資料時一律不通過：那是「還不知道」而不是 0%。若當成 0 處理，
// 服務剛啟動、或上游不回報配額時，門檻會形同虛設而讓偵測全面放行。
func dailyUsageGatePassed(_settings domain.AdvancedSettingsConfig, _dailyUsage dailyUsageReader) bool {
	if _settings.LowReasoningDemotionMinDailyUsagePercent <= 0 {
		return true
	}
	if _dailyUsage == nil {
		return false
	}
	_usage, _known := _dailyUsage()
	if !_known {
		return false
	}
	return _usage >= _settings.LowReasoningDemotionMinDailyUsagePercent
}

// -------------------------------------------------------------------------------------
// shouldDemoteForLowReasoning 判斷密度資料是否同時滿足「高頻率」與「低推理」。
func shouldDemoteForLowReasoning(_density keyusage.RequestDensity, _settings domain.AdvancedSettingsConfig) bool {
	// 上游沒回報推理量時，推理比會是 0 —— 那是「量不到」不是「不需要」。
	// 少了這個守衛，所有非推理模型的金鑰都會被無條件降級。
	if _density.ReasoningSamples < demotionMinReasoningSamples {
		return false
	}
	if _density.PerMinute < _settings.LowReasoningDemotionRequestsPerMin {
		return false
	}
	return _density.ReasoningRatio*100 < _settings.LowReasoningDemotionReasoningPercent
}

// -------------------------------------------------------------------------------------
// ClearDemotion 讓設定變更或測試可以立即解除既有的降級。
func (_t *demotionTracker) ClearDemotion(_keyID string) {
	if _t == nil {
		return
	}
	_t.lock.Lock()
	defer _t.lock.Unlock()
	if _keyID = strings.TrimSpace(_keyID); _keyID == "" {
		_t.states = map[string]demotionState{}
		_t.lastEval = map[string]time.Time{}
		return
	}
	delete(_t.states, _keyID)
	delete(_t.lastEval, _keyID)
}

// -------------------------------------------------------------------------------------
// DemotedKeyCount 回傳目前仍在降級中的金鑰數，供狀態顯示使用。
func (_t *demotionTracker) DemotedKeyCount() int {
	if _t == nil {
		return 0
	}
	_now := time.Now()
	_t.lock.Lock()
	defer _t.lock.Unlock()
	_count := 0
	for _, _state := range _t.states {
		if _now.Before(_state.Until) {
			_count++
		}
	}
	return _count
}
