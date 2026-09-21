package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mobile data-usage tracker. The rmnet counters reset on reboot / iface
// recreation, so we can't read day/week/month directly. Instead we sample the
// cumulative mobile bytes, detect resets, and accumulate deltas into persisted
// per-day buckets keyed by local calendar date (main.go resolves time.Local
// from getprop persist.sys.timezone, since a static GOOS=linux build on
// Android has no zoneinfo and would otherwise silently key by UTC).
//
// today/week/month are calendar-aligned in that zone — today is the local
// date, week is ISO week-to-date (Monday 00:00 local), month is calendar
// month-to-date (1st 00:00 local) — not a rolling 7d/30d window. These three
// are untouched by the quota below (bots keep working).
//
// The quota (Quota, below) is a separate accumulator, PeriodBytes: bytes since
// the last manual or scheduled reset, exact from the reset instant. It is a
// METER ONLY — nothing gates the hotspot, radio, or CPU on it; see
// dashboard.go/cover.go for the display and server.go for the two
// admin-gated writes (/v1/usage/quota, /v1/usage/reset). A schedule
// change/upgrade re-seeds it from the day buckets (see SetQuota), which is
// exact at 00:00 and over-counts the pre-HH:MM slice of the anchor day for a
// non-midnight reset_time — hour-keyed buckets would close that gap if it
// ever matters.
//
// Sampling every usageSampleEvery is a cheap /proc/net/dev read, and totals
// are exact regardless of that cadence: the counters are cumulative, so every
// byte lands in some sample no matter how far apart samples are. The interval
// only bounds two things: (1) attribution slop at a day/week/month or quota
// boundary, closed by bracketWake landing a sample on each side of it, and (2)
// the unsampled tail before a hard reboot. usage.json is persisted at most
// once a minute (see sampleLocked) plus immediately on a counter reset, a
// day-key change, or Flush(), and save() fsyncs the file and its directory
// before returning, so that bound survives a hard reboot or power loss, not
// only a clean one. An unclean shutdown (no Flush) therefore loses at most
// usagePersistEvery of unsaved delta — never a full sample interval's worth
// and never more, since the next delta is always re-derived from the last
// saved last_total. A daemon-only crash (process dies, hardware keeps
// running) loses nothing at all. Note: a runtime timezone change (for example
// travel) isn't picked up until the daemon restarts, since main.go resolves
// time.Local once at startup.

type usageState struct {
	LastTotal uint64            `json:"last_total"` // last observed cumulative bytes
	Days      map[string]uint64 `json:"days"`       // date(YYYY-MM-DD) -> bytes used

	// Period meter: bytes since the last reset (manual or scheduled), exact from
	// the reset instant. Independent of Days/today/week/month, which stay
	// calendar-aligned and untouched by any reset (bots keep working). Zero
	// LastReset means "never reset" (fresh install, or a usage.json from before
	// this field existed).
	PeriodBytes uint64    `json:"period_bytes"`
	LastReset   time.Time `json:"last_reset"`
}

// Quota configures the data-cap meter: a limit to show progress against, and a
// reset schedule for the period meter above. It is a METER ONLY — nothing
// reads LimitBytes to gate the hotspot, radio, or CPU; see dashboard.go /
// cover.go for the display and server.go for the two admin-gated writes.
type Quota struct {
	LimitBytes int64  `json:"limit_bytes"` // 0 = no limit configured
	Period     string `json:"period"`      // daily | weekly | monthly | manual
	ResetTime  string `json:"reset_time"`  // "HH:MM", device-local
	ResetDay   int    `json:"reset_day"`   // 1..28, monthly only (weekly always resets Monday)
}

var resetTimeRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// normalizeQuota fills in defaults for an unconfigured (zero-value) quota and
// clamps anything out of range, returning an error naming what was off. The
// returned value is always safe to use even when err != nil: callers load
// still store the clamped result. HTTP callers turn a non-nil error into a
// 400; config load only logs it — a bad hand-edited config.json must never
// stop the daemon from starting (headless device).
func normalizeQuota(q Quota) (Quota, error) {
	var errs []string
	if q.LimitBytes < 0 {
		errs = append(errs, fmt.Sprintf("limit_bytes %d must be >= 0", q.LimitBytes))
		q.LimitBytes = 0
	}
	if q.Period == "" {
		q.Period = "monthly"
	} else {
		switch q.Period {
		case "daily", "weekly", "monthly", "manual":
		default:
			errs = append(errs, fmt.Sprintf("period %q must be daily, weekly, monthly, or manual", q.Period))
			q.Period = "monthly"
		}
	}
	if q.ResetTime == "" {
		q.ResetTime = "00:00"
	} else if !resetTimeRe.MatchString(q.ResetTime) {
		errs = append(errs, fmt.Sprintf("reset_time %q must match HH:MM", q.ResetTime))
		q.ResetTime = "00:00"
	}
	switch {
	case q.ResetDay == 0:
		q.ResetDay = 1
	case q.ResetDay < 1:
		errs = append(errs, fmt.Sprintf("reset_day %d must be 1..28", q.ResetDay))
		q.ResetDay = 1
	case q.ResetDay > 28:
		errs = append(errs, fmt.Sprintf("reset_day %d must be 1..28", q.ResetDay))
		q.ResetDay = 28
	}
	if len(errs) > 0 {
		return q, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return q, nil
}

// parseHHMM splits a "HH:MM" string already validated by resetTimeRe.
func parseHHMM(s string) (int, int) {
	h, _ := strconv.Atoi(s[0:2])
	m, _ := strconv.Atoi(s[3:5])
	return h, m
}

// mondayOf returns 00:00 local on the Monday of the ISO week containing t.
// Shifting the weekday by +6 mod 7 makes Monday the 0 offset (same trick
// sumWindows uses for week_bytes), so the two stay anchored to the same day.
func mondayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	return midnight.AddDate(0, 0, -int((t.Weekday()+6)%7))
}

// nextReset returns the first reset boundary strictly after `after`, in
// after's own location. Manual quotas never reset on a schedule -> zero time.
func nextReset(after time.Time, q Quota) time.Time {
	if q.Period == "" || q.Period == "manual" {
		return time.Time{}
	}
	loc := after.Location()
	hh, mm := parseHHMM(q.ResetTime)
	switch q.Period {
	case "daily":
		y, mo, d := after.Date()
		cand := time.Date(y, mo, d, hh, mm, 0, 0, loc)
		if cand.After(after) {
			return cand
		}
		return cand.AddDate(0, 0, 1)
	case "weekly":
		mon := mondayOf(after)
		cand := time.Date(mon.Year(), mon.Month(), mon.Day(), hh, mm, 0, 0, loc)
		if cand.After(after) {
			return cand
		}
		return cand.AddDate(0, 0, 7)
	case "monthly":
		day := q.ResetDay
		if day < 1 || day > 28 {
			day = 1
		}
		y, mo, _ := after.Date()
		cand := time.Date(y, mo, day, hh, mm, 0, 0, loc)
		if cand.After(after) {
			return cand
		}
		// day<=28 so this can never spill past the following month.
		return time.Date(y, mo+1, day, hh, mm, 0, 0, loc)
	default:
		return time.Time{}
	}
}

// prevReset returns the latest reset boundary at or before `now`: the same
// construction as nextReset, stepped back one period when the candidate for
// this period hasn't happened yet.
func prevReset(now time.Time, q Quota) time.Time {
	if q.Period == "" || q.Period == "manual" {
		return time.Time{}
	}
	loc := now.Location()
	hh, mm := parseHHMM(q.ResetTime)
	switch q.Period {
	case "daily":
		y, mo, d := now.Date()
		cand := time.Date(y, mo, d, hh, mm, 0, 0, loc)
		if cand.After(now) {
			return cand.AddDate(0, 0, -1)
		}
		return cand
	case "weekly":
		mon := mondayOf(now)
		cand := time.Date(mon.Year(), mon.Month(), mon.Day(), hh, mm, 0, 0, loc)
		if cand.After(now) {
			return cand.AddDate(0, 0, -7)
		}
		return cand
	case "monthly":
		day := q.ResetDay
		if day < 1 || day > 28 {
			day = 1
		}
		y, mo, _ := now.Date()
		cand := time.Date(y, mo, day, hh, mm, 0, 0, loc)
		if cand.After(now) {
			return time.Date(y, mo-1, day, hh, mm, 0, 0, loc)
		}
		return cand
	default:
		return time.Time{}
	}
}

const (
	// /proc/net/dev read; ~4 extra wakeups/min over the daemon's other
	// tickers, noise. Totals are exact regardless of this cadence (the
	// counters are cumulative); the interval only bounds boundary
	// attribution (bracketWake, within 200ms either side) and the
	// unsampled tail before a hard reboot.
	usageSampleEvery  = 15 * time.Second
	usagePersistEvery = 60 * time.Second // usage.json write throttle; reset/day-change/Flush bypass it
	dayLayout         = "2006-01-02"     // zero-padded ISO date: sorts lexically == chronologically
)

type UsageTracker struct {
	mu    sync.Mutex
	path  string
	st    usageState
	now   func() time.Time
	readB func() uint64 // sums current cumulative mobile bytes (rx+tx)
	q     Quota         // quota/schedule; zero value until the server calls SetQuota

	savedDay string    // day key at the last save, for the day-change persist rule
	savedAt  time.Time // time of the last save, for the persist throttle
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

// save writes usage.json through write-tmp, fsync, rename, fsync-directory.
// The rename alone isn't durable on f2fs (the filesystem backing /data on
// this device): a hard reboot or power loss can roll the directory entry
// back to the previous file even though the new one's bytes already reached
// disk, unless the directory's own fsync happens too. This is what makes the
// header comment's persist-loss bound real across an unclean shutdown, not
// only a clean one. Every step is best effort: an error bails without
// touching the sampler, and load() already tolerates a missing or corrupt
// file.
func (u *UsageTracker) save() {
	b, _ := json.MarshalIndent(u.st, "", "  ")
	tmp := u.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return
	}
	if err := f.Close(); err != nil {
		return
	}
	if err := os.Rename(tmp, u.path); err != nil {
		return
	}
	if dir, err := os.Open(filepath.Dir(u.path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
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
	now := u.now()
	// Scheduled period reset first, before the idle check and before the delta
	// below: bytes accrued while the daemon was down across a boundary count
	// toward the NEW period. Conservative for a cap, and it means a crashed
	// daemon can never make the meter look emptier than reality.
	periodReset := u.maybeResetLocked(now)

	cur := u.readB()
	if cur == u.st.LastTotal {
		if periodReset { // reset must survive a crash even with no fresh delta
			today := now.Format(dayLayout)
			u.pruneLocked(today)
			u.save()
			u.savedDay, u.savedAt = today, now
		}
		return // idle: no delta, no flash write otherwise
	}
	reset := cur < u.st.LastTotal
	delta := cur - u.st.LastTotal
	if reset {
		delta = cur // counter restarted (reboot): the new value IS the delta
	}
	u.st.LastTotal = cur
	today := now.Format(dayLayout)
	u.st.Days[today] += delta
	u.st.PeriodBytes += delta

	// Persist on reset (cheap and rare, and it keeps last_total honest across
	// the reboot that just happened), on a period reset, on a new day (so a
	// restart can't hand the unsaved delta to the next day), or at most once a
	// minute. A crash in between loses nothing: the next delta is derived from
	// the saved last_total.
	if reset || periodReset || today != u.savedDay || now.Sub(u.savedAt) >= usagePersistEvery {
		u.pruneLocked(today)
		u.save()
		u.savedDay, u.savedAt = today, now
	}
}

// maybeResetLocked zeroes the period meter and re-anchors last_reset once a
// scheduled boundary has passed since the last reset. Returns whether a reset
// happened, so sampleLocked can bypass its persist throttle exactly like a
// counter reset does.
func (u *UsageTracker) maybeResetLocked(now time.Time) bool {
	due := nextReset(u.st.LastReset, u.q)
	if due.IsZero() || now.Before(due) {
		return false
	}
	anchor := prevReset(now, u.q)
	if anchor.IsZero() { // shouldn't happen (due was non-zero), but never regress to "never reset"
		anchor = now
	}
	u.st.PeriodBytes = 0
	u.st.LastReset = anchor
	return true
}

// SetQuota installs a new quota/schedule. A limit-only change (period, reset
// time, and reset day all unchanged) touches nothing in the meter — a manual
// reset made earlier today must survive a limit edit. A schedule change (or
// the very first call, at startup) re-anchors last_reset to the new anchor
// point ONLY when the tracker's last_reset is older than it — so an
// already-current manual reset is never silently undone — and day-granular
// re-seeds period_bytes from the Days buckets so an upgrade/schedule-change
// doesn't zero out a period that was already partway through.
func (u *UsageTracker) SetQuota(q Quota) {
	u.mu.Lock()
	defer u.mu.Unlock()
	changed := u.q == (Quota{}) || q.Period != u.q.Period || q.ResetTime != u.q.ResetTime || q.ResetDay != u.q.ResetDay
	if changed {
		now := u.now()
		if q.Period == "manual" {
			if u.st.LastReset.IsZero() {
				u.st.LastReset, u.st.PeriodBytes = now, 0
			}
		} else if anchor := prevReset(now, q); u.st.LastReset.Before(anchor) {
			u.st.LastReset = anchor
			var sum uint64
			ad := anchor.Format(dayLayout)
			for d, b := range u.st.Days {
				if d >= ad {
					sum += b
				}
			}
			// ponytail: exact at 00:00, but over-counts the pre-HH:MM slice of the
			// anchor day for a non-midnight reset_time (day-granular buckets can't
			// see inside a day) — hour-keyed buckets if that ever needs to be exact.
			u.st.PeriodBytes = sum
		}
		u.save()
	}
	u.q = q
}

// Reset zeroes the period meter right now (the manual "Reset data usage"
// action). today/week/month buckets are untouched.
func (u *UsageTracker) Reset() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.st.PeriodBytes = 0
	u.st.LastReset = u.now()
	u.save()
}

// NextWake shortens the sampler's usual wait so a scheduled reset gets a
// sample on each side of its boundary, the same bracketWake trick
// nextUsageWake plays for local midnight.
func (u *UsageTracker) NextWake(now time.Time) time.Duration {
	u.mu.Lock()
	q, lastReset := u.q, u.st.LastReset
	u.mu.Unlock()
	w := nextUsageWake(now)
	if due := nextReset(lastReset, q); !due.IsZero() {
		if dw := bracketWake(due.Sub(now), usageSampleEvery); dw < w {
			return dw
		}
	}
	return w
}

// Flush forces sampling and an immediate save, bypassing the persist
// throttle. Used before a device reboot so only the ~2s between Flush and the
// actual reboot is at risk, instead of up to usagePersistEvery of unsaved delta.
func (u *UsageTracker) Flush() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sampleLocked()
	u.save()
}

// pruneLocked drops buckets older than ~40 days (still covers any
// month-to-date window). The cutoff is a lexical day-key compare, not
// time.Parse: dayLayout is zero-padded ISO, so string order == date order,
// and comparing against a parsed time.Time would disagree once `now` carries
// a non-UTC location. today is excluded explicitly so a clock hiccup can
// never prune the bucket sampleLocked just wrote to.
func (u *UsageTracker) pruneLocked(today string) {
	cutoff := u.now().AddDate(0, 0, -40).Format(dayLayout)
	for d := range u.st.Days {
		if d != today && d < cutoff {
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

	// Period meter: bytes since the last reset (manual or scheduled) against the
	// configured limit. A meter only — see Quota's doc comment.
	PeriodBytes uint64 `json:"period_bytes"`
	PeriodHuman string `json:"period_human"`
	LimitBytes  int64  `json:"limit_bytes"` // 0 = no limit configured
	Period      string `json:"period"`
	ResetTime   string `json:"reset_time"`
	ResetDay    int    `json:"reset_day"`
	LastReset   string `json:"last_reset"` // RFC3339, or "" if never reset
	NextReset   string `json:"next_reset"` // RFC3339, or "" for a manual period
}

func (u *UsageTracker) Report() UsageReport {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sampleLocked() // refresh before reporting
	r := sumWindows(u.st.Days, u.now())
	r.PeriodBytes = u.st.PeriodBytes
	r.PeriodHuman = humanBytes(u.st.PeriodBytes)
	r.LimitBytes = u.q.LimitBytes
	r.Period = u.q.Period
	r.ResetTime = u.q.ResetTime
	r.ResetDay = u.q.ResetDay
	if !u.st.LastReset.IsZero() {
		r.LastReset = u.st.LastReset.Format(time.RFC3339)
	}
	if next := nextReset(u.st.LastReset, u.q); !next.IsZero() {
		r.NextReset = next.Format(time.RFC3339)
	}
	return r
}

// sumWindows sums calendar-aligned windows: today (local date), ISO
// week-to-date (Monday 00:00 local), calendar month-to-date (1st 00:00
// local). Keys are compared lexically, never parsed: dayLayout's zero-padded
// ISO format sorts identically to date order across month/year boundaries
// ("2025-12-31" < "2026-01-01"), so no time.Parse (and no UTC/local zone
// mismatch) is needed here.
func sumWindows(days map[string]uint64, now time.Time) UsageReport {
	var today, week, month uint64
	td := now.Format(dayLayout)
	// time.Weekday: Sunday=0..Saturday=6. Shifting by +6 mod 7 makes Monday
	// the 0 offset, so subtracting it from `now` lands on this week's Monday.
	ws := now.AddDate(0, 0, -int((now.Weekday()+6)%7)).Format(dayLayout)
	ms := now.Format("2006-01") + "-01"
	for d, b := range days {
		if d == td {
			today += b
		}
		if d >= ws {
			week += b
		}
		if d >= ms {
			month += b
		}
	}
	return UsageReport{
		TodayBytes: today, WeekBytes: week, MonthBytes: month,
		TodayHuman: humanBytes(today), WeekHuman: humanBytes(week), MonthHuman: humanBytes(month),
		Source: "rmnet-delta",
	}
}

// bracketWake picks the sampler's next sleep so a wake lands close to a
// boundary `until` away, given the sampler's normal cadence `every`: 200ms
// before the boundary while that's still inside one cadence (closing the
// bucket on the near side), or 200ms after it once the boundary is imminent
// or already past. A negative or very late `until` therefore returns 0 or
// less, so time.Sleep returns immediately — the pre-existing catch-up
// behavior. Two boundary samples straddle it: the first call lands just
// before, and the caller's next call — computing `until` fresh from the new
// `now` — lands just after, since by then `until` has fallen into the
// second branch.
const bracketMargin = 200 * time.Millisecond

func bracketWake(until, every time.Duration) time.Duration {
	switch {
	case until > bracketMargin && until-bracketMargin < every:
		return until - bracketMargin
	case until <= bracketMargin:
		return until + bracketMargin
	default:
		return every
	}
}

// nextUsageWake returns how long the background sampler should sleep before
// its next Sample() call: usageSampleEvery, bracketed toward local midnight
// so a sample lands on each side of the day rollover (day-boundary
// attribution, not misattribution) instead of up to usageSampleEvery late.
func nextUsageWake(now time.Time) time.Duration {
	y, m, d := now.Date()
	midnight := time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
	return bracketWake(midnight.Sub(now), usageSampleEvery)
}

// humanBytes formats bytes in SI decimal units (1 GB = 1000 MB = 10^9 bytes),
// matching carrier data meters and Android's own Formatter — not the 1024-
// based GiB/MiB a filesystem tool would use. The base below is the one knob:
// flip it to 1024 (and drop the "B" suffixes back to "iB") if a carrier ever
// bills in binary units instead.
func humanBytes(b uint64) string {
	const u = 1000
	if b < u {
		return strconv.FormatUint(b, 10) + " B"
	}
	div, exp := uint64(u), 0
	for n := b / u; n >= u; n /= u {
		div *= u
		exp++
	}
	val := float64(b) / float64(div)
	return strconv.FormatFloat(val, 'f', 2, 64) + " " + string("KMGTPE"[exp]) + "B"
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
