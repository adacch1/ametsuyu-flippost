package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Health struct {
	CPULoad1    float64 `json:"cpu_load1"`
	CPULoad5    float64 `json:"cpu_load5"`
	CPULoad15   float64 `json:"cpu_load15"`
	CPUCores    int     `json:"cpu_cores"`
	MemTotalKB  int     `json:"mem_total_kb"`
	MemAvailKB  int     `json:"mem_available_kb"`
	MemUsedPct  int     `json:"mem_used_pct"`
	TempBattery float64 `json:"temp_battery_c"`
	TempMax     float64 `json:"temp_max_c"`
	TempMaxZone string  `json:"temp_max_zone"`
	PerCorePct  []int   `json:"per_core_pct"`
}

type Thermal struct {
	BatteryC float64 `json:"battery_c"`
	MaxC     float64 `json:"temp_max_c"`
	WarnC    float64 `json:"warn_c"`
	GateC    float64 `json:"gate_c"`
	Safe     bool    `json:"safe"`
	Source   string  `json:"source"`
	// MaxRawC is set only on the display copy handleStatus serves (see
	// thermalSmoother): the instantaneous value MaxC was smoothed from.
	// omitempty keeps /v1/thermal and every existing test byte-identical.
	MaxRawC float64 `json:"temp_max_raw_c,omitempty"`
}

type Battery struct {
	Level     int     `json:"level"`
	TempC     float64 `json:"temp_c"`
	Plugged   string  `json:"plugged"`
	Available bool    `json:"available"`
}

type Network struct {
	Type      string `json:"type"`
	Override  string `json:"override"`
	NrState   string `json:"nr_state"`
	Display   string `json:"display"` // human tech: 5G+/5G/4G+/4G/3G/...
	Operator  string `json:"operator"`
	Available bool   `json:"available"`
}

// Collector reads device host + thermal facts. Split behind an interface so the
// HTTP/auth layer is unit-tested with a fake, no device required.
type Collector interface {
	Health() Health
	// Thermal returns the gate state given the configured thresholds.
	Thermal(warnC, gateC float64, failClosed bool) Thermal
	Battery() Battery
	Network() Network
}

var (
	batLevelRe   = regexp.MustCompile(`(?m)^\s*level:\s*(\d+)`)
	batTempRe    = regexp.MustCompile(`(?m)^\s*temperature:\s*(-?\d+)`)
	batUsbRe     = regexp.MustCompile(`(?m)^\s*USB powered:\s*(true|false)`)
	batAcRe      = regexp.MustCompile(`(?m)^\s*AC powered:\s*(true|false)`)
	netDisplayRe = regexp.MustCompile(`network=([A-Za-z0-9_+]+),\s*overrideNetwork=([A-Za-z0-9_+]+)`)
	// The dump lists several NetworkRegistrationInfo blocks; the IWLAN one comes
	// first and always says nrState=NONE, which masked real NSA attachment. Only
	// the cellular packet-switched (PS/WWAN) registration carries the NR state.
	nrStateWwanRe = regexp.MustCompile(`(?s)domain=PS transportType=WWAN.*?nrState=([A-Z_]+)`)
	nrStateRe     = regexp.MustCompile(`nrState=([A-Z_]+)`)
	operatorRe    = regexp.MustCompile(`mOperatorAlphaLong=([^,}]+)`)
)

// parseBattery extracts level/temp/plugged from `dumpsys battery`. Android
// reports temperature in deci-Celsius (337 -> 33.7).
func parseBattery(raw string) Battery {
	b := Battery{}
	if m := batLevelRe.FindStringSubmatch(raw); m != nil {
		b.Level, _ = strconv.Atoi(m[1])
		b.Available = true
	}
	if m := batTempRe.FindStringSubmatch(raw); m != nil {
		deci, _ := strconv.Atoi(m[1])
		b.TempC = float64(deci) / 10.0
	}
	switch {
	case batAcRe.FindStringSubmatch(raw) != nil && batAcRe.FindStringSubmatch(raw)[1] == "true":
		b.Plugged = "ac"
	case batUsbRe.FindStringSubmatch(raw) != nil && batUsbRe.FindStringSubmatch(raw)[1] == "true":
		b.Plugged = "usb"
	default:
		b.Plugged = "unplugged"
	}
	return b
}

// parseNetwork extracts display type / NR state / operator from
// `dumpsys telephony.registry`. Never claims 5G unless nrState/override says so.
func parseNetwork(raw string) Network {
	n := Network{}
	if m := netDisplayRe.FindStringSubmatch(raw); m != nil {
		n.Type = m[1]
		n.Override = m[2]
		n.Available = true
	}
	if m := nrStateWwanRe.FindStringSubmatch(raw); m != nil {
		n.NrState = m[1]
	} else if m := nrStateRe.FindStringSubmatch(raw); m != nil {
		n.NrState = m[1] // older dump format without registration blocks
	}
	if m := operatorRe.FindStringSubmatch(raw); m != nil {
		n.Operator = strings.TrimSpace(m[1])
	}
	n.Display = displayTech(n.Type, n.Override, n.NrState)
	return n
}

// displayTech maps raw network/override/NR state to what the status bar would
// show. NSA 5G keeps network=LTE (the anchor) with overrideNetwork=NR_NSA or
// nrState=CONNECTED — reporting raw LTE there is what made 5G zones show 4G.
func displayTech(network, override, nrState string) string {
	switch override {
	case "NR_ADVANCED":
		return "5G+"
	case "NR_NSA", "NR_NSA_MMWAVE":
		return "5G"
	}
	if network == "NR" || nrState == "CONNECTED" {
		return "5G"
	}
	switch override {
	case "LTE_CA", "LTE_ADVANCED_PRO":
		return "4G+"
	}
	switch network {
	case "LTE":
		return "4G"
	case "UMTS", "HSDPA", "HSUPA", "HSPA", "HSPAP", "TD_SCDMA":
		return "3G"
	case "GPRS", "EDGE", "GSM":
		return "2G"
	}
	return network
}

func (deviceCollector) Battery() Battery {
	return parseBattery(runCmd("dumpsys", "battery"))
}

func (deviceCollector) Network() Network {
	return parseNetwork(runCmd("dumpsys", "telephony.registry"))
}

// deviceCollector reads /proc and /sys directly (daemon runs on-device as root).
type deviceCollector struct{}

func (deviceCollector) Health() Health {
	h := Health{}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		if len(f) >= 3 {
			h.CPULoad1, _ = strconv.ParseFloat(f[0], 64)
			h.CPULoad5, _ = strconv.ParseFloat(f[1], 64)
			h.CPULoad15, _ = strconv.ParseFloat(f[2], 64)
		}
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "processor") {
				h.CPUCores++
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) < 2 {
				continue
			}
			v, _ := strconv.Atoi(f[1])
			switch f[0] {
			case "MemTotal:":
				h.MemTotalKB = v
			case "MemAvailable:":
				h.MemAvailKB = v
			}
		}
		if h.MemTotalKB > 0 {
			h.MemUsedPct = int(float64(h.MemTotalKB-h.MemAvailKB) / float64(h.MemTotalKB) * 100)
		}
	}
	bat, maxc, zone := readThermalZones()
	h.TempBattery = bat
	h.TempMax = maxc
	h.TempMaxZone = zone
	h.PerCorePct = perCorePct()
	return h
}

// thermalRoot is the sysfs thermal path; overridable for host-side tests via
// ZF5_THERMAL_ROOT. In production it is always the real /sys/class/thermal.
func thermalRoot() string {
	if r := os.Getenv("ZF5_THERMAL_ROOT"); r != "" {
		return r
	}
	return "/sys/class/thermal"
}

// A full sweep is ~95 zones, several of them modem sensors read over QMI, so
// it wakes the modem. The smoother, the bench watchdog, and every /v1/status
// poll share one sweep for zoneCacheTTL instead of each running their own.
const zoneCacheTTL = 3 * time.Second

var zoneCache struct {
	sync.Mutex
	root     string
	at       time.Time
	bat, max float64
	zone     string
}

// readThermalZones returns (batteryC, hottestC, hottestZone) from sysfs milli-C,
// at most zoneCacheTTL old.
func readThermalZones() (float64, float64, string) {
	root := thermalRoot()
	zoneCache.Lock()
	defer zoneCache.Unlock()
	if zoneCache.root == root && time.Since(zoneCache.at) < zoneCacheTTL {
		return zoneCache.bat, zoneCache.max, zoneCache.zone
	}
	bat, max, zone := sweepThermalZones(root)
	zoneCache.root, zoneCache.at = root, time.Now()
	zoneCache.bat, zoneCache.max, zoneCache.zone = bat, max, zone
	return bat, max, zone
}

const pmicPlaceholderMilliC = 37000

func sweepThermalZones(root string) (float64, float64, string) {
	var bat, max float64
	var zone string
	zones, _ := filepath.Glob(filepath.Join(root, "thermal_zone*"))
	for _, z := range zones {
		tb, err := os.ReadFile(filepath.Join(z, "temp"))
		if err != nil {
			continue
		}
		milli, err := strconv.Atoi(strings.TrimSpace(string(tb)))
		if err != nil {
			continue
		}
		c := float64(milli) / 1000.0
		name := ""
		if nb, err := os.ReadFile(filepath.Join(z, "type")); err == nil {
			name = strings.TrimSpace(string(nb))
		}
		// qcom-spmi-temp-alarm PMICs with no ADC channel (pmr735d_k_tz here)
		// report the driver's DEFAULT_TEMP placeholder, a constant 37 C, which
		// pinned max-of-zones at 37. Real alarm stages report far higher, so
		// skipping the placeholder never hides heat.
		if milli == pmicPlaceholderMilliC && strings.HasSuffix(name, "_tz") {
			continue
		}
		if name == "battery" {
			bat = c
		}
		if c > max {
			max = c
			zone = name
		}
	}
	return bat, max, zone
}

func (deviceCollector) Thermal(warnC, gateC float64, failClosed bool) Thermal {
	bat, max, _ := readThermalZones()
	t := Thermal{BatteryC: bat, MaxC: max, WarnC: warnC, GateC: gateC, Source: "sysfs+battery"}
	// Fail closed: if we read nothing usable, treat as unsafe.
	if bat == 0 && max == 0 {
		t.Source = "degraded"
		t.Safe = !failClosed // failClosed true -> unsafe
		return t
	}
	worst := bat
	if max > worst {
		worst = max
	}
	t.Safe = worst < gateC
	return t
}

// runCmd is a small helper for framework scrapes (dumpsys/cmd). Errors yield "".
// A hard timeout keeps a wedged dumpsys/ip/settings from hanging an HTTP handler
// or a background loop forever (which would leak goroutines and stall polling).
func runCmd(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// thermalSmoother keeps the last thermalSmoothN raw hottest-zone samples and
// serves their median for DISPLAY ONLY. The gate (Thermal) keeps reading raw:
// the median lags a ramp by ~6 s, so it must never feed a safety decision.
// Measured 2026-09-18 at idle: max-of-zones swung 40.8->47.1 C within 12 s
// because a different big-core TSENS spikes each second; battery moved 0.1 C.
type thermalSmoother struct {
	mu   sync.Mutex
	ring []float64 // oldest first, len <= thermalSmoothN
}

const (
	thermalSmoothN     = 5               // 5 x 5 s = 25 s window; tolerates 2 spikes
	thermalSmoothEvery = 5 * time.Second // > zoneCacheTTL, so every sample is a fresh sweep
)

func (t *thermalSmoother) push(maxC float64) {
	if maxC <= 0 { // degraded/unreadable sweep: keep the window honest
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ring = append(t.ring, maxC)
	if len(t.ring) > thermalSmoothN {
		t.ring = t.ring[1:]
	}
}

// median returns (value, ok); ok is false until the first good sample.
func (t *thermalSmoother) median() (float64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.ring) == 0 {
		return 0, false
	}
	s := append([]float64(nil), t.ring...)
	sort.Float64s(s)
	return s[len(s)/2], true // upper median on even counts: biased warm, never below the base
}

func (t *thermalSmoother) run() {
	tick := time.NewTicker(thermalSmoothEvery)
	defer tick.Stop()
	for range tick.C {
		_, max, _ := readThermalZones()
		t.push(max)
	}
}
