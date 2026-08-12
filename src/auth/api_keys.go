package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"LoadBalanceProvider/src/keyusage"
)

// -------------------------------------------------------------------------------------
const defaultAPIKeyPath = "data/api_keys.json"

const (
	APIKeyTypeChat    = "chat"
	APIKeyTypeMCP     = "mcp"
	APIKeyTypeSession = "session"
)

// -------------------------------------------------------------------------------------
var _defaultStore = NewAPIKeyStore(defaultAPIKeyPath)

// -------------------------------------------------------------------------------------
type APIKeyStore struct {
	Path        string
	_lock       sync.Mutex
	_file       APIKeyFile
	_loaded     bool
	_dirty      bool
	_flushTimer *time.Timer
}

const apiKeyUsageFlushDelay = 2 * time.Second

// -------------------------------------------------------------------------------------
type APIKeyFile struct {
	Keys []APIKeyRecord `json:"keys"`
}

// -------------------------------------------------------------------------------------
type APIKeyRecord struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Prefix          string `json:"prefix"`
	Hash            string `json:"hash"`
	Enabled         bool   `json:"enabled"`
	KeyType         string `json:"key_type,omitempty"`
	Temporary       bool   `json:"temporary,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	DisabledAt      string `json:"disabled_at,omitempty"`
	LastUsedAt      string `json:"last_used_at,omitempty"`
	UsageCount      int64  `json:"usage_count"`
	ProviderID      string `json:"provider_id,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// -------------------------------------------------------------------------------------
type APIKeyView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Prefix          string `json:"prefix"`
	MaskedKey       string `json:"masked_key"`
	Enabled         bool   `json:"enabled"`
	KeyType         string `json:"key_type"`
	Temporary       bool   `json:"temporary"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	DisabledAt      string `json:"disabled_at,omitempty"`
	LastUsedAt      string `json:"last_used_at,omitempty"`
	UsageCount      int64  `json:"usage_count"`
	ProviderID      string `json:"provider_id"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	Key             string `json:"key,omitempty"`
}

// -------------------------------------------------------------------------------------
func DefaultAPIKeyStore() *APIKeyStore {
	return _defaultStore
}

// -------------------------------------------------------------------------------------
func NewAPIKeyStore(_path string) *APIKeyStore {
	if strings.TrimSpace(_path) == "" {
		_path = defaultAPIKeyPath
	}
	return &APIKeyStore{Path: _path}
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) List() ([]APIKeyView, error) {
	_s._lock.Lock()
	defer _s._lock.Unlock()

	_file, _err := _s.loadLocked()
	if _err != nil {
		return nil, _err
	}
	_file = _s.pruneExpiredTemporaryLocked(_file, time.Now())

	_views := make([]APIKeyView, 0, len(_file.Keys))
	for _, _record := range _file.Keys {
		_views = append(_views, apiKeyView(_record, ""))
	}
	return _views, nil
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) Create(_name string) (APIKeyView, error) {
	return _s.CreateForType(_name, APIKeyTypeChat)
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) CreateForType(_name string, _keyType string) (APIKeyView, error) {
	_keyType = strings.ToLower(strings.TrimSpace(_keyType))
	if _keyType != APIKeyTypeChat && _keyType != APIKeyTypeMCP {
		return APIKeyView{}, errors.New("api key type must be chat or mcp")
	}
	return _s.create(_name, _keyType, false, time.Time{})
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) CreateTemporary(_name string, _ttl time.Duration) (APIKeyView, error) {
	if _ttl <= 0 {
		_ttl = 24 * time.Hour
	}
	return _s.create(_name, APIKeyTypeSession, true, time.Now().Add(_ttl))
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) create(_name string, _keyType string, _temporary bool, _expiresAt time.Time) (APIKeyView, error) {
	_s._lock.Lock()
	defer _s._lock.Unlock()

	_file, _err := _s.loadLocked()
	if _err != nil {
		return APIKeyView{}, _err
	}
	_file = _s.pruneExpiredTemporaryLocked(_file, time.Now())

	_key, _err := randomAPIKey()
	if _err != nil {
		return APIKeyView{}, _err
	}

	_now := time.Now().Format(time.RFC3339)
	_record := APIKeyRecord{
		ID:              "key-" + randomHex(8),
		Name:            strings.TrimSpace(_name),
		Prefix:          keyPrefix(_key),
		Hash:            hashAPIKey(_key),
		Enabled:         true,
		KeyType:         _keyType,
		Temporary:       _temporary,
		CreatedAt:       _now,
		ProviderID:      "AUTO",
		Model:           "AUTO",
		ReasoningEffort: "AUTO",
	}
	if !_expiresAt.IsZero() {
		_record.ExpiresAt = _expiresAt.Format(time.RFC3339)
	}
	if _record.Name == "" {
		_record.Name = "未命名金鑰"
	}

	_file.Keys = append(_file.Keys, _record)
	if _err := _s.saveLocked(_file); _err != nil {
		return APIKeyView{}, _err
	}

	return apiKeyView(_record, _key), nil
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) SetEnabled(_id string, _enabled bool) (APIKeyView, error) {
	_s._lock.Lock()
	defer _s._lock.Unlock()

	_file, _err := _s.loadLocked()
	if _err != nil {
		return APIKeyView{}, _err
	}

	for _idx := range _file.Keys {
		if _file.Keys[_idx].ID != _id {
			continue
		}
		_file.Keys[_idx].Enabled = _enabled
		if _enabled {
			_file.Keys[_idx].DisabledAt = ""
		} else {
			_file.Keys[_idx].DisabledAt = time.Now().Format(time.RFC3339)
		}
		if _err := _s.saveLocked(_file); _err != nil {
			return APIKeyView{}, _err
		}
		return apiKeyView(_file.Keys[_idx], ""), nil
	}

	return APIKeyView{}, errors.New("api key not found")
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) Update(_id string, _name string, _providerID string, _model string, _reasoningEffort string) (APIKeyView, error) {
	_s._lock.Lock()
	defer _s._lock.Unlock()

	_file, _err := _s.loadLocked()
	if _err != nil {
		return APIKeyView{}, _err
	}
	_file = _s.pruneExpiredTemporaryLocked(_file, time.Now())

	_name = strings.TrimSpace(_name)
	if _name == "" {
		_name = "未命名金鑰"
	}

	for _idx := range _file.Keys {
		if _file.Keys[_idx].ID != _id {
			continue
		}
		_file.Keys[_idx].Name = _name
		if effectiveAPIKeyType(_file.Keys[_idx]) != APIKeyTypeChat {
			_file.Keys[_idx].ProviderID = "AUTO"
			_file.Keys[_idx].Model = "AUTO"
			_file.Keys[_idx].ReasoningEffort = "AUTO"
		} else {
			_file.Keys[_idx].ProviderID = normalizeRoutingSetting(_providerID)
			_file.Keys[_idx].Model = normalizeRoutingSetting(_model)
			_file.Keys[_idx].ReasoningEffort = normalizeRoutingSetting(_reasoningEffort)
		}
		if _err := _s.saveLocked(_file); _err != nil {
			return APIKeyView{}, _err
		}
		return apiKeyView(_file.Keys[_idx], ""), nil
	}

	return APIKeyView{}, errors.New("api key not found")
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) Delete(_id string) error {
	_s._lock.Lock()
	defer _s._lock.Unlock()

	_file, _err := _s.loadLocked()
	if _err != nil {
		return _err
	}

	_next := make([]APIKeyRecord, 0, len(_file.Keys))
	_deleted := false
	for _, _record := range _file.Keys {
		if _record.ID == _id {
			_deleted = true
			continue
		}
		_next = append(_next, _record)
	}
	if !_deleted {
		return errors.New("api key not found")
	}

	_file.Keys = _next
	return _s.saveLocked(_file)
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) HasActiveKeys() (bool, error) {
	_s._lock.Lock()
	defer _s._lock.Unlock()

	_file, _err := _s.loadLocked()
	if _err != nil {
		return false, _err
	}
	_file = _s.pruneExpiredTemporaryLocked(_file, time.Now())

	_now := time.Now()
	for _, _record := range _file.Keys {
		if _record.Enabled && !apiKeyExpired(_record, _now) {
			return true, nil
		}
	}
	return false, nil
}

// -------------------------------------------------------------------------------------
// Validate 驗證金鑰並立即計次。需要「先確認路由權限、再計次」的呼叫端
// 請改用 Verify + RecordUsage，避免被拒絕的請求也累加使用次數。
func (_s *APIKeyStore) Validate(_key string) (APIKeyView, bool, error) {
	_view, _ok, _err := _s.Verify(_key)
	if _err != nil || !_ok {
		return _view, _ok, _err
	}
	if _err := _s.RecordUsage(_view.ID); _err != nil {
		return APIKeyView{}, false, _err
	}
	return _view, true, nil
}

// -------------------------------------------------------------------------------------
// Verify 只確認金鑰有效，不會寫入任何使用紀錄。
func (_s *APIKeyStore) Verify(_key string) (APIKeyView, bool, error) {
	_key = strings.TrimSpace(_key)
	if _key == "" {
		return APIKeyView{}, false, nil
	}

	_s._lock.Lock()
	defer _s._lock.Unlock()

	_file, _err := _s.loadLocked()
	if _err != nil {
		return APIKeyView{}, false, _err
	}
	_now := time.Now()
	_file = _s.pruneExpiredTemporaryLocked(_file, _now)

	_hash := hashAPIKey(_key)
	for _idx := range _file.Keys {
		_record := &_file.Keys[_idx]
		if !_record.Enabled || apiKeyExpired(*_record, _now) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(_record.Hash), []byte(_hash)) != 1 {
			continue
		}
		return apiKeyView(*_record, ""), true, nil
	}

	return APIKeyView{}, false, nil
}

// -------------------------------------------------------------------------------------
// RecordUsage 累加使用次數並寫入每日統計，應在授權通過後才呼叫。
func (_s *APIKeyStore) RecordUsage(_id string) error {
	_id = strings.TrimSpace(_id)
	if _id == "" {
		return nil
	}

	_s._lock.Lock()
	_file, _err := _s.loadLocked()
	if _err != nil {
		_s._lock.Unlock()
		return _err
	}
	_now := time.Now()

	_found := false
	for _idx := range _file.Keys {
		if _file.Keys[_idx].ID != _id {
			continue
		}
		_file.Keys[_idx].LastUsedAt = _now.Format(time.RFC3339)
		_file.Keys[_idx].UsageCount++
		_s._file = _file
		_s.markDirtyLocked()
		_found = true
		break
	}
	_s._lock.Unlock()

	if !_found {
		return nil
	}
	return keyusage.DefaultRecorder().Record(_id, _now)
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) Flush() error {
	_s._lock.Lock()
	defer _s._lock.Unlock()
	if !_s._dirty || !_s._loaded {
		return nil
	}
	if _err := _s.persistLocked(_s._file); _err != nil {
		return _err
	}
	_s._dirty = false
	_s._flushTimer = nil
	return nil
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) markDirtyLocked() {
	_s._dirty = true
	if _s._flushTimer != nil {
		return
	}
	_s._flushTimer = time.AfterFunc(apiKeyUsageFlushDelay, func() {
		if _err := _s.Flush(); _err != nil {
			log.Printf("api key usage flush failed: %v", _err)
		}
	})
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) PruneExpiredTemporary() error {
	_s._lock.Lock()
	defer _s._lock.Unlock()

	_file, _err := _s.loadLocked()
	if _err != nil {
		return _err
	}
	_s.pruneExpiredTemporaryLocked(_file, time.Now())
	return nil
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) pruneExpiredTemporaryLocked(_file APIKeyFile, _now time.Time) APIKeyFile {
	_next := make([]APIKeyRecord, 0, len(_file.Keys))
	_changed := false
	for _, _record := range _file.Keys {
		if _record.Temporary && apiKeyExpired(_record, _now) {
			_changed = true
			continue
		}
		_next = append(_next, _record)
	}
	if !_changed {
		return _file
	}
	_file.Keys = _next
	_ = _s.saveLocked(_file)
	return _file
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) loadLocked() (APIKeyFile, error) {
	if _s._loaded {
		return _s._file, nil
	}
	_bytes, _err := os.ReadFile(_s.Path)
	if _err != nil {
		if os.IsNotExist(_err) {
			_s._file = APIKeyFile{Keys: []APIKeyRecord{}}
			_s._loaded = true
			return _s._file, nil
		}
		return APIKeyFile{}, _err
	}
	if len(_bytes) == 0 {
		_s._file = APIKeyFile{Keys: []APIKeyRecord{}}
		_s._loaded = true
		return _s._file, nil
	}

	var _file APIKeyFile
	if _err := json.Unmarshal(_bytes, &_file); _err != nil {
		return APIKeyFile{}, _err
	}
	if _file.Keys == nil {
		_file.Keys = []APIKeyRecord{}
	}
	_s._file = _file
	_s._loaded = true
	return _s._file, nil
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) saveLocked(_file APIKeyFile) error {
	if _s._flushTimer != nil {
		_s._flushTimer.Stop()
		_s._flushTimer = nil
	}
	if _err := _s.persistLocked(_file); _err != nil {
		return _err
	}
	_s._file = _file
	_s._loaded = true
	_s._dirty = false
	return nil
}

// -------------------------------------------------------------------------------------
func (_s *APIKeyStore) persistLocked(_file APIKeyFile) error {
	if _err := os.MkdirAll(filepath.Dir(_s.Path), 0755); _err != nil {
		return _err
	}

	_bytes, _err := json.MarshalIndent(_file, "", "  ")
	if _err != nil {
		return _err
	}
	_tmp, _err := os.CreateTemp(filepath.Dir(_s.Path), filepath.Base(_s.Path)+".tmp.*")
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
	return os.Rename(_tmpPath, _s.Path)
}

// -------------------------------------------------------------------------------------
func apiKeyView(_record APIKeyRecord, _key string) APIKeyView {
	return APIKeyView{
		ID:              _record.ID,
		Name:            _record.Name,
		Prefix:          _record.Prefix,
		MaskedKey:       _record.Prefix + "************",
		Enabled:         _record.Enabled,
		KeyType:         effectiveAPIKeyType(_record),
		Temporary:       _record.Temporary,
		ExpiresAt:       _record.ExpiresAt,
		CreatedAt:       _record.CreatedAt,
		DisabledAt:      _record.DisabledAt,
		LastUsedAt:      _record.LastUsedAt,
		UsageCount:      _record.UsageCount,
		ProviderID:      normalizeRoutingSetting(_record.ProviderID),
		Model:           normalizeRoutingSetting(_record.Model),
		ReasoningEffort: normalizeRoutingSetting(_record.ReasoningEffort),
		Key:             _key,
	}
}

// -------------------------------------------------------------------------------------
// effectiveAPIKeyType 讓舊資料保持相容：未記錄 key_type 的永久金鑰是 Chat，
// 暫時金鑰則固定是 Web Session。
func effectiveAPIKeyType(_record APIKeyRecord) string {
	if _record.Temporary {
		return APIKeyTypeSession
	}
	if strings.EqualFold(strings.TrimSpace(_record.KeyType), APIKeyTypeMCP) {
		return APIKeyTypeMCP
	}
	return APIKeyTypeChat
}

// -------------------------------------------------------------------------------------
func normalizeRoutingSetting(_value string) string {
	_value = strings.TrimSpace(_value)
	if _value == "" || strings.EqualFold(_value, "AUTO") {
		return "AUTO"
	}
	return _value
}

// -------------------------------------------------------------------------------------
func apiKeyExpired(_record APIKeyRecord, _now time.Time) bool {
	if strings.TrimSpace(_record.ExpiresAt) == "" {
		return false
	}
	_expiresAt, _err := time.Parse(time.RFC3339, _record.ExpiresAt)
	if _err != nil {
		return true
	}
	return !_now.Before(_expiresAt)
}

// -------------------------------------------------------------------------------------
func randomAPIKey() (string, error) {
	_bytes := make([]byte, 32)
	if _, _err := rand.Read(_bytes); _err != nil {
		return "", _err
	}
	return "lbp_" + base64.RawURLEncoding.EncodeToString(_bytes), nil
}

// -------------------------------------------------------------------------------------
func randomHex(_bytes int) string {
	_buffer := make([]byte, _bytes)
	if _, _err := rand.Read(_buffer); _err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(_buffer)
}

// -------------------------------------------------------------------------------------
func keyPrefix(_key string) string {
	if len(_key) <= 12 {
		return _key
	}
	return _key[:12]
}

// -------------------------------------------------------------------------------------
func hashAPIKey(_key string) string {
	_hash := sha256.Sum256([]byte(_key))
	return hex.EncodeToString(_hash[:])
}
