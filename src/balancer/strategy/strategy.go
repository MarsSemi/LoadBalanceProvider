package strategy

import (
	"crypto/rand"
	"math/big"
	"sort"
	"strings"
	"time"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
type ProviderSelection struct {
	Index           int
	ProviderID      string
	ProviderName    string
	ProviderKind    string
	ProviderPurpose string
	Model           string
	Score           float64
	Active          int64
	MaxConcurrent   int64
}

// -------------------------------------------------------------------------------------
type SelectionStrategy interface {
	Name() string
	Select(_candidates []ProviderSelection, _profile domain.RequestProfile, _requestedModel string) (ProviderSelection, string)
}

// -------------------------------------------------------------------------------------
var _registry = map[string]SelectionStrategy{}

// -------------------------------------------------------------------------------------
func Register(_strategy SelectionStrategy) {
	if _strategy == nil {
		return
	}
	_registry[strings.ToLower(strings.TrimSpace(_strategy.Name()))] = _strategy
}

// -------------------------------------------------------------------------------------
func Resolve(_name string) SelectionStrategy {
	_name = strings.ToLower(strings.TrimSpace(_name))
	if _strategy := _registry[_name]; _strategy != nil {
		return _strategy
	}
	return _registry["random"]
}

// -------------------------------------------------------------------------------------
func init() {
	Register(Random{})
	Register(WeightedScore{})
	Register(RoutingTable{})
	Register(Tiered{})
}

// -------------------------------------------------------------------------------------
type Random struct{}

// -------------------------------------------------------------------------------------
func (Random) Name() string {
	return "random"
}

// -------------------------------------------------------------------------------------
func (Random) Select(_candidates []ProviderSelection, _profile domain.RequestProfile, _requestedModel string) (ProviderSelection, string) {
	return _candidates[randomIndex(len(_candidates))], "selected random candidate from eligible providers"
}

// -------------------------------------------------------------------------------------
type WeightedScore struct{}

// -------------------------------------------------------------------------------------
func (WeightedScore) Name() string {
	return "weighted_score"
}

// -------------------------------------------------------------------------------------
func (WeightedScore) Select(_candidates []ProviderSelection, _profile domain.RequestProfile, _requestedModel string) (ProviderSelection, string) {
	return weightedRandomByScore(_candidates), "selected by weighted score probability"
}

// -------------------------------------------------------------------------------------
type RoutingTable struct{}

// -------------------------------------------------------------------------------------
func (RoutingTable) Name() string {
	return "routing_table"
}

// -------------------------------------------------------------------------------------
func (RoutingTable) Select(_candidates []ProviderSelection, _profile domain.RequestProfile, _requestedModel string) (ProviderSelection, string) {
	_matches := routingMatches(_candidates, _profile.TaskType)
	if len(_matches) == 0 {
		return WeightedScore{}.Select(_candidates, _profile, _requestedModel)
	}

	_sorted := sortedByScore(_matches)
	return _sorted[0], "selected by task routing table"
}

// -------------------------------------------------------------------------------------
type Tiered struct{}

// -------------------------------------------------------------------------------------
func (Tiered) Name() string {
	return "tiered"
}

// -------------------------------------------------------------------------------------
func (Tiered) Select(_candidates []ProviderSelection, _profile domain.RequestProfile, _requestedModel string) (ProviderSelection, string) {
	_matches := routingMatches(_candidates, _profile.TaskType)
	if len(_matches) > 0 {
		_sorted := sortedByScore(_matches)
		return _sorted[0], "selected by routing table tier"
	}

	_selected, _ := WeightedScore{}.Select(_candidates, _profile, _requestedModel)
	return _selected, "routing table missed; selected by weighted score fallback"
}

// -------------------------------------------------------------------------------------
func routingMatches(_candidates []ProviderSelection, _taskType string) []ProviderSelection {
	_preferred := routingPreferences(_taskType)
	if len(_preferred.Purposes) == 0 && len(_preferred.Kinds) == 0 {
		return nil
	}

	_matches := make([]ProviderSelection, 0, len(_candidates))
	for _, _candidate := range _candidates {
		if containsFold(_preferred.Purposes, _candidate.ProviderPurpose) ||
			containsFold(_preferred.Kinds, _candidate.ProviderKind) {
			_matches = append(_matches, _candidate)
		}
	}
	return _matches
}

// -------------------------------------------------------------------------------------
type routingPreference struct {
	Purposes []string
	Kinds    []string
}

// -------------------------------------------------------------------------------------
func routingPreferences(_taskType string) routingPreference {
	switch strings.ToLower(strings.TrimSpace(_taskType)) {
	case "summarization", "extraction", "translation":
		return routingPreference{Purposes: []string{"文件轉換"}}
	case "vision", "image_analysis":
		return routingPreference{Purposes: []string{"影像分析"}}
	case "image_generation":
		return routingPreference{Purposes: []string{"影像生成"}}
	case "video_analysis":
		return routingPreference{Purposes: []string{"影片分析"}}
	case "video_generation":
		return routingPreference{Purposes: []string{"影片生成"}}
	case "audio_analysis", "transcription":
		return routingPreference{Purposes: []string{"聲音分析"}}
	case "audio_generation", "tts":
		return routingPreference{Purposes: []string{"聲音生成"}}
	case "chat", "reasoning", "creative", "coding":
		return routingPreference{Purposes: []string{"對話"}}
	default:
		return routingPreference{}
	}
}

// -------------------------------------------------------------------------------------
func sortedByScore(_candidates []ProviderSelection) []ProviderSelection {
	_sorted := append([]ProviderSelection(nil), _candidates...)
	sort.SliceStable(_sorted, func(_i int, _j int) bool {
		return _sorted[_i].Score > _sorted[_j].Score
	})
	return _sorted
}

// -------------------------------------------------------------------------------------
func weightedRandomByScore(_candidates []ProviderSelection) ProviderSelection {
	if len(_candidates) <= 1 {
		return _candidates[0]
	}

	_minScore := _candidates[0].Score
	for _, _candidate := range _candidates[1:] {
		if _candidate.Score < _minScore {
			_minScore = _candidate.Score
		}
	}

	const _scale = 1000
	_weights := make([]int64, len(_candidates))
	var _total int64
	for _idx, _candidate := range _candidates {
		_weight := int64((_candidate.Score-_minScore+1)*_scale + 0.5)
		if _weight < 1 {
			_weight = 1
		}
		_weights[_idx] = _weight
		_total += _weight
	}
	if _total <= 0 {
		return _candidates[randomIndex(len(_candidates))]
	}

	_pick, _err := rand.Int(rand.Reader, big.NewInt(_total))
	if _err != nil {
		return _candidates[randomIndex(len(_candidates))]
	}
	_cursor := _pick.Int64()
	for _idx, _weight := range _weights {
		if _cursor < _weight {
			return _candidates[_idx]
		}
		_cursor -= _weight
	}
	return _candidates[len(_candidates)-1]
}

// -------------------------------------------------------------------------------------
func containsFold(_values []string, _target string) bool {
	for _, _value := range _values {
		if strings.EqualFold(_value, _target) {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func randomIndex(_length int) int {
	if _length <= 1 {
		return 0
	}

	_max := big.NewInt(int64(_length))
	_value, _err := rand.Int(rand.Reader, _max)
	if _err == nil {
		return int(_value.Int64())
	}

	return int(time.Now().UnixNano() % int64(_length))
}

// -------------------------------------------------------------------------------------
