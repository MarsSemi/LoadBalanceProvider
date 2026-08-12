package auth

import (
	"path/filepath"
	"testing"
)

// Verify 不得有副作用：授權失敗的請求不應該累加使用次數。
func TestVerifyDoesNotRecordUsage(t *testing.T) {
	_store := NewAPIKeyStore(filepath.Join(t.TempDir(), "api_keys.json"))
	_created, _err := _store.Create("verify-only")
	if _err != nil {
		t.Fatal(_err)
	}

	for _idx := 0; _idx < 3; _idx++ {
		if _, _valid, _err := _store.Verify(_created.Key); _err != nil || !_valid {
			t.Fatalf("verify failed: valid=%v err=%v", _valid, _err)
		}
	}

	_views, _err := _store.List()
	if _err != nil {
		t.Fatal(_err)
	}
	if _got := _views[0].UsageCount; _got != 0 {
		t.Fatalf("usage count after verify-only = %d, want 0", _got)
	}

	if _err := _store.RecordUsage(_created.ID); _err != nil {
		t.Fatal(_err)
	}
	_views, _err = _store.List()
	if _err != nil {
		t.Fatal(_err)
	}
	if _got := _views[0].UsageCount; _got != 1 {
		t.Fatalf("usage count after RecordUsage = %d, want 1", _got)
	}
}

func TestAPIKeyUsageIsCachedAndFlushed(t *testing.T) {
	_path := filepath.Join(t.TempDir(), "api_keys.json")
	_store := NewAPIKeyStore(_path)
	_created, _err := _store.Create("test")
	if _err != nil {
		t.Fatal(_err)
	}
	for _idx := 0; _idx < 2; _idx++ {
		if _, _valid, _err := _store.Validate(_created.Key); _err != nil || !_valid {
			t.Fatalf("validate failed: valid=%v err=%v", _valid, _err)
		}
	}
	_views, _err := _store.List()
	if _err != nil {
		t.Fatal(_err)
	}
	if _got := _views[0].UsageCount; _got != 2 {
		t.Fatalf("cached usage count = %d, want 2", _got)
	}
	if _err := _store.Flush(); _err != nil {
		t.Fatal(_err)
	}
	_reloaded, _err := NewAPIKeyStore(_path).List()
	if _err != nil {
		t.Fatal(_err)
	}
	if _got := _reloaded[0].UsageCount; _got != 2 {
		t.Fatalf("persisted usage count = %d, want 2", _got)
	}
}

func TestAPIKeyRoutingPolicyDefaultsAndPersists(t *testing.T) {
	_path := filepath.Join(t.TempDir(), "api_keys.json")
	_store := NewAPIKeyStore(_path)
	_created, _err := _store.Create("routing")
	if _err != nil {
		t.Fatal(_err)
	}
	if _created.ProviderID != "AUTO" || _created.Model != "AUTO" || _created.ReasoningEffort != "AUTO" {
		t.Fatalf("created routing policy = %#v", _created)
	}

	_updated, _err := _store.Update(_created.ID, "routing-2", "provider-1", "gpt-test", "high")
	if _err != nil {
		t.Fatal(_err)
	}
	if _updated.ProviderID != "provider-1" || _updated.Model != "gpt-test" || _updated.ReasoningEffort != "high" {
		t.Fatalf("updated routing policy = %#v", _updated)
	}

	_reloaded, _err := NewAPIKeyStore(_path).List()
	if _err != nil {
		t.Fatal(_err)
	}
	if len(_reloaded) != 1 || _reloaded[0].ProviderID != "provider-1" || _reloaded[0].Model != "gpt-test" || _reloaded[0].ReasoningEffort != "high" {
		t.Fatalf("reloaded routing policy = %#v", _reloaded)
	}
}

func TestTemporaryAPIKeyIgnoresRoutingPolicy(t *testing.T) {
	_store := NewAPIKeyStore(filepath.Join(t.TempDir(), "api_keys.json"))
	_created, _err := _store.CreateTemporary("web", 0)
	if _err != nil {
		t.Fatal(_err)
	}
	_updated, _err := _store.Update(_created.ID, "web-2", "provider-1", "gpt-test", "low")
	if _err != nil {
		t.Fatal(_err)
	}
	if _updated.ProviderID != "AUTO" || _updated.Model != "AUTO" || _updated.ReasoningEffort != "AUTO" {
		t.Fatalf("temporary routing policy = %#v", _updated)
	}
}
