package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestHotspotOverrideEndpoint exercises the timed force-on override end to
// end: auth, hours validation, the immediate start on arm, and that
// cancelling (hours:0) clears both the response fields and the persisted
// file. hotspotOverridePath is redirected to a temp file BEFORE NewServer,
// since NewHotspotController loads it at construction.
func TestHotspotOverrideEndpoint(t *testing.T) {
	oldPath := hotspotOverridePath
	hotspotOverridePath = filepath.Join(t.TempDir(), "hotspot_override.json")
	t.Cleanup(func() { hotspotOverridePath = oldPath })

	s := NewServer(testCfg(), fakeCollector{safe: true})
	startCalls := 0
	s.hs.apUp = func() bool { return false } // AP stays down so every arm tries to start it
	s.hs.start = func() bool { startCalls++; return true }

	rc := strings.Repeat("c", 64)
	post := func(body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/hotspot/override", strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w
	}

	if w := post(`{"hours":4}`, ""); w.Code != 401 {
		t.Fatalf("no token: want 401, got %d", w.Code)
	}
	if w := post(`{"hours":25}`, rc); w.Code != 400 {
		t.Fatalf("hours=25: want 400, got %d", w.Code)
	}
	if w := post(`{"hours":-1}`, rc); w.Code != 400 {
		t.Fatalf("hours=-1: want 400, got %d", w.Code)
	}

	w := post(`{"hours":4}`, rc)
	if w.Code != 200 {
		t.Fatalf("hours=4: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var st HotspotStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.OverrideUntil == "" || st.OverrideLeftS <= 0 {
		t.Fatalf("expected override fields in the response, got %+v", st)
	}
	if startCalls != 1 {
		t.Fatalf("start calls = %d, want 1", startCalls)
	}
	if _, err := os.Stat(hotspotOverridePath); err != nil {
		t.Fatalf("override file not written: %v", err)
	}

	w = post(`{"hours":0}`, rc)
	if w.Code != 200 {
		t.Fatalf("hours=0: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var st2 HotspotStatus // fresh struct: json.Unmarshal wouldn't clear st's omitempty fields
	if err := json.Unmarshal(w.Body.Bytes(), &st2); err != nil {
		t.Fatal(err)
	}
	if st2.OverrideUntil != "" || st2.OverrideLeftS != 0 {
		t.Fatalf("override fields should be cleared after cancel: %+v", st2)
	}
	if _, err := os.Stat(hotspotOverridePath); !os.IsNotExist(err) {
		t.Fatalf("override file should be removed after cancel, stat err=%v", err)
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

func TestStatusThermalDisplaySmoothed(t *testing.T) {
	// Proves smoothing is display-only: /v1/status shows the median while
	// safe/policy_state stay derived from the raw (unsmoothed) value, and
	// /v1/thermal never sees the median at all.
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.smooth.push(45)
	s.smooth.push(52)
	s.smooth.push(45)

	w := do(s, "GET", "/v1/status", strings.Repeat("a", 64))
	if w.Code != 200 {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"temp_max_c":45`, `"temp_max_raw_c":40`, `"safe":true`, `"policy_state":"SAFE"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("status body missing %q: %s", want, body)
		}
	}

	w = do(s, "GET", "/v1/thermal", strings.Repeat("a", 64))
	if w.Code != 200 {
		t.Fatalf("thermal: want 200, got %d: %s", w.Code, w.Body.String())
	}
	body = w.Body.String()
	if !strings.Contains(body, `"temp_max_c":40`) {
		t.Fatalf("thermal body: want raw temp_max_c:40, got %s", body)
	}
	if strings.Contains(body, "temp_max_raw_c") {
		t.Fatalf("thermal body must stay raw-only, got %s", body)
	}
}

// TestThermalHistoryEndpoint proves /v1/thermal/history is read-status
// guarded, empty-but-non-null before any sample, wired to the smoother (not
// the raw collector), and that /v1/thermal stays exactly as before -- no
// history/minutes leak into the gate's own endpoint.
func TestThermalHistoryEndpoint(t *testing.T) {
	old := tempsPath
	tempsPath = filepath.Join(t.TempDir(), "temps.json")
	t.Cleanup(func() { tempsPath = old })

	s := NewServer(testCfg(), fakeCollector{safe: true})
	rs := strings.Repeat("a", 64)

	if w := do(s, "GET", "/v1/thermal/history", ""); w.Code != 401 {
		t.Fatalf("no token: want 401, got %d", w.Code)
	}
	if w := do(s, "POST", "/v1/thermal/history", rs); w.Code != 405 {
		t.Fatalf("POST: want 405, got %d", w.Code)
	}

	w := do(s, "GET", "/v1/thermal/history", rs)
	if w.Code != 200 {
		t.Fatalf("GET: want 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"method"`, `"minutes":[]`, `"hours":[]`, `"days":[]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("empty history missing %q: %s", want, body)
		}
	}

	// fakeCollector's raw Thermal() always reports MaxC 40; pushing 45 into
	// the smoother and sampling must show 45, proving temps.go reads the
	// display path (s.smooth) and never col.Thermal.
	s.smooth.push(45)
	s.temps.Sample()
	body = do(s, "GET", "/v1/thermal/history", rs).Body.String()
	if !strings.Contains(body, ",45]") {
		t.Fatalf("history body missing the smoother-fed sample: %s", body)
	}

	thermalBody := do(s, "GET", "/v1/thermal", rs).Body.String()
	if thermalBody != `{"battery_c":30,"temp_max_c":40,"warn_c":44,"gate_c":46,"safe":true,"source":"sysfs+battery"}`+"\n" {
		t.Fatalf("/v1/thermal changed shape: %s", thermalBody)
	}
}

func TestSetCPUModeEndpoint(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	req := httptest.NewRequest("POST", "/v1/cpu/mode", strings.NewReader(`{"mode":"performance"}`))
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("c", 64))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("set cpu mode: %d %s", w.Code, w.Body.String())
	}
	if s.cfg.CPU.Mode != "performance" {
		t.Fatalf("cpu mode not stored: %q", s.cfg.CPU.Mode)
	}
	req = httptest.NewRequest("POST", "/v1/cpu/mode", strings.NewReader(`{"mode":"turbo"}`))
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("c", 64))
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("invalid cpu mode: want 400, got %d", w.Code)
	}
}

// TestUsageQuotaEndpoint exercises the admin-gated quota write end to end:
// scope enforcement, validation, and that a saved quota round-trips through
// GET /v1/usage. usagePath is redirected to a temp file so the tracker's
// background state doesn't leak between tests.
func TestUsageQuotaEndpoint(t *testing.T) {
	old := usagePath
	usagePath = filepath.Join(t.TempDir(), "usage.json")
	t.Cleanup(func() { usagePath = old })

	s := NewServer(testCfg(), fakeCollector{safe: true})
	rc, rs := strings.Repeat("c", 64), strings.Repeat("a", 64)

	if w := postJSON(s, "/v1/usage/quota", rs, `{"limit_bytes":1}`); w.Code != 403 {
		t.Fatalf("read-status token: want 403, got %d: %s", w.Code, w.Body.String())
	}

	for _, bad := range []string{
		`{"period":"yearly"}`,
		`{"reset_time":"25:00"}`,
		`{"limit_bytes":-1}`,
	} {
		if w := postJSON(s, "/v1/usage/quota", rc, bad); w.Code != 400 {
			t.Errorf("body %s: want 400, got %d: %s", bad, w.Code, w.Body.String())
		}
	}

	// 6 GiB = 6442450944 bytes.
	w := postJSON(s, "/v1/usage/quota", rc, `{"limit_bytes":6442450944,"period":"monthly","reset_time":"00:00","reset_day":1}`)
	if w.Code != 200 {
		t.Fatalf("valid quota: want 200, got %d: %s", w.Code, w.Body.String())
	}
	if s.cfg.Quota.LimitBytes != 6442450944 || s.cfg.Quota.Period != "monthly" {
		t.Fatalf("cfg.Quota not updated: %+v", s.cfg.Quota)
	}

	got := do(s, "GET", "/v1/usage", rs).Body.String()
	for _, want := range []string{`"limit_bytes":6442450944`, `"period":"monthly"`, `"reset_time":"00:00"`, `"reset_day":1`} {
		if !strings.Contains(got, want) {
			t.Errorf("GET /v1/usage missing %q: %s", want, got)
		}
	}
}

// TestUsageResetEndpoint proves the manual reset is scope-gated, zeroes only
// period_bytes, and leaves month_bytes (day buckets) untouched.
func TestUsageResetEndpoint(t *testing.T) {
	old := usagePath
	usagePath = filepath.Join(t.TempDir(), "usage.json")
	t.Cleanup(func() { usagePath = old })

	s := NewServer(testCfg(), fakeCollector{safe: true})
	rc, rs := strings.Repeat("c", 64), strings.Repeat("a", 64)

	if w := postJSON(s, "/v1/usage/reset", rs, `{}`); w.Code != 403 {
		t.Fatalf("read-status token: want 403, got %d: %s", w.Code, w.Body.String())
	}

	// Give the tracker some month-to-date bytes to prove the reset doesn't
	// touch them.
	s.usage.st.Days = map[string]uint64{time.Now().Format(dayLayout): 1234}

	w := postJSON(s, "/v1/usage/reset", rc, `{}`)
	if w.Code != 200 {
		t.Fatalf("reset: want 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"period_bytes":0`) {
		t.Errorf("reset response missing period_bytes:0: %s", body)
	}
	if !strings.Contains(body, `"month_bytes":1234`) {
		t.Errorf("reset response: month_bytes changed, want unchanged 1234: %s", body)
	}
}

// TestUsageQuotaPersistsToConfig proves persistQuota's raw-map rewrite
// round-trips through a real config.json on disk: a reload (LoadConfig, as a
// restart would do) sees the new quota, a sibling key the Config struct
// doesn't model survives the rewrite untouched, and the endpoint still gates
// on the token even with a real cfgPath wired up (open_control is off in
// testCfg). TestUsageQuotaEndpoint above covers the same handler with
// s.cfgPath empty (no persist attempted); this test is the persist path.
func TestUsageQuotaPersistsToConfig(t *testing.T) {
	old := usagePath
	usagePath = filepath.Join(t.TempDir(), "usage.json")
	t.Cleanup(func() { usagePath = old })

	// testCfg() through a raw map plus one key the Config struct doesn't
	// model (hotspot.enable_on_boot), so a struct-marshal rewrite (which
	// would silently drop it) is distinguishable from persistQuota's actual
	// raw-map rewrite.
	cfgBytes, err := json.Marshal(testCfg())
	if err != nil {
		t.Fatalf("marshal testCfg: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(cfgBytes, &m); err != nil {
		t.Fatalf("unmarshal testCfg: %v", err)
	}
	hs, _ := m["hotspot"].(map[string]any)
	if hs == nil {
		hs = map[string]any{}
		m["hotspot"] = hs
	}
	hs["enable_on_boot"] = true
	rawCfg, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, rawCfg, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.cfgPath = cfgPath // main.go sets this after NewServer too; mirror that here
	rc := strings.Repeat("c", 64)
	body := `{"limit_bytes":6000000000,"period":"weekly","reset_time":"07:30"}`

	if w := postJSON(s, "/v1/usage/quota", "", body); w.Code != 401 {
		t.Fatalf("no token: want 401, got %d: %s", w.Code, w.Body.String())
	}

	if w := postJSON(s, "/v1/usage/quota", rc, body); w.Code != 200 {
		t.Fatalf("quota post: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// (a) a restart reloads the persisted quota.
	reloaded, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := Quota{LimitBytes: 6000000000, Period: "weekly", ResetTime: "07:30", ResetDay: 1}
	if reloaded.Quota != want {
		t.Fatalf("reloaded quota = %+v, want %+v", reloaded.Quota, want)
	}

	// (b) the sibling key survives the raw-map rewrite.
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), `"enable_on_boot": true`) {
		t.Fatalf("sibling key enable_on_boot lost by persistQuota rewrite: %s", raw)
	}
}
