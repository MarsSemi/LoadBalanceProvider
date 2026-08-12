package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
func DefaultNotificationTargetConfig() domain.NotificationTargetConfig {
	return domain.NotificationTargetConfig{
		Payload: `{"text":"<msg>"}`,
	}
}

// -------------------------------------------------------------------------------------
func LoadNotificationTargetConfig(_path string) (domain.NotificationTargetConfig, error) {
	if strings.TrimSpace(_path) == "" {
		_path = "data/notification_target.json"
	}

	_bytes, _err := os.ReadFile(_path)
	if os.IsNotExist(_err) {
		return DefaultNotificationTargetConfig(), nil
	}
	if _err != nil {
		return domain.NotificationTargetConfig{}, _err
	}

	_config := DefaultNotificationTargetConfig()
	if _err := json.Unmarshal(_bytes, &_config); _err != nil {
		return domain.NotificationTargetConfig{}, _err
	}
	_config.URL = strings.TrimSpace(_config.URL)
	_config.APIKey = strings.TrimSpace(_config.APIKey)
	if strings.TrimSpace(_config.Payload) == "" {
		_config.Payload = DefaultNotificationTargetConfig().Payload
	}
	return _config, nil
}

// -------------------------------------------------------------------------------------
func SaveNotificationTargetConfig(_path string, _config domain.NotificationTargetConfig) error {
	if strings.TrimSpace(_path) == "" {
		_path = "data/notification_target.json"
	}

	_config.URL = strings.TrimSpace(_config.URL)
	_config.APIKey = strings.TrimSpace(_config.APIKey)
	if strings.TrimSpace(_config.Payload) == "" {
		_config.Payload = DefaultNotificationTargetConfig().Payload
	}

	_dir := filepath.Dir(_path)
	if _dir != "." && _dir != "" {
		if _err := os.MkdirAll(_dir, 0755); _err != nil {
			return _err
		}
	}

	_bytes, _err := json.MarshalIndent(_config, "", "  ")
	if _err != nil {
		return _err
	}
	_bytes = append(_bytes, '\n')
	return os.WriteFile(_path, _bytes, 0600)
}
