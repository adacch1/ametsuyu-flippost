package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestUsageBucketsAndWindows(t *testing.T) {
	dir := t.TempDir()
	u := NewUsageTracker(filepath.Join(dir, "usage.json"))
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	u.now = func() time.Time { return base }
	var cur uint64
	u.readB = func() uint64 { return cur }

	cur = 1000
	u.Sample() // first sample: delta 1000 -> today
	cur = 3000
	u.Sample() // +2000 -> today total 3000
	r := u.Report()
	if r.TodayBytes < 3000 {
		t.Fatalf("today = %d, want >=3000", r.TodayBytes)
	}
	if r.WeekBytes < r.TodayBytes || r.MonthBytes < r.WeekBytes {
		t.Fatalf("window monotonicity broken: %+v", r)
	}
}

func TestUsageResetSafe(t *testing.T) {
	dir := t.TempDir()
	u := NewUsageTracker(filepath.Join(dir, "usage.json"))
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	u.now = func() time.Time { return base }
	var cur uint64 = 5000
	u.readB = func() uint64 { return cur }
	u.Sample() // delta 5000
	cur = 200  // reboot: counter reset -> delta treated as 200, not negative
	u.Sample()
	r := u.Report()
	if r.TodayBytes != 5200 {
		t.Fatalf("reset-safe today = %d, want 5200", r.TodayBytes)
	}
}

func TestHumanBytes(t *testing.T) {
	if humanBytes(1023) != "1023 B" {
		t.Fatalf("got %s", humanBytes(1023))
	}
	if humanBytes(1536) != "1.50 KiB" {
		t.Fatalf("got %s", humanBytes(1536))
	}
}

func TestUsageWindowExcludesOld(t *testing.T) {
	days := map[string]uint64{
		"2026-07-01": 100, // today
		"2026-06-28": 50,  // within 7d
		"2026-06-20": 30,  // within 30d, outside 7d
		"2026-05-01": 999, // outside 30d
	}
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	r := sumWindows(days, now)
	if r.TodayBytes != 100 || r.WeekBytes != 150 || r.MonthBytes != 180 {
		t.Fatalf("windows = %+v", r)
	}
}
