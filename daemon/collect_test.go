package main

import "testing"

func TestParseBattery(t *testing.T) {
	raw := "Current Battery Service state:\n  AC powered: false\n  USB powered: true\n  level: 100\n  temperature: 337\n"
	b := parseBattery(raw)
	if !b.Available || b.Level != 100 || b.TempC != 33.7 || b.Plugged != "usb" {
		t.Fatalf("parseBattery = %+v", b)
	}
}

func TestParseNetwork(t *testing.T) {
	raw := "mTelephonyDisplayInfo=TelephonyDisplayInfo {network=LTE, overrideNetwork=LTE_CA, isRoaming=false} " +
		"NetworkRegistrationInfo{ domain=PS transportType=WLAN nrState=NONE} " +
		"NetworkRegistrationInfo{ domain=PS transportType=WWAN cellIdentity=CellIdentityLte:{ mCi=1 } nrState=NONE} " +
		"mOperatorAlphaLong=VN VINAPHONE, x"
	n := parseNetwork(raw)
	if !n.Available || n.Type != "LTE" || n.Override != "LTE_CA" || n.NrState != "NONE" || n.Operator != "VN VINAPHONE" {
		t.Fatalf("parseNetwork = %+v", n)
	}
	if n.Display != "4G+" {
		t.Fatalf("display = %q want 4G+", n.Display)
	}
}

func TestParseNetworkNSA(t *testing.T) {
	// 5G NSA: anchor stays LTE, override flips to NR_NSA, and only the cellular
	// PS registration says CONNECTED — the IWLAN block before it always says
	// NONE (matching that first block was the bug that reported 4G in 5G zones).
	raw := "mTelephonyDisplayInfo=TelephonyDisplayInfo {network=LTE, overrideNetwork=NR_NSA, isRoaming=false} " +
		"NetworkRegistrationInfo{ domain=PS transportType=WLAN nrState=NONE} " +
		"NetworkRegistrationInfo{ domain=CS transportType=WWAN cellIdentity=CellIdentityLte:{ mCi=1 } nrState=NONE} " +
		"NetworkRegistrationInfo{ domain=PS transportType=WWAN cellIdentity=CellIdentityLte:{ mCi=1 } nrState=CONNECTED} " +
		"mOperatorAlphaLong=VN VINAPHONE, x"
	n := parseNetwork(raw)
	if n.NrState != "CONNECTED" {
		t.Fatalf("nr_state = %q want CONNECTED (matched wrong registration block?)", n.NrState)
	}
	if n.Display != "5G" {
		t.Fatalf("display = %q want 5G", n.Display)
	}
}

func TestDisplayTech(t *testing.T) {
	cases := []struct{ network, override, nrState, want string }{
		{"LTE", "NR_ADVANCED", "CONNECTED", "5G+"},
		{"LTE", "NR_NSA", "CONNECTED", "5G"},
		{"LTE", "NONE", "CONNECTED", "5G"}, // NSA attached, override lagging
		{"NR", "NONE", "NONE", "5G"},       // SA
		{"LTE", "LTE_CA", "NONE", "4G+"},
		{"LTE", "NONE", "NOT_RESTRICTED", "4G"}, // 5G available but not attached
		{"UMTS", "NONE", "NONE", "3G"},
		{"EDGE", "NONE", "NONE", "2G"},
	}
	for _, c := range cases {
		if got := displayTech(c.network, c.override, c.nrState); got != c.want {
			t.Errorf("displayTech(%q,%q,%q)=%q want %q", c.network, c.override, c.nrState, got, c.want)
		}
	}
}

func TestParseNetworkNoClaim5G(t *testing.T) {
	// no display info -> not available, empty type (never guessed)
	n := parseNetwork("garbage")
	if n.Available || n.Type != "" {
		t.Fatalf("should not claim network: %+v", n)
	}
}
