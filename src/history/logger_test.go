package history

import (
	"testing"
	"time"
)

func TestRecordChatUsesCacheAndFlushPersists(t *testing.T) {
	_root := t.TempDir()
	_logger := NewLogger(_root)
	_now := time.Now()
	_record := ChatRecord{
		Timestamp:          _now.Format(time.RFC3339Nano),
		SelectedProviderID: "provider-1",
		SelectedProvider:   "Provider 1",
		Success:            true,
		DurationMS:         1200,
		CompletionTokens:   42,
	}
	if _err := _logger.RecordChat(_record); _err != nil {
		t.Fatal(_err)
	}
	if _err := _logger.RecordChat(_record); _err != nil {
		t.Fatal(_err)
	}
	if _err := _logger.Flush(); _err != nil {
		t.Fatal(_err)
	}
	_fallbacks := NewLogger(_root).RecentProviderMetricFallbacks()
	_metric := _fallbacks["provider-1"]
	if _metric.TotalRequests != 2 {
		t.Fatalf("persisted requests = %d, want 2", _metric.TotalRequests)
	}
	if _metric.TotalCompletionTokens != 84 {
		t.Fatalf("persisted completion tokens = %d, want 84", _metric.TotalCompletionTokens)
	}
}
