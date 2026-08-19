package keyusage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultUsageRoot = "usage"

const usageFlushDelay = 2 * time.Second

var (
	_defaultRecorder = NewRecorder(defaultUsageRoot)
	_safePathName    = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	_monthPattern    = regexp.MustCompile(`^\d{4}-\d{2}$`)
)

type Recorder struct {
	Root       string
	lock       sync.Mutex
	files      map[string]MonthFile
	dirty      map[string]bool
	flushTimer *time.Timer

	// 請求時間戳只放在記憶體：密集度看的是分鐘級的視窗，
	// 若把每筆時間寫進每月 JSON，檔案會被灌爆，而且重啟後也不需要保留。
	recentLock sync.Mutex
	recent     map[string][]recentSample
	// 實際消耗是請求「完成後」才知道的，與 recent 各自獨立、刻意不做逐筆關聯：
	// 偵測要的是聚合特徵，關聯只會帶來併發與部分計數的麻煩。
	consumed    map[string][]consumedSample
	lastSweepAt time.Time
}

type MonthFile struct {
	KeyID string           `json:"key_id"`
	Month string           `json:"month"`
	Days  map[string]int64 `json:"days"`
	// Complexity 是每日的複雜度分級次數，附加欄位：
	//   - 舊檔沒有這個欄位 → 解析後為 nil，一律當成 0
	//   - 沒有任何分級資料時不會寫出，舊版讀取者完全不受影響
	// 它與 Days 統計的母體不同：Days 記的是「通過授權」的請求，
	// Complexity 記的是「已完成分類」的請求，兩者不保證相等。
	Complexity map[string]ComplexityCounts `json:"complexity,omitempty"`
	// Tokens 是每日實際輸出的 token 總量，同樣是附加欄位、母體與 Days 不同：
	// 只有「成功完成」的請求會計入。
	Tokens map[string]int64 `json:"tokens,omitempty"`
	// Activity 保存行為訊號的每日彙總，不保存提示詞、工具輸出或任務指紋。
	Activity  map[string]ActivityCounts `json:"activity,omitempty"`
	UpdatedAt string                    `json:"updated_at"`
}

type ComplexityCounts struct {
	Low  int64 `json:"low,omitempty"`
	Mid  int64 `json:"mid,omitempty"`
	High int64 `json:"high,omitempty"`
}

type ActivityCounts struct {
	Requests              int64 `json:"requests,omitempty"`
	Continuations         int64 `json:"continuations,omitempty"`
	FingerprintedRequests int64 `json:"fingerprinted_requests,omitempty"`
	RepeatedRequests      int64 `json:"repeated_requests,omitempty"`
	ToolCalls             int64 `json:"tool_calls,omitempty"`
	ToolRounds            int64 `json:"tool_rounds,omitempty"`
	ToolOutputTokens      int64 `json:"tool_output_tokens,omitempty"`
}

type DayStat struct {
	Date       string            `json:"date"`
	Count      int64             `json:"count"`
	Complexity *ComplexityCounts `json:"complexity,omitempty"`
	Tokens     int64             `json:"tokens,omitempty"`
	Activity   *ActivityCounts   `json:"activity,omitempty"`
}

type MonthStats struct {
	KeyID     string    `json:"key_id"`
	Month     string    `json:"month"`
	Days      []DayStat `json:"days"`
	Total     int64     `json:"total"`
	UpdatedAt string    `json:"updated_at,omitempty"`
}

func DefaultRecorder() *Recorder {
	return _defaultRecorder
}

func NewRecorder(_root string) *Recorder {
	if strings.TrimSpace(_root) == "" {
		_root = defaultUsageRoot
	}
	return &Recorder{
		Root:     _root,
		files:    map[string]MonthFile{},
		dirty:    map[string]bool{},
		recent:   map[string][]recentSample{},
		consumed: map[string][]consumedSample{},
	}
}

func (_r *Recorder) Record(_keyID string, _at time.Time) error {
	_keyID = strings.TrimSpace(_keyID)
	if _keyID == "" {
		return nil
	}
	if _at.IsZero() {
		_at = time.Now()
	}
	_at = _at.Local()
	_month := _at.Format("2006-01")
	_day := _at.Format("2006-01-02")

	_r.lock.Lock()
	defer _r.lock.Unlock()

	_file, _err := _r.loadMonthLocked(_keyID, _month)
	if _err != nil {
		return _err
	}
	if _file.Days == nil {
		_file.Days = map[string]int64{}
	}
	_file.KeyID = _keyID
	_file.Month = _month
	_file.Days[_day]++
	_file.UpdatedAt = time.Now().Format(time.RFC3339)
	_cacheKey := monthCacheKey(_keyID, _month)
	_r.files[_cacheKey] = _file
	_r.dirty[_cacheKey] = true
	_r.scheduleFlushLocked()
	return nil
}

func (_r *Recorder) LoadMonth(_keyID string, _month string) (MonthStats, error) {
	_keyID = strings.TrimSpace(_keyID)
	if _keyID == "" {
		return MonthStats{}, fmt.Errorf("api key id is required")
	}
	_month = normalizeMonth(_month)

	_r.lock.Lock()
	_file, _err := _r.loadMonthLocked(_keyID, _month)
	_r.lock.Unlock()
	if _err != nil {
		return MonthStats{}, _err
	}
	return monthStats(_file), nil
}

func (_r *Recorder) loadMonthLocked(_keyID string, _month string) (MonthFile, error) {
	_month = normalizeMonth(_month)
	_cacheKey := monthCacheKey(_keyID, _month)
	if _file, _ok := _r.files[_cacheKey]; _ok {
		return _file, nil
	}
	_path := _r.monthPath(_keyID, _month)
	_bytes, _err := os.ReadFile(_path)
	if _err != nil {
		if os.IsNotExist(_err) {
			_file := MonthFile{KeyID: _keyID, Month: _month, Days: map[string]int64{}}
			_r.files[_cacheKey] = _file
			return _file, nil
		}
		return MonthFile{}, _err
	}
	if len(_bytes) == 0 {
		_file := MonthFile{KeyID: _keyID, Month: _month, Days: map[string]int64{}}
		_r.files[_cacheKey] = _file
		return _file, nil
	}

	var _file MonthFile
	if _err := json.Unmarshal(_bytes, &_file); _err != nil {
		return MonthFile{}, _err
	}
	if _file.KeyID == "" {
		_file.KeyID = _keyID
	}
	if _file.Month == "" {
		_file.Month = _month
	}
	if _file.Days == nil {
		_file.Days = map[string]int64{}
	}
	_r.files[_cacheKey] = _file
	return _file, nil
}

func (_r *Recorder) scheduleFlushLocked() {
	if _r.flushTimer != nil {
		return
	}
	_r.flushTimer = time.AfterFunc(usageFlushDelay, func() {
		if _err := _r.Flush(); _err != nil {
			log.Printf("api key usage flush failed: %v", _err)
		}
	})
}

// Flush persists all pending monthly counters. It is safe to call during shutdown.
func (_r *Recorder) Flush() error {
	if _r == nil {
		return nil
	}
	_r.lock.Lock()
	defer _r.lock.Unlock()
	if _r.flushTimer != nil {
		_r.flushTimer.Stop()
		_r.flushTimer = nil
	}
	for _cacheKey := range _r.dirty {
		_file, _ok := _r.files[_cacheKey]
		if !_ok {
			delete(_r.dirty, _cacheKey)
			continue
		}
		if _err := _r.saveMonthLocked(_file); _err != nil {
			return _err
		}
		delete(_r.dirty, _cacheKey)
	}
	return nil
}

func (_r *Recorder) saveMonthLocked(_file MonthFile) error {
	_path := _r.monthPath(_file.KeyID, _file.Month)
	if _err := os.MkdirAll(filepath.Dir(_path), 0755); _err != nil {
		return _err
	}

	_bytes, _err := json.MarshalIndent(_file, "", "  ")
	if _err != nil {
		return _err
	}
	_tmp, _err := os.CreateTemp(filepath.Dir(_path), filepath.Base(_path)+".tmp.*")
	if _err != nil {
		return _err
	}
	_tmpPath := _tmp.Name()
	defer os.Remove(_tmpPath)
	if _err := _tmp.Chmod(0600); _err != nil {
		_tmp.Close()
		return _err
	}
	if _, _err := _tmp.Write(append(_bytes, '\n')); _err != nil {
		_tmp.Close()
		return _err
	}
	if _err := _tmp.Sync(); _err != nil {
		_tmp.Close()
		return _err
	}
	if _err := _tmp.Close(); _err != nil {
		return _err
	}
	return os.Rename(_tmpPath, _path)
}

func monthCacheKey(_keyID string, _month string) string {
	return safeKeyID(_keyID) + "\x00" + normalizeMonth(_month)
}

func (_r *Recorder) monthPath(_keyID string, _month string) string {
	return filepath.Join(_r.Root, safeKeyID(_keyID), normalizeMonth(_month)+".json")
}

func monthStats(_file MonthFile) MonthStats {
	// 分級可能出現在沒有 Days 紀錄的日期（母體不同），兩邊的日期都要涵蓋。
	_dates := make(map[string]bool, len(_file.Days)+len(_file.Complexity)+len(_file.Activity))
	for _date := range _file.Days {
		_dates[_date] = true
	}
	for _date := range _file.Complexity {
		_dates[_date] = true
	}
	for _date := range _file.Tokens {
		_dates[_date] = true
	}
	for _date := range _file.Activity {
		_dates[_date] = true
	}

	_days := make([]DayStat, 0, len(_dates))
	var _total int64
	for _date := range _dates {
		_stat := DayStat{Date: _date, Count: _file.Days[_date], Tokens: _file.Tokens[_date]}
		if _counts, _ok := _file.Complexity[_date]; _ok {
			_copy := _counts
			_stat.Complexity = &_copy
		}
		if _activity, _ok := _file.Activity[_date]; _ok {
			_copy := _activity
			_stat.Activity = &_copy
		}
		_days = append(_days, _stat)
		_total += _stat.Count
	}
	sort.Slice(_days, func(_i, _j int) bool {
		return _days[_i].Date < _days[_j].Date
	})
	return MonthStats{
		KeyID:     _file.KeyID,
		Month:     _file.Month,
		Days:      _days,
		Total:     _total,
		UpdatedAt: _file.UpdatedAt,
	}
}

func normalizeMonth(_month string) string {
	_month = strings.TrimSpace(_month)
	if !_monthPattern.MatchString(_month) {
		return time.Now().Local().Format("2006-01")
	}
	return _month
}

func safeKeyID(_keyID string) string {
	_keyID = strings.TrimSpace(_keyID)
	_keyID = _safePathName.ReplaceAllString(_keyID, "_")
	if _keyID == "" || _keyID == "." || _keyID == ".." {
		return "unknown"
	}
	return _keyID
}

// -------------------------------------------------------------------------------------
const (
	// 每支金鑰保留的樣本上限，避免單一金鑰把記憶體吃光。
	maxRecentRequestsPerKey = 2048
	// 可回答的最長視窗；超過這個範圍的樣本會被清掉。
	recentRequestRetention = time.Hour
	// 清掉長期沒有流量的金鑰，避免已刪除的金鑰殘留。
	recentSweepInterval = 5 * time.Minute
)

// -------------------------------------------------------------------------------------
// ComplexityScore（1–10）分成三個等級，分開統計頻率。
// 門檻刻意獨立成常數：實際分布觀察後很可能要調整。
const (
	ComplexityTierLow  = "low"
	ComplexityTierMid  = "mid"
	ComplexityTierHigh = "high"

	complexityLowMaxScore = 3
	complexityMidMaxScore = 6
)

// -------------------------------------------------------------------------------------
// ComplexityTier 依分數回傳等級。分數 <=0 視為未知，一律歸到 low。
func ComplexityTier(_score int) string {
	switch {
	case _score > complexityMidMaxScore:
		return ComplexityTierHigh
	case _score > complexityLowMaxScore:
		return ComplexityTierMid
	default:
		return ComplexityTierLow
	}
}

// -------------------------------------------------------------------------------------
type recentSample struct {
	At               int64
	Complexity       int
	Continuation     bool
	Fingerprint      string
	ToolCalls        int
	ToolRounds       int
	ToolOutputTokens int
}

// RequestSample 是請求完成路由分析後即可取得的行為訊號。
type RequestSample struct {
	Complexity       int
	Continuation     bool
	Fingerprint      string
	ToolCalls        int
	ToolRounds       int
	ToolOutputTokens int
}

type consumedSample struct {
	At     int64
	Tokens int
	// Complexity 是這筆請求「當初被分類的」複雜度分數。它在請求完成時才寫入，
	// 但分數本身是選擇階段就算好的，所以不需要回頭關聯任何既有樣本。
	Complexity int
	// QualityTier 是實際選中的模型等級（大=8 中=6 小=4 極小=2）。
	// PromptTokens 是輸入量的估算值。兩者合起來才能分辨
	// 「拿高階模型跑瑣事」與「拿高階模型跑真任務」—— 前者輸出/輸入比極低。
	QualityTier  int
	PromptTokens int
	// ProseTokens 是「真的給人看的」輸出量（字元估算）。
	// ReasoningTokens 優先是上游回報的精確值。推理在計費上就是輸出，
	// 所以它已經含在 Tokens 裡；獨立記一份是為了看得到它佔多少。
	// StreamedTokens 只當「這筆有沒有分類資料」的旗標，不是分母 ——
	// 分母要用 Tokens，否則 provider 不串流推理內容時分母會少掉絕大部分。
	ProseTokens     int
	ReasoningTokens int
	StreamedTokens  int
	// ReasoningReported 表示上游有回報推理欄位。沒有回報的樣本不能列入
	// 推理比統計 —— 它們會把比例往 0 拉，讓依推理量做的降級判斷誤傷。
	ReasoningReported bool
}

// -------------------------------------------------------------------------------------
// ConsumptionSample 是一筆已完成請求的實測值。用結構而非一長串位置參數，
// 之後要再加訊號時不必動所有呼叫端。
type ConsumptionSample struct {
	Tokens            int
	Complexity        int
	QualityTier       int
	PromptTokens      int
	ProseTokens       int
	ReasoningTokens   int
	StreamedTokens    int
	ReasoningReported bool
}

// -------------------------------------------------------------------------------------
// 產出比分級門檻。它量的是「這輪對話要模型產出多少」，與複雜度分級的母體不同：
// 只有已完成、且輸入量已知的請求才進得來。
//
// 以下為未指定進階設定時的預設邊界：
//   - 2% 以下：15k 輸入只換到 300 個 token 以下，形同沒有要求產出（工具呼叫、瑣事操作）
//   - 20% 以上：模型真的在生成內容
//
// 管理端可透過進階設定覆蓋這兩個值。
const (
	yieldLowMaxRatio = 0.02
	yieldMidMaxRatio = 0.20
)

// YieldThresholds 是產出比分級的兩個有效分界。高產出不另存一份門檻，
// 以免三個設定產生重疊或空白區間。
type YieldThresholds struct {
	LowMaxRatio float64
	MidMaxRatio float64
}

const (
	YieldTierLow  = "low"
	YieldTierMid  = "mid"
	YieldTierHigh = "high"
)

// -------------------------------------------------------------------------------------
// YieldTier 依單筆的輸出/輸入比分級。輸入量不明時回傳空字串 —— 這種樣本
// 不能歸到任何一級，否則會把分布往低產出拉。
func YieldTier(_tokens int, _promptTokens int) string {
	return YieldTierWithThresholds(_tokens, _promptTokens, DefaultYieldThresholds())
}

// -------------------------------------------------------------------------------------
func DefaultYieldThresholds() YieldThresholds {
	return YieldThresholds{LowMaxRatio: yieldLowMaxRatio, MidMaxRatio: yieldMidMaxRatio}
}

// -------------------------------------------------------------------------------------
// YieldTierWithThresholds 允許管理端即時套用設定值；不合法的門檻會退回預設值，
// 避免監看端因損壞的設定檔停止回應。
func YieldTierWithThresholds(_tokens int, _promptTokens int, _thresholds YieldThresholds) string {
	if _promptTokens <= 0 {
		return ""
	}
	if _thresholds.LowMaxRatio < 0 || _thresholds.MidMaxRatio <= _thresholds.LowMaxRatio {
		_thresholds = DefaultYieldThresholds()
	}
	_ratio := float64(_tokens) / float64(_promptTokens)
	switch {
	case _ratio <= _thresholds.LowMaxRatio:
		return YieldTierLow
	case _ratio <= _thresholds.MidMaxRatio:
		return YieldTierMid
	default:
		return YieldTierHigh
	}
}

// -------------------------------------------------------------------------------------
// DensityTier 是單一複雜度等級在視窗內的頻率。
type DensityTier struct {
	Count     int     `json:"count"`
	PerMinute float64 `json:"per_minute"`
	Share     float64 `json:"share"`
	// Tokens / TokensPerMinute 只計入這個等級中「已完成」的請求，
	// 因此三個等級的 Tokens 相加會等於 RequestDensity.Tokens，
	// 但 Count 相加等於 RequestDensity.Count（母體較大）。
	Tokens          int     `json:"tokens"`
	TokensPerMinute float64 `json:"tokens_per_minute"`
	// TokensPerRequest 是這個等級每筆完成請求的平均消耗，
	// 用來分辨「單筆很重」與「單筆很輕但打很多次」。
	TokensPerRequest float64 `json:"tokens_per_request"`
}

// -------------------------------------------------------------------------------------
// RequestDensity 描述一支金鑰在指定時間窗內的請求密集度，並依複雜度分級。
type RequestDensity struct {
	KeyID         string      `json:"key_id"`
	WindowSeconds float64     `json:"window_seconds"`
	Count         int         `json:"count"`
	PerMinute     float64     `json:"per_minute"`
	Low           DensityTier `json:"low"`
	Mid           DensityTier `json:"mid"`
	High          DensityTier `json:"high"`
	// CompletedRequests / Tokens 來自已完成的請求，母體與 Count 不同：
	// 進行中或失敗的請求只會出現在 Count。
	CompletedRequests int     `json:"completed_requests"`
	Tokens            int     `json:"tokens"`
	TokensPerMinute   float64 `json:"tokens_per_minute"`
	TokensPerRequest  float64 `json:"tokens_per_request"`
	// PromptTokens 是視窗內已完成請求的輸入量估算總和。
	PromptTokens int `json:"prompt_tokens"`
	// QualityTierAvg 是實際用到的模型等級平均（以請求數加權）。
	QualityTierAvg float64 `json:"quality_tier_avg"`
	// YieldLow/Mid/High 是依「逐筆」產出比分桶的結果，母體是已完成且輸入量已知的請求。
	// 它和 Low/Mid/High（複雜度分級）量的是不同的東西，兩組不會相等。
	YieldLow  DensityTier `json:"yield_low"`
	YieldMid  DensityTier `json:"yield_mid"`
	YieldHigh DensityTier `json:"yield_high"`
	// ProseRatio / ProseRatioMedian 是輸出裡「給人看的文字」佔的比例。
	// 母體只含串流回應（非串流沒有分類資料）。純工具呼叫的 turn 會是 0。
	ProseRatio       float64 `json:"prose_ratio"`
	ProseRatioMedian float64 `json:"prose_ratio_median"`
	// ProseSamples 是實際有分類資料的請求數。它必須被讀取端檢查：
	// 沒有樣本時比例會是 0，那和「真的沒有產出文字」是兩回事。
	ProseSamples int `json:"prose_samples"`
	// ReasoningTokens 是視窗內的推理量總和，ReasoningRatio 是它佔實際輸出的比例。
	// 推理在計費上就是輸出，但它是成本不是交付 —— 所以它進「輸出比」的分子，
	// 卻在「文字比」裡當分母的一部分。
	ReasoningTokens int     `json:"reasoning_tokens"`
	ReasoningRatio  float64 `json:"reasoning_ratio"`
	// ReasoningSamples 是「上游有回報推理」的請求數。比例只以這些樣本為母體，
	// 而且讀取端必須先檢查它 —— 0 代表量不到，不代表不需要推理。
	ReasoningSamples int `json:"reasoning_samples"`
	// OutputRatioMedian 是逐筆產出比的中位數。OutputRatio 是 Σ/Σ，會被單一
	// 超大 context 的請求主導；中位數才反映「多數對話長什麼樣」。
	OutputRatioMedian float64 `json:"output_ratio_median"`
	// OutputRatio 是輸出/輸入比。它是無因次的，不隨任務類型漂移：
	// 一般對話落在數十個百分點，agentic 的瑣事操作會掉到 1% 以下。
	// 高 QualityTierAvg 搭配極低 OutputRatio ＝ 拿高階模型跑基礎操作。
	OutputRatio float64 `json:"output_ratio"`
	// 以下欄位描述 Agent／OS 操作行為，與產出比分級維持獨立語意。
	ContinuationCount    int     `json:"continuation_count"`
	ContinuationRatio    float64 `json:"continuation_ratio"`
	FingerprintedCount   int     `json:"fingerprinted_requests"`
	RepeatedTaskCount    int     `json:"repeated_requests"`
	RepeatedTaskRatio    float64 `json:"repeated_task_ratio"`
	ToolCallCount        int     `json:"tool_call_count"`
	ToolCallsPerRequest  float64 `json:"tool_calls_per_request"`
	ToolRoundCount       int     `json:"tool_round_count"`
	ToolRoundsPerRequest float64 `json:"tool_rounds_per_request"`
	ToolOutputTokens     int     `json:"tool_output_tokens"`
	FirstAt              string  `json:"first_at,omitempty"`
	LastAt               string  `json:"last_at,omitempty"`
	// Truncated 表示樣本可能不完整（超出保留期限或達到每金鑰上限），
	// 此時各項 Count 是下限而非精確值。
	Truncated bool `json:"truncated"`
}

// -------------------------------------------------------------------------------------
// RecordComplexity 記錄一筆「已完成分類」請求的複雜度，兩邊都寫：
//   - 記憶體視窗：分鐘級，供即時密集度判斷
//   - 每月統計檔：天粒度，可跨重啟累積供長期觀察
//
// 刻意不動 Days：那是授權階段就累加的母體，這裡只附加分級資訊。
func (_r *Recorder) RecordComplexity(_keyID string, _at time.Time, _complexity int) error {
	return _r.RecordRequest(_keyID, _at, RequestSample{Complexity: _complexity})
}

// RecordRequest 同時記錄複雜度與 Agent 行為訊號。
func (_r *Recorder) RecordRequest(_keyID string, _at time.Time, _sample RequestSample) error {
	_keyID = strings.TrimSpace(_keyID)
	if _r == nil || _keyID == "" {
		return nil
	}
	if _at.IsZero() {
		_at = time.Now()
	}
	_at = _at.Local()
	_repeated := _r.noteSample(_keyID, _at, _sample)
	return _r.recordRequestToMonth(_keyID, _at, _sample, _repeated)
}

// -------------------------------------------------------------------------------------
func (_r *Recorder) recordRequestToMonth(_keyID string, _at time.Time, _sample RequestSample, _repeated bool) error {
	_month := _at.Format("2006-01")
	_day := _at.Format("2006-01-02")

	_r.lock.Lock()
	defer _r.lock.Unlock()

	_file, _err := _r.loadMonthLocked(_keyID, _month)
	if _err != nil {
		return _err
	}
	if _file.Complexity == nil {
		_file.Complexity = map[string]ComplexityCounts{}
	}
	_counts := _file.Complexity[_day]
	switch ComplexityTier(_sample.Complexity) {
	case ComplexityTierHigh:
		_counts.High++
	case ComplexityTierMid:
		_counts.Mid++
	default:
		_counts.Low++
	}
	_file.KeyID = _keyID
	_file.Month = _month
	_file.Complexity[_day] = _counts
	if _file.Activity == nil {
		_file.Activity = map[string]ActivityCounts{}
	}
	_activity := _file.Activity[_day]
	_activity.Requests++
	if _sample.Continuation {
		_activity.Continuations++
	}
	if strings.TrimSpace(_sample.Fingerprint) != "" {
		_activity.FingerprintedRequests++
	}
	if _repeated {
		_activity.RepeatedRequests++
	}
	_activity.ToolCalls += int64(maxInt(_sample.ToolCalls, 0))
	_activity.ToolRounds += int64(maxInt(_sample.ToolRounds, 0))
	_activity.ToolOutputTokens += int64(maxInt(_sample.ToolOutputTokens, 0))
	_file.Activity[_day] = _activity
	_file.UpdatedAt = time.Now().Format(time.RFC3339)
	_cacheKey := monthCacheKey(_keyID, _month)
	_r.files[_cacheKey] = _file
	_r.dirty[_cacheKey] = true
	_r.scheduleFlushLocked()
	return nil
}

// -------------------------------------------------------------------------------------
func (_r *Recorder) noteSample(_keyID string, _at time.Time, _sample RequestSample) bool {
	if _r == nil || strings.TrimSpace(_keyID) == "" {
		return false
	}
	if _at.IsZero() {
		_at = time.Now()
	}

	_r.recentLock.Lock()
	defer _r.recentLock.Unlock()

	if _r.recent == nil {
		_r.recent = map[string][]recentSample{}
	}
	_entries := pruneRecentSamples(_r.recent[_keyID], _at.Add(-recentRequestRetention).UnixNano())
	_repeated := false
	if _sample.Fingerprint != "" {
		for _, _entry := range _entries {
			if _entry.Fingerprint == _sample.Fingerprint {
				_repeated = true
				break
			}
		}
	}
	_entries = append(_entries, recentSample{
		At:               _at.UnixNano(),
		Complexity:       _sample.Complexity,
		Continuation:     _sample.Continuation,
		Fingerprint:      strings.TrimSpace(_sample.Fingerprint),
		ToolCalls:        maxInt(_sample.ToolCalls, 0),
		ToolRounds:       maxInt(_sample.ToolRounds, 0),
		ToolOutputTokens: maxInt(_sample.ToolOutputTokens, 0),
	})
	if len(_entries) > maxRecentRequestsPerKey {
		_entries = _entries[len(_entries)-maxRecentRequestsPerKey:]
	}
	_r.recent[_keyID] = _entries
	_r.sweepIdleKeysLocked(_at)
	return _repeated
}

// -------------------------------------------------------------------------------------
// RequestDensity 回傳指定時間窗內的請求頻率，並依複雜度等級分開統計。
// 視窗超過保留期限時會被截到保留期限，並把 Truncated 標為 true。
func (_r *Recorder) RequestDensity(_keyID string, _window time.Duration) RequestDensity {
	return _r.RequestDensityWithYieldThresholds(_keyID, _window, DefaultYieldThresholds())
}

// -------------------------------------------------------------------------------------
// RequestDensityWithYieldThresholds 與 RequestDensity 相同，但使用指定的產出比分界。
func (_r *Recorder) RequestDensityWithYieldThresholds(_keyID string, _window time.Duration, _thresholds YieldThresholds) RequestDensity {
	_keyID = strings.TrimSpace(_keyID)
	_result := RequestDensity{KeyID: _keyID}
	if _r == nil || _keyID == "" || _window <= 0 {
		return _result
	}
	if _window > recentRequestRetention {
		_window = recentRequestRetention
		_result.Truncated = true
	}
	_result.WindowSeconds = _window.Seconds()

	_cutoff := time.Now().Add(-_window).UnixNano()

	_tierCounts := map[string]int{}
	_tierTokens := map[string]int{}
	_tierCompleted := map[string]int{}
	_qualityTotal := 0
	_qualityCount := 0
	_yieldCounts := map[string]int{}
	_yieldTokens := map[string]int{}
	_ratios := make([]float64, 0)
	_proseTotal := 0
	_streamedTotal := 0
	_proseRatios := make([]float64, 0)
	_reasoningBase := 0
	_fingerprints := map[string]int{}

	_r.recentLock.Lock()
	_entries := _r.recent[_keyID]
	_inWindow := make([]recentSample, 0, len(_entries))
	for _, _sample := range _entries {
		if _sample.At >= _cutoff {
			_inWindow = append(_inWindow, _sample)
			_tierCounts[ComplexityTier(_sample.Complexity)]++
			if _sample.Continuation {
				_result.ContinuationCount++
			}
			if _sample.Fingerprint != "" {
				_result.FingerprintedCount++
				if _fingerprints[_sample.Fingerprint] > 0 {
					_result.RepeatedTaskCount++
				}
				_fingerprints[_sample.Fingerprint]++
			}
			_result.ToolCallCount += _sample.ToolCalls
			_result.ToolRoundCount += _sample.ToolRounds
			_result.ToolOutputTokens += _sample.ToolOutputTokens
		}
	}
	if len(_entries) >= maxRecentRequestsPerKey {
		_result.Truncated = true
	}
	for _, _sample := range _r.consumed[_keyID] {
		if _sample.At < _cutoff {
			continue
		}
		_tier := ComplexityTier(_sample.Complexity)
		_result.CompletedRequests++
		_result.Tokens += _sample.Tokens
		_result.PromptTokens += _sample.PromptTokens
		_tierCompleted[_tier]++
		_tierTokens[_tier] += _sample.Tokens
		if _sample.QualityTier > 0 {
			_qualityTotal += _sample.QualityTier
			_qualityCount++
		}
		// 非串流回應拆不出用途分類，整筆略過而不是當成 0 ——
		// 留成 0 會被誤讀成「整輪都在呼叫工具」。
		if _sample.ReasoningReported {
			_result.ReasoningSamples++
			_result.ReasoningTokens += _sample.ReasoningTokens
			_reasoningBase += _sample.Tokens
		}
		if _sample.StreamedTokens > 0 && _sample.Tokens > 0 {
			_proseTotal += _sample.ProseTokens
			_streamedTotal += _sample.Tokens
			_proseRatios = append(_proseRatios, clampRatio(float64(_sample.ProseTokens)/float64(_sample.Tokens)))
		}
		if _yield := YieldTierWithThresholds(_sample.Tokens, _sample.PromptTokens, _thresholds); _yield != "" {
			_yieldCounts[_yield]++
			_yieldTokens[_yield] += _sample.Tokens
			_ratios = append(_ratios, float64(_sample.Tokens)/float64(_sample.PromptTokens))
		}
	}
	_r.recentLock.Unlock()

	_result.Count = len(_inWindow)
	if _result.Count == 0 && _result.CompletedRequests == 0 {
		return _result
	}

	_minutes := _window.Minutes()
	_result.Low = buildDensityTier(ComplexityTierLow, _tierCounts, _tierCompleted, _tierTokens, _result.Count, _minutes)
	_result.Mid = buildDensityTier(ComplexityTierMid, _tierCounts, _tierCompleted, _tierTokens, _result.Count, _minutes)
	_result.High = buildDensityTier(ComplexityTierHigh, _tierCounts, _tierCompleted, _tierTokens, _result.Count, _minutes)
	if _minutes > 0 {
		_result.PerMinute = float64(_result.Count) / _minutes
		_result.TokensPerMinute = float64(_result.Tokens) / _minutes
	}
	if _result.CompletedRequests > 0 {
		_result.TokensPerRequest = float64(_result.Tokens) / float64(_result.CompletedRequests)
	}
	if _result.Count > 0 {
		_result.ContinuationRatio = float64(_result.ContinuationCount) / float64(_result.Count)
		_result.ToolCallsPerRequest = float64(_result.ToolCallCount) / float64(_result.Count)
		_result.ToolRoundsPerRequest = float64(_result.ToolRoundCount) / float64(_result.Count)
	}
	if _result.FingerprintedCount > 0 {
		_result.RepeatedTaskRatio = float64(_result.RepeatedTaskCount) / float64(_result.FingerprintedCount)
	}
	// 未設定等級的樣本不列入平均，否則會被 0 拉低。
	if _qualityCount > 0 {
		_result.QualityTierAvg = float64(_qualityTotal) / float64(_qualityCount)
	}
	if _result.PromptTokens > 0 {
		_result.OutputRatio = float64(_result.Tokens) / float64(_result.PromptTokens)
	}
	// 產出比分級的母體是「已完成且輸入量已知」的請求，不是 Count。
	_yielded := len(_ratios)
	_result.YieldLow = buildDensityTier(YieldTierLow, _yieldCounts, _yieldCounts, _yieldTokens, _yielded, _minutes)
	_result.YieldMid = buildDensityTier(YieldTierMid, _yieldCounts, _yieldCounts, _yieldTokens, _yielded, _minutes)
	_result.YieldHigh = buildDensityTier(YieldTierHigh, _yieldCounts, _yieldCounts, _yieldTokens, _yielded, _minutes)
	_result.OutputRatioMedian = medianRatio(_ratios)
	if _streamedTotal > 0 {
		_result.ProseRatio = clampRatio(float64(_proseTotal) / float64(_streamedTotal))
	}
	_result.ProseRatioMedian = medianRatio(_proseRatios)
	_result.ProseSamples = len(_proseRatios)
	// 母體只含有回報的樣本，否則沒回報的請求會把比例稀釋到 0。
	if _reasoningBase > 0 {
		_result.ReasoningRatio = clampRatio(float64(_result.ReasoningTokens) / float64(_reasoningBase))
	}
	if len(_inWindow) == 0 {
		return _result
	}

	_result.FirstAt = time.Unix(0, _inWindow[0].At).Format(time.RFC3339)
	_result.LastAt = time.Unix(0, _inWindow[len(_inWindow)-1].At).Format(time.RFC3339)
	return _result
}

// -------------------------------------------------------------------------------------
func buildDensityTier(_name string, _counts, _completed, _tokens map[string]int, _total int, _minutes float64) DensityTier {
	_count := _counts[_name]
	_tier := DensityTier{Count: _count, Tokens: _tokens[_name]}
	if _minutes > 0 {
		_tier.PerMinute = float64(_count) / _minutes
		_tier.TokensPerMinute = float64(_tier.Tokens) / _minutes
	}
	if _total > 0 {
		_tier.Share = float64(_count) / float64(_total)
	}
	if _done := _completed[_name]; _done > 0 {
		_tier.TokensPerRequest = float64(_tier.Tokens) / float64(_done)
	}
	return _tier
}

// -------------------------------------------------------------------------------------
// clampRatio 把比例壓在 0..1。分子是字元估算、分母是上游回報的真實值，
// 估算偏高時可能超過 1；那是估算誤差，不是真的「產出超過總量」。
func clampRatio(_value float64) float64 {
	if _value < 0 {
		return 0
	}
	if _value > 1 {
		return 1
	}
	return _value
}

// -------------------------------------------------------------------------------------
// medianRatio 取中位數。偶數筆時取兩個中間值的平均。
func medianRatio(_values []float64) float64 {
	if len(_values) == 0 {
		return 0
	}
	_sorted := append([]float64(nil), _values...)
	sort.Float64s(_sorted)
	_mid := len(_sorted) / 2
	if len(_sorted)%2 == 1 {
		return _sorted[_mid]
	}
	return (_sorted[_mid-1] + _sorted[_mid]) / 2
}

func maxInt(_left int, _right int) int {
	if _left > _right {
		return _left
	}
	return _right
}

// -------------------------------------------------------------------------------------
func pruneRecentSamples(_entries []recentSample, _cutoff int64) []recentSample {
	_start := 0
	for _start < len(_entries) && _entries[_start].At < _cutoff {
		_start++
	}
	if _start == 0 {
		return _entries
	}
	return append(_entries[:0], _entries[_start:]...)
}

// -------------------------------------------------------------------------------------
// sweepIdleKeysLocked 清掉整個保留期限都沒有流量的金鑰。呼叫端必須持有 recentLock。
func (_r *Recorder) sweepIdleKeysLocked(_now time.Time) {
	if _now.Sub(_r.lastSweepAt) < recentSweepInterval {
		return
	}
	_r.lastSweepAt = _now
	_cutoff := _now.Add(-recentRequestRetention).UnixNano()
	for _key, _entries := range _r.recent {
		_kept := pruneRecentSamples(_entries, _cutoff)
		if len(_kept) == 0 {
			delete(_r.recent, _key)
			continue
		}
		_r.recent[_key] = _kept
	}
	for _key, _entries := range _r.consumed {
		_kept := pruneConsumedSamples(_entries, _cutoff)
		if len(_kept) == 0 {
			delete(_r.consumed, _key)
			continue
		}
		_r.consumed[_key] = _kept
	}
}

// -------------------------------------------------------------------------------------
// SetDefaultRecorderForTest 替換預設 Recorder，回傳原本的值供測試還原。
func SetDefaultRecorderForTest(_recorder *Recorder) *Recorder {
	_previous := _defaultRecorder
	if _recorder != nil {
		_defaultRecorder = _recorder
	}
	return _previous
}

// -------------------------------------------------------------------------------------
// AllRequestDensity 回傳目前視窗內有流量的所有金鑰密集度，依請求數由多到少排序。
// 沒有樣本的金鑰不會出現在結果中 —— 呼叫端若需要「全部金鑰」，
// 應自行以金鑰清單為主體再併入這裡的資料。
func (_r *Recorder) AllRequestDensity(_window time.Duration) []RequestDensity {
	return _r.AllRequestDensityWithYieldThresholds(_window, DefaultYieldThresholds())
}

// -------------------------------------------------------------------------------------
// AllRequestDensityWithYieldThresholds 使用指定產出比分界彙整全部金鑰。
func (_r *Recorder) AllRequestDensityWithYieldThresholds(_window time.Duration, _thresholds YieldThresholds) []RequestDensity {
	if _r == nil || _window <= 0 {
		return []RequestDensity{}
	}

	// 兩組樣本各自獨立汰除，取聯集才不會漏掉「請求樣本已過期、但消耗還在視窗內」的金鑰。
	_r.recentLock.Lock()
	_seen := make(map[string]bool, len(_r.recent)+len(_r.consumed))
	_keys := make([]string, 0, len(_r.recent)+len(_r.consumed))
	for _key := range _r.recent {
		if !_seen[_key] {
			_seen[_key] = true
			_keys = append(_keys, _key)
		}
	}
	for _key := range _r.consumed {
		if !_seen[_key] {
			_seen[_key] = true
			_keys = append(_keys, _key)
		}
	}
	_r.recentLock.Unlock()

	_result := make([]RequestDensity, 0, len(_keys))
	for _, _key := range _keys {
		_density := _r.RequestDensityWithYieldThresholds(_key, _window, _thresholds)
		if _density.Count == 0 && _density.CompletedRequests == 0 {
			continue
		}
		_result = append(_result, _density)
	}
	// 以實際消耗為主鍵排序：它是唯一直接對應配額的數字。
	sort.Slice(_result, func(_i int, _j int) bool {
		if _result[_i].Tokens != _result[_j].Tokens {
			return _result[_i].Tokens > _result[_j].Tokens
		}
		if _result[_i].Count != _result[_j].Count {
			return _result[_i].Count > _result[_j].Count
		}
		return _result[_i].KeyID < _result[_j].KeyID
	})
	return _result
}

// -------------------------------------------------------------------------------------
// RecordConsumption 記錄一筆「已完成」請求的實際輸出 token 數。
// 只寫記憶體視窗與每月統計，不與複雜度樣本關聯 —— 偵測用的是聚合特徵。
func (_r *Recorder) RecordConsumption(_keyID string, _at time.Time, _sample ConsumptionSample) error {
	_keyID = strings.TrimSpace(_keyID)
	if _r == nil || _keyID == "" || _sample.Tokens <= 0 {
		return nil
	}
	if _at.IsZero() {
		_at = time.Now()
	}
	_at = _at.Local()

	_r.recentLock.Lock()
	if _r.consumed == nil {
		_r.consumed = map[string][]consumedSample{}
	}
	_entries := append(_r.consumed[_keyID], consumedSample{
		At:                _at.UnixNano(),
		Tokens:            _sample.Tokens,
		Complexity:        _sample.Complexity,
		QualityTier:       _sample.QualityTier,
		PromptTokens:      _sample.PromptTokens,
		ProseTokens:       _sample.ProseTokens,
		ReasoningTokens:   _sample.ReasoningTokens,
		StreamedTokens:    _sample.StreamedTokens,
		ReasoningReported: _sample.ReasoningReported,
	})
	_entries = pruneConsumedSamples(_entries, _at.Add(-recentRequestRetention).UnixNano())
	if len(_entries) > maxRecentRequestsPerKey {
		_entries = _entries[len(_entries)-maxRecentRequestsPerKey:]
	}
	_r.consumed[_keyID] = _entries
	_r.recentLock.Unlock()

	return _r.recordTokensToMonth(_keyID, _at, _sample.Tokens)
}

// -------------------------------------------------------------------------------------
func (_r *Recorder) recordTokensToMonth(_keyID string, _at time.Time, _tokens int) error {
	_month := _at.Format("2006-01")
	_day := _at.Format("2006-01-02")

	_r.lock.Lock()
	defer _r.lock.Unlock()

	_file, _err := _r.loadMonthLocked(_keyID, _month)
	if _err != nil {
		return _err
	}
	if _file.Tokens == nil {
		_file.Tokens = map[string]int64{}
	}
	_file.KeyID = _keyID
	_file.Month = _month
	_file.Tokens[_day] += int64(_tokens)
	_file.UpdatedAt = time.Now().Format(time.RFC3339)
	_cacheKey := monthCacheKey(_keyID, _month)
	_r.files[_cacheKey] = _file
	_r.dirty[_cacheKey] = true
	_r.scheduleFlushLocked()
	return nil
}

// -------------------------------------------------------------------------------------
func pruneConsumedSamples(_entries []consumedSample, _cutoff int64) []consumedSample {
	_start := 0
	for _start < len(_entries) && _entries[_start].At < _cutoff {
		_start++
	}
	if _start == 0 {
		return _entries
	}
	return append(_entries[:0], _entries[_start:]...)
}
