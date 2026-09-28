package main

import (
	"sort"
	"strings"
)

// Client is one device connected to the Wi-Fi hotspot, keyed by MAC, with its
// IPv4 + any global IPv6 and its neighbour reachability state. Modern phones use
// randomized (locally-administered) MACs, so the MAC is an opaque handle, not a
// vendor identity.
type Client struct {
	MAC   string   `json:"mac"`
	IPv4  string   `json:"ipv4"`
	IPv6  []string `json:"ipv6"`
	State string   `json:"state"` // REACHABLE / STALE / DELAY / PROBE / FAILED
}

// ClientsReport is the hotspot client list served at /v1/clients.
type ClientsReport struct {
	Iface     string   `json:"iface"`
	Count     int      `json:"count"`
	Clients   []Client `json:"clients"`
	Available bool     `json:"available"`
}

// stateRank orders neighbour states so a client's "best" observed state wins
// when it has several neighbour entries (v4 + v6 + link-local).
func stateRank(s string) int {
	switch s {
	case "REACHABLE":
		return 5
	case "DELAY", "PROBE":
		return 4
	case "STALE":
		return 3
	case "NOARP", "PERMANENT":
		return 2
	case "FAILED":
		return 1
	default:
		return 0
	}
}

// parseClients turns `ip neigh show dev <iface>` output into a per-MAC client
// list. Entries without a lladdr (unresolved) are skipped; link-local fe80
// addresses are dropped as noise. The softAP's own router addresses are the
// "router" flagged entries — kept, they're just a peer's global v6.
func parseClients(raw, iface string) ClientsReport {
	byMAC := map[string]*Client{}
	for _, ln := range strings.Split(raw, "\n") {
		f := strings.Fields(ln)
		if len(f) < 4 {
			continue
		}
		addr := f[0]
		li := indexOf(f, "lladdr")
		if li < 0 || li+1 >= len(f) {
			continue // unresolved neighbour, no MAC
		}
		mac := f[li+1]
		state := f[len(f)-1] // last token is the NUD state (after an optional "router")
		c := byMAC[mac]
		if c == nil {
			c = &Client{MAC: mac}
			byMAC[mac] = c
		}
		if strings.Contains(addr, ":") {
			if !strings.HasPrefix(addr, "fe80") { // skip link-local
				c.IPv6 = append(c.IPv6, addr)
			}
		} else {
			c.IPv4 = addr
		}
		if stateRank(state) > stateRank(c.State) {
			c.State = state
		}
	}
	rep := ClientsReport{Iface: iface, Available: raw != ""}
	for _, c := range byMAC {
		sort.Strings(c.IPv6)
		rep.Clients = append(rep.Clients, *c)
	}
	// Stable order: clients with an IPv4 first, then by IPv4/MAC.
	sort.Slice(rep.Clients, func(i, j int) bool {
		a, b := rep.Clients[i], rep.Clients[j]
		if (a.IPv4 == "") != (b.IPv4 == "") {
			return a.IPv4 != ""
		}
		if a.IPv4 != b.IPv4 {
			return a.IPv4 < b.IPv4
		}
		return a.MAC < b.MAC
	})
	rep.Count = len(rep.Clients)
	return rep
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}

// softApIface is the Samsung SoftAP tether interface on the Z Flip 5.
// bridgeIface replaces it as the tethered (IP-bearing) interface while the
// dual-band bridged AP runs: swlan0 (2.4GHz) + wlan2 (5GHz) are its members.
const (
	softApIface = "swlan0"
	bridgeIface = "ap_br_swlan0"
)

// deviceClients reads the live hotspot client list on-device.
func deviceClients() ClientsReport {
	iface := softApIface
	if bridgedAPUp() {
		iface = bridgeIface
	}
	return parseClients(runCmd("ip", "neigh", "show", "dev", iface), iface)
}
