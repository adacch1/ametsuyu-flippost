package main

import "testing"

func TestDecideHotspot(t *testing.T) {
	cases := []struct {
		matched, misses int
		active, safe    bool
		wantAction      string
		wantMisses      int
	}{
		{1, 0, true, true, "stop", 0}, // whitelisted SSID visible -> stop now
		{2, 5, false, true, "", 0},    // visible, already off -> stay off, reset misses
		{0, 0, true, true, "", 1},     // first miss -> wait (hysteresis)
		{0, 1, false, true, "start", 2},
		{0, 1, true, true, "", 2},   // already on -> nothing to start
		{0, 1, false, false, "", 2}, // hot -> never auto-start
		{0, 9, false, true, "start", 10},
	}
	for _, c := range cases {
		action, misses := decideHotspot(c.matched, c.active, c.misses, c.safe)
		if action != c.wantAction || misses != c.wantMisses {
			t.Errorf("decideHotspot(%d,%v,%d,%v) = (%q,%d) want (%q,%d)",
				c.matched, c.active, c.misses, c.safe, action, misses, c.wantAction, c.wantMisses)
		}
	}
}

func TestParseScanAPs(t *testing.T) {
	// HomeNet appears twice (-60 and -70); the stronger -60 must win, and the
	// list must sort strongest-first. The hidden SSID (empty name) is dropped.
	out := "AP\tHomeNet\taa:bb:cc:dd:ee:ff\t-70\nAP\t\td0:15:a6:d4:88:f2\t-69\nAP\tHomeNet\t11:22:33:44:55:66\t-60\nAP\tCafe 5G\tc0:06:c3:d0:eb:ab\t-73\nRESULT=OK\n"
	aps, ok := parseScanAPs(out)
	if !ok {
		t.Fatal("expected RESULT=OK to be trusted")
	}
	if len(aps) != 2 {
		t.Fatalf("aps = %v (hidden SSIDs skipped, duplicates collapsed)", aps)
	}
	if aps[0].SSID != "HomeNet" || aps[0].RSSI != -60 {
		t.Errorf("strongest-first + strongest-RSSI wrong: %+v", aps[0])
	}
	if aps[1].SSID != "Cafe 5G" {
		t.Errorf("second entry = %+v", aps[1])
	}
	if names := apNames(aps); len(names) != 2 || names[0] != "HomeNet" {
		t.Errorf("apNames = %v", names)
	}
}

func TestParseScanAPsFailure(t *testing.T) {
	if _, ok := parseScanAPs("AP\tX\ta\t-1\nRESULT=FAIL reason=timeout\n"); ok {
		t.Fatal("must not trust a failed scan")
	}
	if _, ok := parseScanAPs(""); ok {
		t.Fatal("must not trust empty output")
	}
	// A short (malformed) AP line missing the RSSI column must be skipped.
	if aps, ok := parseScanAPs("AP\tShort\nRESULT=OK\n"); !ok || len(aps) != 0 {
		t.Fatalf("malformed AP line not skipped: ok=%v aps=%v", ok, aps)
	}
}

func TestMatchWhitelist(t *testing.T) {
	m := matchWhitelist([]string{"HomeNet", "Cafe"}, []string{"Office", "HomeNet"})
	if len(m) != 1 || m[0] != "HomeNet" {
		t.Fatalf("matched = %v", m)
	}
	if m := matchWhitelist([]string{"homenet"}, []string{"HomeNet"}); len(m) != 0 {
		t.Fatalf("SSID match must be case-sensitive, got %v", m)
	}
}
