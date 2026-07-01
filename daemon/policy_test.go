package main

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		c    float64
		want ThermalState
	}{
		{30, StateSafe},
		{44, StateWarm},
		{45, StateWarm},
		{46, StateHot},
		{50, StateHot},
	}
	for _, tc := range cases {
		if got := classify(tc.c, 44, 46); got != tc.want {
			t.Errorf("classify(%.0f) = %s, want %s", tc.c, got, tc.want)
		}
	}
}

func TestPrefer5GAllowed(t *testing.T) {
	allow := map[ThermalState]bool{
		StateSafe: true, StateWarm: true, StateRecovery: true,
		StateHot: false, StateCooldown: false,
	}
	for st, want := range allow {
		if got, _ := Prefer5GAllowed(st); got != want {
			t.Errorf("Prefer5GAllowed(%s) = %v, want %v", st, got, want)
		}
	}
}

func TestCooldownStickiness(t *testing.T) {
	p := NewPolicyEngine(44, 46)
	if s := p.Evaluate(30); s != StateSafe {
		t.Fatalf("cool start: want SAFE, got %s", s)
	}
	if s := p.Evaluate(50); s != StateHot {
		t.Fatalf("hot: want HOT, got %s", s)
	}
	// still warm while cooling -> COOLDOWN, prefer5g refused
	if s := p.Evaluate(45); s != StateCooldown {
		t.Fatalf("cooling+warm: want COOLDOWN, got %s", s)
	}
	if ok, _ := Prefer5GAllowed(p.Evaluate(45)); ok {
		t.Fatal("prefer5g must stay refused during COOLDOWN")
	}
	// first safe reading after cooldown -> RECOVERY
	if s := p.Evaluate(30); s != StateRecovery {
		t.Fatalf("recovery: want RECOVERY, got %s", s)
	}
	// subsequent safe -> SAFE
	if s := p.Evaluate(30); s != StateSafe {
		t.Fatalf("post-recovery: want SAFE, got %s", s)
	}
}

func TestPrefer5GHandlerDeniesWhenHot(t *testing.T) {
	cfg := testCfg()
	// collector reporting a hot device
	s := NewServer(cfg, hotCollector{})
	w := do(s, "POST", "/v1/prefer5g", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	if w.Code != 409 {
		t.Fatalf("prefer5g when HOT: want 409, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestPrefer5GHandlerAllowsWhenSafe(t *testing.T) {
	cfg := testCfg()
	s := NewServer(cfg, fakeCollector{safe: true})
	w := do(s, "POST", "/v1/prefer5g", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	if w.Code != 200 {
		t.Fatalf("prefer5g when SAFE: want 200, got %d (%s)", w.Code, w.Body.String())
	}
}

type hotCollector struct{}

func (hotCollector) Health() Health { return Health{} }
func (hotCollector) Thermal(warnC, gateC float64, failClosed bool) Thermal {
	return Thermal{BatteryC: 48, MaxC: 55, WarnC: warnC, GateC: gateC, Safe: false, Source: "sysfs+battery"}
}
