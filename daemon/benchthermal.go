package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Bench thermal mode: for battery-less donor hardware on a bench supply in a
// monitored lab. While active, the thermal gates are suspended: thermal zones
// forced "disabled", cooling devices zeroed, and Samsung's kernel cpufreq_limit
// ceiling lifted to the hardware max. The vendor thermal HALs are deliberately
// NOT stopped: sec-thermal-1-0 provides android.hardware.thermal, which
// system_server blocks on at boot (HardwarePropertiesManagerService.nativeInit)
// — stopping it watchdog-resets the framework on the next system_server
// restart. The 5s watchdog re-asserts the zone/cpufreq/SIOP bypass to win the
// tug-of-war against the running HAL. The daemon's app gate is lifted to
// benchTripC. A critical trip at benchTripC restores all mitigation: beyond
// that temperature silicon damage is permanent, so there is no mode above the
// trip. Originals are recorded so Disable/trip restores the stock posture.
const (
	benchWarnC = 65.0
	benchTripC = 70.0
	// benchRearmC is the no-touch recovery point: once a 70C trip fires, the
	// bypass stays down until the hottest zone falls back to this temperature,
	// then re-arms automatically. Hysteresis stops enable/trip oscillation.
	benchRearmC = 55.0
)

// writeIfChanged skips the write when the file already holds want: this runs
// for ~160 sysfs nodes every 5 s, and a redundant write still makes the
// thermal core re-evaluate the zone.
func writeIfChanged(path, want string) error {
	if cur, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(cur)) == want {
		return nil
	}
	return os.WriteFile(path, []byte(want), 0o644)
}

// cpufreqLimitPath is Samsung's kernel-side hard CPU ceiling (the firmware
// thermal gate). The framework's thermal service writes 1478400 here when hot;
// bench mode lifts it to the hardware max and restores the original on
// disable/trip.
const cpufreqLimitPath = "/sys/devices/system/cpu/cpufreq_limit/cpufreq_max_limit"
const cpufreqVolPath = "/sys/devices/system/cpu/cpufreq_limit/vol_based_clk"

type BenchThermalStatus struct {
	Enabled       bool    `json:"enabled"`
	Tripped       bool    `json:"tripped"`
	ZonesDisabled int     `json:"zones_disabled"`
	WarnC         float64 `json:"warn_c"`
	TripC         float64 `json:"trip_c"`
	RearmC        float64 `json:"rearm_c"`
}

type BenchThermalController struct {
	mu            sync.Mutex
	enabled       bool
	tripped       bool
	originals     map[string]string // zone path -> original mode
	cdevOriginals map[string]string // cooling device path -> original cur_state
	cpufreqLimit  string            // original cpufreq_max_limit
	cpufreqVol    string            // original vol_based_clk
	net           NetPerf           // throughput sysctls while bench active
	zones         int
}

func NewBenchThermalController() *BenchThermalController {
	return &BenchThermalController{originals: map[string]string{}, cdevOriginals: map[string]string{}}
}

// Active reports whether the bypass is currently in force (enabled, not tripped).
func (b *BenchThermalController) Active() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled && !b.tripped
}

// Tripped reports whether a critical trip has fired while still enabled.
func (b *BenchThermalController) Tripped() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled && b.tripped
}

func (b *BenchThermalController) Status() BenchThermalStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BenchThermalStatus{
		Enabled: b.enabled, Tripped: b.tripped, ZonesDisabled: b.zones,
		WarnC: benchWarnC, TripC: benchTripC, RearmC: benchRearmC,
	}
}

// allowThermalWrites injects the scoped SELinux allow live, so the bypass works
// immediately without a reboot. magisk/sepolicy.rule carries the same rule for
// next boot. Best-effort: on failure the zone writes fail and the trip stays
// as the backstop.
func allowThermalWrites() {
	_ = exec.Command("magiskpolicy", "--live", "allow magisk sysfs_thermal file write").Run()
}

// applyLocked disables every readable thermal zone, recording its original
// mode on first touch, and flips the kernel msm_thermal switch when present.
func (b *BenchThermalController) applyLocked() {
	zones, _ := filepath.Glob(filepath.Join(thermalRoot(), "thermal_zone*"))
	b.zones = 0
	for _, z := range zones {
		mode := filepath.Join(z, "mode")
		if _, ok := b.originals[z]; !ok {
			orig := ""
			if cur, err := os.ReadFile(mode); err == nil {
				orig = strings.TrimSpace(string(cur))
			}
			b.originals[z] = orig
		}
		if writeIfChanged(mode, "disabled") == nil {
			b.zones++
		}
	}
	// Cooling devices: force cur_state to 0 (no throttle) on top of the zone
	// disable, recording originals so restore puts back what stock had.
	cdevs, _ := filepath.Glob(filepath.Join(thermalRoot(), "cooling_device*"))
	for _, cd := range cdevs {
		cur := filepath.Join(cd, "cur_state")
		if _, ok := b.cdevOriginals[cd]; !ok {
			orig := ""
			if v, err := os.ReadFile(cur); err == nil {
				orig = strings.TrimSpace(string(v))
			}
			b.cdevOriginals[cd] = orig
		}
		_ = writeIfChanged(cur, "0")
	}
	_ = os.WriteFile("/sys/module/msm_thermal/parameters/enabled", []byte("N"), 0o644)
	// Samsung kernel cpufreq_limit: the firmware thermal ceiling. Lift it to the
	// hardware max so the CPU can reach the frequencies our CPU policy asks for.
	if b.cpufreqLimit == "" {
		if cur, err := os.ReadFile(cpufreqLimitPath); err == nil {
			b.cpufreqLimit = strings.TrimSpace(string(cur))
		}
	}
	if max := hwCPUFreqMax(); max != "" {
		_ = os.WriteFile(cpufreqLimitPath, []byte(max), 0o644)
	}
	// Bench supply: don't let VBAT-droop logic downclock. Original is restored
	// on disable/trip.
	if b.cpufreqVol == "" {
		if cur, err := os.ReadFile(cpufreqVolPath); err == nil {
			b.cpufreqVol = strings.TrimSpace(string(cur))
		}
	}
	_ = os.WriteFile(cpufreqVolPath, []byte("0"), 0o644)
	// Pin Samsung SIOP at "normal" (0) — the thermal HAL that would raise it is
	// already stopped; this guards against any other writer.
	_ = exec.Command("setprop", "sys.siop.level", "0").Run()
	b.net.Apply()
}

// restoreLocked puts every zone back to its recorded mode and re-enables the
// kernel switch.
func (b *BenchThermalController) restoreLocked() {
	for z, orig := range b.originals {
		if orig == "" {
			continue
		}
		_ = os.WriteFile(filepath.Join(z, "mode"), []byte(orig), 0o644)
	}
	for cd, orig := range b.cdevOriginals {
		if orig == "" {
			continue
		}
		_ = os.WriteFile(filepath.Join(cd, "cur_state"), []byte(orig), 0o644)
	}
	if b.cpufreqLimit != "" {
		_ = os.WriteFile(cpufreqLimitPath, []byte(b.cpufreqLimit), 0o644)
	}
	if b.cpufreqVol != "" {
		_ = os.WriteFile(cpufreqVolPath, []byte(b.cpufreqVol), 0o644)
	}
	b.net.Restore()
	_ = os.WriteFile("/sys/module/msm_thermal/parameters/enabled", []byte("Y"), 0o644)
}

// hwCPUFreqMax reads the hardware max from the prime core's cpuinfo_max_freq
// (the cpufreq_limit ceiling is shared across clusters).
func hwCPUFreqMax() string {
	b, err := os.ReadFile(cpuPath(7, "cpufreq/cpuinfo_max_freq"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (b *BenchThermalController) Enable() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = true
	b.tripped = false
	allowThermalWrites()
	b.applyLocked()
}

func (b *BenchThermalController) Disable() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.enabled && !b.tripped {
		return
	}
	b.restoreLocked()
	b.enabled = false
	b.tripped = false
}

func (b *BenchThermalController) SetEnabled(on bool) BenchThermalStatus {
	if on {
		b.Enable()
	} else {
		b.Disable()
	}
	return b.Status()
}

// TripCheck is the last-resort failsafe: at benchTripC the bypass is dropped
// and stock mitigation restored. Returns true when the trip fired.
func (b *BenchThermalController) TripCheck(tempC float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.enabled || b.tripped || tempC < benchTripC {
		return false
	}
	b.restoreLocked()
	b.tripped = true
	return true
}

// Step runs one watchdog cycle: re-assert the bypass while active, trip at
// benchTripC, and auto re-arm once the device has cooled back to benchRearmC.
// Returns the action taken ("apply", "trip", "rearm", "off", "tripped").
func (b *BenchThermalController) Step(maxC float64) string {
	if b.Active() {
		if b.TripCheck(maxC) {
			return "trip"
		}
		b.mu.Lock()
		b.applyLocked()
		b.mu.Unlock()
		return "apply"
	}
	if b.Tripped() {
		if maxC <= benchRearmC {
			b.Enable()
			return "rearm"
		}
		return "tripped"
	}
	return "off"
}

// Watch re-asserts the bypass every few seconds (some firmware re-enables its
// zones/services), arms the critical trip, and auto re-arms after cooldown.
func (b *BenchThermalController) Watch() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for range tick.C {
		_, maxC, _ := readThermalZones()
		switch b.Step(maxC) {
		case "trip":
			log.Printf("bench thermal: critical trip at %.1fC — mitigation restored; auto re-arm below %.0fC", maxC, benchRearmC)
		case "rearm":
			log.Printf("bench thermal: cooled to %.1fC — bypass re-armed", maxC)
		}
	}
}
