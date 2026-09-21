package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestTempHistoryAverages pins the aggregation method: hour buckets are the
// sample-weighted mean of that hour's minute samples, and so are day
// buckets -- a day is never the mean of its hour means. Samples 40, 40 in
// hour T10 and 46 in hour T11 (same local day) give day C = (40+40+46)/3 =
// 42.0, not (40+46)/2 = 43.0.
func TestTempHistoryAverages(t *testing.T) {
	dir := t.TempDir()
	var cur float64
	h := NewTempHistory(filepath.Join(dir, "temps.json"), func() (float64, bool) { return cur, true })
	ict := time.FixedZone("ICT", 7*3600)
	clock := time.Date(2026, 7, 1, 10, 0, 0, 0, ict)
	h.now = func() time.Time { return clock }

	cur = 40.0
	h.Sample() // 10:00
	clock = clock.Add(time.Minute)
	cur = 40.0
	h.Sample() // 10:01
	clock = time.Date(2026, 7, 1, 11, 0, 0, 0, ict)
	cur = 46.0
	h.Sample() // 11:00, same local day

	r := h.Report()
	if len(r.Minutes) != 3 {
		t.Fatalf("minutes len = %d, want 3", len(r.Minutes))
	}
	if r.Minutes[2][1] != 46 {
		t.Fatalf("last minute = %v, want c=46", r.Minutes[2])
	}
	if len(r.Hours) != 2 {
		t.Fatalf("hours len = %d, want 2: %+v", len(r.Hours), r.Hours)
	}
	if r.Hours[0].C != 40 || r.Hours[0].N != 2 {
		t.Errorf("hours[0] = %+v, want C=40 N=2 (the T10 bucket)", r.Hours[0])
	}
	if r.Hours[1].C != 46 || r.Hours[1].N != 1 {
		t.Errorf("hours[1] = %+v, want C=46 N=1 (the T11 bucket)", r.Hours[1])
	}
	if r.Hours[0].T >= r.Hours[1].T {
		t.Errorf("hours not ascending: %q then %q", r.Hours[0].T, r.Hours[1].T)
	}
	if len(r.Days) != 1 {
		t.Fatalf("days len = %d, want 1", len(r.Days))
	}
	if r.Days[0].N != 3 {
		t.Errorf("day N = %d, want 3", r.Days[0].N)
	}
	if r.Days[0].C != 42.0 {
		t.Errorf("day C = %v, want 42.0 (sample-weighted mean), not 43.0 (mean of hour means)", r.Days[0].C)
	}
}

// TestTempHistorySkipsUnavailable proves an unavailable or degraded read
// records nothing (no zero-filled sample), while a real, sub-tenth-degree
// reading is recorded rounded to 0.1 C.
func TestTempHistorySkipsUnavailable(t *testing.T) {
	dir := t.TempDir()
	var cur float64
	var ok bool
	h := NewTempHistory(filepath.Join(dir, "temps.json"), func() (float64, bool) { return cur, ok })
	h.now = func() time.Time { return time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC) }

	cur, ok = 0, false // smoother empty (first ~2s after start)
	h.Sample()
	cur, ok = 0, true // degraded sweep: ok but non-positive
	h.Sample()
	if len(h.st.Minutes) != 0 {
		t.Fatalf("unavailable/degraded samples recorded: %d entries", len(h.st.Minutes))
	}

	cur, ok = 41.63, true
	h.Sample()
	r := h.Report()
	if len(r.Minutes) != 1 || r.Minutes[0][1] != 41.6 {
		t.Fatalf("minutes = %+v, want one entry rounded to 41.6", r.Minutes)
	}
}

// TestTempHistoryRetention proves minute samples cap at tempKeepMinutes and
// that pruneLocked drops hour keys older than 7 days and day keys older than
// 40 days while never touching the current hour/day key.
func TestTempHistoryRetention(t *testing.T) {
	dir := t.TempDir()
	var cur float64 = 40
	h := NewTempHistory(filepath.Join(dir, "temps.json"), func() (float64, bool) { return cur, true })
	ict := time.FixedZone("ICT", 7*3600)
	clock := time.Date(2026, 7, 1, 0, 0, 0, 0, ict)
	h.now = func() time.Time { return clock }

	for i := 0; i < tempKeepMinutes+1; i++ {
		h.Sample()
		clock = clock.Add(time.Minute)
	}
	if len(h.st.Minutes) != tempKeepMinutes {
		t.Fatalf("minutes len = %d, want %d", len(h.st.Minutes), tempKeepMinutes)
	}

	// Synthesize buckets spanning well past both horizons: an old hour/day
	// that must be pruned, and the current hour/day that must survive.
	now := clock
	oldHourKey := now.Add(-8 * 24 * time.Hour).Format(hourLayout)
	oldDayKey := now.AddDate(0, 0, -41).Format(dayLayout)
	curHourKey := now.Format(hourLayout)
	curDayKey := now.Format(dayLayout)
	h.st.Hours[oldHourKey] = tempBucket{Sum: 40, N: 1}
	h.st.Days[oldDayKey] = tempBucket{Sum: 40, N: 1}
	h.st.Hours[curHourKey] = tempBucket{Sum: 40, N: 1}
	h.st.Days[curDayKey] = tempBucket{Sum: 40, N: 1}

	h.Flush()

	if _, ok := h.st.Hours[oldHourKey]; ok {
		t.Errorf("hour key older than 7d not pruned: %s", oldHourKey)
	}
	if _, ok := h.st.Days[oldDayKey]; ok {
		t.Errorf("day key older than 40d not pruned: %s", oldDayKey)
	}
	if _, ok := h.st.Hours[curHourKey]; !ok {
		t.Errorf("current hour key was pruned: %s", curHourKey)
	}
	if _, ok := h.st.Days[curDayKey]; !ok {
		t.Errorf("current day key was pruned: %s", curDayKey)
	}
	if len(h.st.Minutes) != tempKeepMinutes {
		t.Fatalf("minutes len after flush = %d, want %d", len(h.st.Minutes), tempKeepMinutes)
	}
}

// TestTempHistoryPersistRoundTrip proves Flush -> reload equality, the file's
// permission bits, and that a corrupt file is tolerated (empty state, no
// panic) rather than crashing the daemon at startup.
func TestTempHistoryPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "temps.json")
	var cur float64
	stub := func() (float64, bool) { return cur, true }
	h := NewTempHistory(path, stub)
	ict := time.FixedZone("ICT", 7*3600)
	clock := time.Date(2026, 7, 1, 9, 0, 0, 0, ict)
	h.now = func() time.Time { return clock }

	cur = 40.0
	h.Sample()
	clock = clock.Add(time.Minute)
	cur = 41.0
	h.Sample()
	clock = clock.Add(time.Minute)
	cur = 42.0
	h.Sample()
	h.Flush()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}

	h2 := NewTempHistory(path, stub)
	if !reflect.DeepEqual(h.st.Minutes, h2.st.Minutes) {
		t.Errorf("minutes mismatch after reload:\n got  %+v\n want %+v", h2.st.Minutes, h.st.Minutes)
	}
	if !reflect.DeepEqual(h.st.Hours, h2.st.Hours) {
		t.Errorf("hours mismatch after reload:\n got  %+v\n want %+v", h2.st.Hours, h.st.Hours)
	}
	if !reflect.DeepEqual(h.st.Days, h2.st.Days) {
		t.Errorf("days mismatch after reload:\n got  %+v\n want %+v", h2.st.Days, h.st.Days)
	}

	// A corrupt file must never panic the daemon at startup: empty state instead.
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	h3 := NewTempHistory(path, stub)
	if len(h3.st.Minutes) != 0 || len(h3.st.Hours) != 0 || len(h3.st.Days) != 0 {
		t.Errorf("corrupt file did not yield empty state: %+v", h3.st)
	}
}

// TestTempHistoryPersistThrottle pins the 5-minute persist throttle: the
// first sample always writes (savedAt is the zero time), a sample 60s later
// must not rewrite the file, and one at +5 minutes must.
func TestTempHistoryPersistThrottle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "temps.json")
	var cur float64 = 40
	h := NewTempHistory(path, func() (float64, bool) { return cur, true })
	ict := time.FixedZone("ICT", 7*3600)
	clock := time.Date(2026, 7, 1, 9, 0, 0, 0, ict)
	h.now = func() time.Time { return clock }

	readDiskMinutes := func() int {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var st tempState
		if err := json.Unmarshal(b, &st); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return len(st.Minutes)
	}

	h.Sample() // savedAt is zero -> writes immediately
	if n := readDiskMinutes(); n != 1 {
		t.Fatalf("first sample: disk has %d minutes, want 1", n)
	}

	clock = clock.Add(60 * time.Second)
	cur = 41
	h.Sample() // 60s later, under the 5-minute throttle -> file unchanged
	if n := readDiskMinutes(); n != 1 {
		t.Fatalf("throttled sample rewrote the file: disk has %d minutes, want 1", n)
	}

	clock = clock.Add(5 * time.Minute)
	cur = 42
	h.Sample() // >=5 minutes since the last save -> persists
	if n := readDiskMinutes(); n != 3 {
		t.Fatalf("sample at +5m did not persist: disk has %d minutes, want 3", n)
	}
}
