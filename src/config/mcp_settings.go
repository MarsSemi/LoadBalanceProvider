package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
func DefaultMCPSettingsConfig() domain.MCPSettingsConfig {
	return domain.MCPSettingsConfig{
		Enabled:        true,
		ReadOnly:       false,
		AllowedOrigins: []string{},
	}
}

// -------------------------------------------------------------------------------------
func LoadMCPSettingsConfig(_path string) (domain.MCPSettingsConfig, error) {
	if strings.TrimSpace(_path) == "" {
		_path = "data/mcp_settings.json"
	}

	_bytes, _err := os.ReadFile(_path)
	if os.IsNotExist(_err) {
		return DefaultMCPSettingsConfig(), nil
	}
	if _err != nil {
		return domain.MCPSettingsConfig{}, _err
	}

	_config := DefaultMCPSettingsConfig()
	if _err := json.Unmarshal(_bytes, &_config); _err != nil {
		return domain.MCPSettingsConfig{}, _err
	}
	_config.AllowedOrigins = normalizeMCPOrigins(_config.AllowedOrigins)
	if _err := ValidateMCPSettingsConfig(_config); _err != nil {
		return domain.MCPSettingsConfig{}, _err
	}
	return _config, nil
}

// -------------------------------------------------------------------------------------
func SaveMCPSettingsConfig(_path string, _config domain.MCPSettingsConfig) error {
	_config.AllowedOrigins = normalizeMCPOrigins(_config.AllowedOrigins)
	if _err := ValidateMCPSettingsConfig(_config); _err != nil {
		return _err
	}
	if strings.TrimSpace(_path) == "" {
		_path = "data/mcp_settings.json"
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
func ValidateMCPSettingsConfig(_config domain.MCPSettingsConfig) error {
	if len(_config.AllowedOrigins) > 100 {
		return fmt.Errorf("allowed origins cannot exceed 100 entries")
	}
	for _, _origin := range _config.AllowedOrigins {
		_parsed, _err := url.Parse(_origin)
		if _err != nil || (_parsed.Scheme != "http" && _parsed.Scheme != "https") || _parsed.Host == "" || _parsed.User != nil || _parsed.Path != "" || _parsed.RawQuery != "" || _parsed.Fragment != "" {
			return fmt.Errorf("invalid MCP origin: %s", _origin)
		}
	}
	return nil
}

// -------------------------------------------------------------------------------------
func normalizeMCPOrigins(_origins []string) []string {
	_seen := map[string]bool{}
	_result := make([]string, 0, len(_origins))
	for _, _origin := range _origins {
		_origin = strings.TrimRight(strings.TrimSpace(_origin), "/")
		if _origin == "" || _seen[_origin] {
			continue
		}
		_seen[_origin] = true
		_result = append(_result, _origin)
	}
	sort.Strings(_result)
	return _result
}
