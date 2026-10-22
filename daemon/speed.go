package main

import (
	"log"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var procNetDevPath = "/proc/net/dev"
var bridgeBrifDir = "/sys/class/net/" + bridgeIface + "/brif"

type ifaceBytes struct{ RX, TX uint64 }

// parseNetDev maps each interface in /proc/net/dev to its rx and tx byte counters.
func parseNetDev(raw string) map[string]ifaceBytes {
	out := map[string]ifaceBytes{}
	for _, ln := range strings.Split(raw, "\n") {
		if !strings.Contains(ln, ":") {
			continue
		}
		parts := strings.Fields(strings.ReplaceAll(ln, ":", " "))
		if len(parts) < 10 {
			continue
		}
		rx, e1 := strconv.ParseUint(parts[1], 10, 64)
		tx, e2 := strconv.ParseUint(parts[9], 10, 64)
		if e1 == nil && e2 == nil {
			out[parts[0]] = ifaceBytes{rx, tx}
		}
	}
	return out
}

type ClientSpeed struct {
	MAC        string  `json:"mac"`
	IPv4       *string `json:"ipv4"`
	DownBytesS uint64  `json:"down_bytes_s"`
	UpBytesS   uint64  `json:"up_bytes_s"`
}

type SpeedSnap struct {
	IntervalMs      int     `json:"interval_ms"`
	WanDownBytesS   uint64  `json:"wan_down_bytes_s"`
	WanUpBytesS     uint64  `json:"wan_up_bytes_s"`
	PhoneDownBytesS *uint64 `json:"phone_down_bytes_s"`
	PhoneUpBytesS   *uint64 `json:"phone_up_bytes_s"`
	PerClient       any     `json:"per_client"` // []ClientSpeed or "unavailable"
}

type speedSampler struct {
	mu      sync.Mutex
	prevWAN ifaceBytes
	prevSta map[string]ifaceBytes
	prevAt  time.Time
	now     func() time.Time
	readDev func() (string, error)
	dump    func(string) ([]Station, error)
	snap    SpeedSnap
	still   int
	warned  bool
}

func newSpeedSampler() *speedSampler {
	return &speedSampler{
		now: time.Now,
		readDev: func() (string, error) {
			b, err := os.ReadFile(procNetDevPath)
			return string(b), err
		},
		dump: dumpStations,
		snap: SpeedSnap{PerClient: "unavailable"},
	}
}

// delta returns cur-prev, or 0 after a counter reset (cur < prev).
func delta(cur, prev uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}

func sumRmnet(m map[string]ifaceBytes) (w ifaceBytes) {
	for n, v := range m {
		if strings.HasPrefix(n, "rmnet_data") {
			w.RX += v.RX
			w.TX += v.TX
		}
	}
	return
}

// tick samples WAN and station counters. The first call only records baselines.
func (sp *speedSampler) tick(apIfaces []string, clients []Client) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	raw, err := sp.readDev()
	if err != nil {
		log.Printf("speed: read /proc/net/dev: %v", err)
		return
	}
	now := sp.now()
	wan := sumRmnet(parseNetDev(raw))
	stas := map[string]Station{}
	var dumpErr error
	for _, ifc := range apIfaces {
		ss, e := sp.dump(ifc)
		if e != nil {
			dumpErr = e
			break
		}
		for _, s := range ss {
			stas[strings.ToLower(s.MAC)] = s
		}
	}
	prevSta, prevAt, prevWAN := sp.prevSta, sp.prevAt, sp.prevWAN
	sp.prevAt, sp.prevWAN, sp.prevSta = now, wan, nil
	if dumpErr == nil {
		sp.prevSta = make(map[string]ifaceBytes, len(stas))
		for m, s := range stas {
			sp.prevSta[m] = ifaceBytes{s.RXBytes, s.TXBytes}
		}
	}
	if prevAt.IsZero() {
		return
	}
	ms := now.Sub(prevAt).Milliseconds()
	if ms <= 0 {
		return
	}
	rate := func(d uint64) uint64 { return d * 1000 / uint64(ms) }
	wanDelta := delta(wan.RX, prevWAN.RX) + delta(wan.TX, prevWAN.TX)
	snap := SpeedSnap{IntervalMs: int(ms), WanDownBytesS: rate(delta(wan.RX, prevWAN.RX)), WanUpBytesS: rate(delta(wan.TX, prevWAN.TX)), PerClient: "unavailable"}

	reachable := false
	ip := map[string]string{}
	for _, c := range clients {
		reachable = reachable || c.State == "REACHABLE"
		ip[strings.ToLower(c.MAC)] = c.IPv4
	}
	reason := ""
	var list []ClientSpeed
	var sumDown, sumUp uint64
	changed := false
	if dumpErr != nil {
		reason = "station dump failed: " + dumpErr.Error()
	} else if len(stas) == 0 && reachable {
		reason = "no stations while a client is reachable"
	} else {
		list = []ClientSpeed{}
		for m, s := range stas {
			p, seen := prevSta[m]
			cs := ClientSpeed{MAC: s.MAC}
			if seen {
				cs.DownBytesS, cs.UpBytesS = rate(delta(s.TXBytes, p.TX)), rate(delta(s.RXBytes, p.RX))
				changed = changed || p != ifaceBytes{s.RXBytes, s.TXBytes}
			} else {
				changed = true
			}
			if v := ip[m]; v != "" {
				cs.IPv4 = &v
			}
			sumDown += cs.DownBytesS
			sumUp += cs.UpBytesS
			list = append(list, cs)
		}
		// Stable order: fastest first, then MAC (map iteration is random).
		sort.Slice(list, func(i, j int) bool {
			ri, rj := list[i].DownBytesS+list[i].UpBytesS, list[j].DownBytesS+list[j].UpBytesS
			if ri != rj {
				return ri > rj
			}
			return list[i].MAC < list[j].MAC
		})
		if len(stas) > 0 && !changed && wanDelta > 0 {
			sp.still++
		} else {
			sp.still = 0
		}
		if sp.still >= 3 {
			reason = "station counters unchanged for 3 ticks while WAN moves"
		}
	}
	if reason != "" {
		if !sp.warned {
			log.Printf("speed: per_client unavailable: %s", reason)
			sp.warned = true
		}
	} else {
		sp.warned = false
		pd, pu := uint64(0), uint64(0)
		if snap.WanDownBytesS > sumDown {
			pd = snap.WanDownBytesS - sumDown
		}
		if snap.WanUpBytesS > sumUp {
			pu = snap.WanUpBytesS - sumUp
		}
		snap.PhoneDownBytesS, snap.PhoneUpBytesS, snap.PerClient = &pd, &pu, list
	}
	sp.snap = snap
}

func (sp *speedSampler) Snapshot() SpeedSnap {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.snap
}

// apIfaces lists the interfaces that carry hotspot clients: the bridge members
// while the dual-band bridge runs, else swlan0 when it exists.
func apIfaces() []string {
	if es, err := os.ReadDir(bridgeBrifDir); err == nil {
		out := make([]string, 0, len(es))
		for _, e := range es {
			out = append(out, e.Name())
		}
		return out
	}
	if _, err := net.InterfaceByName(softApIface); err == nil {
		return []string{softApIface}
	}
	return nil
}
