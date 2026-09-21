package main

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"sort"
	"sync"
	"time"
)

// Temperature history: minute samples of the DISPLAY-path reader
// (thermalSmoother.median — the same 14 s median /v1/status serves as
// temp_max_c), folded into hour/day averages for the dashboard's history
// chart. Mirrors usage.go's persisted-bucket pattern (atomic JSON write,
// per-period buckets keyed by local time) but for temperature instead of
// mobile bytes, and persists through config.go's writeConfigAtomic
// (tmp+rename, no fsync) rather than usage.go's fsync save() — this is
// display history, not a meter, so losing a few minutes of samples to a
// crash is acceptable.
//
// AGGREGATION METHOD (kept in sync with TempHistoryReport.Method and the
// OpenAPI ThermalHistory description): every bucket is the sample-weighted
// mean of the 1-minute samples that fell inside it in device-local time:
// hour = sum(c)/n over that hour's samples, day = sum(c)/n over that day's
// samples — never a mean of the hour means, so a partial hour after a
// restart doesn't get a full hour's weight. Each minute sample is the 14 s
// display median (thermalSmoother.median, the same number /v1/status serves
// as temp_max_c) rounded to 0.1 C; the raw gate reader is never involved. N
// counts real samples, so a gap while the daemon was down is simply absent,
// not zero-filled.
//
// Retention: 24h of minute samples, 7d of hour buckets, 40d of day buckets
// (usage.go's horizon, so the JSON file doesn't grow unbounded). Persisted
// every 5 minutes, plus Flush() before an API-triggered device reboot; a
// crash between persists loses at most that much.
const tempHistoryMethod = "Every bucket is the sample-weighted mean of the 1-minute samples that fell inside it in device-local time: hour = sum(c)/n over that hour's samples, day = sum(c)/n over that day's samples (never a mean of the hour means, so a partial hour after a restart does not get a full hour's weight). Each minute sample is the 14 s display median (thermalSmoother.median, the same number /v1/status serves as temp_max_c) rounded to 0.1 C; the raw gate reader is never involved. N counts real samples, so a gap while the daemon was down is simply absent."

const (
	tempSampleEvery  = time.Minute
	tempPersistEvery = 5 * time.Minute
	tempKeepMinutes  = 24 * 60
	tempKeepHours    = 7 * 24
	tempKeepDays     = 40
	hourLayout       = "2006-01-02T15" // local, zero-padded: sorts lexically == chronologically, like dayLayout
)

// tempsPath is where minute/hour/day temperature buckets persist, beside
// usage.json and hotspot_override.json. Tests override it to a temp file.
var tempsPath = "/data/adb/zflip5-modem/temps.json"

type tempBucket struct {
	Sum float64 `json:"sum"`
	N   int     `json:"n"`
}

type tempState struct {
	Minutes [][2]float64          `json:"minutes"` // [unixSec, degC], oldest first
	Hours   map[string]tempBucket `json:"hours"`
	Days    map[string]tempBucket `json:"days"`
}

// TempHistory samples a display-path reader once a minute and folds the
// result into persisted minute/hour/day buckets. Zero value isn't usable;
// construct with NewTempHistory.
type TempHistory struct {
	mu   sync.Mutex
	path string
	st   tempState
	now  func() time.Time
	// read is the SOLE temperature source: wired to s.smooth.median (display
	// path) in NewServer. Never the raw safety-gate collector — the gate must
	// never be re-sourced from this history.
	read    func() (float64, bool)
	savedAt time.Time
}

// NewTempHistory loads path tolerantly (missing or corrupt -> empty state;
// a corrupt file logs once, same as hotspot.go's loadOverride) and wires read
// as the sole temperature source.
func NewTempHistory(path string, read func() (float64, bool)) *TempHistory {
	h := &TempHistory{path: path, now: time.Now, read: read}
	h.st.Hours = map[string]tempBucket{}
	h.st.Days = map[string]tempBucket{}
	h.load()
	return h
}

func (h *TempHistory) load() {
	b, err := os.ReadFile(h.path)
	if err != nil {
		return // missing file: fresh install, nothing to log
	}
	if err := json.Unmarshal(b, &h.st); err != nil {
		log.Printf("temps: bad history file %s, starting fresh: %v", h.path, err)
		h.st = tempState{}
	}
	if h.st.Hours == nil {
		h.st.Hours = map[string]tempBucket{}
	}
	if h.st.Days == nil {
		h.st.Days = map[string]tempBucket{}
	}
}

// Sample reads the display-path median once and folds it into the current
// minute/hour/day buckets, persisting on the tempPersistEvery throttle.
func (h *TempHistory) Sample() {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.read()
	if !ok || c <= 0 {
		// Smoother empty (first ~2s after daemon start) or a degraded sweep:
		// record nothing rather than a bogus zero.
		return
	}
	c = math.Round(c*10) / 10
	now := h.now()
	h.st.Minutes = append(h.st.Minutes, [2]float64{float64(now.Unix()), c})
	if len(h.st.Minutes) > tempKeepMinutes {
		h.st.Minutes = h.st.Minutes[len(h.st.Minutes)-tempKeepMinutes:]
	}
	addTempSample(h.st.Hours, now.Format(hourLayout), c)
	addTempSample(h.st.Days, now.Format(dayLayout), c)

	if now.Sub(h.savedAt) >= tempPersistEvery {
		h.pruneLocked(now)
		h.saveLocked()
		h.savedAt = now
	}
}

func addTempSample(m map[string]tempBucket, key string, c float64) {
	b := m[key]
	b.Sum += c
	b.N++
	m[key] = b
}

// Flush prunes and saves immediately, bypassing the persist throttle. Called
// before an API-triggered device reboot so at most the gap between Flush and
// the actual reboot is at risk, not up to tempPersistEvery.
func (h *TempHistory) Flush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	h.pruneLocked(now)
	h.saveLocked()
	h.savedAt = now
}

// run samples once a minute, forever. Started from ListenAndServe only
// (never NewServer, which every unit test calls) — same rule thermalSmoother
// already follows.
func (h *TempHistory) run() {
	tick := time.NewTicker(tempSampleEvery)
	defer tick.Stop()
	for range tick.C {
		h.Sample()
	}
}

// pruneLocked drops hour/day buckets past their retention horizon. Lexical
// key compare, the same trick usage.go's pruneLocked uses: hourLayout and
// dayLayout are zero-padded, so string order == chronological order. The
// cutoff is strictly before now's own key, so the current hour/day is never
// pruned even right at a boundary.
func (h *TempHistory) pruneLocked(now time.Time) {
	hourCutoff := now.Add(-tempKeepHours * time.Hour).Format(hourLayout)
	for k := range h.st.Hours {
		if k < hourCutoff {
			delete(h.st.Hours, k)
		}
	}
	dayCutoff := now.AddDate(0, 0, -tempKeepDays).Format(dayLayout)
	for k := range h.st.Days {
		if k < dayCutoff {
			delete(h.st.Days, k)
		}
	}
}

// saveLocked writes compact JSON (not MarshalIndent: at full retention 1440
// minute samples run ~26 KB, more than double that indented, and this is
// written every 5 minutes) via config.go's atomic tmp+rename writer. No
// fsync copy of usage.go's save() — display history, not a meter.
func (h *TempHistory) saveLocked() {
	b, err := json.Marshal(h.st)
	if err != nil {
		log.Printf("temps: marshal failed: %v", err)
		return
	}
	if err := writeConfigAtomic(h.path, b); err != nil {
		log.Printf("temps: persist failed: %v", err)
	}
}

type tempPoint struct {
	T string  `json:"t"`
	C float64 `json:"c"`
	N int     `json:"n"`
}

// TempHistoryReport is served by GET /v1/thermal/history.
type TempHistoryReport struct {
	Method  string       `json:"method"`
	Minutes [][2]float64 `json:"minutes"`
	Hours   []tempPoint  `json:"hours"`
	Days    []tempPoint  `json:"days"`
}

// Report renders the current buckets, hours/days sorted ascending by key. All
// slices are non-nil ([] not null) even when empty.
func (h *TempHistory) Report() TempHistoryReport {
	h.mu.Lock()
	defer h.mu.Unlock()
	minutes := append([][2]float64{}, h.st.Minutes...)
	return TempHistoryReport{
		Method:  tempHistoryMethod,
		Minutes: minutes,
		Hours:   tempBucketPoints(h.st.Hours),
		Days:    tempBucketPoints(h.st.Days),
	}
}

func tempBucketPoints(m map[string]tempBucket) []tempPoint {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pts := make([]tempPoint, len(keys))
	for i, k := range keys {
		b := m[k]
		c := 0.0
		if b.N > 0 {
			c = math.Round(b.Sum/float64(b.N)*10) / 10
		}
		pts[i] = tempPoint{T: k, C: c, N: b.N}
	}
	return pts
}
