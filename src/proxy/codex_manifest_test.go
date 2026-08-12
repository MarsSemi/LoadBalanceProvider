package proxy

import (
	"encoding/json"
	"testing"
)

// -------------------------------------------------------------------------------------
func TestAddAutoModelToCodexManifest(t *testing.T) {
	_body := []byte(`{
		"models":[{
			"slug":"gpt-5.6-sol",
			"display_name":"GPT-5.6-Sol",
			"description":"model",
			"visibility":"list",
			"supported_in_api":true,
			"priority":1,
			"default_reasoning_level":"low",
			"supported_reasoning_levels":[{"effort":"low","description":"fast"}],
			"input_modalities":["text","image"],
			"context_window":272000
		}],
		"metadata":{"version":1}
	}`)

	_updated, _err := AddAutoModelToCodexManifest(_body)
	if _err != nil {
		t.Fatal(_err)
	}
	_root := map[string]interface{}{}
	if _err := json.Unmarshal(_updated, &_root); _err != nil {
		t.Fatal(_err)
	}
	_models, _ok := _root["models"].([]interface{})
	if !_ok || len(_models) != 2 {
		t.Fatalf("models = %#v, want original plus AUTO", _root["models"])
	}
	_auto, _ok := _models[0].(map[string]interface{})
	if !_ok {
		t.Fatalf("AUTO model = %#v", _models[0])
	}
	if _auto["slug"] != "AUTO" || _auto["display_name"] != "AUTO" || _auto["visibility"] != "list" {
		t.Fatalf("AUTO model metadata = %#v", _auto)
	}
	if _auto["context_window"] != float64(272000) || _auto["default_reasoning_level"] != "high" {
		t.Fatalf("AUTO model did not retain full model metadata: %#v", _auto)
	}
	if _auto["priority"] != float64(0) {
		t.Fatalf("AUTO priority = %#v, want lower priority than upstream models", _auto["priority"])
	}
	_original, _ok := _models[1].(map[string]interface{})
	if !_ok || _original["slug"] != "gpt-5.6-sol" {
		t.Fatalf("original model order changed: %#v", _models)
	}
	if _, _ok := _root["metadata"]; !_ok {
		t.Fatal("manifest root metadata was lost")
	}
}

// -------------------------------------------------------------------------------------
func TestAddAutoModelToCodexManifestIsIdempotent(t *testing.T) {
	_body := []byte(`{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5","priority":7,"visibility":"list","supported_in_api":true}]}`)
	_first, _err := AddAutoModelToCodexManifest(_body)
	if _err != nil {
		t.Fatal(_err)
	}
	_second, _err := AddAutoModelToCodexManifest(_first)
	if _err != nil {
		t.Fatal(_err)
	}
	if string(_second) != string(_first) {
		t.Fatalf("manifest changed after second injection:\nfirst=%s\nsecond=%s", _first, _second)
	}
}

// -------------------------------------------------------------------------------------
func TestCodexUsableModelNamesKeepsAllVisibleManifestModels(t *testing.T) {
	_body := []byte(`{
		"models":[
			{"slug":"gpt-5.6-sol","visibility":"list","supported_in_api":true},
			{"slug":"gpt-5.6","visibility":"list","supported_in_api":false},
			{"slug":"codex-auto-review","visibility":"hide","supported_in_api":true},
			{"slug":"gpt-5.5","visibility":"list","supported_in_api":true},
			{"slug":"GPT-5.5","visibility":"list","supported_in_api":true}
		]
	}`)

	_models, _err := CodexUsableModelNames(_body)
	if _err != nil {
		t.Fatal(_err)
	}
	_want := []string{"gpt-5.6-sol", "gpt-5.6", "gpt-5.5"}
	if len(_models) != len(_want) {
		t.Fatalf("models = %#v, want %#v", _models, _want)
	}
	for _idx := range _want {
		if _models[_idx] != _want[_idx] {
			t.Fatalf("models = %#v, want %#v", _models, _want)
		}
	}
}
