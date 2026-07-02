package main

import (
	"os/exec"
	"strings"
	"sync"
)

// ThermalState is the 5G policy state machine. Transitions are driven by the
// hottest observed temperature relative to warn/gate thresholds, with a sticky
// COOLDOWN after a HOT reading that only clears via RECOVERY once safe again.
type ThermalState string

const (
	StateSafe     ThermalState = "SAFE"
	StateWarm     ThermalState = "WARM"
	StateHot      ThermalState = "HOT"
	StateCooldown ThermalState = "COOLDOWN"
	StateRecovery ThermalState = "RECOVERY"
)

// classify maps a temperature to the base state (no history).
func classify(worstC, warnC, gateC float64) ThermalState {
	switch {
	case worstC >= gateC:
		return StateHot
	case worstC >= warnC:
		return StateWarm
	default:
		return StateSafe
	}
}

// PolicyEngine tracks cooldown stickiness across requests.
type PolicyEngine struct {
	mu      sync.Mutex
	cooling bool
	warnC   float64
	gateC   float64
}

func NewPolicyEngine(warnC, gateC float64) *PolicyEngine {
	return &PolicyEngine{warnC: warnC, gateC: gateC}
}

// Evaluate returns the current state given a fresh worst-case temperature.
// Once HOT is seen we enter COOLDOWN and stay there until a SAFE reading, which
// yields RECOVERY (one transition) then SAFE on the next safe reading.
func (p *PolicyEngine) Evaluate(worstC float64) ThermalState {
	p.mu.Lock()
	defer p.mu.Unlock()
	base := classify(worstC, p.warnC, p.gateC)
	if base == StateHot {
		p.cooling = true
		return StateHot
	}
	if p.cooling {
		if base == StateSafe {
			// first safe reading after cooldown -> RECOVERY, then clear
			p.cooling = false
			return StateRecovery
		}
		return StateCooldown // still warm while cooling
	}
	return base
}

// SetLimits retunes the policy engine's thresholds when the owner adjusts the
// thermal gate at runtime.
func (p *PolicyEngine) SetLimits(warnC, gateC float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.warnC, p.gateC = warnC, gateC
}

// Prefer5GAllowed reports whether an NR-preference write may proceed in `st`.
// Allowed only in SAFE/WARM/RECOVERY; refused in HOT/COOLDOWN.
func Prefer5GAllowed(st ThermalState) (bool, string) {
	switch st {
	case StateSafe, StateWarm, StateRecovery:
		return true, string(st)
	default:
		return false, "refused: thermal safety gate blocked prefer5g (" + string(st) + ")"
	}
}

// applyPrefer5G runs the reversible NR-preference verb discovered in Todo 1:
// `cmd phone set-allowed-network-types-for-users`. Reading the current value
// first lets the caller restore it. Returns (before, err).
func readAllowedTypes() string {
	out, err := exec.Command("cmd", "phone", "get-allowed-network-types-for-users").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
