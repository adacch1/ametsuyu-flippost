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
	raw := "mTelephonyDisplayInfo=TelephonyDisplayInfo {network=LTE, overrideNetwork=LTE_CA, isRoaming=false} nrState=NONE mOperatorAlphaLong=VN VINAPHONE, x"
	n := parseNetwork(raw)
	if !n.Available || n.Type != "LTE" || n.Override != "LTE_CA" || n.NrState != "NONE" || n.Operator != "VN VINAPHONE" {
		t.Fatalf("parseNetwork = %+v", n)
	}
}

func TestParseNetworkNoClaim5G(t *testing.T) {
	// no display info -> not available, empty type (never guessed)
	n := parseNetwork("garbage")
	if n.Available || n.Type != "" {
		t.Fatalf("should not claim network: %+v", n)
	}
}
