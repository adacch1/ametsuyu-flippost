package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Hotspot presets: named SoftAP configurations (SSID / passphrase / security /
// band) the owner can switch to, manually or automatically by Wi-Fi fingerprint.
// Applying one pushes the config through the SetSoftApConfig root helper and,
// if the hotspot is up, bounces it so the new SSID takes effect.
//
// Auto-switch is EDGE-TRIGGERED: a preset is applied only when the matched
// preset changes (a transition into a new "place"), never re-applied while the
// device stays put — so a manual pick is not fought by the scanner until you
// physically move to another trigger zone.

const (
	maxPresets    = 12
	maxTriggers   = 16
	ssidMaxBytes  = 32 // Wi-Fi SSID hard limit
	passMinLen    = 8  // WPA2/WPA3 PSK bounds
	passMaxLen    = 63
	presetNameMax = 32
)

var errApplyBusy = errors.New("a preset apply is already in progress")

// Preset is one saved hotspot configuration. Passphrase is stored plaintext
// (same as the SoftAP config store already is) and is NEVER emitted in the
// read-status API — see statusRedacted.
type Preset struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	SSID       string   `json:"ssid"`
	Passphrase string   `json:"passphrase"`
	Security   string   `json:"security"` // open | wpa2 | wpa3
	Band       string   `json:"band"`     // 2 | 5 | 6 | dual (bridged 2.4+5, one SSID)
	Triggers   []string `json:"triggers"` // SSIDs whose presence means "apply me"
}

// HotspotPresets is the persisted preset state (config.json "hotspot_presets").
type HotspotPresets struct {
	Active     string   `json:"active"`
	AutoSwitch bool     `json:"auto_switch"`
	Presets    []Preset `json:"presets"`
}

// validatePreset enforces the field bounds. For an edit that keeps the existing
// passphrase, the caller inherits it BEFORE validating.
func validatePreset(p Preset) error {
	if p.Name == "" || len(p.Name) > presetNameMax || strings.ContainsAny(p.Name, "\t\r\n") {
		return fmt.Errorf("name must be 1-%d chars", presetNameMax)
	}
	if p.SSID == "" || len(p.SSID) > ssidMaxBytes || strings.ContainsAny(p.SSID, "\t\r\n") {
		return fmt.Errorf("ssid must be 1-%d bytes", ssidMaxBytes)
	}
	switch p.Security {
	case "open":
	case "wpa2", "wpa3":
		if len(p.Passphrase) < passMinLen || len(p.Passphrase) > passMaxLen {
			return fmt.Errorf("passphrase must be %d-%d chars", passMinLen, passMaxLen)
		}
	default:
		return fmt.Errorf("security must be open, wpa2, or wpa3")
	}
	switch p.Band {
	case "2", "5", "6", "dual":
	default:
		return fmt.Errorf("band must be 2, 5, 6, or dual")
	}
	if len(p.Triggers) > maxTriggers {
		return fmt.Errorf("too many triggers (max %d)", maxTriggers)
	}
	for _, t := range p.Triggers {
		if t == "" || len(t) > ssidMaxBytes || strings.ContainsAny(t, "\t\r\n") {
			return fmt.Errorf("invalid trigger ssid")
		}
	}
	return nil
}

// slugify turns a preset name into a URL-safe id stem (lowercase, [a-z0-9-]).
func slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "preset"
	}
	return s
}

// matchPreset returns the id of the first preset (in list order) whose triggers
// intersect the seen SSIDs, and whether any matched. First-match-wins keeps the
// behaviour predictable: order the presets to prioritise overlapping triggers.
func matchPreset(seen []string, presets []Preset) (string, bool) {
	seenSet := make(map[string]bool, len(seen))
	for _, s := range seen {
		seenSet[s] = true
	}
	for _, p := range presets {
		for _, t := range p.Triggers {
			if seenSet[t] {
				return p.ID, true
			}
		}
	}
	return "", false
}

// PresetManager owns preset state, thread-safe. Device I/O (the helper call +
// AP bounce) runs under applyMu and NEVER while holding mu, so status reads and
// edits stay responsive during a multi-second apply.
type PresetManager struct {
	mu         sync.Mutex
	presets    []Preset
	activeID   string
	autoSwitch bool
	lastMatch  string        // id of last auto-matched preset (edge-trigger state)
	cfgPath    func() string // resolved at persist time (main sets cfgPath after NewServer)
	apply      func(Preset) error

	applyMu sync.Mutex // serializes device applies (manual vs auto)
}

func NewPresetManager(hp HotspotPresets, cfgPath func() string) *PresetManager {
	return &PresetManager{
		presets:    append([]Preset(nil), hp.Presets...),
		activeID:   hp.Active,
		autoSwitch: hp.AutoSwitch,
		cfgPath:    cfgPath,
		apply:      applyPresetLive,
	}
}

func (pm *PresetManager) findLocked(id string) (Preset, bool) {
	for _, p := range pm.presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

func (pm *PresetManager) uniqueIDLocked(base string) string {
	exists := func(id string) bool {
		_, ok := pm.findLocked(id)
		return ok
	}
	if !exists(base) {
		return base
	}
	for n := 2; ; n++ {
		if cand := base + "-" + itoa(n); !exists(cand) {
			return cand
		}
	}
}

func (pm *PresetManager) snapshotLocked() HotspotPresets {
	return HotspotPresets{
		Active:     pm.activeID,
		AutoSwitch: pm.autoSwitch,
		Presets:    append([]Preset(nil), pm.presets...),
	}
}

func (pm *PresetManager) persistLocked() {
	if pm.cfgPath == nil {
		return
	}
	path := pm.cfgPath()
	if path == "" {
		return
	}
	if err := persistHotspotPresets(path, pm.snapshotLocked()); err != nil {
		log.Printf("presets: persist failed: %v", err)
	}
}

// statusRedacted is the API view: passphrases are replaced with a has_pass flag
// so a tokenless (open-reads) tailnet reader can't harvest hotspot passwords.
func (pm *PresetManager) statusRedacted() map[string]any {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	list := make([]map[string]any, 0, len(pm.presets))
	for _, p := range pm.presets {
		triggers := p.Triggers
		if triggers == nil {
			triggers = []string{}
		}
		list = append(list, map[string]any{
			"id": p.ID, "name": p.Name, "ssid": p.SSID,
			"security": p.Security, "band": p.Band,
			"triggers": triggers, "has_pass": p.Passphrase != "",
		})
	}
	return map[string]any{"active": pm.activeID, "auto_switch": pm.autoSwitch, "presets": list}
}

func (pm *PresetManager) AutoOn() bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.autoSwitch
}

// Upsert creates a preset (empty ID) or edits one (existing ID). On edit, an
// empty passphrase inherits the stored one (so the UI never has to echo secrets).
func (pm *PresetManager) Upsert(in Preset) (Preset, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.SSID = strings.TrimSpace(in.SSID)
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if in.ID != "" {
		for i := range pm.presets {
			if pm.presets[i].ID == in.ID {
				if in.Passphrase == "" && in.Security != "open" {
					in.Passphrase = pm.presets[i].Passphrase // keep existing
				}
				if err := validatePreset(in); err != nil {
					return Preset{}, err
				}
				pm.presets[i] = in
				pm.persistLocked()
				return in, nil
			}
		}
		return Preset{}, fmt.Errorf("no preset with id %q", in.ID)
	}
	if err := validatePreset(in); err != nil {
		return Preset{}, err
	}
	if len(pm.presets) >= maxPresets {
		return Preset{}, fmt.Errorf("too many presets (max %d)", maxPresets)
	}
	in.ID = pm.uniqueIDLocked(slugify(in.Name))
	pm.presets = append(pm.presets, in)
	pm.persistLocked()
	return in, nil
}

func (pm *PresetManager) Delete(id string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i := range pm.presets {
		if pm.presets[i].ID == id {
			pm.presets = append(pm.presets[:i], pm.presets[i+1:]...)
			if pm.activeID == id {
				pm.activeID = ""
			}
			if pm.lastMatch == id {
				pm.lastMatch = ""
			}
			pm.persistLocked()
			return nil
		}
	}
	return fmt.Errorf("no preset with id %q", id)
}

// Apply pushes a preset to the SoftAP now (manual). Serialized: returns
// errApplyBusy if another apply (manual or auto) is already running.
func (pm *PresetManager) Apply(id string) (Preset, error) {
	if !pm.applyMu.TryLock() {
		return Preset{}, errApplyBusy
	}
	defer pm.applyMu.Unlock()
	pm.mu.Lock()
	p, found := pm.findLocked(id)
	applyFn := pm.apply
	pm.mu.Unlock()
	if !found {
		return Preset{}, fmt.Errorf("no preset with id %q", id)
	}
	if err := applyFn(p); err != nil {
		return Preset{}, err
	}
	pm.mu.Lock()
	pm.activeID = id
	pm.persistLocked()
	pm.mu.Unlock()
	return p, nil
}

func (pm *PresetManager) SetAuto(on bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.autoSwitch = on
	if !on {
		pm.lastMatch = "" // reset edge state so re-enabling re-triggers on the next match
	}
	pm.persistLocked()
}

// MaybeAutoSwitch applies the preset whose triggers newly appear in a scan.
// Edge-triggered (acts only on a transition); no-op when auto is off, no preset
// matches, or the matched preset is already active. On a busy/failed apply it
// leaves lastMatch un-advanced so the next scan retries.
func (pm *PresetManager) MaybeAutoSwitch(seen []string) {
	pm.mu.Lock()
	if !pm.autoSwitch {
		pm.mu.Unlock()
		return
	}
	id, ok := matchPreset(seen, pm.presets)
	if !ok {
		pm.lastMatch = "" // left every known zone; re-entering one re-triggers
		pm.mu.Unlock()
		return
	}
	if id == pm.lastMatch { // still in the same zone; no transition
		pm.mu.Unlock()
		return
	}
	if id == pm.activeID { // transitioned, but this preset is already applied
		pm.lastMatch = id
		pm.mu.Unlock()
		return
	}
	p, _ := pm.findLocked(id)
	applyFn := pm.apply
	pm.mu.Unlock()

	if !pm.applyMu.TryLock() {
		return // an apply is already running; retry next scan (lastMatch not advanced)
	}
	defer pm.applyMu.Unlock()
	if err := applyFn(p); err != nil {
		log.Printf("presets: auto-switch to %q failed: %v", id, err)
		return
	}
	pm.mu.Lock()
	pm.activeID = id
	pm.lastMatch = id
	pm.persistLocked()
	pm.mu.Unlock()
	log.Printf("presets: auto-switched to %q (%s)", p.Name, id)
}

// helperResultLine pulls the RESULT= line out of the helper's stdout.
func helperResultLine(out string) string {
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "RESULT=") {
			return strings.TrimSpace(ln)
		}
	}
	return strings.TrimSpace(out)
}

// applyPresetLive pushes a preset's config to the SoftAP via the root helper,
// then bounces the AP if it's up so the new SSID/passphrase takes effect
// (clients drop for a few seconds — unavoidable when renaming a live network).
func applyPresetLive(p Preset) error {
	sec := p.Security
	if sec == "" {
		sec = "wpa2"
	}
	band := p.Band
	if band == "" {
		band = "5"
	}
	out := runHelper("com.zflip5.tether.SetSoftApConfig", "set", p.SSID, sec, p.Passphrase, band)
	if !strings.Contains(out, "RESULT=OK") {
		return fmt.Errorf("set softap config: %s", helperResultLine(out))
	}
	if hotspotActive() {
		stopHotspot()
		time.Sleep(1500 * time.Millisecond) // let the teardown land before restart
		restartHotspotRetry()
	}
	return nil
}
