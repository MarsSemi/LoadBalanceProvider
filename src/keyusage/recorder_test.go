package keyusage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordUsesCacheAndFlushPersists(t *testing.T) {
	_root := t.TempDir()
	_recorder := NewRecorder(_root)
	_at := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.Local)
	if _err := _recorder.Record("key-1", _at); _err != nil {
		t.Fatal(_err)
	}
	if _err := _recorder.Record("key-1", _at); _err != nil {
		t.Fatal(_err)
	}
	_stats, _err := _recorder.LoadMonth("key-1", "2026-07")
	if _err != nil {
		t.Fatal(_err)
	}
	if _stats.Total != 2 {
		t.Fatalf("cached total = %d, want 2", _stats.Total)
	}
	if _err := _recorder.Flush(); _err != nil {
		t.Fatal(_err)
	}
	_reloaded, _err := NewRecorder(_root).LoadMonth("key-1", "2026-07")
	if _err != nil {
		t.Fatal(_err)
	}
	if _reloaded.Total != 2 {
		t.Fatalf("persisted total = %d, want 2", _reloaded.Total)
	}
}

// -------------------------------------------------------------------------------------
// 向下相容 1：沒有任何分級資料時，每月 JSON 不得多出欄位（舊版讀取者完全不受影響）。
func TestMonthFileOmitsComplexityWhenUnused(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_at := time.Date(2026, 8, 12, 10, 30, 0, 0, time.Local)
	for _idx := 0; _idx < 3; _idx++ {
		if _err := _recorder.Record("key-a", _at); _err != nil {
			t.Fatal(_err)
		}
	}
	if _err := _recorder.Flush(); _err != nil {
		t.Fatal(_err)
	}

	_bytes, _err := os.ReadFile(_recorder.monthPath("key-a", "2026-08"))
	if _err != nil {
		t.Fatal(_err)
	}
	var _payload map[string]interface{}
	if _err := json.Unmarshal(_bytes, &_payload); _err != nil {
		t.Fatal(_err)
	}
	if _, _exists := _payload["complexity"]; _exists {
		t.Fatalf("complexity must be omitted when unused: %s", _bytes)
	}
	for _field := range _payload {
		switch _field {
		case "key_id", "month", "days", "updated_at":
		default:
			t.Fatalf("unexpected field %q: %s", _field, _bytes)
		}
	}
}

// -------------------------------------------------------------------------------------
// 向下相容 2：讀得懂沒有 complexity 欄位的舊檔，且不會破壞既有的 days 統計。
func TestLoadsLegacyMonthFileWithoutComplexity(t *testing.T) {
	_root := filepath.Join(t.TempDir(), "usage")
	_recorder := NewRecorder(_root)
	_path := _recorder.monthPath("key-a", "2026-08")
	if _err := os.MkdirAll(filepath.Dir(_path), 0o755); _err != nil {
		t.Fatal(_err)
	}
	_legacy := `{"key_id":"key-a","month":"2026-08","days":{"2026-08-01":7},"updated_at":"2026-08-01T00:00:00Z"}`
	if _err := os.WriteFile(_path, []byte(_legacy), 0o644); _err != nil {
		t.Fatal(_err)
	}

	_stats, _err := _recorder.LoadMonth("key-a", "2026-08")
	if _err != nil {
		t.Fatal(_err)
	}
	if _stats.Total != 7 || len(_stats.Days) != 1 || _stats.Days[0].Count != 7 {
		t.Fatalf("legacy stats = %#v", _stats)
	}
	if _stats.Days[0].Complexity != nil {
		t.Fatalf("legacy day must not report complexity: %#v", _stats.Days[0])
	}

	// 在舊檔上追加分級，既有的 days 不得被更動。
	if _err := _recorder.RecordComplexity("key-a", time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local), 8); _err != nil {
		t.Fatal(_err)
	}
	_stats, _err = _recorder.LoadMonth("key-a", "2026-08")
	if _err != nil {
		t.Fatal(_err)
	}
	if _stats.Days[0].Count != 7 {
		t.Fatalf("existing day count must stay untouched: %#v", _stats.Days[0])
	}
	if _stats.Days[0].Complexity == nil || _stats.Days[0].Complexity.High != 1 {
		t.Fatalf("complexity was not appended: %#v", _stats.Days[0].Complexity)
	}
}

// -------------------------------------------------------------------------------------
// 三個等級要分開累積，並跨重啟保留（重新建立 Recorder 後仍讀得到）。
func TestComplexityTiersPersistPerDay(t *testing.T) {
	_root := filepath.Join(t.TempDir(), "usage")
	_recorder := NewRecorder(_root)
	_at := time.Date(2026, 8, 12, 11, 0, 0, 0, time.Local)

	for _, _score := range []int{1, 2, 3, 5, 6, 9} {
		if _err := _recorder.RecordComplexity("key-a", _at, _score); _err != nil {
			t.Fatal(_err)
		}
	}
	if _err := _recorder.Flush(); _err != nil {
		t.Fatal(_err)
	}

	_reloaded := NewRecorder(_root)
	_stats, _err := _reloaded.LoadMonth("key-a", "2026-08")
	if _err != nil {
		t.Fatal(_err)
	}
	if len(_stats.Days) != 1 || _stats.Days[0].Complexity == nil {
		t.Fatalf("stats = %#v", _stats)
	}
	_counts := _stats.Days[0].Complexity
	if _counts.Low != 3 || _counts.Mid != 2 || _counts.High != 1 {
		t.Fatalf("tier counts = %#v, want low=3 mid=2 high=1", _counts)
	}
}

// -------------------------------------------------------------------------------------
func TestComplexityTierBoundaries(t *testing.T) {
	for _score, _want := range map[int]string{
		0: ComplexityTierLow, 1: ComplexityTierLow, 3: ComplexityTierLow,
		4: ComplexityTierMid, 6: ComplexityTierMid,
		7: ComplexityTierHigh, 10: ComplexityTierHigh,
	} {
		if _got := ComplexityTier(_score); _got != _want {
			t.Fatalf("score %d → %s, want %s", _score, _got, _want)
		}
	}
}

// -------------------------------------------------------------------------------------
// 密集度：只看視窗內的請求，並依三個等級分開統計頻率。
func TestRequestDensitySplitsByComplexityTier(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()

	// 視窗外（10 分鐘前）不該被計入
	for _idx := 0; _idx < 5; _idx++ {
		if _err := _recorder.RecordComplexity("key-a", _now.Add(-10*time.Minute), 2); _err != nil {
			t.Fatal(_err)
		}
	}
	// 視窗內：8 低、3 中、1 高
	for _idx := 0; _idx < 8; _idx++ {
		_ = _recorder.RecordComplexity("key-a", _now.Add(-30*time.Second), 2)
	}
	for _idx := 0; _idx < 3; _idx++ {
		_ = _recorder.RecordComplexity("key-a", _now.Add(-20*time.Second), 5)
	}
	_ = _recorder.RecordComplexity("key-a", _now.Add(-10*time.Second), 9)

	_density := _recorder.RequestDensity("key-a", 2*time.Minute)
	if _density.Count != 12 {
		t.Fatalf("count = %d, want 12（10 分鐘前的不算）", _density.Count)
	}
	if _density.Low.Count != 8 || _density.Mid.Count != 3 || _density.High.Count != 1 {
		t.Fatalf("tiers = low %d / mid %d / high %d", _density.Low.Count, _density.Mid.Count, _density.High.Count)
	}
	if _density.PerMinute != 6 {
		t.Fatalf("per minute = %.2f, want 6", _density.PerMinute)
	}
	if _density.Low.PerMinute != 4 {
		t.Fatalf("low per minute = %.2f, want 4", _density.Low.PerMinute)
	}
	if _density.Low.Share < 0.66 || _density.Low.Share > 0.67 {
		t.Fatalf("low share = %.3f, want ≈0.667", _density.Low.Share)
	}
	if _density.FirstAt == "" || _density.LastAt == "" {
		t.Fatalf("density should report the sample range: %#v", _density)
	}

	if _other := _recorder.RequestDensity("key-b", time.Minute); _other.Count != 0 {
		t.Fatalf("unknown key density = %#v", _other)
	}
}

// -------------------------------------------------------------------------------------
// 實際消耗與複雜度是兩組獨立樣本：不需要逐筆關聯，也不必回頭修改既有樣本。
func TestConsumptionTracksSeparatelyFromComplexity(t *testing.T) {
	_root := filepath.Join(t.TempDir(), "usage")
	_recorder := NewRecorder(_root)
	_now := time.Now()

	// 3 筆請求進入視窗，但只有 2 筆完成
	for _idx := 0; _idx < 3; _idx++ {
		_ = _recorder.RecordComplexity("key-a", _now.Add(-30*time.Second), 2)
	}
	_ = _recorder.RecordConsumption("key-a", _now.Add(-25*time.Second), ConsumptionSample{Tokens: 400, Complexity: 5})
	_ = _recorder.RecordConsumption("key-a", _now.Add(-20*time.Second), ConsumptionSample{Tokens: 600, Complexity: 5})

	_density := _recorder.RequestDensity("key-a", 2*time.Minute)
	if _density.Count != 3 {
		t.Fatalf("count = %d, want 3", _density.Count)
	}
	if _density.CompletedRequests != 2 || _density.Tokens != 1000 {
		t.Fatalf("completed=%d tokens=%d, want 2 / 1000", _density.CompletedRequests, _density.Tokens)
	}
	if _density.TokensPerMinute != 500 {
		t.Fatalf("tokens per minute = %.1f, want 500", _density.TokensPerMinute)
	}

	// 視窗外的消耗不得計入
	_ = _recorder.RecordConsumption("key-a", _now.Add(-30*time.Minute), ConsumptionSample{Tokens: 9999, Complexity: 5})
	if _again := _recorder.RequestDensity("key-a", 2*time.Minute); _again.Tokens != 1000 {
		t.Fatalf("stale consumption leaked in: %d", _again.Tokens)
	}
}

// -------------------------------------------------------------------------------------
// 每日 token 總量要能跨重啟保留，且沒有資料時不寫出欄位。
func TestDailyTokensPersistAndStayOptional(t *testing.T) {
	_root := filepath.Join(t.TempDir(), "usage")
	_recorder := NewRecorder(_root)
	_at := time.Date(2026, 8, 19, 10, 0, 0, 0, time.Local)

	_ = _recorder.RecordConsumption("key-a", _at, ConsumptionSample{Tokens: 120, Complexity: 5})
	_ = _recorder.RecordConsumption("key-a", _at, ConsumptionSample{Tokens: 80, Complexity: 5})
	if _err := _recorder.Flush(); _err != nil {
		t.Fatal(_err)
	}

	_stats, _err := NewRecorder(_root).LoadMonth("key-a", "2026-08")
	if _err != nil {
		t.Fatal(_err)
	}
	if len(_stats.Days) != 1 || _stats.Days[0].Tokens != 200 {
		t.Fatalf("daily tokens = %#v", _stats.Days)
	}

	// 只記請求、沒有消耗時，tokens 欄位不得出現
	_other := NewRecorder(filepath.Join(t.TempDir(), "usage2"))
	_ = _other.Record("key-b", _at)
	if _err := _other.Flush(); _err != nil {
		t.Fatal(_err)
	}
	_bytes, _err := os.ReadFile(_other.monthPath("key-b", "2026-08"))
	if _err != nil {
		t.Fatal(_err)
	}
	if strings.Contains(string(_bytes), "tokens") {
		t.Fatalf("tokens must be omitted when unused: %s", _bytes)
	}
}

// -------------------------------------------------------------------------------------
// 消耗量要能歸戶到複雜度等級，否則看不出「低階請求卻吃掉大量配額」這個關鍵特徵。
func TestConsumptionAttributesToComplexityTier(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()

	// 低階請求打很多次，每次吃得不多，但總量很可觀。
	for _i := 0; _i < 20; _i++ {
		_ = _recorder.RecordComplexity("key-a", _now.Add(-30*time.Second), 1)
		_ = _recorder.RecordConsumption("key-a", _now.Add(-30*time.Second), ConsumptionSample{Tokens: 500, Complexity: 1})
	}
	// 高階請求只有一次，但單筆很重。
	_ = _recorder.RecordComplexity("key-a", _now.Add(-20*time.Second), 9)
	_ = _recorder.RecordConsumption("key-a", _now.Add(-20*time.Second), ConsumptionSample{Tokens: 4000, Complexity: 9})

	_density := _recorder.RequestDensity("key-a", time.Minute)

	if _density.Low.Tokens != 10000 {
		t.Fatalf("low tier tokens = %d, want 10000", _density.Low.Tokens)
	}
	if _density.High.Tokens != 4000 {
		t.Fatalf("high tier tokens = %d, want 4000", _density.High.Tokens)
	}
	if _sum := _density.Low.Tokens + _density.Mid.Tokens + _density.High.Tokens; _sum != _density.Tokens {
		t.Fatalf("tier tokens should sum to total: %d != %d", _sum, _density.Tokens)
	}
	// 每筆平均才是分辨「單筆很重」與「單筆很輕但打很多次」的依據。
	if _density.Low.TokensPerRequest != 500 {
		t.Fatalf("low tokens/request = %v, want 500", _density.Low.TokensPerRequest)
	}
	if _density.High.TokensPerRequest != 4000 {
		t.Fatalf("high tokens/request = %v, want 4000", _density.High.TokensPerRequest)
	}
	// 低階雖然單筆輕，總消耗卻超過高階 —— 這正是要偵測的濫用形態。
	if _density.Low.Tokens <= _density.High.Tokens {
		t.Fatalf("expected low tier to out-consume high tier: low=%d high=%d", _density.Low.Tokens, _density.High.Tokens)
	}
}

// -------------------------------------------------------------------------------------
// 請求樣本已過期、但消耗樣本還在視窗內的金鑰不能從全域清單消失。
func TestAllDensityIncludesKeysWithOnlyConsumption(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_recorder.recentLock.Lock()
	_recorder.consumed["orphan"] = []consumedSample{{At: time.Now().UnixNano(), Tokens: 777, Complexity: 2}}
	_recorder.recentLock.Unlock()

	_all := _recorder.AllRequestDensity(time.Minute)
	if len(_all) != 1 || _all[0].KeyID != "orphan" {
		t.Fatalf("orphan consumption should still be listed, got %+v", _all)
	}
	if _all[0].Tokens != 777 || _all[0].Count != 0 {
		t.Fatalf("unexpected orphan density: %+v", _all[0])
	}
}

// -------------------------------------------------------------------------------------
// 全域清單以實際消耗排序：它是唯一直接對應配額的數字。
func TestAllDensitySortsByConsumption(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()

	// 請求數多但消耗少。
	for _i := 0; _i < 30; _i++ {
		_ = _recorder.RecordComplexity("chatty", _now, 1)
	}
	_ = _recorder.RecordConsumption("chatty", _now, ConsumptionSample{Tokens: 100, Complexity: 1})

	// 請求數少但消耗大。
	_ = _recorder.RecordComplexity("heavy", _now, 8)
	_ = _recorder.RecordConsumption("heavy", _now, ConsumptionSample{Tokens: 50000, Complexity: 8})

	_all := _recorder.AllRequestDensity(time.Minute)
	if len(_all) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(_all))
	}
	if _all[0].KeyID != "heavy" {
		t.Fatalf("consumption should outrank request count, got %s first", _all[0].KeyID)
	}
}

// -------------------------------------------------------------------------------------
// 這是最想抓的行為：拿高階模型跑基礎操作 —— 吃大量 context、幾乎不產出。
// 它必須能和「拿高階模型跑真任務」分開，也必須放過「拿低階模型跑瑣事」。
func TestOutputRatioSeparatesTrivialWorkOnExpensiveModels(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()

	// 基礎 OS 操作 @ 高階模型：15000 輸入換 50 輸出。
	for _i := 0; _i < 40; _i++ {
		_ = _recorder.RecordComplexity("瑣事高階", _now, 9)
		_ = _recorder.RecordConsumption("瑣事高階", _now, ConsumptionSample{
			Tokens: 50, Complexity: 9, QualityTier: 8, PromptTokens: 15000,
		})
	}
	// 真實長任務 @ 高階模型：一樣的 context，但真的在產出。
	for _i := 0; _i < 4; _i++ {
		_ = _recorder.RecordComplexity("真任務", _now, 9)
		_ = _recorder.RecordConsumption("真任務", _now, ConsumptionSample{
			Tokens: 4000, Complexity: 9, QualityTier: 8, PromptTokens: 15000,
		})
	}
	// 瑣事 @ 低階模型：無所謂，不該被抓。
	for _i := 0; _i < 40; _i++ {
		_ = _recorder.RecordComplexity("瑣事低階", _now, 2)
		_ = _recorder.RecordConsumption("瑣事低階", _now, ConsumptionSample{
			Tokens: 50, Complexity: 2, QualityTier: 2, PromptTokens: 15000,
		})
	}

	_abuse := _recorder.RequestDensity("瑣事高階", time.Minute)
	_real := _recorder.RequestDensity("真任務", time.Minute)
	_cheap := _recorder.RequestDensity("瑣事低階", time.Minute)

	// 複雜度分級完全分不開前兩者 —— 這正是需要新訊號的原因。
	if _abuse.High.Share != _real.High.Share {
		t.Fatalf("complexity tier is expected to be useless here: abuse=%v real=%v", _abuse.High.Share, _real.High.Share)
	}

	if _abuse.QualityTierAvg != 8 || _real.QualityTierAvg != 8 || _cheap.QualityTierAvg != 2 {
		t.Fatalf("quality tier avg wrong: abuse=%v real=%v cheap=%v",
			_abuse.QualityTierAvg, _real.QualityTierAvg, _cheap.QualityTierAvg)
	}

	// 輸出/輸入比才是判別軸：0.33% vs 26.7%，差兩個數量級。
	if _abuse.OutputRatio >= 0.02 {
		t.Fatalf("abuse output ratio should be far below 2%%, got %v", _abuse.OutputRatio)
	}
	if _real.OutputRatio <= 0.02 {
		t.Fatalf("real work output ratio should be well above 2%%, got %v", _real.OutputRatio)
	}

	// 規則本身：高等級 + 極低產出比 = 抓；其餘放行。
	_flagged := func(_d RequestDensity) bool {
		return _d.QualityTierAvg >= 6 && _d.OutputRatio < 0.02 && _d.CompletedRequests >= 10
	}
	if !_flagged(_abuse) {
		t.Fatalf("abuse pattern should be flagged: %+v", _abuse)
	}
	if _flagged(_real) {
		t.Fatalf("genuine long task must not be flagged: %+v", _real)
	}
	if _flagged(_cheap) {
		t.Fatalf("trivial work on a cheap model must not be flagged: %+v", _cheap)
	}
}

// -------------------------------------------------------------------------------------
// 舊樣本沒有等級資訊，不能把平均值拉到 0。
func TestQualityTierAverageIgnoresUnsetSamples(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()

	_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{Tokens: 100, PromptTokens: 1000})
	_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{Tokens: 100, QualityTier: 8, PromptTokens: 1000})

	_density := _recorder.RequestDensity("key-a", time.Minute)
	if _density.QualityTierAvg != 8 {
		t.Fatalf("unset tiers should be excluded from the average, got %v", _density.QualityTierAvg)
	}
	// 但兩筆的輸出與輸入都要照算。
	if _density.CompletedRequests != 2 || _density.PromptTokens != 2000 || _density.Tokens != 200 {
		t.Fatalf("totals should include both samples: %+v", _density)
	}
}

// -------------------------------------------------------------------------------------
// 複雜度分級對長 context 的流量沒有鑑別力（分數只能取 5 或 6），
// 產出比分級必須能把同一批樣本分開。
func TestYieldTiersSeparateWhereComplexityCannot(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()

	_add := func(_count int, _out int, _in int) {
		for _i := 0; _i < _count; _i++ {
			_ = _recorder.RecordComplexity("key-a", _now, 5)
			_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{
				Tokens: _out, Complexity: 5, QualityTier: 8, PromptTokens: _in,
			})
		}
	}
	_add(12, 50, 15000)  // 0.33%：形同沒有要求產出
	_add(5, 922, 15000)  // 6.1%：一般對話
	_add(2, 6000, 15000) // 40%：真的在生成

	_density := _recorder.RequestDensity("key-a", time.Minute)

	// 複雜度分級整批擠在同一格。
	if _density.Mid.Count != 19 || _density.Low.Count != 0 || _density.High.Count != 0 {
		t.Fatalf("complexity tiers expected to be degenerate here: %+v", _density)
	}
	// 產出比分級把同一批樣本分開。
	if _density.YieldLow.Count != 12 || _density.YieldMid.Count != 5 || _density.YieldHigh.Count != 2 {
		t.Fatalf("yield tiers wrong: low=%d mid=%d high=%d",
			_density.YieldLow.Count, _density.YieldMid.Count, _density.YieldHigh.Count)
	}
	// 佔比的母體是已完成且輸入量已知的請求，不是 Count。
	if _share := _density.YieldLow.Share; _share < 0.63 || _share > 0.64 {
		t.Fatalf("yield low share = %v, want ~0.63", _share)
	}
}

// -------------------------------------------------------------------------------------
// Σ/Σ 會被單一超大 context 的請求主導，中位數才反映多數對話的樣子。
func TestOutputRatioMedianResistsSkew(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()

	// 20 筆正常對話：40%
	for _i := 0; _i < 20; _i++ {
		_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{
			Tokens: 800, Complexity: 5, QualityTier: 8, PromptTokens: 2000,
		})
	}
	// 1 筆超大 context 的瑣事操作
	_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{
		Tokens: 50, Complexity: 5, QualityTier: 8, PromptTokens: 200000,
	})

	_density := _recorder.RequestDensity("key-a", time.Minute)

	if _density.OutputRatio >= 0.10 {
		t.Fatalf("aggregate ratio should be dragged down by the outlier, got %v", _density.OutputRatio)
	}
	if _density.OutputRatioMedian < 0.39 || _density.OutputRatioMedian > 0.41 {
		t.Fatalf("median should stay at ~40%%, got %v", _density.OutputRatioMedian)
	}
}

// -------------------------------------------------------------------------------------
// 輸入量不明的樣本不能歸進任何一級，否則會把分布往低產出拉。
func TestYieldTierSkipsSamplesWithoutPromptTokens(t *testing.T) {
	if _tier := YieldTier(50, 0); _tier != "" {
		t.Fatalf("unknown prompt size should yield no tier, got %q", _tier)
	}

	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()
	_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{Tokens: 50})
	_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{Tokens: 800, PromptTokens: 2000})

	_density := _recorder.RequestDensity("key-a", time.Minute)
	if _density.YieldLow.Count != 0 || _density.YieldHigh.Count != 1 {
		t.Fatalf("only the sample with a known prompt size counts: %+v", _density)
	}
	// 但總量仍要含兩筆。
	if _density.CompletedRequests != 2 || _density.Tokens != 850 {
		t.Fatalf("totals should include both samples: %+v", _density)
	}
}

// -------------------------------------------------------------------------------------
func TestRequestDensityAcceptsCustomYieldThresholds(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()
	for _, _tokens := range []int{30, 100, 300} {
		_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{
			Tokens: _tokens, PromptTokens: 1000,
		})
	}

	_density := _recorder.RequestDensityWithYieldThresholds("key-a", time.Minute, YieldThresholds{
		LowMaxRatio: 0.05,
		MidMaxRatio: 0.15,
	})
	if _density.YieldLow.Count != 1 || _density.YieldMid.Count != 1 || _density.YieldHigh.Count != 1 {
		t.Fatalf("custom yield thresholds were not applied: %+v", _density)
	}
}

// -------------------------------------------------------------------------------------
func TestRequestActivitySignalsAggregateAndPersist(t *testing.T) {
	_root := filepath.Join(t.TempDir(), "usage")
	_recorder := NewRecorder(_root)
	_at := time.Now()
	_sample := RequestSample{
		Complexity:       2,
		Continuation:     true,
		Fingerprint:      "same-task",
		ToolCalls:        2,
		ToolRounds:       1,
		ToolOutputTokens: 120,
	}
	if _err := _recorder.RecordRequest("key-a", _at, _sample); _err != nil {
		t.Fatal(_err)
	}
	if _err := _recorder.RecordRequest("key-a", _at.Add(time.Second), _sample); _err != nil {
		t.Fatal(_err)
	}

	_density := _recorder.RequestDensity("key-a", time.Hour)
	if _density.ToolCallCount != 4 || _density.ToolRoundCount != 2 || _density.ToolOutputTokens != 240 {
		t.Fatalf("tool totals = %+v", _density)
	}
	if _density.ContinuationRatio != 1 || _density.RepeatedTaskRatio != 0.5 {
		t.Fatalf("activity ratios = %+v", _density)
	}
	if _density.ToolCallsPerRequest != 2 || _density.ToolRoundsPerRequest != 1 {
		t.Fatalf("per-request tool averages = %+v", _density)
	}

	if _err := _recorder.Flush(); _err != nil {
		t.Fatal(_err)
	}
	_stats, _err := NewRecorder(_root).LoadMonth("key-a", _at.Format("2006-01"))
	if _err != nil {
		t.Fatal(_err)
	}
	if len(_stats.Days) != 1 || _stats.Days[0].Activity == nil {
		t.Fatalf("monthly activity missing: %+v", _stats)
	}
	_activity := _stats.Days[0].Activity
	if _activity.Requests != 2 || _activity.RepeatedRequests != 1 || _activity.ToolCalls != 4 || _activity.ToolOutputTokens != 240 {
		t.Fatalf("monthly activity = %+v", _activity)
	}
}

// -------------------------------------------------------------------------------------
// 文字比：純工具呼叫的 turn 是 0，一般對話接近 1。
// 非串流回應拆不出用途，整筆必須略過而不是當成 0。
func TestProseRatioSkipsSamplesWithoutStreamData(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()

	// 8 筆純工具呼叫：輸出全是工具參數
	for _i := 0; _i < 8; _i++ {
		_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{
			Tokens: 60, PromptTokens: 15000, QualityTier: 8, ProseTokens: 0, StreamedTokens: 55,
		})
	}
	// 2 筆一般對話：幾乎都是給人看的文字
	for _i := 0; _i < 2; _i++ {
		_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{
			Tokens: 900, PromptTokens: 15000, QualityTier: 8, ProseTokens: 850, StreamedTokens: 880,
		})
	}
	// 1 筆非串流：沒有分類資料，不能被當成「整輪都在呼叫工具」
	_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{
		Tokens: 700, PromptTokens: 15000, QualityTier: 8,
	})

	_density := _recorder.RequestDensity("key-a", time.Minute)

	// 中位數落在工具呼叫那一側：多數對話沒有產出文字。
	if _density.ProseRatioMedian != 0 {
		t.Fatalf("median prose ratio should be 0, got %v", _density.ProseRatioMedian)
	}
	// Σ/Σ 被兩筆長回覆拉高，兩個數字要能同時看到才不會誤判。
	if _density.ProseRatio <= _density.ProseRatioMedian {
		t.Fatalf("aggregate should exceed the median here: agg=%v median=%v", _density.ProseRatio, _density.ProseRatioMedian)
	}
	// 非串流那筆不列入比例，但仍要計入消耗總量。
	if _density.CompletedRequests != 11 || _density.Tokens != 60*8+900*2+700 {
		t.Fatalf("totals must include the non-streaming sample: %+v", _density)
	}
}

// -------------------------------------------------------------------------------------
// 沒有分類樣本時，比例會是 0 —— 讀取端必須靠 ProseSamples 才能和
// 「真的沒有產出文字」分開。這正是先前顯示成 0.00% 的成因。
func TestProseSamplesDistinguishNoDataFromZero(t *testing.T) {
	_now := time.Now()

	_noData := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	for _i := 0; _i < 5; _i++ {
		_ = _noData.RecordConsumption("key-a", _now, ConsumptionSample{
			Tokens: 400, PromptTokens: 15000, QualityTier: 8,
		})
	}
	_blank := _noData.RequestDensity("key-a", time.Minute)
	if _blank.ProseSamples != 0 {
		t.Fatalf("samples without stream data must not count: %+v", _blank)
	}
	if _blank.ProseRatioMedian != 0 || _blank.CompletedRequests != 5 {
		t.Fatalf("no-data case: ratio should be 0 but requests still counted: %+v", _blank)
	}

	_realZero := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	for _i := 0; _i < 5; _i++ {
		_ = _realZero.RecordConsumption("key-b", _now, ConsumptionSample{
			Tokens: 400, PromptTokens: 15000, QualityTier: 8, ProseTokens: 0, StreamedTokens: 380,
		})
	}
	_zero := _realZero.RequestDensity("key-b", time.Minute)
	if _zero.ProseSamples != 5 {
		t.Fatalf("real zero must be backed by samples: %+v", _zero)
	}
	if _zero.ProseRatioMedian != 0 {
		t.Fatalf("real zero ratio = %v", _zero.ProseRatioMedian)
	}

	// 兩者的比例相同，只有 ProseSamples 分得開。
	if _blank.ProseRatioMedian != _zero.ProseRatioMedian {
		t.Fatalf("both cases are expected to report the same ratio")
	}
	if _blank.ProseSamples == _zero.ProseSamples {
		t.Fatalf("ProseSamples must be the discriminator")
	}
}

// -------------------------------------------------------------------------------------
// 分母必須是上游回報的總輸出，不是字元估算的總和。
// provider 不串流推理內容時，字元估算會少掉絕大部分，讓文字比嚴重虛高。
func TestProseRatioUsesReportedOutputAsDenominator(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_now := time.Now()

	// 一輪推理很重、只發工具呼叫的請求：
	// 上游回報 1589（其中 1400 推理），但只有 5 個 token 的工具參數被串流出來。
	_ = _recorder.RecordConsumption("key-a", _now, ConsumptionSample{
		Tokens: 1589, PromptTokens: 15000, QualityTier: 8,
		ProseTokens: 0, ReasoningTokens: 1400, StreamedTokens: 5, ReasoningReported: true,
	})
	// 同一輪若有 200 token 的文字：用真實分母是 12.6%，用字元估算會變成 100%
	_ = _recorder.RecordConsumption("key-b", _now, ConsumptionSample{
		Tokens: 1589, PromptTokens: 15000, QualityTier: 8,
		ProseTokens: 200, ReasoningTokens: 1400, StreamedTokens: 205, ReasoningReported: true,
	})

	_toolOnly := _recorder.RequestDensity("key-a", time.Minute)
	if _toolOnly.ProseRatioMedian != 0 || _toolOnly.ProseSamples != 1 {
		t.Fatalf("tool-only turn: %+v", _toolOnly)
	}
	if _toolOnly.ReasoningRatio < 0.88 || _toolOnly.ReasoningRatio > 0.89 {
		t.Fatalf("reasoning should be ~88%% of output, got %v", _toolOnly.ReasoningRatio)
	}

	_withProse := _recorder.RequestDensity("key-b", time.Minute)
	if _withProse.ProseRatioMedian > 0.13 {
		t.Fatalf("prose ratio must use the reported total, got %v (估算分母會給出 ~1.0)", _withProse.ProseRatioMedian)
	}
}

// -------------------------------------------------------------------------------------
// 分子是估算、分母是真值，估算偏高時比例可能超過 1 —— 那是誤差，不是真的超額產出。
func TestProseRatioIsClamped(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "usage"))
	_ = _recorder.RecordConsumption("key-a", time.Now(), ConsumptionSample{
		Tokens: 100, PromptTokens: 1000, ProseTokens: 140, StreamedTokens: 140,
	})
	_density := _recorder.RequestDensity("key-a", time.Minute)
	if _density.ProseRatioMedian != 1 || _density.ProseRatio != 1 {
		t.Fatalf("ratios should clamp to 1: median=%v agg=%v", _density.ProseRatioMedian, _density.ProseRatio)
	}
}
