package serviceupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	UploadChunkBytes = 2 << 20
	uploadSessionTTL = 2 * time.Hour
)

var uploadSessionMu sync.Mutex

type UploadSession struct {
	ID           string `json:"session_id"`
	FileName     string `json:"file_name"`
	TotalSize    int64  `json:"total_size"`
	ReceivedSize int64  `json:"received_size"`
	NextIndex    int    `json:"next_index"`
	CreatedAt    string `json:"created_at"`
}

func CreateUploadSession(_fileName string, _totalSize int64) (UploadSession, error) {
	uploadSessionMu.Lock()
	defer uploadSessionMu.Unlock()

	_fileName = filepath.Base(strings.TrimSpace(_fileName))
	if _fileName == "" || !strings.EqualFold(filepath.Ext(_fileName), ".zip") {
		return UploadSession{}, errors.New("只接受 ZIP 更新檔")
	}
	if _totalSize <= 0 || _totalSize > MaxUploadBytes {
		return UploadSession{}, fmt.Errorf("更新檔案大小必須介於 1 byte 與 %d MB 之間", MaxUploadBytes>>20)
	}

	_uploadRoot, _err := uploadRoot()
	if _err != nil {
		return UploadSession{}, _err
	}
	_ = cleanupExpiredUploads(_uploadRoot)
	_session := UploadSession{
		ID:        time.Now().Format("20060102_150405") + "_" + randomSuffix(),
		FileName:  _fileName,
		TotalSize: _totalSize,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	_sessionDir := filepath.Join(_uploadRoot, _session.ID)
	if _err := os.MkdirAll(_sessionDir, 0750); _err != nil {
		return UploadSession{}, fmt.Errorf("建立切片上傳目錄失敗: %w", _err)
	}
	if _err := os.WriteFile(filepath.Join(_sessionDir, "package.part"), nil, 0600); _err != nil {
		_ = os.RemoveAll(_sessionDir)
		return UploadSession{}, fmt.Errorf("建立切片上傳檔案失敗: %w", _err)
	}
	if _err := saveUploadSession(_sessionDir, _session); _err != nil {
		_ = os.RemoveAll(_sessionDir)
		return UploadSession{}, _err
	}
	return _session, nil
}

func AppendUploadChunk(_sessionID string, _index int, _offset int64, _chunk []byte) (UploadSession, error) {
	uploadSessionMu.Lock()
	defer uploadSessionMu.Unlock()

	if len(_chunk) == 0 || len(_chunk) > UploadChunkBytes {
		return UploadSession{}, fmt.Errorf("每個更新切片必須介於 1 byte 與 %d MB 之間", UploadChunkBytes>>20)
	}
	_sessionDir, _session, _err := loadUploadSession(_sessionID)
	if _err != nil {
		return UploadSession{}, _err
	}
	if _index != _session.NextIndex || _offset != _session.ReceivedSize {
		return UploadSession{}, fmt.Errorf("更新切片順序不正確，預期 index=%d offset=%d", _session.NextIndex, _session.ReceivedSize)
	}
	if _session.ReceivedSize+int64(len(_chunk)) > _session.TotalSize {
		return UploadSession{}, errors.New("更新切片超過宣告的檔案大小")
	}

	_partPath := filepath.Join(_sessionDir, "package.part")
	_info, _err := os.Stat(_partPath)
	if _err != nil || _info.Size() != _session.ReceivedSize {
		return UploadSession{}, errors.New("更新切片暫存檔案大小不一致")
	}
	_file, _err := os.OpenFile(_partPath, os.O_WRONLY|os.O_APPEND, 0600)
	if _err != nil {
		return UploadSession{}, fmt.Errorf("開啟更新切片暫存檔案失敗: %w", _err)
	}
	_written, _writeErr := _file.Write(_chunk)
	_syncErr := _file.Sync()
	_closeErr := _file.Close()
	if _writeErr != nil || _syncErr != nil || _closeErr != nil || _written != len(_chunk) {
		return UploadSession{}, errors.New("寫入更新切片失敗")
	}

	_session.ReceivedSize += int64(len(_chunk))
	_session.NextIndex++
	if _err := saveUploadSession(_sessionDir, _session); _err != nil {
		return UploadSession{}, _err
	}
	return _session, nil
}

func CompleteUploadSession(_sessionID string, _fileName string) ([]byte, string, error) {
	uploadSessionMu.Lock()
	defer uploadSessionMu.Unlock()

	_sessionDir, _session, _err := loadUploadSession(_sessionID)
	if _err != nil {
		return nil, "", _err
	}
	_fileName = filepath.Base(strings.TrimSpace(_fileName))
	if _fileName != "" && _fileName != _session.FileName {
		return nil, "", errors.New("更新檔名與切片工作不一致")
	}
	if _session.ReceivedSize != _session.TotalSize {
		return nil, "", fmt.Errorf("更新檔案尚未上傳完成: %d/%d", _session.ReceivedSize, _session.TotalSize)
	}

	_partPath := filepath.Join(_sessionDir, "package.part")
	_file, _err := os.Open(_partPath)
	if _err != nil {
		return nil, "", fmt.Errorf("開啟完整更新檔案失敗: %w", _err)
	}
	_data, _readErr := io.ReadAll(io.LimitReader(_file, MaxUploadBytes+1))
	_closeErr := _file.Close()
	if _readErr != nil || _closeErr != nil {
		return nil, "", errors.New("讀取完整更新檔案失敗")
	}
	if int64(len(_data)) != _session.TotalSize {
		return nil, "", errors.New("完整更新檔案大小與宣告不一致")
	}
	_ = os.RemoveAll(_sessionDir)
	return _data, _session.FileName, nil
}

func uploadRoot() (string, error) {
	_root, _err := serviceRoot()
	if _err != nil {
		return "", _err
	}
	_uploadRoot := filepath.Join(_root, "data", "system", "service_update", "uploads")
	if _err := os.MkdirAll(_uploadRoot, 0750); _err != nil {
		return "", fmt.Errorf("建立切片上傳根目錄失敗: %w", _err)
	}
	return _uploadRoot, nil
}

func loadUploadSession(_sessionID string) (string, UploadSession, error) {
	if !validUploadSessionID(_sessionID) {
		return "", UploadSession{}, errors.New("更新切片 session id 不合法")
	}
	_uploadRoot, _err := uploadRoot()
	if _err != nil {
		return "", UploadSession{}, _err
	}
	_sessionDir := filepath.Join(_uploadRoot, _sessionID)
	_data, _err := os.ReadFile(filepath.Join(_sessionDir, "session.json"))
	if errors.Is(_err, os.ErrNotExist) {
		return "", UploadSession{}, errors.New("找不到更新切片工作，可能已逾時")
	}
	if _err != nil {
		return "", UploadSession{}, fmt.Errorf("讀取更新切片工作失敗: %w", _err)
	}
	var _session UploadSession
	if _err := json.Unmarshal(_data, &_session); _err != nil || _session.ID != _sessionID {
		return "", UploadSession{}, errors.New("更新切片工作資料損毀")
	}
	return _sessionDir, _session, nil
}

func saveUploadSession(_sessionDir string, _session UploadSession) error {
	_data, _err := json.MarshalIndent(_session, "", "  ")
	if _err != nil {
		return _err
	}
	if _err := writeAtomicFile(filepath.Join(_sessionDir, "session.json"), append(_data, '\n'), 0600); _err != nil {
		return fmt.Errorf("儲存更新切片工作失敗: %w", _err)
	}
	return nil
}

func validUploadSessionID(_sessionID string) bool {
	if len(_sessionID) < 10 || len(_sessionID) > 64 {
		return false
	}
	for _, _char := range _sessionID {
		if (_char >= 'a' && _char <= 'z') || (_char >= 'A' && _char <= 'Z') ||
			(_char >= '0' && _char <= '9') || _char == '_' || _char == '-' {
			continue
		}
		return false
	}
	return true
}

func cleanupExpiredUploads(_uploadRoot string) error {
	_entries, _err := os.ReadDir(_uploadRoot)
	if _err != nil {
		return _err
	}
	_deadline := time.Now().Add(-uploadSessionTTL)
	for _, _entry := range _entries {
		if !_entry.IsDir() {
			continue
		}
		_info, _infoErr := _entry.Info()
		if _infoErr == nil && _info.ModTime().Before(_deadline) {
			_ = os.RemoveAll(filepath.Join(_uploadRoot, _entry.Name()))
		}
	}
	return nil
}
