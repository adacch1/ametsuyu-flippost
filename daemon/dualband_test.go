package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func dualServer(t *testing.T, helperOut string) (*Server, *[]string, *int) {
	t.Helper()
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.cfgPath = filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(s.cfgPath, []byte(`{"hotspot":{"enable_on_boot":true}}`), 0o600)
	s.hs.apUp = func() bool { return false }
	s.hs.readAP = func() (APConfig, bool) { return APConfig{"Flippost", "5", "1"}, true }
	var args []string
	bounces := 0
	oh, ob := helperRun, bounceHotspot
	t.Cleanup(func() { helperRun, bounceHotspot = oh, ob })
	helperRun = func(c string, a ...string) string { args = a; return helperOut }
	bounceHotspot = func() { bounces++ }
	return s, &args, &bounces
}

func TestDualBandBlockedByWhitelist(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=OK")
	s.hs.SetWhitelist([]string{"Home"})
	if _, _, code, reason, _ := s.applyDualBand(true, false); code != 409 || reason != "whitelist" {
		t.Fatalf("%d %q", code, reason)
	}
	if _, _, code, _, _ := s.applyDualBand(false, false); code != 200 {
		t.Fatalf("disable blocked: %d", code)
	}
}

func TestDualBandBlockedByAutoSwitch(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=OK")
	s.presets.SetAuto(true)
	if _, _, code, reason, _ := s.applyDualBand(true, false); code != 409 || reason != "auto_switch" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestDualBandBusy(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=OK")
	s.airplaneBusy.Store(true)
	if _, _, code, reason, _ := s.applyDualBand(false, false); code != 409 || reason != "busy" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestDualBandPersistsAndCallsHelper(t *testing.T) {
	s, args, bounces := dualServer(t, "RESULT=OK")
	_, applied, code, _, err := s.applyDualBand(true, true)
	if err != nil || code != 200 || applied != "next_start" || *bounces != 0 {
		t.Fatalf("%v %d %q %d", err, code, applied, *bounces)
	}
	b, _ := os.ReadFile(s.cfgPath)
	if !reflect.DeepEqual(*args, []string{"band", "dual"}) || !s.hs.DualBand() || !s.cfg.Hotspot.DualBand ||
		!strings.Contains(string(b), `"dual_band": true`) || !strings.Contains(string(b), `"enable_on_boot": true`) {
		t.Fatalf("args %v cfg %s", *args, b)
	}
	s.applyDualBand(false, false)
	if !reflect.DeepEqual(*args, []string{"band", "5"}) {
		t.Fatalf("disable args %v", *args)
	}
}

func TestDualBandHelperFailureReverts(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=FAILED reason=x")
	if _, _, code, _, _ := s.applyDualBand(true, false); code != 502 {
		t.Fatalf("code %d", code)
	}
	b, _ := os.ReadFile(s.cfgPath)
	if s.hs.DualBand() || s.cfg.Hotspot.DualBand || strings.Contains(string(b), `"dual_band": true`) {
		t.Fatalf("not reverted: %s", b)
	}
}

func TestDualBandApplyNowBounces(t *testing.T) {
	s, _, bounces := dualServer(t, "RESULT=OK")
	s.hs.apUp = func() bool { return true }
	if _, applied, _, _, _ := s.applyDualBand(true, true); applied != "now" || *bounces != 1 {
		t.Fatalf("%q %d", applied, *bounces)
	}
}

func TestHandleHotspotDualBody(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=OK")
	for _, body := range []string{`{bad`, `{"enabled":true,"apply_now":false,"x":"` + strings.Repeat("a", 300) + `"}`} {
		w := httptest.NewRecorder()
		s.handleHotspotDual(w, httptest.NewRequest("POST", "/v1/hotspot/dual", strings.NewReader(body)))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "bad body") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	s.handleHotspotDual(w, httptest.NewRequest("POST", "/v1/hotspot/dual", strings.NewReader(`{"enabled":true}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"applied":"next_start"`) || !strings.Contains(w.Body.String(), `"dual_band":true`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
