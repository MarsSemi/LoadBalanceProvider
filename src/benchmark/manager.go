package benchmark

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sort"
	"sync"
	"time"
)

// -------------------------------------------------------------------------------------
type Manager struct {
	_lock sync.Mutex
	Jobs  map[string]*Job
}

// -------------------------------------------------------------------------------------
func NewManager() *Manager {
	return &Manager{Jobs: map[string]*Job{}}
}

// -------------------------------------------------------------------------------------
func (_m *Manager) Start(_req StartRequest) (*Job, error) {
	if _m == nil {
		return nil, errors.New("benchmark manager is not initialized")
	}
	if _req.ProviderID == "" || _req.ProviderBaseURL == "" {
		return nil, errors.New("provider is required")
	}
	if _req.Model == "" {
		return nil, errors.New("model is required")
	}
	if len(_req.Benchmarks) == 0 {
		return nil, errors.New("at least one benchmark is required")
	}
	if _req.BatchSize <= 0 {
		_req.BatchSize = 1
	}

	_catalog := CatalogByID()
	_requested := make([]string, 0, len(_req.Benchmarks))
	for _id := range _req.Benchmarks {
		if _, _ok := _catalog[_id]; !_ok {
			return nil, errors.New("invalid benchmark: " + _id)
		}
		_requested = append(_requested, _id)
	}
	sort.Strings(_requested)

	_ctx, _cancel := context.WithCancel(context.Background())
	_job := &Job{
		ID:             newJobID(),
		Status:         "queued",
		ProviderID:     _req.ProviderID,
		ProviderName:   _req.ProviderName,
		Model:          _req.Model,
		BatchSize:      _req.BatchSize,
		EnableThinking: _req.EnableThinking,
		CreatedAt:      time.Now(),
		Requested:      _requested,
		cancel:         _cancel,
	}

	_m._lock.Lock()
	_m.Jobs[_job.ID] = _job
	_m._lock.Unlock()

	go _m.run(_ctx, _job.ID, _req, _requested)
	return _m.snapshotJob(_job.ID), nil
}

// -------------------------------------------------------------------------------------
func (_m *Manager) Get(_id string) (*Job, bool) {
	_job := _m.snapshotJob(_id)
	return _job, _job != nil
}

// -------------------------------------------------------------------------------------
func (_m *Manager) Cancel(_id string) (*Job, bool) {
	_m._lock.Lock()
	_job := _m.Jobs[_id]
	if _job == nil {
		_m._lock.Unlock()
		return nil, false
	}
	_job.cancelRequested = true
	if _job.cancel != nil {
		_job.cancel()
	}
	_m._lock.Unlock()
	return _m.snapshotJob(_id), true
}

// -------------------------------------------------------------------------------------
func (_m *Manager) run(_ctx context.Context, _jobID string, _req StartRequest, _requested []string) {
	_m.update(_jobID, func(_job *Job) {
		_job.Status = "running"
		_job.StartedAt = time.Now()
	})

	for _idx, _benchmarkID := range _requested {
		if _ctx.Err() != nil {
			break
		}
		_result := runBenchmark(_ctx, _req, _benchmarkID, _req.Benchmarks[_benchmarkID], _idx, len(_requested), func(_progress Progress) {
			_m.update(_jobID, func(_job *Job) {
				_job.Progress = _progress
			})
		})
		_m.update(_jobID, func(_job *Job) {
			_job.Results = append(_job.Results, _result)
		})
	}

	_m.update(_jobID, func(_job *Job) {
		_job.EndedAt = time.Now()
		if _job.cancelRequested || _ctx.Err() != nil {
			_job.Status = "cancelled"
			_job.Progress.Label = "已取消"
			return
		}
		_job.Status = "completed"
		_job.Progress.Label = "已完成"
	})
}

// -------------------------------------------------------------------------------------
func (_m *Manager) update(_jobID string, _fn func(*Job)) {
	_m._lock.Lock()
	defer _m._lock.Unlock()
	if _job := _m.Jobs[_jobID]; _job != nil {
		_fn(_job)
	}
}

// -------------------------------------------------------------------------------------
func (_m *Manager) snapshotJob(_id string) *Job {
	_m._lock.Lock()
	defer _m._lock.Unlock()
	_job := _m.Jobs[_id]
	if _job == nil {
		return nil
	}
	_copy := *_job
	_copy.cancel = nil
	_copy.Results = append([]Result(nil), _job.Results...)
	return &_copy
}

// -------------------------------------------------------------------------------------
func newJobID() string {
	var _buf [6]byte
	if _, _err := rand.Read(_buf[:]); _err == nil {
		return hex.EncodeToString(_buf[:])
	}
	return hex.EncodeToString([]byte(time.Now().Format("150405.000000")))
}

// -------------------------------------------------------------------------------------
func ProviderAPIKey(_apiKey string, _apiKeyEnv string) string {
	if _apiKey != "" {
		return _apiKey
	}
	if _apiKeyEnv != "" {
		return os.Getenv(_apiKeyEnv)
	}
	return ""
}
