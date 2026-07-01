package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
}

type Thermal struct {
	BatteryC float64 `json:"battery_c"`
	MaxC     float64 `json:"temp_max_c"`
	WarnC    float64 `json:"warn_c"`
	GateC    float64 `json:"gate_c"`
	Safe     bool    `json:"safe"`
	Source   string  `json:"source"`
}

// Collector reads device host + thermal facts. Split behind an interface so the
// HTTP/auth layer is unit-tested with a fake, no device required.
type Collector interface {
	Health() Health
	// Thermal returns the gate state given the configured thresholds.
	Thermal(warnC, gateC float64, failClosed bool) Thermal
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

// readThermalZones returns (batteryC, hottestC, hottestZone) from sysfs milli-C.
func readThermalZones() (float64, float64, string) {
	var bat, max float64
	var zone string
	zones, _ := filepath.Glob(filepath.Join(thermalRoot(), "thermal_zone*"))
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
func runCmd(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}
