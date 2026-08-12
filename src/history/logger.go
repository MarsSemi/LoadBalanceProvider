package history

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"LoadBalanceProvider/src/balancer"
	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
const DefaultRoot = "data/history"

const (
	historyFlushDelay       = 2 * time.Second
	historyFallbackCacheTTL = 30 * time.Second
)

// -------------------------------------------------------------------------------------
var _defaultLogger = NewLogger(DefaultRoot)

// -------------------------------------------------------------------------------------
type Logger struct {
	Root             string
	_lock            sync.Mutex
	_days            map[string]DayFile
	_dirty           map[string]bool
	_flushTimer      *time.Timer
	_fallbackCache   map[string]ProviderMetricFallback
	_fallbackExpires time.Time
}

// -------------------------------------------------------------------------------------
type ChatRecord struct {
	RequestID             string                            `json:"request_id"`
	Timestamp             string                            `json:"timestamp"`
	StartedAt             string                            `json:"started_at"`
	EndedAt               string                            `json:"ended_at"`
	DurationMS            int64                             `json:"duration_ms"`
	Success               bool                              `json:"success"`
	Error                 string                            `json:"error,omitempty"`
	Stream                bool                              `json:"stream"`
	RequestedModel        string                            `json:"requested_model"`
	SelectedProviderID    string                            `json:"selected_provider_id,omitempty"`
	SelectedProvider      string                            `json:"selected_provider,omitempty"`
	SelectedProviderKind  string                            `json:"selected_provider_kind,omitempty"`
	SelectedProviderRole  string                            `json:"selected_provider_role,omitempty"`
	SelectedModel         string                            `json:"selected_model,omitempty"`
	Strategy              string                            `json:"strategy,omitempty"`
	SelectionReason       string                            `json:"selection_reason,omitempty"`
	CandidateCount        int                               `json:"candidate_count"`
	Candidates            []balancer.SelectionCandidateMeta `json:"candidates,omitempty"`
	TaskType              string                            `json:"task_type"`
	ComplexityScore       int                               `json:"complexity_score"`
	HardRequirements      []string                          `json:"hard_requirements,omitempty"`
	Signals               []string                          `json:"signals,omitempty"`
	InputCharacters       int                               `json:"input_characters"`
	EstimatedInputTokens  int                               `json:"estimated_input_tokens"`
	RequestedOutputTokens int                               `json:"requested_output_tokens"`
	CompletionTokens      int                               `json:"completion_tokens,omitempty"`
	ReactionTimeMS        float64                           `json:"reaction_time_ms,omitempty"`
	TokenGenerationSpeed  float64                           `json:"token_generation_speed,omitempty"`
	ClientDeliverySpeed   float64                           `json:"client_delivery_tps,omitempty"`
	TokensEstimated       bool                              `json:"tokens_estimated,omitempty"`
	MessageCount          int                               `json:"message_count"`
	ProviderSuccesses     int64                             `json:"provider_successes,omitempty"`
	ProviderFailures      int64                             `json:"provider_failures,omitempty"`
	ProviderActive        int64                             `json:"provider_active,omitempty"`
	ProviderLatencyP50MS  float64                           `json:"provider_latency_p50_ms,omitempty"`
	ProviderLatencyP95MS  float64                           `json:"provider_latency_p95_ms,omitempty"`
	ProviderReactionMS    float64                           `json:"provider_reaction_time_ms,omitempty"`
	ProviderTokenSpeed    float64                           `json:"provider_token_generation_speed,omitempty"`
	ProviderLastTokens    int64                             `json:"provider_last_completion_tokens,omitempty"`
	ProviderCircuitOpen   bool                              `json:"provider_circuit_open"`
}

// -------------------------------------------------------------------------------------
type DayFile struct {
	Date            string                     `json:"date"`
	UpdatedAt       string                     `json:"updated_at"`
	ProviderSummary map[string]ProviderSummary `json:"provider_summary"`
	Records         []ChatRecord               `json:"records"`
}

// -------------------------------------------------------------------------------------
type ProviderSummary struct {
	ProviderID            string  `json:"provider_id"`
	ProviderName          string  `json:"provider_name"`
	ProviderKind          string  `json:"provider_kind,omitempty"`
	SelectedCount         int64   `json:"selected_count"`
	SuccessCount          int64   `json:"success_count"`
	FailureCount          int64   `json:"failure_count"`
	TotalDurationMS       int64   `json:"total_duration_ms"`
	TotalCompletionTokens int64   `json:"total_completion_tokens,omitempty"`
	AverageDuration       float64 `json:"average_duration_ms"`
	LastTokenSpeed        float64 `json:"last_token_generation_speed,omitempty"`
	LastClientDeliveryTPS float64 `json:"last_client_delivery_tps,omitempty"`
	LastTokens            int64   `json:"last_completion_tokens,omitempty"`
	LastReactionMS        float64 `json:"last_reaction_time_ms,omitempty"`
	LastDurationMS        int64   `json:"last_duration_ms,omitempty"`
	LastSelectedAt        string  `json:"last_selected_at"`
	LastModel             string  `json:"last_model,omitempty"`
	LastStrategy          string  `json:"last_strategy,omitempty"`
}

// -------------------------------------------------------------------------------------
func NewLogger(_root string) *Logger {
	if _root == "" {
		_root = DefaultRoot
	}
	return &Logger{
		Root:   _root,
		_days:  map[string]DayFile{},
		_dirty: map[string]bool{},
	}
}

// -------------------------------------------------------------------------------------
func RecordChat(_record ChatRecord) error {
	return _defaultLogger.RecordChat(_record)
}

// -------------------------------------------------------------------------------------
func (_l *Logger) RecordChat(_record ChatRecord) error {
	if _l == nil {
		return nil
	}

	_now := time.Now()
	if _record.RequestID == "" {
		_record.RequestID = NewRequestID()
	}
	if _record.Timestamp == "" {
		_record.Timestamp = _now.Format(time.RFC3339Nano)
	}

	_l._lock.Lock()
	defer _l._lock.Unlock()

	_path := _l.dayPath(_now)
	_day, _err := _l.loadDayLocked(_path, _now)
	if _err != nil {
		return _err
	}

	_day.Records = append(_day.Records, _record)
	_day.UpdatedAt = _now.Format(time.RFC3339Nano)
	_day.updateProviderSummary(_record)
	_l._days[_path] = _day
	_l._dirty[_path] = true
	_l.scheduleFlushLocked()
	return nil
}

// Flush persists all pending daily history files. It is safe to call during shutdown.
func (_l *Logger) Flush() error {
	if _l == nil {
		return nil
	}
	_l._lock.Lock()
	defer _l._lock.Unlock()
	return _l.flushLocked()
}

func Flush() error {
	return _defaultLogger.Flush()
}

func (_l *Logger) loadDayLocked(_path string, _now time.Time) (DayFile, error) {
	if _day, _ok := _l._days[_path]; _ok {
		return _day, nil
	}
	_day, _err := readDayFile(_path, _now)
	if _err == nil {
		_l._days[_path] = _day
	}
	return _day, _err
}

func (_l *Logger) scheduleFlushLocked() {
	if _l._flushTimer != nil {
		return
	}
	_l._flushTimer = time.AfterFunc(historyFlushDelay, func() {
		if _err := _l.Flush(); _err != nil {
			log.Printf("chat history flush failed: %v", _err)
		}
	})
}

func (_l *Logger) flushLocked() error {
	if _l._flushTimer != nil {
		_l._flushTimer.Stop()
		_l._flushTimer = nil
	}
	for _path := range _l._dirty {
		_day, _ok := _l._days[_path]
		if !_ok {
			delete(_l._dirty, _path)
			continue
		}
		if _err := writeDayFile(_path, _day); _err != nil {
			return _err
		}
		delete(_l._dirty, _path)
	}
	return nil
}

// -------------------------------------------------------------------------------------
func (_l *Logger) dayPath(_now time.Time) string {
	_month := _now.Format("200601")
	_day := _now.Format("20060102")
	return filepath.Join(_l.Root, _month, _day+".json")
}

// -------------------------------------------------------------------------------------
func readDayFile(_path string, _now time.Time) (DayFile, error) {
	_day := DayFile{
		Date:            _now.Format("20060102"),
		ProviderSummary: map[string]ProviderSummary{},
		Records:         []ChatRecord{},
	}

	_bytes, _err := os.ReadFile(_path)
	if _err != nil {
		if os.IsNotExist(_err) {
			return _day, nil
		}
		return _day, _err
	}
	if len(_bytes) == 0 {
		return _day, nil
	}

	if _err := json.Unmarshal(_bytes, &_day); _err != nil {
		return _day, _err
	}
	if _day.ProviderSummary == nil {
		_day.ProviderSummary = map[string]ProviderSummary{}
	}
	if _day.Records == nil {
		_day.Records = []ChatRecord{}
	}
	return _day, nil
}

// -------------------------------------------------------------------------------------
func writeDayFile(_path string, _day DayFile) error {
	_dir := filepath.Dir(_path)
	if _err := os.MkdirAll(_dir, 0755); _err != nil {
		return _err
	}

	_bytes, _err := json.MarshalIndent(_day, "", "  ")
	if _err != nil {
		return _err
	}
	_bytes = append(_bytes, '\n')

	_tmpPath := fmt.Sprintf("%s.tmp.%d", _path, time.Now().UnixNano())
	if _err := os.WriteFile(_tmpPath, _bytes, 0644); _err != nil {
		return _err
	}
	return os.Rename(_tmpPath, _path)
}

// -------------------------------------------------------------------------------------
func (_d *DayFile) updateProviderSummary(_record ChatRecord) {
	if _record.SelectedProviderID == "" {
		return
	}
	if _d.ProviderSummary == nil {
		_d.ProviderSummary = map[string]ProviderSummary{}
	}

	_summary := _d.ProviderSummary[_record.SelectedProviderID]
	_summary.ProviderID = _record.SelectedProviderID
	_summary.ProviderName = _record.SelectedProvider
	_summary.ProviderKind = _record.SelectedProviderKind
	_summary.SelectedCount++
	if _record.Success {
		_summary.SuccessCount++
	} else {
		_summary.FailureCount++
	}
	_summary.TotalDurationMS += _record.DurationMS
	if _record.CompletionTokens > 0 {
		_summary.TotalCompletionTokens += int64(_record.CompletionTokens)
	}
	if _summary.SelectedCount > 0 {
		_summary.AverageDuration = float64(_summary.TotalDurationMS) / float64(_summary.SelectedCount)
	}
	_summary.LastSelectedAt = _record.Timestamp
	_summary.LastModel = _record.SelectedModel
	_summary.LastStrategy = _record.Strategy
	if _record.TokenGenerationSpeed > 0 {
		_summary.LastTokenSpeed = _record.TokenGenerationSpeed
	} else if _record.ProviderTokenSpeed > 0 {
		_summary.LastTokenSpeed = _record.ProviderTokenSpeed
	}
	if _record.ClientDeliverySpeed > 0 {
		_summary.LastClientDeliveryTPS = _record.ClientDeliverySpeed
	}
	if _record.CompletionTokens > 0 {
		_summary.LastTokens = int64(_record.CompletionTokens)
	} else if _record.ProviderLastTokens > 0 {
		_summary.LastTokens = _record.ProviderLastTokens
	}
	if _record.ReactionTimeMS > 0 {
		_summary.LastReactionMS = _record.ReactionTimeMS
	}
	if _record.DurationMS > 0 {
		_summary.LastDurationMS = _record.DurationMS
	}
	_d.ProviderSummary[_record.SelectedProviderID] = _summary
}

// -------------------------------------------------------------------------------------
type ProviderMetricFallback struct {
	TokenSpeed            float64
	ClientDeliveryTPS     float64
	Tokens                int64
	ReactionMS            float64
	DurationMS            float64
	TotalRequests         int64
	TotalCompletionTokens int64
	Successes             int64
	Failures              int64
}

// -------------------------------------------------------------------------------------
func RecentProviderMetricFallbacks() map[string]ProviderMetricFallback {
	return _defaultLogger.RecentProviderMetricFallbacks()
}

// -------------------------------------------------------------------------------------
func (_l *Logger) RecentProviderMetricFallbacks() map[string]ProviderMetricFallback {
	if _l == nil {
		return map[string]ProviderMetricFallback{}
	}

	_l._lock.Lock()
	defer _l._lock.Unlock()
	_now := time.Now()
	if _now.Before(_l._fallbackExpires) && _l._fallbackCache != nil {
		return cloneProviderMetricFallbacks(_l._fallbackCache)
	}
	if _err := _l.flushLocked(); _err != nil {
		log.Printf("chat history flush before metrics scan failed: %v", _err)
	}

	_result := map[string]ProviderMetricFallback{}

	_files, _err := filepath.Glob(filepath.Join(_l.Root, "*", "*.json"))
	if _err != nil || len(_files) == 0 {
		_l._fallbackCache = _result
		_l._fallbackExpires = _now.Add(historyFallbackCacheTTL)
		return cloneProviderMetricFallbacks(_result)
	}
	sort.Strings(_files)

	for _fileIdx := len(_files) - 1; _fileIdx >= 0; _fileIdx-- {
		_day, _err := readDayFileByPath(_files[_fileIdx])
		if _err != nil {
			continue
		}
		_recordTokenTotals := providerRecordTokenTotals(_day.Records)
		_recordClientDeliveryTPS := latestProviderRecordClientDeliveryTPS(_day.Records)
		for _providerID, _summary := range _day.ProviderSummary {
			_metric := _result[_providerID]
			_metric.TotalRequests += _summary.SelectedCount
			_metric.Successes += _summary.SuccessCount
			_metric.Failures += _summary.FailureCount
			_recordTokens := _recordTokenTotals[_providerID]
			if _summary.TotalCompletionTokens > _recordTokens {
				_metric.TotalCompletionTokens += _summary.TotalCompletionTokens
			} else if _recordTokens > 0 {
				_metric.TotalCompletionTokens += _recordTokens
			}
			if _metric.TokenSpeed <= 0 && _summary.LastTokenSpeed > 0 {
				_metric.TokenSpeed = _summary.LastTokenSpeed
			}
			if _metric.ClientDeliveryTPS <= 0 && _summary.LastClientDeliveryTPS > 0 {
				_metric.ClientDeliveryTPS = _summary.LastClientDeliveryTPS
			} else if _metric.ClientDeliveryTPS <= 0 && _recordClientDeliveryTPS[_providerID] > 0 {
				_metric.ClientDeliveryTPS = _recordClientDeliveryTPS[_providerID]
			}
			if _metric.Tokens <= 0 && _summary.LastTokens > 0 {
				_metric.Tokens = _summary.LastTokens
			}
			if _metric.ReactionMS <= 0 && _summary.LastReactionMS > 0 {
				_metric.ReactionMS = _summary.LastReactionMS
			}
			if _metric.DurationMS <= 0 && _summary.LastDurationMS > 0 {
				_metric.DurationMS = float64(_summary.LastDurationMS)
			}
			_result[_providerID] = _metric
		}
		if len(_day.ProviderSummary) == 0 {
			for _recordIdx := len(_day.Records) - 1; _recordIdx >= 0; _recordIdx-- {
				_record := _day.Records[_recordIdx]
				if _record.SelectedProviderID == "" {
					continue
				}
				_metric := _result[_record.SelectedProviderID]
				_metric.TotalRequests++
				if _record.Success {
					_metric.Successes++
				} else {
					_metric.Failures++
				}
				if _record.CompletionTokens > 0 {
					_metric.TotalCompletionTokens += int64(_record.CompletionTokens)
				}
				if _metric.TokenSpeed <= 0 && _record.TokenGenerationSpeed > 0 {
					_metric.TokenSpeed = _record.TokenGenerationSpeed
				}
				if _metric.ClientDeliveryTPS <= 0 && _record.ClientDeliverySpeed > 0 {
					_metric.ClientDeliveryTPS = _record.ClientDeliverySpeed
				}
				if _metric.Tokens <= 0 && _record.CompletionTokens > 0 {
					_metric.Tokens = int64(_record.CompletionTokens)
				}
				if _metric.ReactionMS <= 0 && _record.ReactionTimeMS > 0 {
					_metric.ReactionMS = _record.ReactionTimeMS
				}
				if _metric.DurationMS <= 0 && _record.DurationMS > 0 {
					_metric.DurationMS = float64(_record.DurationMS)
				}
				_result[_record.SelectedProviderID] = _metric
			}
		}
	}
	_l._fallbackCache = _result
	_l._fallbackExpires = _now.Add(historyFallbackCacheTTL)
	return cloneProviderMetricFallbacks(_result)
}

func cloneProviderMetricFallbacks(_source map[string]ProviderMetricFallback) map[string]ProviderMetricFallback {
	_result := make(map[string]ProviderMetricFallback, len(_source))
	for _providerID, _metric := range _source {
		_result[_providerID] = _metric
	}
	return _result
}

// -------------------------------------------------------------------------------------
func providerRecordTokenTotals(_records []ChatRecord) map[string]int64 {
	_totals := map[string]int64{}
	for _, _record := range _records {
		if _record.SelectedProviderID == "" || _record.CompletionTokens <= 0 {
			continue
		}
		_totals[_record.SelectedProviderID] += int64(_record.CompletionTokens)
	}
	return _totals
}

// -------------------------------------------------------------------------------------
func latestProviderRecordClientDeliveryTPS(_records []ChatRecord) map[string]float64 {
	_latest := map[string]float64{}
	for _idx := len(_records) - 1; _idx >= 0; _idx-- {
		_record := _records[_idx]
		if _record.SelectedProviderID == "" || _record.ClientDeliverySpeed <= 0 {
			continue
		}
		if _latest[_record.SelectedProviderID] <= 0 {
			_latest[_record.SelectedProviderID] = _record.ClientDeliverySpeed
		}
	}
	return _latest
}

// -------------------------------------------------------------------------------------
func readDayFileByPath(_path string) (DayFile, error) {
	_day := DayFile{
		ProviderSummary: map[string]ProviderSummary{},
		Records:         []ChatRecord{},
	}
	_bytes, _err := os.ReadFile(_path)
	if _err != nil {
		return _day, _err
	}
	if _err := json.Unmarshal(_bytes, &_day); _err != nil {
		return _day, _err
	}
	if _day.ProviderSummary == nil {
		_day.ProviderSummary = map[string]ProviderSummary{}
	}
	if _day.Records == nil {
		_day.Records = []ChatRecord{}
	}
	return _day, nil
}

// -------------------------------------------------------------------------------------
func NewRequestID() string {
	var _buf [8]byte
	if _, _err := rand.Read(_buf[:]); _err == nil {
		return "chat-" + hex.EncodeToString(_buf[:])
	}
	return fmt.Sprintf("chat-%d", time.Now().UnixNano())
}

// -------------------------------------------------------------------------------------
func RecordFromSelection(_started time.Time, _ended time.Time, _req domain.ChatCompletionRequest, _target *balancer.ProviderRuntime, _model *domain.LLMModelConfig, _profile domain.RequestProfile, _selectionMeta balancer.SelectionMeta, _success bool, _err error) ChatRecord {
	return RecordFromSelectionWithUsage(_started, _ended, _req, _target, _model, _profile, _selectionMeta, _success, _err, 0, 0, 0, false, 0)
}

// -------------------------------------------------------------------------------------
func RecordFromSelectionWithUsage(_started time.Time, _ended time.Time, _req domain.ChatCompletionRequest, _target *balancer.ProviderRuntime, _model *domain.LLMModelConfig, _profile domain.RequestProfile, _selectionMeta balancer.SelectionMeta, _success bool, _err error, _completionTokens int, _tokenSpeed float64, _clientDeliveryTPS float64, _tokensEstimated bool, _reactionMS float64) ChatRecord {
	_record := ChatRecord{
		RequestID:             NewRequestID(),
		Timestamp:             _ended.Format(time.RFC3339Nano),
		StartedAt:             _started.Format(time.RFC3339Nano),
		EndedAt:               _ended.Format(time.RFC3339Nano),
		DurationMS:            _ended.Sub(_started).Milliseconds(),
		Success:               _success,
		Stream:                _req.Stream,
		RequestedModel:        _req.Model,
		Strategy:              _selectionMeta.Strategy,
		SelectionReason:       _selectionMeta.Reason,
		CandidateCount:        _selectionMeta.CandidateCount,
		Candidates:            _selectionMeta.Candidates,
		TaskType:              _profile.TaskType,
		ComplexityScore:       _profile.ComplexityScore,
		HardRequirements:      _profile.HardRequirements,
		Signals:               _profile.Signals,
		InputCharacters:       _profile.InputCharacters,
		EstimatedInputTokens:  _profile.EstimatedInputTokens,
		RequestedOutputTokens: _profile.RequestedOutputTokens,
		CompletionTokens:      _completionTokens,
		ReactionTimeMS:        _reactionMS,
		TokenGenerationSpeed:  _tokenSpeed,
		ClientDeliverySpeed:   _clientDeliveryTPS,
		TokensEstimated:       _tokensEstimated,
		MessageCount:          _profile.MessageCount,
	}
	if _err != nil {
		_record.Error = _err.Error()
	}
	if _target != nil && _target.Config != nil {
		_record.SelectedProviderID = _target.Config.ID
		_record.SelectedProvider = _target.Config.Name
		_record.SelectedProviderKind = _target.Config.Kind
		_record.SelectedProviderRole = _target.Config.Role
		_record.ProviderSuccesses = _target.SuccessCount()
		_record.ProviderFailures = _target.FailureCount()
		_record.ProviderActive = _target.ActiveCount()
		_record.ProviderLatencyP50MS, _record.ProviderLatencyP95MS, _, _record.ProviderReactionMS, _, _record.ProviderTokenSpeed, _, _record.ProviderLastTokens = _target.MetricsSnapshot()
		_record.ProviderCircuitOpen = _target.CircuitOpen(_ended)
	}
	if _model != nil {
		_record.SelectedModel = _model.Name
	}
	return _record
}

// -------------------------------------------------------------------------------------
