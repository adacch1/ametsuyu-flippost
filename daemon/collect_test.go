package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

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

func TestThermalSmootherMedian(t *testing.T) {
	var ts thermalSmoother
	if _, ok := ts.median(); ok {
		t.Fatal("empty ring should report ok=false")
	}
	ts.push(0) // degraded sweep: must be ignored
	if _, ok := ts.median(); ok {
		t.Fatal("push(0) should not seed the ring")
	}
	// 12 raw max-of-zones samples observed on-device at idle 2026-09-18.
	samples := []float64{41.2, 42.0, 42.4, 44.0, 44.4, 41.6, 42.4, 41.0, 43.6, 44.8, 40.8, 47.1}
	for _, s := range samples {
		ts.push(s)
	}
	if len(ts.ring) != thermalSmoothN {
		t.Fatalf("ring len = %d, want capped at %d", len(ts.ring), thermalSmoothN)
	}
	// last 5 pushed = [41.0,43.6,44.8,40.8,47.1]; sorted =
	// [40.8,41.0,43.6,44.8,47.1] -> median 43.6 (the 47.1 spike is dropped).
	got, ok := ts.median()
	if !ok || got != 43.6 {
		t.Fatalf("median = (%v, %v), want (43.6, true)", got, ok)
	}
}

// A PMIC temp-alarm zone with no ADC reads a constant 37000 placeholder; it
// must not set the floor of max-of-zones, but a real 37 C elsewhere still counts.
func TestSweepSkipsPMICPlaceholder(t *testing.T) {
	dir := t.TempDir()
	for i, z := range []struct{ name, temp string }{
		{"pmr735d_k_tz", "37000"}, {"cpu-0-0", "33500"}, {"battery", "29000"},
	} {
		d := filepath.Join(dir, "thermal_zone"+strconv.Itoa(i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(d, "type"), []byte(z.name+"\n"), 0o644)
		os.WriteFile(filepath.Join(d, "temp"), []byte(z.temp+"\n"), 0o644)
	}
	bat, max, zone := sweepThermalZones(dir)
	if max != 33.5 || zone != "cpu-0-0" || bat != 29 {
		t.Fatalf("got bat=%v max=%v zone=%q, want 29 / 33.5 / cpu-0-0", bat, max, zone)
	}
	os.WriteFile(filepath.Join(dir, "thermal_zone1", "temp"), []byte("37000"), 0o644)
	if _, max, _ := sweepThermalZones(dir); max != 37 {
		t.Fatalf("real 37 C on a non-alarm zone was dropped: max=%v", max)
	}
}
