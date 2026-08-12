package providerusage

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

// -------------------------------------------------------------------------------------
func TestRecorderAggregatesCompletedDailyWindows(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "provider_usage"))
	_dayStart := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.Local)
	_dayEnd := time.Date(2026, time.August, 10, 23, 59, 0, 0, time.Local)

	if _err := _recorder.RecordDayStart("provider-a", 90, _dayStart); _err != nil {
		t.Fatalf("record provider-a start: %v", _err)
	}
	if _err := _recorder.RecordDayEnd("provider-a", 52, _dayEnd); _err != nil {
		t.Fatalf("record provider-a end: %v", _err)
	}
	if _err := _recorder.RecordDayStart("provider-b", 100, _dayStart); _err != nil {
		t.Fatalf("record provider-b start: %v", _err)
	}
	if _err := _recorder.RecordDayEnd("provider-b", 70, _dayEnd); _err != nil {
		t.Fatalf("record provider-b end: %v", _err)
	}
	if _err := _recorder.Flush(); _err != nil {
		t.Fatalf("flush: %v", _err)
	}

	_stats, _err := _recorder.LoadMonth([]string{"provider-a", "provider-b"}, "2026-08")
	if _err != nil {
		t.Fatalf("load month: %v", _err)
	}
	if _stats.ProviderCount != 2 || len(_stats.Days) != 1 {
		t.Fatalf("stats = %#v", _stats)
	}
	_day := _stats.Days[0]
	if _day.Date != "2026-08-10" || _day.ProviderCount != 2 {
		t.Fatalf("day = %#v", _day)
	}
	if math.Abs(_day.UsagePercent-34) > 0.001 || math.Abs(_day.RemainingPercent-61) > 0.001 {
		t.Fatalf("aggregate percentages = %.1f / %.1f; want 34.0 / 61.0", _day.UsagePercent, _day.RemainingPercent)
	}
}

// -------------------------------------------------------------------------------------
func TestRecorderExcludesProvidersWithoutCompletedDayFromDailyAverage(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "provider_usage"))
	_start := time.Date(2026, time.February, 28, 0, 0, 0, 0, time.Local)
	_end := time.Date(2026, time.February, 28, 23, 59, 0, 0, time.Local)
	if _err := _recorder.RecordDayStart("provider-a", 100, _start); _err != nil {
		t.Fatalf("record start: %v", _err)
	}
	if _err := _recorder.RecordDayEnd("provider-a", 85, _end); _err != nil {
		t.Fatalf("record end: %v", _err)
	}

	_stats, _err := _recorder.LoadMonth([]string{"provider-a", "provider-missing"}, "2026-02")
	if _err != nil {
		t.Fatalf("load month: %v", _err)
	}
	if len(_stats.Days) != 1 || _stats.Days[0].ProviderCount != 1 || _stats.Days[0].UsagePercent != 15 {
		t.Fatalf("stats = %#v", _stats)
	}
}

// -------------------------------------------------------------------------------------
func TestRecorderUsesStartEndBoundariesAndAvoidsNegativeUsageAfterReset(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "provider_usage"))
	_dayOneStart := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.Local)
	_dayOneEnd := time.Date(2026, time.August, 1, 23, 59, 0, 0, time.Local)
	_dayTwoStart := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.Local)
	_dayTwoEnd := time.Date(2026, time.August, 2, 23, 59, 0, 0, time.Local)
	_dayThreeEnd := time.Date(2026, time.August, 3, 23, 59, 0, 0, time.Local)

	for _, _record := range []struct {
		start bool
		at    time.Time
		value float64
	}{
		{true, _dayOneStart, 90},
		{false, _dayOneEnd, 72},
		{true, _dayTwoStart, 72},
		{false, _dayTwoEnd, 88},   // Quota reset during the day: 72 - 88 must become 0.
		{false, _dayThreeEnd, 40}, // Missing start boundary defaults to 100.
	} {
		var _err error
		if _record.start {
			_err = _recorder.RecordDayStart("provider-a", _record.value, _record.at)
		} else {
			_err = _recorder.RecordDayEnd("provider-a", _record.value, _record.at)
		}
		if _err != nil {
			t.Fatalf("record boundary: %v", _err)
		}
	}

	_stats, _err := _recorder.LoadMonth([]string{"provider-a"}, "2026-08")
	if _err != nil {
		t.Fatalf("load month: %v", _err)
	}
	if len(_stats.Days) != 3 {
		t.Fatalf("days = %#v", _stats.Days)
	}

	_usageByDate := map[string]float64{}
	for _, _day := range _stats.Days {
		_usageByDate[_day.Date] = _day.UsagePercent
	}
	if _usageByDate["2026-08-01"] != 18 {
		t.Fatalf("August 1 usage = %.1f, want 18.0 from its own day boundaries", _usageByDate["2026-08-01"])
	}
	if _usageByDate["2026-08-02"] != 0 {
		t.Fatalf("August 2 usage = %.1f, want 0.0 after quota reset", _usageByDate["2026-08-02"])
	}
	if _usageByDate["2026-08-03"] != 60 {
		t.Fatalf("August 3 usage = %.1f, want 60.0 with missing start treated as 100", _usageByDate["2026-08-03"])
	}
}

// -------------------------------------------------------------------------------------
func TestRecorderReportsLiveDailyUsageBeforeDayEnd(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "provider_usage"))
	_start := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.Local)
	_noon := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.Local)

	if _err := _recorder.RecordDayStart("provider-a", 90, _start); _err != nil {
		t.Fatalf("record day start: %v", _err)
	}
	if _err := _recorder.Record("provider-a", 15, 75, _noon); _err != nil {
		t.Fatalf("record live usage: %v", _err)
	}

	_stats, _err := _recorder.LoadMonth([]string{"provider-a"}, "2026-08")
	if _err != nil {
		t.Fatalf("load month: %v", _err)
	}
	if len(_stats.Days) != 1 {
		t.Fatalf("days = %#v", _stats.Days)
	}
	_day := _stats.Days[0]
	if _day.UsagePercent != 15 || _day.RemainingPercent != 75 || _day.Completed {
		t.Fatalf("live day = %#v", _day)
	}
}

// -------------------------------------------------------------------------------------
func TestRecorderUsesLatestLiveRemainingAfterQuotaReset(t *testing.T) {
	_recorder := NewRecorder(filepath.Join(t.TempDir(), "provider_usage"))
	_start := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.Local)

	if _err := _recorder.RecordDayStart("provider-a", 100, _start); _err != nil {
		t.Fatalf("record day start: %v", _err)
	}
	if _err := _recorder.Record("provider-a", 20, 80, _start.Add(8*time.Hour)); _err != nil {
		t.Fatalf("record first live usage: %v", _err)
	}
	if _err := _recorder.Record("provider-a", 10, 90, _start.Add(12*time.Hour)); _err != nil {
		t.Fatalf("record reset live usage: %v", _err)
	}

	_stats, _err := _recorder.LoadMonth([]string{"provider-a"}, "2026-08")
	if _err != nil {
		t.Fatalf("load month: %v", _err)
	}
	if len(_stats.Days) != 1 || _stats.Days[0].UsagePercent != 10 || _stats.Days[0].RemainingPercent != 90 {
		t.Fatalf("stats = %#v", _stats)
	}
}
