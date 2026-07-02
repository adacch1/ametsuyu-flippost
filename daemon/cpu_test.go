package main

import "testing"

func TestDecideMode(t *testing.T) {
	cases := []struct {
		req      string
		clients  int
		safe     bool
		wantMode string
	}{
		{"auto", 2, true, "performance"},        // in use + cool -> speed
		{"auto", 0, true, "eco"},                // idle + cool -> battery
		{"auto", 5, false, "eco"},               // hot ALWAYS reduces, even with clients
		{"performance", 0, true, "performance"}, // fixed
		{"performance", 0, false, "eco"},        // hot overrides a fixed performance request
		{"eco", 9, true, "eco"},
		{"off", 3, true, "off"},
		{"off", 3, false, "off"}, // off stays off (owner opted out entirely)
	}
	for _, c := range cases {
		got, _ := decideMode(c.req, c.clients, c.safe)
		if got != c.wantMode {
			t.Errorf("decideMode(%q,%d,%v)=%q want %q", c.req, c.clients, c.safe, got, c.wantMode)
		}
	}
}

func TestNewServerClampsThermalAtLoad(t *testing.T) {
	// A hand-edited / restored config with a disabled-wide gate must be clamped
	// to the ceiling at load, not just on the runtime retune path.
	cfg := testCfg()
	cfg.Thermal.WarnC, cfg.Thermal.GateC = 80, 90
	s := NewServer(cfg, fakeCollector{safe: true})
	if s.gateC() > gateCeilingC {
		t.Errorf("gate not clamped at load: got %v want <= %v", s.gateC(), gateCeilingC)
	}
	if s.warnC() >= s.gateC() {
		t.Errorf("warn must stay below gate: warn=%v gate=%v", s.warnC(), s.gateC())
	}
}

func TestSetThermalLimitsClamp(t *testing.T) {
	cfg := testCfg()
	cfg.Thermal.WarnC, cfg.Thermal.GateC, cfg.Thermal.FailClosed = 44, 46, true
	s := NewServer(cfg, fakeCollector{safe: true}) // cfgPath empty -> no persist

	// Gate above the hard ceiling is clamped; the gate can be retuned but never disabled.
	w, g := s.setThermalLimits(45, 60)
	if g != gateCeilingC {
		t.Errorf("gate not clamped to ceiling: got %v", g)
	}
	if w >= g {
		t.Errorf("warn must stay below gate: warn=%v gate=%v", w, g)
	}

	// Warn below floor is raised; warn kept below gate.
	w, g = s.setThermalLimits(10, 46)
	if w < warnFloorC {
		t.Errorf("warn not floored: %v", w)
	}

	// A sane retune passes through unchanged.
	w, g = s.setThermalLimits(42, 45)
	if w != 42 || g != 45 {
		t.Errorf("sane values altered: warn=%v gate=%v", w, g)
	}
	if s.warnC() != 42 || s.gateC() != 45 {
		t.Errorf("atomics not updated: %v/%v", s.warnC(), s.gateC())
	}
}
