package main

import (
	"context"
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

// moduleDir locates the installed Magisk module (for the helper jar). The
// watchdog exports ZF5_MODDIR; the fallback is the module's install path.
func moduleDir() string {
	if d := os.Getenv("ZF5_MODDIR"); d != "" {
		return d
	}
	return "/data/adb/modules/zflip5_modem"
}

type HotspotStatus struct {
	Active     bool       `json:"active"`
	Auto       bool       `json:"auto"`             // whitelist non-empty
	Paused     string     `json:"paused,omitempty"` // location_off | scan_failed
	Whitelist  []string   `json:"whitelist"`
	Matched    []string   `json:"matched"`               // whitelisted SSIDs seen in last scan
	Nearby     []NearbyAP `json:"nearby"`                // last scanned networks, each flagged whitelisted
	NearbyScan string     `json:"nearby_scan,omitempty"` // RFC3339 of the scan that produced Nearby
	APCount    int        `json:"ap_count"`
	LastScan   string     `json:"last_scan,omitempty"` // RFC3339
	LastAction string     `json:"last_action,omitempty"`
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
	mu         sync.Mutex
	whitelist  []string
	misses     int      // consecutive scans with no whitelisted SSID seen
	gen        uint     // bumped on every SetWhitelist; guards the miss-counter write in step()
	lastNearby []ScanAP // last scan result (from the auto loop OR a manual Scan)
	lastScanAt string   // RFC3339 of lastNearby
	status     HotspotStatus
}

func NewHotspotController(whitelist []string) *HotspotController {
	return &HotspotController{whitelist: whitelist}
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
	st.Active = hotspotActive()
	st.Auto = len(h.whitelist) > 0
	st.Whitelist = append([]string{}, h.whitelist...)
	if !st.Auto { // feature off: drop stale scan facts, keep only the last action
		st.Paused, st.Matched, st.APCount, st.LastScan = "", nil, 0, ""
	}
	if st.Matched == nil {
		st.Matched = []string{}
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
	aps, ok := scanAPs()
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

func scanAPs() ([]ScanAP, bool) {
	return parseScanAPs(runHelper("com.zflip5.tether.WifiScan"))
}

func startHotspot() bool {
	out := runHelper("com.zflip5.tether.TetherStart")
	return strings.Contains(out, "RESULT=STARTED") || strings.Contains(out, "code=5")
}

func stopHotspot() bool {
	return strings.Contains(runHelper("com.zflip5.tether.TetherStart", "stop"), "RESULT=STOPPED")
}

// step runs one scan/decide/act cycle.
func (h *HotspotController) step() {
	h.mu.Lock()
	wl := append([]string(nil), h.whitelist...)
	misses := h.misses
	gen := h.gen
	h.mu.Unlock()
	if len(wl) == 0 {
		return // feature off; Status() reports auto=false live
	}

	st := HotspotStatus{LastScan: time.Now().Format(time.RFC3339)}
	defer func() {
		h.mu.Lock()
		if st.LastAction == "" {
			st.LastAction = h.status.LastAction // keep last real action visible
		}
		h.status = st
		h.mu.Unlock()
	}()

	if !locationEnabled() {
		st.Paused = "location_off"
		return
	}
	aps, ok := scanAPs()
	if !ok {
		st.Paused = "scan_failed"
		return
	}
	seen := apNames(aps)
	st.APCount = len(aps)
	st.Matched = matchWhitelist(seen, wl)
	active := hotspotActive()
	action, newMisses := decideHotspot(len(st.Matched), active, misses)

	h.mu.Lock()
	h.lastNearby = aps // feed the Settings "Nearby networks" list from the auto loop too
	h.lastScanAt = st.LastScan
	if h.gen == gen { // don't clobber a SetWhitelist reset that landed mid-scan
		h.misses = newMisses
	}
	h.mu.Unlock()

	switch action {
	case "stop":
		if stopHotspot() {
			st.LastAction = "stopped: saw " + strings.Join(st.Matched, ", ")
		} else {
			st.LastAction = "stop failed"
		}
	case "start":
		if startHotspot() {
			st.LastAction = "started: whitelist not in range"
		} else {
			st.LastAction = "start failed"
		}
	}
}
