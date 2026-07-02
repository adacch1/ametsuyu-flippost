package main

import "testing"

// Real `ip neigh show dev swlan0` shape: v4 + global v6 + link-local per MAC,
// plus an unresolved (no lladdr) entry that must be ignored.
const neighDump = `192.168.11.104 lladdr 2a:4c:b5:81:07:b5 REACHABLE
2001:ee0:1b31:2fc1:c19:4110:4807:15e lladdr 2a:4c:b5:81:07:b5 STALE
fe80::af:20ea:b514:6760 lladdr 2a:4c:b5:81:07:b5 STALE
192.168.11.200 lladdr 76:4e:76:5f:81:ae REACHABLE
2001:ee0:1b31:2fc1:5833:d5ff:feeb:55f4 lladdr 5a:33:d5:eb:55:f4 router STALE
192.168.11.150 lladdr 5a:33:d5:eb:55:f4 DELAY
2001:ee0:1b31:2fc1:bc88:16f5:7750:2e20  FAILED`

func TestParseClients(t *testing.T) {
	rep := parseClients(neighDump, "swlan0")
	if !rep.Available || rep.Iface != "swlan0" {
		t.Fatalf("meta wrong: %+v", rep)
	}
	if rep.Count != 3 { // three distinct MACs; the no-lladdr line is ignored
		t.Fatalf("count=%d want 3", rep.Count)
	}
	// First client (sorted: has IPv4, lowest IPv4) is 192.168.11.104.
	c := rep.Clients[0]
	if c.IPv4 != "192.168.11.104" || c.MAC != "2a:4c:b5:81:07:b5" {
		t.Fatalf("client0 wrong: %+v", c)
	}
	if c.State != "REACHABLE" { // REACHABLE beats the STALE v6 entry
		t.Errorf("state=%q want REACHABLE", c.State)
	}
	for _, ip := range c.IPv6 {
		if len(ip) >= 4 && ip[:4] == "fe80" {
			t.Errorf("link-local leaked: %s", ip)
		}
	}
	if len(c.IPv6) != 1 || c.IPv6[0] != "2001:ee0:1b31:2fc1:c19:4110:4807:15e" {
		t.Errorf("client0 ipv6 wrong: %v", c.IPv6)
	}
}

func TestParseClientsStateRank(t *testing.T) {
	// The 5a:.. MAC has a STALE v6 (router) and a DELAY v4 -> DELAY wins.
	rep := parseClients(neighDump, "swlan0")
	for _, c := range rep.Clients {
		if c.MAC == "5a:33:d5:eb:55:f4" && c.State != "DELAY" {
			t.Errorf("state=%q want DELAY", c.State)
		}
	}
}

func TestParseClientsEmpty(t *testing.T) {
	rep := parseClients("", "swlan0")
	if rep.Available || rep.Count != 0 {
		t.Errorf("empty must be unavailable/0: %+v", rep)
	}
}
