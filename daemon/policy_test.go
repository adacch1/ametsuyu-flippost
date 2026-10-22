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

func TestCooldownStickiness(t *testing.T) {
	p := NewPolicyEngine(44, 46)
	if s := p.Evaluate(30); s != StateSafe {
		t.Fatalf("cool start: want SAFE, got %s", s)
	}
	if s := p.Evaluate(50); s != StateHot {
		t.Fatalf("hot: want HOT, got %s", s)
	}
	// still warm while cooling -> COOLDOWN
	if s := p.Evaluate(45); s != StateCooldown {
		t.Fatalf("cooling+warm: want COOLDOWN, got %s", s)
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
