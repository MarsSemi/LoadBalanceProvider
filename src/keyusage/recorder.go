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
}

type MonthFile struct {
	KeyID     string           `json:"key_id"`
	Month     string           `json:"month"`
	Days      map[string]int64 `json:"days"`
	UpdatedAt string           `json:"updated_at"`
}

type DayStat struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
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
		Root:  _root,
		files: map[string]MonthFile{},
		dirty: map[string]bool{},
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
	_days := make([]DayStat, 0, len(_file.Days))
	var _total int64
	for _date, _count := range _file.Days {
		_days = append(_days, DayStat{Date: _date, Count: _count})
		_total += _count
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
