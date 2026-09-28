package main

import (
	"regexp"
	"strings"
	"time"
)

// WAN IP + airplane-mode control. Toggling airplane tears down and re-establishes
// the cellular PDP context, which normally yields a fresh carrier-assigned WAN
// IP — a quick "rotate my IP" for the modem. Airplane on also drops the hotspot,
// so the flows here re-enable it afterward (thermal-gated).

// wanSrcRe pulls "dev <iface> ... src <ipv4>" from `ip route get`: the source
// address the kernel uses to reach the internet = the cellular WAN IP (STA is
// off, so the default route is cellular, not Wi-Fi). The `.*?` skips the
// "table <name>" that Android inserts between dev and src.
var wanSrcRe = regexp.MustCompile(`\bdev\s+(\S+).*?\bsrc\s+(\d+\.\d+\.\d+\.\d+)`)

type WanIP struct {
	IP        string `json:"ip"`
	Iface     string `json:"iface,omitempty"`
	Available bool   `json:"available"`
}

// deviceWanIP returns the cellular WAN IP (egress source to the internet).
// Empty when data is down (e.g. mid-airplane-cycle).
func deviceWanIP() WanIP {
	out := runCmd("ip", "route", "get", "8.8.8.8")
	if m := wanSrcRe.FindStringSubmatch(out); m != nil {
		iface := m[1]
		// A downstream/loopback egress isn't the WAN; only report a real route.
		if iface == softApIface || iface == bridgeIface || iface == "lo" {
			return WanIP{}
		}
		return WanIP{IP: m[2], Iface: iface, Available: true}
	}
	return WanIP{}
}

// airplaneSet toggles airplane mode as root. Primary path is the Connectivity
// service shell verb; the settings+broadcast pair is a fallback for builds
// without it (root can send the protected AIRPLANE_MODE broadcast).
func airplaneSet(on bool) {
	verb, val, boolStr := "disable", "0", "false"
	if on {
		verb, val, boolStr = "enable", "1", "true"
	}
	runCmd("cmd", "connectivity", "airplane-mode", verb)
	// Belt-and-suspenders: also set the global + broadcast so the toggle sticks
	// on ROMs where the cmd verb is a no-op. Setting an already-correct value is
	// harmless.
	if got := strings.TrimSpace(runCmd("settings", "get", "global", "airplane_mode_on")); got != val {
		runCmd("settings", "put", "global", "airplane_mode_on", val)
		runCmd("am", "broadcast", "-a", "android.intent.action.AIRPLANE_MODE", "--ez", "state", boolStr)
	}
}

func airplaneOn() bool {
	return strings.TrimSpace(runCmd("settings", "get", "global", "airplane_mode_on")) == "1"
}

// RotateResult reports an airplane IP-rotation cycle.
type RotateResult struct {
	OldIP    string `json:"old_ip"`
	NewIP    string `json:"new_ip"`
	Changed  bool   `json:"changed"`
	DataBack bool   `json:"data_back"`      // WAN IP re-acquired after airplane off
	Hotspot  bool   `json:"hotspot_active"` // hotspot up after the cycle
	Note     string `json:"note,omitempty"`
}

// restartHotspotRetry brings the SoftAP back up after airplane-off. The Wi-Fi
// framework keeps churning for several seconds post-airplane, so an immediate
// start can come up and then drop to NO-CARRIER. Each attempt starts, waits,
// and only accepts the AP if it STAYS up — retrying until it holds.
func restartHotspotRetry() bool {
	for i := 0; i < 6; i++ {
		startHotspot()
		time.Sleep(2 * time.Second)
		if hotspotActive() {
			time.Sleep(3 * time.Second) // confirm it doesn't drop back to NO-CARRIER
			if hotspotActive() {
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return hotspotActive()
}

// airplaneCycle rotates the WAN IP: record IP, airplane ON, wait, airplane OFF,
// wait for data to re-attach, then restore the hotspot to its PRE-CYCLE state.
// Restoring a hotspot the owner already had running is status-quo, not a new
// radio-on, so it isn't thermal-gated here (Samsung's own mitigation still
// applies, and the CPU stays in eco while hot); a warm device is noted.
// Reports old vs new IP and whether it changed. Blocks ~10-30s.
func airplaneCycle(thermalSafe bool) RotateResult {
	wasActive := hotspotActive()
	old := deviceWanIP().IP
	airplaneSet(true)
	time.Sleep(5 * time.Second) // let the radio fully drop the PDP context
	airplaneSet(false)

	// Poll for the WAN IP to come back (PDP re-establish can take a few to ~30s).
	var neu string
	for i := 0; i < 24; i++ {
		time.Sleep(1500 * time.Millisecond)
		neu = deviceWanIP().IP
		if neu == "" {
			continue
		}
		if neu != old {
			break // fresh IP — done
		}
		if i >= 8 {
			break // data is back but the IP is unchanged (CGNAT sticky); stop waiting
		}
	}

	// Changed requires a known starting IP; if data was already down at the
	// start (old==""), "recovered" is not the same as "changed".
	res := RotateResult{OldIP: old, NewIP: neu, Changed: old != "" && neu != "" && neu != old, DataBack: neu != ""}
	if wasActive {
		res.Hotspot = restartHotspotRetry()
		if !thermalSafe {
			res.Note = "hotspot restored while device is warm — monitor temperature"
		}
	} else {
		res.Hotspot = hotspotActive()
	}
	return res
}
