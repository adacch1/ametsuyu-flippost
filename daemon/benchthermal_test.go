package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withThermalRoot redirects thermalRoot() to a temp sysfs-like tree with two
// zones (mode enabled, temp 40C) so Enable/Disable/trip are testable offline.
func withThermalRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		z := filepath.Join(dir, "thermal_zone"+itoa(i))
		if err := os.MkdirAll(z, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(z, "mode"), []byte("enabled"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(z, "temp"), []byte("40000"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ZF5_THERMAL_ROOT", dir)
	return dir
}

func TestBenchThermalZoneDisableRestore(t *testing.T) {
	dir := withThermalRoot(t)
	b := NewBenchThermalController()
	b.Enable()
	for i := 0; i < 2; i++ {
		got, err := os.ReadFile(filepath.Join(dir, "thermal_zone"+itoa(i), "mode"))
		if err != nil || string(got) != "disabled" {
			t.Fatalf("zone %d mode = %q err %v; want disabled", i, got, err)
		}
	}
	if st := b.Status(); !st.Enabled || st.Tripped || st.ZonesDisabled != 2 {
		t.Fatalf("status after enable: %+v", st)
	}
	b.Disable()
	for i := 0; i < 2; i++ {
		got, err := os.ReadFile(filepath.Join(dir, "thermal_zone"+itoa(i), "mode"))
		if err != nil || string(got) != "enabled" {
			t.Fatalf("zone %d not restored: %q err %v", i, got, err)
		}
	}
	if b.Active() {
		t.Fatal("must be inactive after disable")
	}
}

func TestBenchThermalTripRestoresMitigation(t *testing.T) {
	dir := withThermalRoot(t)
	b := NewBenchThermalController()
	b.Enable()
	if b.TripCheck(65) {
		t.Fatal("TripCheck must not fire below the 70C gate")
	}
	if !b.TripCheck(71) {
		t.Fatal("TripCheck should fire at 71C (gate is 70C)")
	}
	if b.Active() {
		t.Fatal("must not be active after trip")
	}
	if !b.Status().Tripped {
		t.Fatal("status should show tripped")
	}
	for i := 0; i < 2; i++ {
		got, _ := os.ReadFile(filepath.Join(dir, "thermal_zone"+itoa(i), "mode"))
		if string(got) != "enabled" {
			t.Fatalf("zone %d should be restored after trip, got %q", i, got)
		}
	}
	b.Enable() // explicit re-arm after cooldown
	if !b.Active() {
		t.Fatal("re-arm must reactivate the bypass")
	}
}

func TestBenchThermalAutoRearm(t *testing.T) {
	withThermalRoot(t)
	b := NewBenchThermalController()
	b.Enable()
	if act := b.Step(40); act != "apply" {
		t.Fatalf("cool cycle = %q, want apply", act)
	}
	if act := b.Step(71); act != "trip" {
		t.Fatalf("hot cycle = %q, want trip", act)
	}
	// hysteresis: still too warm, must stay tripped
	if act := b.Step(60); act != "tripped" {
		t.Fatalf("warm cycle = %q, want tripped", act)
	}
	if act := b.Step(50); act != "rearm" {
		t.Fatalf("cooled cycle = %q, want rearm", act)
	}
	if !b.Active() {
		t.Fatal("must be active after auto re-arm")
	}
	if st := b.Status(); st.RearmC != 55 {
		t.Fatalf("rearm_c = %v, want 55", st.RearmC)
	}
}

func TestBenchThermalEndpoint(t *testing.T) {
	withThermalRoot(t)
	cfg := testCfg()
	cfg.Thermal.Bench = true
	s := NewServer(cfg, fakeCollector{safe: true})
	if !s.bench.Active() {
		t.Fatal("bench should be active from config")
	}
	if s.effectiveGateC() != benchTripC {
		t.Fatalf("effective gate in bench = %v, want %v", s.effectiveGateC(), benchTripC)
	}
	readTok := strings.Repeat("a", 64)
	radioTok := strings.Repeat("c", 64)
	if w := do(s, "GET", "/v1/thermal/bench", readTok); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatalf("GET bench: %d %s", w.Code, w.Body.String())
	}
	req := httptest.NewRequest("POST", "/v1/thermal/bench", strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Authorization", "Bearer "+radioTok)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("POST bench disable: %d %s", w.Code, w.Body.String())
	}
	if s.bench.Active() || cfg.Thermal.Bench {
		t.Fatal("bench should be off after disable")
	}
	if s.effectiveGateC() != 46 {
		t.Fatalf("gate should return to configured 46, got %v", s.effectiveGateC())
	}
	// re-enable works and lifts the gate again
	req = httptest.NewRequest("POST", "/v1/thermal/bench", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Authorization", "Bearer "+radioTok)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 || !s.bench.Active() {
		t.Fatalf("POST bench enable: %d %s", w.Code, w.Body.String())
	}
	if s.effectiveGateC() != benchTripC {
		t.Fatalf("gate should lift to %v, got %v", benchTripC, s.effectiveGateC())
	}
}
