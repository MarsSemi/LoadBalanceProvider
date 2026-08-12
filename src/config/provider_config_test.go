package config

import (
	"testing"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
func TestApplyDefaultsLiftsLlamaCppMaxInputTokens(t *testing.T) {
	_config := &domain.ProxyConfig{
		Providers: []domain.LLMProviderConfig{
			{
				Kind: "llamacpp",
				Models: []domain.LLMModelConfig{
					{Name: "vision-model", MaxInputTokens: 8192},
				},
			},
		},
	}

	ApplyDefaults(_config)

	_got := _config.Providers[0].Models[0].MaxInputTokens
	if _got != 1048576 {
		t.Fatalf("llama.cpp max input tokens = %d, want 1048576", _got)
	}
}
