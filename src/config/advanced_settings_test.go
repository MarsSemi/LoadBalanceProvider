package config

import (
	"os"
	"path/filepath"
	"testing"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
// 改名相容：舊設定檔用的是 max_conversations_per_provider，載入後仍要生效。
func TestLoadAdvancedSettingsAcceptsLegacyBindingKey(t *testing.T) {
	_path := filepath.Join(t.TempDir(), "advanced_settings.json")
	_legacy := `{"conversation_affinity_ttl_minutes":30,"conversation_affinity_quota_tolerance_points":10,
		"response_route_max_entries":2000,"provider_capacity_cooldown_seconds":10,
		"max_conversations_per_provider":3}`
	if _err := os.WriteFile(_path, []byte(_legacy), 0o644); _err != nil {
		t.Fatal(_err)
	}

	_config, _err := LoadAdvancedSettingsConfig(_path)
	if _err != nil {
		t.Fatal(_err)
	}
	if _config.MaxBindingsPerProvider != 3 {
		t.Fatalf("legacy key ignored: %#v", _config)
	}
	if _config.YieldLowMaxPercent != 2 || _config.YieldMidMaxPercent != 20 {
		t.Fatalf("missing yield thresholds should use defaults: %#v", _config)
	}

	// 新鍵存在時以新鍵為準
	_both := `{"conversation_affinity_ttl_minutes":30,"conversation_affinity_quota_tolerance_points":10,
		"response_route_max_entries":2000,"provider_capacity_cooldown_seconds":10,
		"max_conversations_per_provider":3,"max_bindings_per_provider":9}`
	if _err := os.WriteFile(_path, []byte(_both), 0o644); _err != nil {
		t.Fatal(_err)
	}
	_config, _err = LoadAdvancedSettingsConfig(_path)
	if _err != nil {
		t.Fatal(_err)
	}
	if _config.MaxBindingsPerProvider != 9 {
		t.Fatalf("new key should win: %#v", _config)
	}
}

// -------------------------------------------------------------------------------------
// 低推理降級的門檻要有邊界檢查，尤其是持續時間 —— 0 會讓降級立刻失效。
func TestLowReasoningDemotionValidation(t *testing.T) {
	_valid := DefaultAdvancedSettingsConfig()
	_valid.LowReasoningDemotionEnabled = true
	if _err := ValidateAdvancedSettingsConfig(_valid); _err != nil {
		t.Fatalf("defaults must validate: %v", _err)
	}

	for _name, _mutate := range map[string]func(*domain.AdvancedSettingsConfig){
		"頻率過低":   func(_c *domain.AdvancedSettingsConfig) { _c.LowReasoningDemotionRequestsPerMin = 0 },
		"頻率過高":   func(_c *domain.AdvancedSettingsConfig) { _c.LowReasoningDemotionRequestsPerMin = 601 },
		"推理比為負":  func(_c *domain.AdvancedSettingsConfig) { _c.LowReasoningDemotionReasoningPercent = -1 },
		"推理比超過百": func(_c *domain.AdvancedSettingsConfig) { _c.LowReasoningDemotionReasoningPercent = 101 },
		"等級為零":   func(_c *domain.AdvancedSettingsConfig) { _c.LowReasoningDemotionTargetTier = 0 },
		"持續為零":   func(_c *domain.AdvancedSettingsConfig) { _c.LowReasoningDemotionMinutes = 0 },
		"啟動門檻為負": func(_c *domain.AdvancedSettingsConfig) { _c.LowReasoningDemotionMinDailyUsagePercent = -1 },
		"啟動門檻超過百": func(_c *domain.AdvancedSettingsConfig) {
			_c.LowReasoningDemotionMinDailyUsagePercent = 101
		},
	} {
		_config := DefaultAdvancedSettingsConfig()
		_mutate(&_config)
		if _err := ValidateAdvancedSettingsConfig(_config); _err == nil {
			t.Fatalf("%s 應該被擋下", _name)
		}
	}
}

// -------------------------------------------------------------------------------------
// 舊設定檔沒有這些欄位，載入後必須拿到預設值而不是零值 ——
// 零值會讓持續時間變成 0、目標等級變成 0，等於功能靜默失效。
func TestLegacyConfigGetsDemotionDefaults(t *testing.T) {
	_path := filepath.Join(t.TempDir(), "advanced_settings.json")
	if _err := os.WriteFile(_path, []byte(`{"conversation_affinity_ttl_minutes":45}`), 0600); _err != nil {
		t.Fatal(_err)
	}
	_config, _err := LoadAdvancedSettingsConfig(_path)
	if _err != nil {
		t.Fatalf("legacy config should load: %v", _err)
	}
	if _config.ConversationAffinityTTLMinutes != 45 {
		t.Fatalf("existing value lost: %+v", _config)
	}
	if _config.LowReasoningDemotionMinDailyUsagePercent != 18 {
		t.Fatalf("daily usage gate default missing: %+v", _config)
	}
	if _config.LowReasoningDemotionTargetTier != 4 || _config.LowReasoningDemotionMinutes != 10 {
		t.Fatalf("demotion defaults missing: %+v", _config)
	}
	if _config.LowReasoningDemotionEnabled {
		t.Fatalf("demotion must stay off by default")
	}
}
