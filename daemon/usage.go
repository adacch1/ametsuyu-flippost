package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mobile data-usage tracker. The rmnet counters reset on reboot / iface
// recreation, so we can't read day/week/month directly. Instead we sample the
// cumulative mobile bytes, detect resets, and accumulate deltas into persisted
// per-day buckets, from which today / 7-day / 30-day totals are derived.

type usageState struct {
	LastTotal uint64            `json:"last_total"` // last observed cumulative bytes
	Days      map[string]uint64 `json:"days"`       // date(YYYY-MM-DD) -> bytes used
}

type UsageTracker struct {
	mu    sync.Mutex
	path  string
	st    usageState
	now   func() time.Time
	readB func() uint64 // sums current cumulative mobile bytes (rx+tx)
}

func NewUsageTracker(path string) *UsageTracker {
	u := &UsageTracker{path: path, now: time.Now, readB: readMobileBytes}
	u.st.Days = map[string]uint64{}
	u.load()
	return u
}

func (u *UsageTracker) load() {
	b, err := os.ReadFile(u.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &u.st)
	if u.st.Days == nil {
		u.st.Days = map[string]uint64{}
	}
}

func (u *UsageTracker) save() {
	b, _ := json.MarshalIndent(u.st, "", "  ")
	tmp := u.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, u.path)
	}
}

// Sample folds the current cumulative counter into today's bucket. Reset-safe:
// if the counter dropped (reboot), the current value is treated as the delta.
func (u *UsageTracker) Sample() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sampleLocked()
}

func (u *UsageTracker) sampleLocked() {
	cur := u.readB()
	var delta uint64
	if cur >= u.st.LastTotal {
		delta = cur - u.st.LastTotal
	} else {
		delta = cur // counter reset
	}
	u.st.LastTotal = cur
	today := u.now().Format("2006-01-02")
	u.st.Days[today] += delta
	u.pruneLocked()
	u.save()
}

// pruneLocked drops buckets older than ~40 days.
func (u *UsageTracker) pruneLocked() {
	cutoff := u.now().AddDate(0, 0, -40)
	for d := range u.st.Days {
		if t, err := time.Parse("2006-01-02", d); err == nil && t.Before(cutoff) {
			delete(u.st.Days, d)
		}
	}
}

type UsageReport struct {
	TodayBytes uint64 `json:"today_bytes"`
	WeekBytes  uint64 `json:"week_bytes"`
	MonthBytes uint64 `json:"month_bytes"`
	TodayHuman string `json:"today_human"`
	WeekHuman  string `json:"week_human"`
	MonthHuman string `json:"month_human"`
	Source     string `json:"source"`
}

func (u *UsageTracker) Report() UsageReport {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sampleLocked() // refresh before reporting
	return sumWindows(u.st.Days, u.now())
}

func sumWindows(days map[string]uint64, now time.Time) UsageReport {
	var today, week, month uint64
	td := now.Format("2006-01-02")
	for d, b := range days {
		if d == td {
			today += b
		}
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		age := now.Sub(t).Hours() / 24
		if age < 7 {
			week += b
		}
		if age < 30 {
			month += b
		}
	}
	return UsageReport{
		TodayBytes: today, WeekBytes: week, MonthBytes: month,
		TodayHuman: humanBytes(today), WeekHuman: humanBytes(week), MonthHuman: humanBytes(month),
		Source: "rmnet-delta",
	}
}

func humanBytes(b uint64) string {
	const u = 1024
	if b < u {
		return strconv.FormatUint(b, 10) + " B"
	}
	div, exp := uint64(u), 0
	for n := b / u; n >= u; n /= u {
		div *= u
		exp++
	}
	val := float64(b) / float64(div)
	return strconv.FormatFloat(val, 'f', 2, 64) + " " + string("KMGTPE"[exp]) + "iB"
}

// readMobileBytes sums rx+tx across mobile (rmnet_data*) interfaces from
// /proc/net/dev. Excludes rmnet_ipa (aggregate shadow) to avoid double counting.
func readMobileBytes() uint64 {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return sysfsMobileBytes()
	}
	var total uint64
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "rmnet_data") {
			continue
		}
		parts := strings.Fields(strings.ReplaceAll(ln, ":", " "))
		// name rx_bytes ... (col1) ; tx_bytes is col9
		if len(parts) >= 10 {
			rx, _ := strconv.ParseUint(parts[1], 10, 64)
			tx, _ := strconv.ParseUint(parts[9], 10, 64)
			total += rx + tx
		}
	}
	return total
}

func sysfsMobileBytes() uint64 {
	var total uint64
	ifaces, _ := filepath.Glob("/sys/class/net/rmnet_data*/statistics")
	for _, s := range ifaces {
		for _, f := range []string{"rx_bytes", "tx_bytes"} {
			if d, err := os.ReadFile(filepath.Join(s, f)); err == nil {
				v, _ := strconv.ParseUint(strings.TrimSpace(string(d)), 10, 64)
				total += v
			}
		}
	}
	return total
}
