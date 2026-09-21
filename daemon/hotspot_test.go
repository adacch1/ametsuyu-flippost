package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// hotspotStub backs newTestHC's seam functions: counters for the actuators,
// a settable clock/location/scan result for the probes.
type hotspotStub struct {
	now        time.Time
	locOn      bool
	apUp       bool
	scanAPs    []ScanAP
	scanOK     bool
	scanReason string
	startOK    bool
	stopOK     bool
	startCalls int
	stopCalls  int
}

// newTestHC builds a HotspotController with every seam stubbed (so no test
// ever shells out to app_process/settings/ip) and hotspotOverridePath pointed
// at a fresh temp file. Defaults: location on, scan ok with no APs, AP down,
// start/stop both succeed — override per test as needed.
func newTestHC(t *testing.T, wl []string) (*HotspotController, *hotspotStub) {
	t.Helper()
	hotspotOverridePath = filepath.Join(t.TempDir(), "hotspot_override.json")
	s := &hotspotStub{now: time.Now(), locOn: true, scanOK: true, startOK: true, stopOK: true}
	h := &HotspotController{whitelist: append([]string(nil), wl...)}
	h.now = func() time.Time { return s.now }
	h.locOn = func() bool { return s.locOn }
	h.scan = func() ([]ScanAP, bool, string) { return s.scanAPs, s.scanOK, s.scanReason }
	h.apUp = func() bool { return s.apUp }
	h.start = func() bool {
		s.startCalls++
		if s.startOK {
			s.apUp = true
		}
		return s.startOK
	}
	h.stop = func() bool {
		s.stopCalls++
		if s.stopOK {
			s.apUp = false
		}
		return s.stopOK
	}
	h.loadOverride() // picks up a pre-seeded override file, if the test wrote one first
	return h, s
}

func TestDecideHotspot(t *testing.T) {
	cases := []struct {
		matched, misses int
		active          bool
		wantAction      string
		wantMisses      int
	}{
		{1, 0, true, "stop", 0}, // whitelisted SSID visible -> stop now
		{2, 5, false, "", 0},    // visible, already off -> stay off, reset misses
		{0, 0, true, "", 1},     // first miss -> wait (hysteresis)
		{0, 1, false, "start", 2},
		{0, 1, true, "", 2}, // already on -> nothing to start
		{0, 9, false, "start", 10},
	}
	for _, c := range cases {
		action, misses := decideHotspot(c.matched, c.active, c.misses)
		if action != c.wantAction || misses != c.wantMisses {
			t.Errorf("decideHotspot(%d,%v,%d) = (%q,%d) want (%q,%d)",
				c.matched, c.active, c.misses, action, misses, c.wantAction, c.wantMisses)
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

// The bug this guards: a probe that is not yet true when asked, but becomes
// true shortly after, must be reported as true — reporting the first sample is
// what made a working hotspot read as "did not come up".
func TestWaitForSettlesLate(t *testing.T) {
	calls := 0
	probe := func() bool { calls++; return calls >= 3 }
	if got := waitFor(true, probe, time.Second, time.Millisecond); !got {
		t.Errorf("want true once the probe settles, got false after %d calls", calls)
	}
	if calls < 3 {
		t.Errorf("returned before the probe settled (%d calls)", calls)
	}
}

// And the other half: something that never arrives must still return, with the
// truth, inside the budget rather than hanging the HTTP handler.
func TestWaitForGivesUpWithTheTruth(t *testing.T) {
	start := time.Now()
	if got := waitFor(true, func() bool { return false }, 30*time.Millisecond, 5*time.Millisecond); got {
		t.Error("want false when the probe never settles")
	}
	if el := time.Since(start); el > 500*time.Millisecond {
		t.Errorf("budget not honoured: took %v", el)
	}
}

// A refused command must not be dressed up as a settle wait: waitFor is only
// reached once the helper accepted the toggle.
func TestWaitForReturnsImmediatelyWhenAlreadyThere(t *testing.T) {
	calls := 0
	probe := func() bool { calls++; return true }
	if !waitFor(true, probe, time.Second, time.Second) {
		t.Error("want true")
	}
	if calls != 1 {
		t.Errorf("want one probe when already settled, got %d", calls)
	}
}

// The bug this guards: step() used to leave a "location_off"/"scan_failed"
// pause showing forever even after the precondition came back, because
// nothing re-ran the check. It re-checks fresh every tick, so the pause must
// clear the moment location comes back on — and the 2-consecutive-miss
// debounce must still hold once it does.
func TestStepPausedThenRecovers(t *testing.T) {
	h, s := newTestHC(t, []string{"HomeNet"})
	s.locOn = false

	h.step()
	if got := h.Status().Paused; got != "location_off" {
		t.Fatalf("Paused = %q, want location_off", got)
	}
	if misses := h.misses; misses != 0 {
		t.Fatalf("misses = %d, want 0 while paused", misses)
	}

	// Location is back; scan succeeds but sees nothing whitelisted -> pause
	// clears immediately, first miss recorded, no start yet (debounce).
	s.locOn = true
	h.step()
	if got := h.Status().Paused; got != "" {
		t.Fatalf("Paused = %q, want cleared once location is back on", got)
	}
	if s.startCalls != 0 {
		t.Fatalf("start called after only one miss, debounce broken")
	}
	if misses := h.misses; misses != 1 {
		t.Fatalf("misses = %d, want 1", misses)
	}

	// Second consecutive miss -> starts.
	h.step()
	if s.startCalls != 1 {
		t.Fatalf("start calls = %d, want 1 after the second miss", s.startCalls)
	}
	if got := h.Status().LastAction; got != "started: whitelist not in range" {
		t.Fatalf("LastAction = %q", got)
	}
}

// A failed scan must carry the helper's reason so the dashboard can show
// something more actionable than a bare "paused".
func TestStepScanFailedDetail(t *testing.T) {
	h, s := newTestHC(t, []string{"HomeNet"})
	s.scanOK = false
	s.scanReason = "timeout"

	h.step()
	got := h.Status()
	if got.Paused != "scan_failed" || got.PausedDetail != "timeout" {
		t.Fatalf("Paused=%q PausedDetail=%q, want scan_failed/timeout", got.Paused, got.PausedDetail)
	}
}

func TestScanFailReason(t *testing.T) {
	cases := []struct{ out, want string }{
		{"RESULT=FAIL reason=-1 not available\n", "-1 not available"},
		{"", "no output"},
	}
	for _, c := range cases {
		if got := scanFailReason(c.out); got != c.want {
			t.Errorf("scanFailReason(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

// SetOverride must bring the AP up right away — even with location off and
// the AP currently down — rather than waiting for the next tick, and the
// pause it reports is about SCANNING, not about the forced-on AP.
func TestOverrideForcesOnWhilePaused(t *testing.T) {
	h, s := newTestHC(t, []string{"HomeNet"})
	s.locOn = false
	s.apUp = false

	got := h.SetOverride(2 * time.Hour)
	if s.startCalls != 1 {
		t.Fatalf("start calls = %d, want 1 (immediate start on arm)", s.startCalls)
	}
	if got.OverrideLeftS <= 7190 || got.OverrideLeftS > 7200 {
		t.Fatalf("OverrideLeftS = %d, want in (7190,7200]", got.OverrideLeftS)
	}

	h.step()
	if s.startCalls != 1 {
		t.Fatalf("start calls = %d after a step with the AP already up, want still 1", s.startCalls)
	}
	if got := h.Status().Paused; got != "location_off" {
		t.Fatalf("Paused = %q, want location_off (override forces the AP, not the scan)", got)
	}
}

// An override must suppress the whitelist's "stop" decision, and leave the
// miss counter untouched (normal logic resumes exactly where it left off).
func TestOverrideSuppressesStop(t *testing.T) {
	h, s := newTestHC(t, []string{"HomeNet"})
	s.apUp = true
	h.SetOverride(2 * time.Hour)
	s.scanAPs = []ScanAP{{SSID: "HomeNet", RSSI: -40}}
	h.mu.Lock()
	h.misses = 3
	h.mu.Unlock()

	h.step()
	if s.stopCalls != 0 {
		t.Fatalf("stop called %d times, want 0 (override suppresses the whitelist stop)", s.stopCalls)
	}
	if got := h.Status().Matched; len(got) != 1 || got[0] != "HomeNet" {
		t.Fatalf("Matched = %v, want [HomeNet]", got)
	}
	if misses := h.misses; misses != 3 {
		t.Fatalf("misses = %d, want unchanged at 3", misses)
	}
}

// Once the deadline passes, normal whitelist logic must resume on the very
// next tick and the override fields must disappear from Status.
func TestOverrideExpiryReverts(t *testing.T) {
	h, s := newTestHC(t, []string{"HomeNet"})
	s.apUp = true
	h.SetOverride(2 * time.Hour)
	s.now = s.now.Add(2*time.Hour + time.Second) // past the deadline
	s.scanAPs = []ScanAP{{SSID: "HomeNet", RSSI: -40}}

	h.step()
	if s.stopCalls != 1 {
		t.Fatalf("stop calls = %d, want 1 (override expired, normal logic resumes)", s.stopCalls)
	}
	got := h.Status()
	if got.OverrideUntil != "" || got.OverrideLeftS != 0 {
		t.Fatalf("override fields still present after expiry: %+v", got)
	}
	if left := h.OverrideLeft(); left != 0 {
		t.Fatalf("OverrideLeft() = %v, want 0", left)
	}
}

// The override deadline is a wall-clock file, not in-memory state: it must
// survive a fresh controller on the same path (daemon restart), and cancel /
// an already-past deadline must both leave a fresh controller inactive.
func TestOverridePersists(t *testing.T) {
	h, _ := newTestHC(t, nil)
	if st := h.SetOverride(4 * time.Hour); st.OverrideUntil == "" {
		t.Fatal("expected override_until after SetOverride(4h)")
	}

	// s.now was seeded from time.Now(), so a real controller's real wall clock
	// reads back within about a second of the persisted deadline.
	h2 := NewHotspotController(nil)
	if diff := h2.OverrideLeft() - 4*time.Hour; diff > time.Second || diff < -time.Second {
		t.Fatalf("override did not persist across a fresh controller: left=%v", h2.OverrideLeft())
	}

	h.SetOverride(0)
	if _, err := os.Stat(hotspotOverridePath); !os.IsNotExist(err) {
		t.Fatalf("cancel should remove the override file, stat err=%v", err)
	}
	if h3 := NewHotspotController(nil); h3.OverrideLeft() != 0 {
		t.Fatalf("controller loaded after cancel should be inactive, left=%v", h3.OverrideLeft())
	}

	past, _ := json.Marshal(struct {
		Until time.Time `json:"until"`
	}{time.Now().Add(-time.Hour)})
	if err := os.WriteFile(hotspotOverridePath, past, 0o600); err != nil {
		t.Fatal(err)
	}
	if h4 := NewHotspotController(nil); h4.OverrideLeft() != 0 {
		t.Fatalf("a past deadline should load as inactive, left=%v", h4.OverrideLeft())
	}
}
