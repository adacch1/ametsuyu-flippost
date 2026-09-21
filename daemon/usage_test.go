package main

import (
	"encoding/json"
	"os"
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

// TestHumanBytes pins the SI decimal formatting (1 GB = 1000 MB = 10^9
// bytes), matching carrier data meters and Android's own Formatter -- not the
// 1024-based GiB/MiB a filesystem tool would use. The last case is the real
// today_bytes value observed on-device.
func TestHumanBytes(t *testing.T) {
	if humanBytes(999) != "999 B" {
		t.Fatalf("got %s", humanBytes(999))
	}
	if humanBytes(1500) != "1.50 KB" {
		t.Fatalf("got %s", humanBytes(1500))
	}
	if humanBytes(7473753986) != "7.47 GB" {
		t.Fatalf("got %s", humanBytes(7473753986))
	}
}

// TestUsageWindowExcludesOld: now is Wed 2026-07-01, so the ISO week starts
// Mon 2026-06-29 and the calendar month starts 2026-07-01. Only the "today"
// bucket falls inside every window; the old rolling-7d/30d buckets (06-28,
// 06-20, 05-01) are all outside the calendar week/month.
func TestUsageWindowExcludesOld(t *testing.T) {
	days := map[string]uint64{
		"2026-07-01": 100, // today; also the week start's Wednesday and the month start
		"2026-06-28": 50,  // Sunday before the week starts Monday -> excluded
		"2026-06-20": 30,  // previous month -> excluded
		"2026-05-01": 999, // previous month -> excluded
	}
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	r := sumWindows(days, now)
	if r.TodayBytes != 100 || r.WeekBytes != 100 || r.MonthBytes != 100 {
		t.Fatalf("windows = %+v, want today=100 week=100 month=100", r)
	}
}

// TestUsageWindowsCalendar covers the boundary shapes sumWindows must get
// right: week start on a Monday, a full Sunday-ending week, a month-start
// day, a 31-day month crossing a year boundary, and a non-UTC `now` location.
func TestUsageWindowsCalendar(t *testing.T) {
	ict := time.FixedZone("ICT", 7*3600)

	cases := []struct {
		name                        string
		now                         time.Time
		days                        map[string]uint64
		wantToday, wantWeek, wantMo uint64
	}{
		{
			name: "monday: week starts today",
			now:  time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC), // Monday
			days: map[string]uint64{
				"2026-06-29": 100, // today == week start
				"2026-06-28": 50,  // Sunday before -> excluded from week
			},
			wantToday: 100, wantWeek: 100, wantMo: 150, // both June days -> same month
		},
		{
			name: "sunday: full 7-day week summed",
			now:  time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC), // Sunday, week Mon 06-29..Sun 07-05
			days: map[string]uint64{
				"2026-06-28": 999, // day before the week -> excluded
				"2026-06-29": 10, "2026-06-30": 10,
				"2026-07-01": 10, "2026-07-02": 10, "2026-07-03": 10, "2026-07-04": 10, "2026-07-05": 10,
			},
			wantToday: 10, wantWeek: 70, wantMo: 50, // only the five July days count for the month
		},
		{
			name: "month start: month equals today",
			now:  time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), // Saturday, week starts Mon 07-27
			days: map[string]uint64{
				"2026-08-01": 100, // today == month start
				"2026-07-31": 50,  // day before -> excluded from month, same ISO week
			},
			wantToday: 100, wantWeek: 150, wantMo: 100,
		},
		{
			name: "31-day month across a year boundary",
			now:  time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), // Saturday, week starts Mon 01-26
			days: map[string]uint64{
				"2025-12-31": 999, // previous year, previous month -> excluded from both
				"2026-01-01": 1, "2026-01-02": 1, "2026-01-03": 1, "2026-01-04": 1, "2026-01-05": 1,
				"2026-01-06": 1, "2026-01-07": 1, "2026-01-08": 1, "2026-01-09": 1, "2026-01-10": 1,
				"2026-01-11": 1, "2026-01-12": 1, "2026-01-13": 1, "2026-01-14": 1, "2026-01-15": 1,
				"2026-01-16": 1, "2026-01-17": 1, "2026-01-18": 1, "2026-01-19": 1, "2026-01-20": 1,
				"2026-01-21": 1, "2026-01-22": 1, "2026-01-23": 1, "2026-01-24": 1, "2026-01-25": 1,
				"2026-01-26": 1, "2026-01-27": 1, "2026-01-28": 1, "2026-01-29": 1, "2026-01-30": 1,
				"2026-01-31": 1,
			},
			wantToday: 1, wantWeek: 6, wantMo: 31, // week = 01-26..01-31 inclusive
		},
		{
			name: "non-UTC now: local date wins, not the UTC date",
			now:  time.Date(2026, 9, 18, 1, 0, 0, 0, ict), // 01:00 +07 == 2026-09-17T18:00Z
			days: map[string]uint64{
				"2026-09-18": 100, // local today
				"2026-09-17": 5,   // would be "today" under UTC; same ISO week/month either way
			},
			wantToday: 100, wantWeek: 105, wantMo: 105,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := sumWindows(c.days, c.now)
			if r.TodayBytes != c.wantToday || r.WeekBytes != c.wantWeek || r.MonthBytes != c.wantMo {
				t.Fatalf("windows = %+v, want today=%d week=%d month=%d", r, c.wantToday, c.wantWeek, c.wantMo)
			}
		})
	}
}

// TestUsageLocalDayKey proves Sample() buckets by the local date carried by
// u.now(), not by that instant's UTC date.
func TestUsageLocalDayKey(t *testing.T) {
	dir := t.TempDir()
	u := NewUsageTracker(filepath.Join(dir, "usage.json"))
	ict := time.FixedZone("ICT", 7*3600)
	local := time.Date(2026, 9, 18, 1, 0, 0, 0, ict) // 2026-09-18 01:00 +07 == 2026-09-17T18:00Z
	u.now = func() time.Time { return local }
	u.readB = func() uint64 { return 1000 }

	u.Sample()

	if _, ok := u.st.Days["2026-09-18"]; !ok {
		t.Fatalf("expected bucket under local date 2026-09-18, got days=%+v", u.st.Days)
	}
	if _, ok := u.st.Days["2026-09-17"]; ok {
		t.Fatalf("bucketed under UTC date 2026-09-17 instead of the local date: days=%+v", u.st.Days)
	}
}

func readOnDiskLastTotal(t *testing.T, path string) (uint64, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var st usageState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("bad usage.json at %s: %v", path, err)
	}
	return st.LastTotal, true
}

func readOnDiskUsageState(t *testing.T, path string) (usageState, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return usageState{}, false
	}
	var st usageState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("bad usage.json at %s: %v", path, err)
	}
	return st, true
}

// TestUsagePersistThrottle pins every branch of sampleLocked's persist
// gate: first sample, the <60s/>=60s throttle, an idle (unchanged-counter)
// no-write, an immediate persist on day change, and an immediate persist on
// counter reset -- the last two proven with elapsed time well under 60s so
// the throttle itself can't be what triggers the write.
func TestUsagePersistThrottle(t *testing.T) {
	t.Run("first sample always persists", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "usage.json")
		u := NewUsageTracker(path)
		u.now = func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }
		u.readB = func() uint64 { return 1000 }
		u.Sample()
		if got, ok := readOnDiskLastTotal(t, path); !ok || got != 1000 {
			t.Fatalf("on-disk last_total = %d, ok=%v, want 1000, ok=true", got, ok)
		}
	})

	t.Run("throttled under 60s, written at 60s", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "usage.json")
		u := NewUsageTracker(path)
		now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		var cur uint64
		u.now = func() time.Time { return now }
		u.readB = func() uint64 { return cur }

		cur = 1000
		u.Sample() // first sample: persists

		for i := 0; i < 3; i++ { // 15s, 30s, 45s since the save above: all throttled
			now = now.Add(15 * time.Second)
			cur += 100
			u.Sample()
			if got, _ := readOnDiskLastTotal(t, path); got != 1000 {
				t.Fatalf("step %d (t+%ds): on-disk last_total = %d, want unchanged 1000", i, 15*(i+1), got)
			}
		}

		now = now.Add(15 * time.Second) // 60s since the save: persists
		cur += 100
		u.Sample()
		if got, _ := readOnDiskLastTotal(t, path); got != cur {
			t.Fatalf("60s sample: on-disk last_total = %d, want %d", got, cur)
		}
	})

	t.Run("idle counter never writes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "usage.json")
		u := NewUsageTracker(path)
		now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		u.now = func() time.Time { return now }
		u.readB = func() uint64 { return 1000 }
		u.Sample() // baseline write
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove baseline file: %v", err)
		}

		now = now.Add(10 * time.Minute) // well past the throttle; counter unchanged
		u.Sample()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("idle sample: usage.json exists (err=%v), want no write at all", err)
		}
	})

	t.Run("day change persists immediately", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "usage.json")
		u := NewUsageTracker(path)
		now := time.Date(2026, 7, 1, 23, 59, 59, 0, time.UTC)
		var cur uint64 = 1000
		u.now = func() time.Time { return now }
		u.readB = func() uint64 { return cur }
		u.Sample() // baseline: persists (first sample)

		now = time.Date(2026, 7, 2, 0, 0, 1, 0, time.UTC) // 2s later, new day, well under 60s
		cur = 1100
		u.Sample()
		if got, _ := readOnDiskLastTotal(t, path); got != 1100 {
			t.Fatalf("day-change sample: on-disk last_total = %d, want 1100", got)
		}
	})

	t.Run("counter reset persists immediately", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "usage.json")
		u := NewUsageTracker(path)
		now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		var cur uint64 = 5000
		u.now = func() time.Time { return now }
		u.readB = func() uint64 { return cur }
		u.Sample() // baseline: persists (first sample)

		now = now.Add(5 * time.Second) // well under 60s, same day
		cur = 200                      // reboot: counter restarted
		u.Sample()
		if got, _ := readOnDiskLastTotal(t, path); got != 200 {
			t.Fatalf("reset sample: on-disk last_total = %d, want 200", got)
		}
	})
}

// TestUsageFlush proves Flush() writes immediately, bypassing the persist
// throttle that would otherwise skip this sample.
func TestUsageFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	u := NewUsageTracker(path)
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	var cur uint64 = 1000
	u.now = func() time.Time { return now }
	u.readB = func() uint64 { return cur }
	u.Sample() // baseline: persists (first sample)

	now = now.Add(10 * time.Second) // well inside the 60s throttle, same day
	cur += 50
	u.Flush()

	if got, _ := readOnDiskLastTotal(t, path); got != cur {
		t.Fatalf("Flush: on-disk last_total = %d, want %d", got, cur)
	}
}

// TestNextUsageWake pins the sampler's sleep duration: usageSampleEvery
// normally, bracketed 200ms to either side of local midnight (bracketWake),
// and computed from `now`'s own location rather than UTC.
func TestNextUsageWake(t *testing.T) {
	ict := time.FixedZone("ICT", 7*3600)
	cases := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{"midday: full interval", time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), usageSampleEvery},
		{"10s before midnight, +07 local", time.Date(2026, 7, 1, 23, 59, 50, 0, ict), 10*time.Second - 200*time.Millisecond},
		{"150ms before midnight: inside the margin, treated as imminent", time.Date(2026, 7, 1, 23, 59, 59, 850_000_000, ict), 350 * time.Millisecond},
		{"exactly 200ms before midnight: margin boundary, still imminent", time.Date(2026, 7, 1, 23, 59, 59, 800_000_000, ict), 400 * time.Millisecond},
		{"just after midnight: full interval, local not UTC", time.Date(2026, 7, 2, 0, 0, 1, 0, time.UTC), usageSampleEvery},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nextUsageWake(c.now); got != c.want {
				t.Fatalf("nextUsageWake(%v) = %v, want %v", c.now, got, c.want)
			}
		})
	}
}

// TestNormalizeQuota pins the clamp table: the zero value defaults silently
// (fresh install, no error), but any other out-of-range value is both clamped
// AND reported, so a caller can 400 while config load merely logs it.
func TestNormalizeQuota(t *testing.T) {
	cases := []struct {
		name    string
		in      Quota
		want    Quota
		wantErr bool
	}{
		{"zero value: defaults, no error", Quota{},
			Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 1}, false},
		{"already valid: unchanged, no error", Quota{LimitBytes: 100, Period: "daily", ResetTime: "23:59", ResetDay: 15},
			Quota{LimitBytes: 100, Period: "daily", ResetTime: "23:59", ResetDay: 15}, false},
		{"negative limit: clamps to 0, errors", Quota{LimitBytes: -5},
			Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 1}, true},
		{"bad period: clamps to monthly, errors", Quota{Period: "yearly"},
			Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 1}, true},
		{"bad reset_time: clamps to 00:00, errors", Quota{ResetTime: "25:00"},
			Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 1}, true},
		{"reset_time missing leading zero: rejected", Quota{ResetTime: "9:30"},
			Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 1}, true},
		{"reset_day 0: means unset, -> 1, no error", Quota{ResetDay: 0},
			Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 1}, false},
		{"reset_day negative: clamps to 1, errors", Quota{ResetDay: -3},
			Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 1}, true},
		{"reset_day 40: clamps to 28, errors", Quota{ResetDay: 40},
			Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 28}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := normalizeQuota(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("normalizeQuota(%+v) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

// TestNextReset pins the boundary math every reset decision is built on:
// strictly-after semantics (an exact match steps to the NEXT period, not this
// one), month rollover off a day<=28 anchor, week anchored to Monday, manual
// never firing, and the result carried in `after`'s own location.
func TestNextReset(t *testing.T) {
	ict := time.FixedZone("ICT", 7*3600)
	pst := time.FixedZone("PST", -8*3600)
	cases := []struct {
		name  string
		after time.Time
		q     Quota
		want  time.Time
	}{
		{
			"monthly day1 00:00: mid-month -> the 1st of next month",
			time.Date(2026, 9, 18, 14, 14, 0, 0, ict),
			Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 1},
			time.Date(2026, 10, 1, 0, 0, 0, 0, ict),
		},
		{
			"monthly day28 09:30: exactly on the boundary -> next month, strict (day<=28 never overflows)",
			time.Date(2026, 2, 28, 9, 30, 0, 0, ict),
			Quota{Period: "monthly", ResetTime: "09:30", ResetDay: 28},
			time.Date(2026, 3, 28, 9, 30, 0, 0, ict),
		},
		{
			"weekly: Wednesday -> next Monday (this week's Monday already passed)",
			time.Date(2026, 9, 16, 10, 0, 0, 0, ict), // Wednesday
			Quota{Period: "weekly", ResetTime: "00:00"},
			time.Date(2026, 9, 21, 0, 0, 0, 0, ict),
		},
		{
			"weekly: exactly this week's Monday boundary -> next Monday, strict",
			time.Date(2026, 9, 14, 0, 0, 0, 0, ict), // Monday
			Quota{Period: "weekly", ResetTime: "00:00"},
			time.Date(2026, 9, 21, 0, 0, 0, 0, ict),
		},
		{
			"daily 14:00: one minute before -> same day",
			time.Date(2026, 9, 18, 13, 59, 0, 0, ict),
			Quota{Period: "daily", ResetTime: "14:00"},
			time.Date(2026, 9, 18, 14, 0, 0, 0, ict),
		},
		{
			"daily 14:00: exactly at the boundary -> next day, strict",
			time.Date(2026, 9, 18, 14, 0, 0, 0, ict),
			Quota{Period: "daily", ResetTime: "14:00"},
			time.Date(2026, 9, 19, 14, 0, 0, 0, ict),
		},
		{
			"manual: never resets on a schedule",
			time.Date(2026, 9, 18, 14, 14, 0, 0, ict),
			Quota{Period: "manual"},
			time.Time{},
		},
		{
			"non-UTC location carried through: daily boundary computed in PST, not UTC",
			time.Date(2026, 9, 18, 23, 0, 0, 0, pst),
			Quota{Period: "daily", ResetTime: "00:00"},
			time.Date(2026, 9, 19, 0, 0, 0, 0, pst),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nextReset(c.after, c.q)
			if !got.Equal(c.want) {
				t.Fatalf("nextReset(%v, %+v) = %v, want %v", c.after, c.q, got, c.want)
			}
			if !c.want.IsZero() {
				_, gotOff := got.Zone()
				_, wantOff := c.want.Zone()
				if gotOff != wantOff {
					t.Fatalf("nextReset(%v, %+v) offset = %ds, want %ds (location not carried through)", c.after, c.q, gotOff, wantOff)
				}
			}
		})
	}
}

// TestUsageSetQuotaSeeds covers the two SetQuota paths: the first schedule
// (startup/upgrade) day-granular re-seeds period_bytes from the Days buckets,
// and a later limit-only change (same period/reset_time/reset_day) must not
// touch a meter a manual reset already moved.
func TestUsageSetQuotaSeeds(t *testing.T) {
	dir := t.TempDir()
	u := NewUsageTracker(filepath.Join(dir, "usage.json"))
	ict := time.FixedZone("ICT", 7*3600)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, ict)
	u.now = func() time.Time { return now }
	u.st.Days = map[string]uint64{
		"2026-08-31": 999, // before the anchor -> excluded
		"2026-09-01": 100, // the anchor day itself -> included
		"2026-09-17": 50,  // after the anchor -> included
	}

	u.SetQuota(Quota{LimitBytes: 1, Period: "monthly", ResetTime: "00:00", ResetDay: 1})

	wantAnchor := time.Date(2026, 9, 1, 0, 0, 0, 0, ict)
	if !u.st.LastReset.Equal(wantAnchor) {
		t.Fatalf("LastReset = %v, want %v", u.st.LastReset, wantAnchor)
	}
	if u.st.PeriodBytes != 150 {
		t.Fatalf("PeriodBytes = %d, want 150 (100+50, 08-31 excluded)", u.st.PeriodBytes)
	}

	// Simulate a manual reset made earlier today, after the monthly anchor.
	manualAt := time.Date(2026, 9, 17, 15, 0, 0, 0, ict)
	u.st.LastReset = manualAt
	u.st.PeriodBytes = 7

	// A limit-only change (period/reset_time/reset_day identical) must leave
	// the meter exactly as the manual reset left it.
	u.SetQuota(Quota{LimitBytes: 2, Period: "monthly", ResetTime: "00:00", ResetDay: 1})
	if !u.st.LastReset.Equal(manualAt) {
		t.Fatalf("limit-only change moved LastReset: got %v, want %v", u.st.LastReset, manualAt)
	}
	if u.st.PeriodBytes != 7 {
		t.Fatalf("limit-only change touched PeriodBytes: got %d, want 7", u.st.PeriodBytes)
	}
	if u.q.LimitBytes != 2 {
		t.Fatalf("limit not applied: got %d, want 2", u.q.LimitBytes)
	}
}

// TestUsageScheduledReset proves a live boundary crossing inside sampleLocked:
// the period zeroes and re-anchors BEFORE the delta is folded in, the write
// bypasses the persist throttle, and today/day buckets are untouched.
func TestUsageScheduledReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	u := NewUsageTracker(path)
	ict := time.FixedZone("ICT", 7*3600)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, ict)
	u.now = func() time.Time { return now }
	var cur uint64 = 1000
	u.readB = func() uint64 { return cur }

	u.SetQuota(Quota{Period: "daily", ResetTime: "00:00"})
	u.Sample() // baseline: Days["2026-09-17"] += 1000, PeriodBytes 0->1000

	if u.st.PeriodBytes != 1000 {
		t.Fatalf("baseline PeriodBytes = %d, want 1000", u.st.PeriodBytes)
	}

	// Cross local midnight with a fresh delta, 200ms after the boundary.
	now = time.Date(2026, 9, 18, 0, 0, 0, 200_000_000, ict)
	u.now = func() time.Time { return now }
	cur += 700
	u.Sample()

	wantReset := time.Date(2026, 9, 18, 0, 0, 0, 0, ict)
	if !u.st.LastReset.Equal(wantReset) {
		t.Fatalf("LastReset = %v, want %v (anchored to the boundary, not the poll instant)", u.st.LastReset, wantReset)
	}
	if u.st.PeriodBytes != 700 {
		t.Fatalf("PeriodBytes after boundary = %d, want 700 (the fresh delta only)", u.st.PeriodBytes)
	}
	if u.st.Days["2026-09-17"] != 1000 {
		t.Fatalf("day bucket 2026-09-17 = %d, want 1000 (untouched by the reset)", u.st.Days["2026-09-17"])
	}
	if u.st.Days["2026-09-18"] != 700 {
		t.Fatalf("day bucket 2026-09-18 = %d, want 700", u.st.Days["2026-09-18"])
	}

	// The reset must persist immediately even though well under 60s has
	// passed since the baseline save.
	got, ok := readOnDiskUsageState(t, path)
	if !ok {
		t.Fatal("boundary sample did not persist usage.json")
	}
	if got.LastTotal != cur || got.PeriodBytes != 700 || !got.LastReset.Equal(wantReset) {
		t.Fatalf("on-disk state = %+v, want last_total=%d period_bytes=700 last_reset=%v", got, cur, wantReset)
	}
}

// TestUsageManualReset proves Reset() zeroes the meter, stamps last_reset to
// now, and persists immediately.
func TestUsageManualReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	u := NewUsageTracker(path)
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	u.now = func() time.Time { return now }
	u.st.PeriodBytes = 12345

	u.Reset()

	if u.st.PeriodBytes != 0 {
		t.Fatalf("PeriodBytes after Reset = %d, want 0", u.st.PeriodBytes)
	}
	if !u.st.LastReset.Equal(now) {
		t.Fatalf("LastReset = %v, want %v", u.st.LastReset, now)
	}
	got, ok := readOnDiskUsageState(t, path)
	if !ok {
		t.Fatal("Reset did not persist usage.json")
	}
	if got.PeriodBytes != 0 || !got.LastReset.Equal(now) {
		t.Fatalf("on-disk state = %+v, want period_bytes=0 last_reset=%v", got, now)
	}
}

// TestUsageResetSurvivesRestart is the acceptance proof for "survives a
// restart": a manual reset made by one tracker must be exactly what a brand
// new tracker on the same usage.json reports, even after SetQuota runs again
// — it must not re-seed period_bytes from the day buckets, because the
// manual reset is already newer than the schedule's anchor point.
func TestUsageResetSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")

	a := NewUsageTracker(path)
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	a.st.Days = map[string]uint64{"2026-09-01": 500, "2026-09-18": 20}
	a.Reset()

	b := NewUsageTracker(path) // fresh tracker, same file: loads A's persisted state
	later := now.Add(time.Hour)
	b.now = func() time.Time { return later }
	b.SetQuota(Quota{Period: "monthly", ResetTime: "00:00", ResetDay: 1})

	if !b.st.LastReset.Equal(now) {
		t.Fatalf("LastReset after restart = %v, want %v (A's manual reset)", b.st.LastReset, now)
	}
	if b.st.PeriodBytes != 0 {
		t.Fatalf("PeriodBytes re-seeded from the day buckets after restart: got %d, want 0", b.st.PeriodBytes)
	}
}

// TestUsageTrackerNextWake proves the sampler's sleep also shortens toward an
// upcoming scheduled reset, not just local midnight, and falls back to the
// plain nextUsageWake when no quota is configured.
func TestUsageTrackerNextWake(t *testing.T) {
	dir := t.TempDir()
	u := NewUsageTracker(filepath.Join(dir, "usage.json"))
	now := time.Date(2026, 9, 18, 10, 12, 57, 0, time.UTC)

	if got, want := u.NextWake(now), nextUsageWake(now); got != want {
		t.Fatalf("NextWake with no quota configured = %v, want %v (plain nextUsageWake)", got, want)
	}

	// The next daily boundary is 3s away (last reset was exactly one day
	// earlier, at the same HH:MM) -- well under the 15s sampler interval, so
	// NextWake must shorten to it the same bracketWake way it shortens toward
	// midnight: 200ms on the near side of the boundary.
	u.st.LastReset = time.Date(2026, 9, 17, 10, 13, 0, 0, time.UTC)
	u.q = Quota{Period: "daily", ResetTime: "10:13"}
	got := u.NextWake(now)
	want := 3*time.Second - 200*time.Millisecond
	if got != want {
		t.Fatalf("NextWake with a near reset = %v, want %v", got, want)
	}

	// 100ms from the same boundary falls inside bracketWake's margin, so it
	// takes the far-side (+200ms) arm even though the boundary is still ahead.
	now = time.Date(2026, 9, 18, 10, 12, 59, 900_000_000, time.UTC)
	got = u.NextWake(now)
	want = 300 * time.Millisecond
	if got != want {
		t.Fatalf("NextWake 100ms from a reset = %v, want %v", got, want)
	}
}

// TestBracketWake pins bracketWake's three arms directly: far from the
// boundary returns the normal cadence, inside one cadence but still outside
// the margin returns the near-side (boundary-margin) wait, and inside (or
// past) the margin returns the far-side (boundary+margin) wait -- including a
// negative `until` (a missed wake), which must come back <= 0 so the caller's
// time.Sleep returns immediately instead of waiting almost a full cadence.
func TestBracketWake(t *testing.T) {
	cases := []struct {
		name  string
		until time.Duration
		want  time.Duration
	}{
		{"far from the boundary: normal cadence", 30 * time.Second, usageSampleEvery},
		{"within one cadence: near-side margin", 10 * time.Second, 10*time.Second - bracketMargin},
		{"just past the margin: still near-side", 201 * time.Millisecond, time.Millisecond},
		{"exactly at the margin: far-side arm", 200 * time.Millisecond, 400 * time.Millisecond},
		{"inside the margin: far-side arm", 50 * time.Millisecond, 250 * time.Millisecond},
		{"negative (missed wake): immediate", -3 * time.Second, -2800 * time.Millisecond},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := bracketWake(c.until, usageSampleEvery); got != c.want {
				t.Fatalf("bracketWake(%v, %v) = %v, want %v", c.until, usageSampleEvery, got, c.want)
			}
		})
	}
}

// TestUsageBoundaryBracket proves bracketWake's straddle does what it exists
// for: a sample 200ms before local midnight buckets under the old day, and
// the following sample 200ms after buckets under the new day, leaving the
// old bucket untouched and persisting immediately (a day change bypasses the
// throttle). The scenario also configures a daily quota anchored to
// midnight, so the same midnight crossing fires a scheduled reset, letting
// this test also prove NextWake doesn't come back <= 0 right after a reset
// fires -- which would spin the sampler in a busy loop.
func TestUsageBoundaryBracket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	u := NewUsageTracker(path)
	ict := time.FixedZone("ICT", 7*3600)

	now := time.Date(2026, 9, 18, 23, 59, 59, 800_000_000, ict) // 200ms before midnight
	var cur uint64 = 1000
	u.now = func() time.Time { return now }
	u.readB = func() uint64 { return cur }
	u.SetQuota(Quota{Period: "daily", ResetTime: "00:00"})
	u.Sample() // baseline: buckets under 2026-09-18, period_bytes 0 -> 1000

	if u.st.Days["2026-09-18"] != 1000 {
		t.Fatalf("pre-midnight sample: days = %+v, want 1000 under 2026-09-18", u.st.Days)
	}

	now = time.Date(2026, 9, 19, 0, 0, 0, 200_000_000, ict) // 200ms after midnight
	cur += 500
	u.Sample() // crosses the boundary: new day bucket, and the daily reset fires

	if u.st.Days["2026-09-18"] != 1000 {
		t.Fatalf("old bucket changed after midnight: got %d, want unchanged 1000", u.st.Days["2026-09-18"])
	}
	if u.st.Days["2026-09-19"] != 500 {
		t.Fatalf("new bucket = %d, want 500", u.st.Days["2026-09-19"])
	}

	got, ok := readOnDiskUsageState(t, path)
	if !ok {
		t.Fatal("day-change sample did not persist usage.json")
	}
	if got.LastTotal != cur || got.Days["2026-09-19"] != 500 {
		t.Fatalf("on-disk state = %+v, want last_total=%d days[2026-09-19]=500", got, cur)
	}

	if w := u.NextWake(now); w <= 0 {
		t.Fatalf("NextWake right after a fired reset = %v, want > 0 (busy-loop risk)", w)
	}
}
