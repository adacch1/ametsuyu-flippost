package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeCollector struct{ safe bool }

func (f fakeCollector) Health() Health {
	return Health{CPULoad1: 0.5, CPUCores: 8, MemTotalKB: 100, MemAvailKB: 60, MemUsedPct: 40, TempBattery: 30, TempMax: 40, TempMaxZone: "cpu-1-0"}
}
func (f fakeCollector) Thermal(warnC, gateC float64, failClosed bool) Thermal {
	return Thermal{BatteryC: 30, MaxC: 40, WarnC: warnC, GateC: gateC, Safe: f.safe, Source: "sysfs+battery"}
}
func (f fakeCollector) Battery() Battery {
	return Battery{Level: 80, TempC: 30, Plugged: "usb", Available: true}
}
func (f fakeCollector) Network() Network {
	return Network{Type: "LTE", Override: "LTE_CA", NrState: "NONE", Available: true}
}

func testCfg() *Config {
	c := &Config{BindHost: "127.0.0.1", BindPort: 18080}
	c.Tokens = map[string]string{
		"read-status":   strings.Repeat("a", 64),
		"sms":           strings.Repeat("b", 64),
		"radio-control": strings.Repeat("c", 64),
	}
	c.Thermal.WarnC, c.Thermal.GateC, c.Thermal.FailClosed = 44, 46, true
	c.RateLimits.DefaultPerMin, c.RateLimits.SMSPerMin, c.RateLimits.RadioPerMin = 30, 3, 6
	c.SMS.Enabled, c.SMS.RedactDefault = true, true
	return c
}

func do(s *Server, method, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestStatusNoToken401(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if w := do(s, "GET", "/v1/status", ""); w.Code != 401 {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestStatusBadToken401(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if w := do(s, "GET", "/v1/status", "deadbeef"); w.Code != 401 {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestStatusValid200(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	w := do(s, "GET", "/v1/status", strings.Repeat("a", 64))
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("want json, got %q", ct)
	}
	for _, key := range []string{"health", "thermal", "network", "battery", "service"} {
		if !strings.Contains(w.Body.String(), key) {
			t.Fatalf("status missing %q: %s", key, w.Body.String())
		}
	}
}

func TestUnknownPath404(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if w := do(s, "GET", "/v1/nope", strings.Repeat("a", 64)); w.Code != 404 {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestWrongMethod405(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	// status is GET-only
	if w := do(s, "POST", "/v1/status", strings.Repeat("a", 64)); w.Code != 405 {
		t.Fatalf("want 405, got %d", w.Code)
	}
	// prefer5g is POST-only
	if w := do(s, "GET", "/v1/prefer5g", strings.Repeat("c", 64)); w.Code != 405 {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

func TestWrongScope403(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	// read-status token cannot drive a radio write
	if w := do(s, "POST", "/v1/prefer5g", strings.Repeat("a", 64)); w.Code != 403 {
		t.Fatalf("want 403, got %d", w.Code)
	}
	// read-status token cannot read sms
	if w := do(s, "GET", "/v1/sms/recent", strings.Repeat("a", 64)); w.Code != 403 {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

func TestHotspotNotThermalGated(t *testing.T) {
	// The hotspot is the modem's primary function and is NOT app-thermal-gated:
	// /v1/tether start succeeds even when the collector reports unsafe (Samsung's
	// own mitigation is the thermal backstop).
	s := NewServer(testCfg(), fakeCollector{safe: false})
	if w := do(s, "POST", "/v1/tether", strings.Repeat("c", 64)); w.Code != 200 {
		t.Fatalf("tether must not be thermal-gated, got %d", w.Code)
	}
}

func TestRateLimit429(t *testing.T) {
	cfg := testCfg()
	cfg.RateLimits.RadioPerMin = 2
	s := NewServer(cfg, fakeCollector{safe: true})
	// freeze time so the window doesn't roll
	frozen := time.Now()
	s.rl.now = func() time.Time { return frozen }
	tok := strings.Repeat("c", 64)
	codes := []int{}
	for i := 0; i < 4; i++ {
		codes = append(codes, do(s, "POST", "/v1/cooldown", tok).Code)
	}
	// first 2 allowed (200), then 429
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 || codes[3] != 429 {
		t.Fatalf("want [200 200 429 429], got %v", codes)
	}
}

func TestConfigRejectsPublicBind(t *testing.T) {
	c := testCfg()
	c.BindHost = "0.0.0.0"
	if err := c.Validate(); err == nil {
		t.Fatal("expected public bind to be rejected")
	}
}

func TestConfigRequiresReadStatusToken(t *testing.T) {
	c := testCfg()
	delete(c.Tokens, "read-status")
	if err := c.Validate(); err == nil {
		t.Fatal("expected missing read-status token to be rejected")
	}
}

func TestConfigThermalFailClosedRequired(t *testing.T) {
	c := testCfg()
	c.Thermal.FailClosed = false
	if err := c.Validate(); err == nil {
		t.Fatal("expected fail_closed=false to be rejected")
	}
}
