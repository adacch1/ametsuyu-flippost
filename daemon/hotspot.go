package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SSID-whitelist hotspot auto-toggle: when any whitelisted SSID is visible in
// a Wi-Fi scan (e.g. home network in range), the hotspot is stopped; when no
// whitelisted SSID has been seen for two consecutive scans, it is started
// again. Scanning uses the root WifiScan helper (see helper/tether/WifiScan.java)
// because Samsung blocks the public scan API while the SoftAP is up. Scans need
// location services ON — when off, the loop pauses visibly instead of toggling.

// Adaptive, device-comfortable scan cadence: scan often while the hotspot is
// OFF (react quickly to leaving a whitelisted network — no clients to disrupt),
// and back off while it's ON (a scan is off-channel and dips client throughput;
// we only need to catch arriving at a whitelisted network, which isn't urgent).
const (
	hotspotScanIdle   = 60 * time.Second // hotspot off: responsive
	hotspotScanActive = 3 * time.Minute  // hotspot on: gentle on clients
)

// hotspotOverridePath persists the timed force-on deadline (see SetOverride)
// across a daemon restart or reboot. Runtime state, not owner config: it
// lives beside usage.json, never in config.json (which is schema-validated).
// Tests point it at a t.TempDir() file.
var hotspotOverridePath = "/data/adb/zflip5-modem/hotspot_override.json"

// hotspotOverrideMaxH bounds SetOverride's hours parameter so a forgotten
// override can't pin the hotspot up indefinitely.
const hotspotOverrideMaxH = 24

// moduleDir locates the installed Magisk module (for the helper jar). The
// watchdog exports ZF5_MODDIR; the fallback is the module's install path.
func moduleDir() string {
	if d := os.Getenv("ZF5_MODDIR"); d != "" {
		return d
	}
	return "/data/adb/modules/zflip5_modem"
}

type HotspotStatus struct {
	Active       bool       `json:"active"`
	Auto         bool       `json:"auto"`                    // whitelist non-empty
	Paused       string     `json:"paused,omitempty"`        // location_off | scan_failed
	PausedDetail string     `json:"paused_detail,omitempty"` // helper's RESULT=FAIL reason (scan_failed only)
	Whitelist    []string   `json:"whitelist"`
	Matched      []string   `json:"matched"`               // whitelisted SSIDs seen in last scan
	Nearby       []NearbyAP `json:"nearby"`                // last scanned networks, each flagged whitelisted
	NearbyScan   string     `json:"nearby_scan,omitempty"` // RFC3339 of the scan that produced Nearby
	APCount      int        `json:"ap_count"`
	LastScan     string     `json:"last_scan,omitempty"` // RFC3339
	LastAction   string     `json:"last_action,omitempty"`
	// OverrideUntil/OverrideLeftS are present only while SetOverride's timed
	// force-on is active (see step()); the whitelist stop decision is
	// suppressed until this deadline.
	OverrideUntil string `json:"override_until,omitempty"` // RFC3339
	OverrideLeftS int    `json:"override_left_s,omitempty"`
}

// ScanAP is one scanned network (dedup'd by SSID, strongest RSSI kept).
type ScanAP struct {
	SSID string
	RSSI int
}

// NearbyAP is a scanned network as reported to the dashboard.
type NearbyAP struct {
	SSID        string `json:"ssid"`
	RSSI        int    `json:"rssi"`
	Whitelisted bool   `json:"whitelisted"`
}

type HotspotController struct {
	mu            sync.Mutex
	whitelist     []string
	misses        int      // consecutive scans with no whitelisted SSID seen
	gen           uint     // bumped on every SetWhitelist; guards the miss-counter write in step()
	lastNearby    []ScanAP // last scan result (from the auto loop OR a manual Scan)
	lastScanAt    string   // RFC3339 of lastNearby
	status        HotspotStatus
	overrideUntil time.Time // zero when no forced-on override is active

	// Seams: tests swap these for stubs; NewHotspotController wires the real
	// probes/actuators (same pattern as UsageTracker.now/readB in usage.go).
	now   func() time.Time
	locOn func() bool
	scan  func() ([]ScanAP, bool, string)
	apUp  func() bool
	start func() bool
	stop  func() bool
}

func NewHotspotController(whitelist []string) *HotspotController {
	h := &HotspotController{
		whitelist: whitelist,
		now:       time.Now,
		locOn:     locationEnabled,
		scan:      scanAPs,
		apUp:      hotspotActive,
		start:     startHotspot,
		stop:      stopHotspot,
	}
	h.loadOverride()
	return h
}

// loadOverride restores a persisted force-on deadline so it survives a daemon
// restart or a reboot mid-window. Absent, unparseable, or already-past
// deadlines leave the override inactive; only a present-but-corrupt file logs
// (a merely-expired one is the normal end state and not worth a log line).
func (h *HotspotController) loadOverride() {
	b, err := os.ReadFile(hotspotOverridePath)
	if err != nil {
		return
	}
	var v struct {
		Until time.Time `json:"until"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		log.Printf("hotspot override: bad override file %s, ignoring: %v", hotspotOverridePath, err)
		return
	}
	if v.Until.After(h.now()) {
		h.overrideUntil = v.Until
	}
}

// persistOverride writes the override deadline via the shared atomic
// tmp+rename writer (config.go's writeConfigAtomic) rather than duplicating it.
func (h *HotspotController) persistOverride(until time.Time) error {
	b, err := json.Marshal(struct {
		Until time.Time `json:"until"`
	}{until})
	if err != nil {
		return err
	}
	return writeConfigAtomic(hotspotOverridePath, b)
}

// SetOverride forces the hotspot on until now+d (d <= 0 cancels), persists the
// deadline so it survives a daemon restart or reboot, and — when arming —
// brings the AP up right away instead of waiting for the next tick.
func (h *HotspotController) SetOverride(d time.Duration) HotspotStatus {
	var until time.Time
	if d > 0 {
		until = h.now().Add(d)
	}
	h.mu.Lock()
	h.overrideUntil = until
	h.mu.Unlock()

	if until.IsZero() {
		if err := os.Remove(hotspotOverridePath); err != nil && !os.IsNotExist(err) {
			log.Printf("hotspot override: remove failed: %v", err)
		}
		log.Printf("hotspot override: cancelled")
	} else {
		if err := h.persistOverride(until); err != nil {
			log.Printf("hotspot override: persist failed: %v", err)
		}
		log.Printf("hotspot override: forced on until %s", until.Format(time.RFC3339))
		if !h.apUp() {
			action := "start failed (forced on)"
			if h.start() {
				action = "started: forced on"
			}
			h.mu.Lock()
			h.status.LastAction = action
			h.mu.Unlock()
			log.Printf("hotspot auto: %s", action)
		}
	}
	return h.Status()
}

// OverrideLeft reports time remaining on a forced-on override, 0 when none is
// active.
func (h *HotspotController) OverrideLeft() time.Duration {
	h.mu.Lock()
	until := h.overrideUntil
	h.mu.Unlock()
	if left := until.Sub(h.now()); left > 0 {
		return left
	}
	return 0
}

func (h *HotspotController) Whitelist() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.whitelist...)
}

// LastSeenSSIDs returns the SSIDs from the most recent scan (whitelist loop or a
// manual/preset scan). Feeds the preset auto-switch without triggering a scan.
func (h *HotspotController) LastSeenSSIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return apNames(h.lastNearby)
}

func (h *HotspotController) SetWhitelist(ssids []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.whitelist = append([]string(nil), ssids...)
	h.misses = 0
	h.gen++ // invalidate any in-flight step() miss-counter write
}

// Status returns the last loop snapshot plus the live interface state.
func (h *HotspotController) Status() HotspotStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.status
	st.Active = h.apUp()
	st.Auto = len(h.whitelist) > 0
	st.Whitelist = append([]string{}, h.whitelist...)
	if !st.Auto { // feature off: drop stale scan facts, keep only the last action
		st.Paused, st.PausedDetail, st.Matched, st.APCount, st.LastScan = "", "", nil, 0, ""
	}
	if st.Matched == nil {
		st.Matched = []string{}
	}
	// Override fields ride independently of Auto: a force-on can run with an
	// empty whitelist too (see step()).
	if left := h.overrideUntil.Sub(h.now()); left > 0 {
		st.OverrideUntil = h.overrideUntil.Format(time.RFC3339)
		secs := int64(left / time.Second)
		if left%time.Second != 0 {
			secs++ // ceil: 1ms left should still read as "1s", not "0s"
		}
		st.OverrideLeftS = int(secs)
	}
	// Nearby survives the auto-off drop above: it is populated by manual scans
	// too, so the owner can see what's in range and pick what to whitelist even
	// with the auto-toggle off. Each entry is flagged against the current list.
	wlset := map[string]bool{}
	for _, wname := range h.whitelist {
		wlset[wname] = true
	}
	st.Nearby = make([]NearbyAP, 0, len(h.lastNearby))
	for _, a := range h.lastNearby {
		st.Nearby = append(st.Nearby, NearbyAP{SSID: a.SSID, RSSI: a.RSSI, Whitelisted: wlset[a.SSID]})
	}
	st.NearbyScan = h.lastScanAt
	return st
}

// Scan runs an immediate scan-only refresh of the nearby list WITHOUT toggling
// the hotspot — the Settings "Scan now" button. Returns a paused reason
// ("location_off" / "scan_failed") when it couldn't scan, else "".
func (h *HotspotController) Scan(now string) string {
	if !locationEnabled() {
		return "location_off"
	}
	aps, ok, _ := scanAPs()
	if !ok {
		return "scan_failed"
	}
	h.mu.Lock()
	h.lastNearby = aps
	h.lastScanAt = now
	h.mu.Unlock()
	return ""
}

// decideHotspot is the pure toggle policy. matched = whitelisted SSIDs seen in
// this scan; misses = consecutive all-miss scans BEFORE this one. Turning off
// is immediate (positive evidence); turning on waits for a second consecutive
// miss so one flaky scan can't bounce the hotspot.
//
// Auto-start is NOT app-thermal-gated: the hotspot is the modem's primary
// function, the app-level heat source is CPU compute (throttled by the eco CPU
// policy) not the Wi-Fi radio, and Samsung's own thermal mitigation shuts the
// AP at genuinely dangerous temperatures regardless. Gating it on the 46 °C
// app limit just left the modem unable to share internet whenever it ran warm.
func decideHotspot(matched int, active bool, misses int) (action string, newMisses int) {
	if matched > 0 {
		if active {
			return "stop", 0
		}
		return "", 0
	}
	misses++
	if misses >= 2 && !active {
		return "start", misses
	}
	return "", misses
}

// parseScanAPs extracts networks from WifiScan helper output. Lines look like
// "AP\t<ssid>\t<bssid>\t<rssi>"; the run is only trusted if RESULT=OK arrived.
// Deduplicated by SSID (strongest RSSI wins) and sorted strongest-first.
func parseScanAPs(out string) ([]ScanAP, bool) {
	best := map[string]int{}
	var order []string
	ok := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "RESULT=OK") {
			ok = true
			continue
		}
		if !strings.HasPrefix(line, "AP\t") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 4 || f[1] == "" { // skip hidden SSIDs and malformed lines
			continue
		}
		rssi, _ := strconv.Atoi(f[3])
		if prev, dup := best[f[1]]; !dup {
			best[f[1]] = rssi
			order = append(order, f[1])
		} else if rssi > prev {
			best[f[1]] = rssi
		}
	}
	aps := make([]ScanAP, 0, len(order))
	for _, s := range order {
		aps = append(aps, ScanAP{SSID: s, RSSI: best[s]})
	}
	sort.SliceStable(aps, func(i, j int) bool { return aps[i].RSSI > aps[j].RSSI })
	return aps, ok
}

func apNames(aps []ScanAP) []string {
	names := make([]string, len(aps))
	for i, a := range aps {
		names[i] = a.SSID
	}
	return names
}

func matchWhitelist(seen, whitelist []string) []string {
	set := map[string]bool{}
	for _, s := range seen {
		set[s] = true
	}
	matched := []string{}
	for _, w := range whitelist {
		if set[w] {
			matched = append(matched, w)
		}
	}
	return matched
}

// hotspotActive reports whether the SoftAP interface is up. The interface is
// removed entirely when tethering stops, so presence+UP is the ground truth.
func hotspotActive() bool {
	return strings.Contains(runCmd("ip", "link", "show", softApIface), "state UP")
}

func locationEnabled() bool {
	return strings.TrimSpace(runCmd("settings", "get", "secure", "location_mode")) != "0"
}

// runHelper executes a class from the module's helper jar via app_process.
func runHelper(class string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	argv := append([]string{"/system/bin", class}, args...)
	cmd := exec.CommandContext(ctx, "app_process", argv...)
	cmd.Env = append(os.Environ(), "CLASSPATH="+moduleDir()+"/tether/tether.jar")
	out, _ := cmd.Output()
	return string(out)
}

// scanAPs runs the WifiScan helper. On failure it also self-heals the one
// scan precondition the daemon owns (see rearmScanning) and reports why
// (helper's RESULT=FAIL reason, or "no output") so the dashboard can show
// something more actionable than "paused".
func scanAPs() ([]ScanAP, bool, string) {
	out := runHelper("com.zflip5.tether.WifiScan")
	aps, ok := parseScanAPs(out)
	if !ok {
		rearmScanning()
		return nil, false, scanFailReason(out)
	}
	return aps, true, ""
}

// scanFailReason extracts the text after the helper's "RESULT=FAIL reason="
// marker, defaulting to "no output" when the helper produced nothing to
// explain the failure from.
func scanFailReason(out string) string {
	const marker = "RESULT=FAIL reason="
	idx := strings.Index(out, marker)
	if idx < 0 {
		return "no output"
	}
	rest := out[idx+len(marker):]
	if nl := strings.IndexAny(rest, "\r\n"); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest)
}

// rearmScanning re-asserts the ONE scan precondition the daemon owns:
// Android's wifi_scan_always_enabled setting (service.sh sets it once at
// boot; nothing re-asserts it after that). A no-op on a healthy device.
// Deliberately does NOT touch wifi_on or Location: bringing up a 5GHz STA
// would force the SoftAP off its HE80 band onto 2.4GHz (DBS), and Location is
// the owner's own switch.
func rearmScanning() {
	if strings.TrimSpace(runCmd("settings", "get", "global", "wifi_scan_always_enabled")) == "1" {
		return
	}
	runCmd("settings", "put", "global", "wifi_scan_always_enabled", "1")
	log.Printf("hotspot auto: wifi_scan_always_enabled was off — re-armed (wifi_on untouched)")
}

// SoftAP bring-up is asynchronous: the framework answers RESULT=STARTED while
// swlan0 is still coming up, so a status read taken at that moment reports a
// hotspot that is on its way as one that failed — the "did not come up, retry"
// the dashboard showed over a working AP. Both toggles wait for the interface
// to agree before returning, which fixes every caller at once (the HTTP
// toggle, the airplane cycle's restore, and the auto-toggle loop) instead of
// teaching each one to re-poll. service.sh's boot path already assumed this
// delay with its sleep-10 retries.
const (
	hotspotSettle   = 10 * time.Second
	hotspotPollWait = 400 * time.Millisecond
)

// waitFor polls until probe reports want or the budget runs out, and returns
// the LAST observation either way — so callers report what the interface is
// actually doing, not what it was asked to do.
func waitFor(want bool, probe func() bool, budget, interval time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		got := probe()
		if got == want || !time.Now().Before(deadline) {
			return got
		}
		time.Sleep(interval)
	}
}

// startHotspot reports whether the AP is UP, not merely whether the command was
// accepted. code=5 is the framework's "already active".
func startHotspot() bool {
	out := runHelper("com.zflip5.tether.TetherStart")
	if !strings.Contains(out, "RESULT=STARTED") && !strings.Contains(out, "code=5") {
		return false
	}
	return waitFor(true, hotspotActive, hotspotSettle, hotspotPollWait)
}

// stopHotspot reports whether the AP is actually down.
func stopHotspot() bool {
	if !strings.Contains(runHelper("com.zflip5.tether.TetherStart", "stop"), "RESULT=STOPPED") {
		return false
	}
	return !waitFor(false, hotspotActive, hotspotSettle, hotspotPollWait)
}

// step runs one scan/decide/act cycle. A timed override (SetOverride) forces
// the hotspot on and suppresses the whitelist stop decision until it expires;
// scanning still runs during an override so the nearby list and the preset
// auto-switch keep working, and misses is left untouched so the debounce
// picks up where it left off once the override ends. Logs only on
// transitions (pause/resume, each start/stop taken or failed) — a steadily
// paused or steadily forced-on loop adds zero log lines per tick.
func (h *HotspotController) step() {
	h.mu.Lock()
	wl := append([]string(nil), h.whitelist...)
	misses := h.misses
	gen := h.gen
	until := h.overrideUntil
	h.mu.Unlock()
	forced := h.now().Before(until)
	if len(wl) == 0 && !forced {
		return // feature off; Status() reports auto=false live
	}

	st := HotspotStatus{}
	acted := ""
	defer func() {
		h.mu.Lock()
		prev := h.status
		if st.LastAction == "" {
			st.LastAction = prev.LastAction // keep last real action visible
		}
		h.status = st
		h.mu.Unlock()
		if st.Paused != prev.Paused {
			switch {
			case st.Paused == "":
				log.Printf("hotspot auto: scanning resumed")
			case st.PausedDetail != "":
				log.Printf("hotspot auto: scanning paused: %s (%s)", st.Paused, st.PausedDetail)
			default:
				log.Printf("hotspot auto: scanning paused: %s", st.Paused)
			}
		}
		if acted != "" {
			log.Printf("hotspot auto: %s", acted)
		}
	}()

	if forced && !h.apUp() {
		st.LastAction = "start failed (forced on)"
		if h.start() {
			st.LastAction = "started: forced on"
		}
		acted = st.LastAction
	}
	if len(wl) == 0 {
		return // forced on, no whitelist configured: nothing to scan for
	}

	st.LastScan = h.now().Format(time.RFC3339)
	if !h.locOn() {
		st.Paused = "location_off"
		return
	}
	aps, ok, reason := h.scan()
	if !ok {
		st.Paused, st.PausedDetail = "scan_failed", reason
		return
	}
	seen := apNames(aps)
	st.APCount = len(aps)
	st.Matched = matchWhitelist(seen, wl)

	h.mu.Lock()
	h.lastNearby = aps // feed the Settings "Nearby networks" list from the auto loop too
	h.lastScanAt = st.LastScan
	h.mu.Unlock()

	if forced {
		return // whitelist decision suppressed; normal logic resumes at the deadline
	}

	active := h.apUp()
	action, newMisses := decideHotspot(len(st.Matched), active, misses)

	h.mu.Lock()
	if h.gen == gen { // don't clobber a SetWhitelist reset that landed mid-scan
		h.misses = newMisses
	}
	h.mu.Unlock()

	switch action {
	case "stop":
		// Re-check right before acting: an override armed while this scan was
		// in flight must win, so a start doesn't immediately follow this stop.
		h.mu.Lock()
		stillForced := h.now().Before(h.overrideUntil)
		h.mu.Unlock()
		if stillForced {
			return
		}
		if h.stop() {
			st.LastAction = "stopped: saw " + strings.Join(st.Matched, ", ")
		} else {
			st.LastAction = "stop failed"
		}
		acted = st.LastAction
	case "start":
		if h.start() {
			st.LastAction = "started: whitelist not in range"
		} else {
			st.LastAction = "start failed"
		}
		acted = st.LastAction
	}
}
