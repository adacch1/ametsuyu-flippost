package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// NetPerf applies throughput-oriented kernel net tuning while bench mode is
// active, and restores stock values on disable/trip. Everything is best-effort
// and reversible; a missing knob (kernel without the option) is skipped.
//
// Why these knobs:
//   - cubic: best available CC on this kernel (reno/bic/cubic, no bbr) — bic
//     over-ramps on lossy cellular links; cubic is more stable under load.
//   - tcp_slow_start_after_idle=0: keeps the congestion window warm between
//     bursts so idle clients don't re-ramp from scratch (multi-device pattern).
//   - tcp_mtu_probing=1: finds the real path MTU through the cellular tunnel
//     instead of stalling on blackhole ICMP.
//   - bigger tcp_rmem/tcp_wmem + rmem_max/wmem_max: room for high-BDP links.
//   - fq_codel on swlan0 (the SoftAP interface): per-flow fair queueing +
//     AQM, so one heavy device can't starve the others (bufferbloat control).
var netPerfDefaults = map[string]string{
	"net/ipv4/tcp_congestion_control":    "cubic",
	"net/ipv4/tcp_slow_start_after_idle": "0",
	"net/ipv4/tcp_mtu_probing":           "1",
	"net/ipv4/tcp_rmem":                  "4096 87380 16777216",
	"net/ipv4/tcp_wmem":                  "4096 65536 16777216",
	"net/core/rmem_max":                  "16777216",
	"net/core/wmem_max":                  "16777216",
	"net/core/default_qdisc":             "fq_codel",
	"net/ipv4/tcp_fastopen":              "3",
	"net/ipv4/tcp_ecn":                   "1",
}

// softApIf is the SoftAP interface (wlan0 is the STA interface on this device);
// rmnetIf is the WWAN data interface (uplink bufferbloat control).
const (
	softApIf = "swlan0"
	rmnetIf  = "rmnet_data0"
)

var netPerfQdiscs = []string{softApIf, rmnetIf}

type NetPerf struct {
	mu        sync.Mutex
	originals map[string]string
	qdiscs    map[string]string // iface -> original qdisc kind
}

func netperfRoot() string {
	if r := os.Getenv("ZF5_NETPERF_ROOT"); r != "" {
		return r
	}
	return "/proc/sys"
}

func (n *NetPerf) Apply() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.originals == nil {
		n.originals = map[string]string{}
	}
	root := netperfRoot()
	for rel, val := range netPerfDefaults {
		p := filepath.Join(root, rel)
		if _, ok := n.originals[p]; !ok {
			if cur, err := os.ReadFile(p); err == nil {
				n.originals[p] = strings.TrimSpace(string(cur))
			}
		}
		_ = os.WriteFile(p, []byte(val), 0o644)
	}
	if root == "/proc/sys" {
		if n.qdiscs == nil {
			n.qdiscs = map[string]string{}
		}
		for _, iface := range netPerfQdiscs {
			kind := qdiscKind(iface)
			if kind == "" || kind == "fq_codel" {
				n.qdiscs[iface] = kind
				continue
			}
			if _, ok := n.qdiscs[iface]; !ok {
				n.qdiscs[iface] = kind
			}
			_ = exec.Command("tc", "qdisc", "replace", "dev", iface, "root", "fq_codel").Run()
		}
	}
}

func (n *NetPerf) Restore() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for p, orig := range n.originals {
		if orig != "" {
			_ = os.WriteFile(p, []byte(orig), 0o644)
		}
	}
	if netperfRoot() == "/proc/sys" {
		for iface, kind := range n.qdiscs {
			if kind == "" || kind == "fq_codel" {
				continue
			}
			_ = exec.Command("tc", "qdisc", "replace", "dev", iface, "root", kind).Run()
		}
	}
}

// qdiscKind returns the root qdisc kind of an interface (e.g. "mq",
// "pfifo_fast"), or "" when it can't be read.
func qdiscKind(iface string) string {
	out, err := exec.Command("tc", "qdisc", "show", "dev", iface).Output()
	if err != nil {
		return ""
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "qdisc" {
		return ""
	}
	return f[1]
}
