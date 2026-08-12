package dashboard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// -------------------------------------------------------------------------------------
const DefaultBaselinePath = "data/dashboard_metric_baseline.json"

// -------------------------------------------------------------------------------------
var _baselineLock sync.Mutex

// -------------------------------------------------------------------------------------
type MetricBaselines struct {
	UpdatedAt string                      `json:"updated_at,omitempty"`
	Providers map[string]ProviderBaseline `json:"providers"`
}

// -------------------------------------------------------------------------------------
type ProviderBaseline struct {
	Requests         int64   `json:"requests"`
	SessionRequests  int64   `json:"sessionRequests"`
	CompletionTokens int64   `json:"completionTokens"`
	ReactionMS       float64 `json:"reactionMS"`
	ProcessingMS     float64 `json:"processingMS"`
	TokenSpeed       float64 `json:"tokenSpeed"`
	StreamOutSpeed   float64 `json:"streamOutSpeed"`
}

// -------------------------------------------------------------------------------------
func LoadMetricBaselines() (MetricBaselines, error) {
	return LoadMetricBaselinesFrom(DefaultBaselinePath)
}

// -------------------------------------------------------------------------------------
func LoadMetricBaselinesFrom(_path string) (MetricBaselines, error) {
	_baselineLock.Lock()
	defer _baselineLock.Unlock()

	_data := MetricBaselines{Providers: map[string]ProviderBaseline{}}
	_bytes, _err := os.ReadFile(_path)
	if _err != nil {
		if errors.Is(_err, os.ErrNotExist) {
			return _data, nil
		}
		return _data, _err
	}
	if len(_bytes) == 0 {
		return _data, nil
	}
	if _err := json.Unmarshal(_bytes, &_data); _err != nil {
		return _data, _err
	}
	if _data.Providers == nil {
		_data.Providers = map[string]ProviderBaseline{}
	}
	return _data, nil
}

// -------------------------------------------------------------------------------------
func SaveMetricBaselines(_data MetricBaselines) error {
	return SaveMetricBaselinesTo(DefaultBaselinePath, _data)
}

// -------------------------------------------------------------------------------------
func SaveMetricBaselinesTo(_path string, _data MetricBaselines) error {
	_baselineLock.Lock()
	defer _baselineLock.Unlock()

	if _data.Providers == nil {
		_data.Providers = map[string]ProviderBaseline{}
	}
	_data.UpdatedAt = time.Now().Format(time.RFC3339Nano)

	_dir := filepath.Dir(_path)
	if _err := os.MkdirAll(_dir, 0755); _err != nil {
		return _err
	}
	_bytes, _err := json.MarshalIndent(_data, "", "  ")
	if _err != nil {
		return _err
	}
	_bytes = append(_bytes, '\n')

	_tmpPath := _path + ".tmp"
	if _err := os.WriteFile(_tmpPath, _bytes, 0644); _err != nil {
		return _err
	}
	return os.Rename(_tmpPath, _path)
}
