package keyusage

import (
	"testing"
	"time"
)

func TestRecordUsesCacheAndFlushPersists(t *testing.T) {
	_root := t.TempDir()
	_recorder := NewRecorder(_root)
	_at := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.Local)
	if _err := _recorder.Record("key-1", _at); _err != nil {
		t.Fatal(_err)
	}
	if _err := _recorder.Record("key-1", _at); _err != nil {
		t.Fatal(_err)
	}
	_stats, _err := _recorder.LoadMonth("key-1", "2026-07")
	if _err != nil {
		t.Fatal(_err)
	}
	if _stats.Total != 2 {
		t.Fatalf("cached total = %d, want 2", _stats.Total)
	}
	if _err := _recorder.Flush(); _err != nil {
		t.Fatal(_err)
	}
	_reloaded, _err := NewRecorder(_root).LoadMonth("key-1", "2026-07")
	if _err != nil {
		t.Fatal(_err)
	}
	if _reloaded.Total != 2 {
		t.Fatalf("persisted total = %d, want 2", _reloaded.Total)
	}
}
