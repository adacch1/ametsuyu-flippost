package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Per-core utilization sampler (/proc/stat deltas). Feeds Health.PerCorePct so
// the dashboard's per-core bars show real load. Read-only.
// ---------------------------------------------------------------------------

type cpuJiffies struct{ total, idle uint64 }

var (
	cpuSampleMu sync.Mutex
	lastCPU     = map[string]cpuJiffies{}
)

// perCorePct reads /proc/stat and returns each core's busy % since the previous
// call (first call seeds state and returns zeros).
func perCorePct() []int {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil
	}
	cpuSampleMu.Lock()
	defer cpuSampleMu.Unlock()
	type row struct {
		name string
		j    cpuJiffies
	}
	var rows []row
	for _, ln := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(ln, "cpu") || ln == "" {
			continue
		}
		f := strings.Fields(ln)
		if len(f) < 5 || f[0] == "cpu" { // skip the aggregate "cpu" line
			continue
		}
		var total, idle uint64
		for i := 1; i < len(f); i++ {
			v, _ := strconv.ParseUint(f[i], 10, 64)
			total += v
			if i == 4 || i == 5 { // idle + iowait
				idle += v
			}
		}
		rows = append(rows, row{f[0], cpuJiffies{total, idle}})
	}
	out := make([]int, len(rows))
	for i, r := range rows {
		prev, ok := lastCPU[r.name]
		if ok {
			dt := r.j.total - prev.total
			di := r.j.idle - prev.idle
			if dt > 0 {
				pct := int(float64(dt-di) / float64(dt) * 100)
				if pct < 0 {
					pct = 0
				} else if pct > 100 {
					pct = 100
				}
				out[i] = pct
			}
		}
		lastCPU[r.name] = r.j
	}
	return out
}

// ---------------------------------------------------------------------------
// CPU power/perf policy. Adjusts which cores are online and each cluster's max
// frequency to trade throughput for battery — WITHOUT ever fighting thermal
// mitigation (when hot it only ever REDUCES, never raises), and never taking
// the little cluster offline (cpu0-2 stay up so the system never loses its base).
// All changes are reversible: originals are captured and restored on "off".
// ---------------------------------------------------------------------------

const cpuRoot = "/sys/devices/system/cpu"

// clusters on the SM-F731B (SD 8 Gen 2): little 0-2, mid 3-6, prime 7.
var (
	primeCPUs = []int{7}
	midCPUs   = []int{3, 4, 5, 6}
	// littleCPUs (0,1,2) are never touched — the safe base.
)

// CPUReport is served at /v1/cpu.
type CPUReport struct {
	Mode      string     `json:"mode"`      // effective mode this cycle
	Requested string     `json:"requested"` // configured mode (auto/performance/balanced/eco/off)
	Reason    string     `json:"reason"`    // why the effective mode was chosen
	Clients   int        `json:"clients"`
	ThermSafe bool       `json:"thermal_safe"`
	Cores     []CoreInfo `json:"cores"`
}

type CoreInfo struct {
	ID       int    `json:"id"`
	Cluster  string `json:"cluster"` // little/mid/prime
	Online   bool   `json:"online"`
	CurKHz   int    `json:"cur_khz"`
	MaxKHz   int    `json:"max_khz"`
	HwMaxKHz int    `json:"hw_max_khz"`
}

type CPUController struct {
	mu       sync.Mutex
	origMax  map[int]int // policy-leader cpu -> original scaling_max_freq (KHz)
	lastMode string
}

func NewCPUController() *CPUController { return &CPUController{origMax: map[int]int{}} }

func cpuPath(cpu int, sub string) string {
	return filepath.Join(cpuRoot, "cpu"+strconv.Itoa(cpu), sub)
}

func readCPUInt(cpu int, sub string) int {
	b, err := os.ReadFile(cpuPath(cpu, sub))
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return v
}

func writeCPU(path, val string) error { return os.WriteFile(path, []byte(val), 0o644) }

func setOnline(cpu int, on bool) {
	v := "0"
	if on {
		v = "1"
	}
	_ = writeCPU(cpuPath(cpu, "online"), v)
}

// setClusterMax caps a cluster's scaling_max_freq via any of its cpus'
// cpufreq dir. Clamped to the hardware max so we never over-clock.
func (c *CPUController) setClusterMax(cpus []int, khz int) {
	for _, cpu := range cpus {
		hw := readCPUInt(cpu, "cpufreq/cpuinfo_max_freq")
		if hw == 0 {
			continue
		}
		if _, ok := c.origMax[cpu]; !ok {
			c.origMax[cpu] = readCPUInt(cpu, "cpufreq/scaling_max_freq")
		}
		target := khz
		if target == 0 || target > hw {
			target = hw
		}
		_ = writeCPU(cpuPath(cpu, "cpufreq/scaling_max_freq"), strconv.Itoa(target))
	}
}

func (c *CPUController) restoreMax(cpus []int) {
	for _, cpu := range cpus {
		if orig, ok := c.origMax[cpu]; ok && orig > 0 {
			_ = writeCPU(cpuPath(cpu, "cpufreq/scaling_max_freq"), strconv.Itoa(orig))
		}
	}
}

// apply enforces one of the effective modes. Idempotent-ish; safe to call each cycle.
func (c *CPUController) apply(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	all := append(append([]int{}, midCPUs...), primeCPUs...)
	switch mode {
	case "performance":
		// Everything online; unlock every cluster to its hardware max for speed.
		for _, cpu := range all {
			setOnline(cpu, true)
		}
		c.setClusterMax(primeCPUs, 0) // 0 => hw max
		c.setClusterMax(midCPUs, 0)
		c.setClusterMax([]int{0}, 0) // little cluster leader
	case "eco":
		// Park the prime core, cap the mid cluster low; little cluster carries idle.
		c.setClusterMax(midCPUs, 1500000)
		for _, cpu := range primeCPUs {
			setOnline(cpu, false)
		}
	case "balanced":
		fallthrough
	default:
		// Restore Samsung's own caps and bring everything back online.
		for _, cpu := range all {
			setOnline(cpu, true)
		}
		c.restoreMax(all)
		c.restoreMax([]int{0})
	}
	c.lastMode = mode
}

// restoreAll returns the CPU to stock: all cores online, original caps. Called
// when the mode is "off" (and should be called on daemon shutdown).
func (c *CPUController) restoreAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	all := append(append([]int{}, midCPUs...), primeCPUs...)
	for _, cpu := range all {
		setOnline(cpu, true)
	}
	c.restoreMax(all)
	c.restoreMax([]int{0})
	c.lastMode = "off"
}

// decide picks the effective mode from the configured mode + live state. "auto"
// is the interesting one: performance while clients are actively connected, eco
// when idle, and always cool (eco) when thermal is unsafe — the safety override.
func decideMode(requested string, clients int, thermalSafe bool) (string, string) {
	if requested == "off" {
		return "off", "disabled by config"
	}
	if !thermalSafe {
		return "eco", "thermal gate: reducing to shed heat" // never raise when hot
	}
	switch requested {
	case "performance", "balanced", "eco":
		return requested, "fixed mode"
	default: // auto
		if clients > 0 {
			return "performance", "clients connected — maximize throughput"
		}
		return "eco", "idle — save battery"
	}
}

// clusterOf labels a cpu id.
func clusterOf(id int) string {
	switch {
	case id <= 2:
		return "little"
	case id <= 6:
		return "mid"
	default:
		return "prime"
	}
}

// report reads the live per-core online/freq state for /v1/cpu.
func (c *CPUController) report(requested, effective, reason string, clients int, safe bool) CPUReport {
	rep := CPUReport{Mode: effective, Requested: requested, Reason: reason, Clients: clients, ThermSafe: safe}
	for id := 0; id < 8; id++ {
		online := readCPUInt(id, "online")
		// cpu0 has no "online" node on many kernels (always on) -> treat as up.
		up := online == 1
		if _, err := os.Stat(cpuPath(id, "online")); err != nil {
			up = true
		}
		rep.Cores = append(rep.Cores, CoreInfo{
			ID: id, Cluster: clusterOf(id), Online: up,
			CurKHz:   readCPUInt(id, "cpufreq/scaling_cur_freq"),
			MaxKHz:   readCPUInt(id, "cpufreq/scaling_max_freq"),
			HwMaxKHz: readCPUInt(id, "cpufreq/cpuinfo_max_freq"),
		})
	}
	return rep
}
