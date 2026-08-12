package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"LoadBalanceProvider/src/domain"
)

const (
	MinConversationAffinityTTLMinutes = 1
	MaxConversationAffinityTTLMinutes = 7 * 24 * 60
	MinResponseRouteMaxEntries        = 100
	MaxResponseRouteMaxEntries        = 100000
	MinProviderCapacityCooldownSecs   = 1
	MaxProviderCapacityCooldownSecs   = 300
)

// -------------------------------------------------------------------------------------
func DefaultAdvancedSettingsConfig() domain.AdvancedSettingsConfig {
	return domain.AdvancedSettingsConfig{
		ConversationAffinityTTLMinutes:           30,
		ConversationAffinityQuotaTolerancePoints: 10,
		ResponseRouteMaxEntries:                  2000,
		ProviderCapacityCooldownSeconds:          10,
	}
}

// -------------------------------------------------------------------------------------
func LoadAdvancedSettingsConfig(_path string) (domain.AdvancedSettingsConfig, error) {
	if strings.TrimSpace(_path) == "" {
		_path = "data/advanced_settings.json"
	}

	_bytes, _err := os.ReadFile(_path)
	if os.IsNotExist(_err) {
		return DefaultAdvancedSettingsConfig(), nil
	}
	if _err != nil {
		return domain.AdvancedSettingsConfig{}, _err
	}

	_config := DefaultAdvancedSettingsConfig()
	if _err := json.Unmarshal(_bytes, &_config); _err != nil {
		return domain.AdvancedSettingsConfig{}, _err
	}
	if _err := ValidateAdvancedSettingsConfig(_config); _err != nil {
		return domain.AdvancedSettingsConfig{}, _err
	}
	return _config, nil
}

// -------------------------------------------------------------------------------------
func SaveAdvancedSettingsConfig(_path string, _config domain.AdvancedSettingsConfig) error {
	if _err := ValidateAdvancedSettingsConfig(_config); _err != nil {
		return _err
	}
	if strings.TrimSpace(_path) == "" {
		_path = "data/advanced_settings.json"
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

// -------------------------------------------------------------------------------------
func ValidateAdvancedSettingsConfig(_config domain.AdvancedSettingsConfig) error {
	if _config.ConversationAffinityTTLMinutes < MinConversationAffinityTTLMinutes ||
		_config.ConversationAffinityTTLMinutes > MaxConversationAffinityTTLMinutes {
		return fmt.Errorf("conversation affinity TTL must be between %d and %d minutes", MinConversationAffinityTTLMinutes, MaxConversationAffinityTTLMinutes)
	}
	if _config.ConversationAffinityQuotaTolerancePoints < 0 || _config.ConversationAffinityQuotaTolerancePoints > 100 {
		return fmt.Errorf("conversation affinity quota tolerance must be between 0 and 100 percentage points")
	}
	if _config.ResponseRouteMaxEntries < MinResponseRouteMaxEntries || _config.ResponseRouteMaxEntries > MaxResponseRouteMaxEntries {
		return fmt.Errorf("response route max entries must be between %d and %d", MinResponseRouteMaxEntries, MaxResponseRouteMaxEntries)
	}
	if _config.ProviderCapacityCooldownSeconds < MinProviderCapacityCooldownSecs ||
		_config.ProviderCapacityCooldownSeconds > MaxProviderCapacityCooldownSecs {
		return fmt.Errorf("provider capacity cooldown must be between %d and %d seconds", MinProviderCapacityCooldownSecs, MaxProviderCapacityCooldownSecs)
	}
	return nil
}
