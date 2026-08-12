package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
func DefaultGeneralSettingsConfig() domain.GeneralSettingsConfig {
	return domain.GeneralSettingsConfig{
		ShowProviderModels: false,
	}
}

// -------------------------------------------------------------------------------------
func LoadGeneralSettingsConfig(_path string) (domain.GeneralSettingsConfig, error) {
	if strings.TrimSpace(_path) == "" {
		_path = "data/general_settings.json"
	}

	_bytes, _err := os.ReadFile(_path)
	if os.IsNotExist(_err) {
		return DefaultGeneralSettingsConfig(), nil
	}
	if _err != nil {
		return domain.GeneralSettingsConfig{}, _err
	}

	_config := DefaultGeneralSettingsConfig()
	if _err := json.Unmarshal(_bytes, &_config); _err != nil {
		return domain.GeneralSettingsConfig{}, _err
	}
	return _config, nil
}

// -------------------------------------------------------------------------------------
func SaveGeneralSettingsConfig(_path string, _config domain.GeneralSettingsConfig) error {
	if strings.TrimSpace(_path) == "" {
		_path = "data/general_settings.json"
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
