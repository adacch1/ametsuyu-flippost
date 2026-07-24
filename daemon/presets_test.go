package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func mustUpsert(t *testing.T, pm *PresetManager, p Preset) Preset {
	t.Helper()
	saved, err := pm.Upsert(p)
	if err != nil {
		t.Fatalf("Upsert(%v): %v", p, err)
	}
	return saved
}

// recorder is a fake applyFn: records applied preset ids, can be told to fail
// the next call (to exercise the retry path).
type recorder struct {
	mu       sync.Mutex
	calls    []string
	failNext bool
}

func (r *recorder) fn(p Preset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNext {
		r.failNext = false
		return fmt.Errorf("boom")
	}
	r.calls = append(r.calls, p.ID)
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func newTestPM() (*PresetManager, *recorder) {
	pm := NewPresetManager(HotspotPresets{}, func() string { return "" })
	rec := &recorder{}
	pm.apply = rec.fn
	return pm, rec
}

func TestValidatePreset(t *testing.T) {
	base := Preset{Name: "Home", SSID: "myssid", Passphrase: "longenough", Security: "wpa2", Band: "5"}
	if err := validatePreset(base); err != nil {
		t.Fatalf("valid preset rejected: %v", err)
	}
	bad := []Preset{
		{Name: "", SSID: "s", Passphrase: "longenough", Security: "wpa2", Band: "5"},
		{Name: "n", SSID: "", Passphrase: "longenough", Security: "wpa2", Band: "5"},
		{Name: "n", SSID: "s", Passphrase: "short", Security: "wpa2", Band: "5"},       // <8
		{Name: "n", SSID: "s", Passphrase: "longenough", Security: "wep", Band: "5"},   // bad sec
		{Name: "n", SSID: "s", Passphrase: "longenough", Security: "wpa2", Band: "60"}, // bad band
		{Name: "n\tx", SSID: "s", Passphrase: "longenough", Security: "wpa2", Band: "5"},
		{Name: "n", SSID: "s", Passphrase: "longenough", Security: "wpa2", Band: "5", Triggers: []string{""}},
	}
	for i, p := range bad {
		if err := validatePreset(p); err == nil {
			t.Errorf("bad preset %d accepted", i)
		}
	}
	// open needs no passphrase
	if err := validatePreset(Preset{Name: "guest", SSID: "s", Security: "open", Band: "2"}); err != nil {
		t.Errorf("open preset rejected: %v", err)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Home":         "home",
		"My Office 5G": "my-office-5g",
		"  café !!  ":  "caf",
		"???":          "preset",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMatchPreset(t *testing.T) {
	ps := []Preset{
		{ID: "home", Triggers: []string{"HomeAP", "HomeAP_5G"}},
		{ID: "office", Triggers: []string{"OfficeAP"}},
	}
	if id, ok := matchPreset([]string{"OfficeAP"}, ps); !ok || id != "office" {
		t.Errorf("got %q,%v want office", id, ok)
	}
	// first-match-wins: both present -> home (earlier in list)
	if id, _ := matchPreset([]string{"OfficeAP", "HomeAP"}, ps); id != "home" {
		t.Errorf("first-match: got %q want home", id)
	}
	if _, ok := matchPreset([]string{"Random"}, ps); ok {
		t.Errorf("unexpected match")
	}
}

func TestUpsertCreateEditUniqueIDs(t *testing.T) {
	pm, _ := newTestPM()
	a := mustUpsert(t, pm, Preset{Name: "Home", SSID: "s1", Passphrase: "pass1234", Security: "wpa2", Band: "5"})
	if a.ID != "home" {
		t.Fatalf("id=%q want home", a.ID)
	}
	// same name -> unique suffix
	b := mustUpsert(t, pm, Preset{Name: "Home", SSID: "s2", Passphrase: "pass1234", Security: "wpa2", Band: "5"})
	if b.ID != "home-2" {
		t.Fatalf("id=%q want home-2", b.ID)
	}
	// edit with empty passphrase inherits the stored one
	edited, err := pm.Upsert(Preset{ID: "home", Name: "Home", SSID: "s1b", Passphrase: "", Security: "wpa2", Band: "2"})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if edited.Passphrase != "pass1234" {
		t.Errorf("passphrase not inherited: %q", edited.Passphrase)
	}
	if edited.SSID != "s1b" || edited.Band != "2" {
		t.Errorf("edit not applied: %+v", edited)
	}
	// editing a missing id fails
	if _, err := pm.Upsert(Preset{ID: "nope", Name: "x", SSID: "s", Passphrase: "pass1234", Security: "wpa2", Band: "5"}); err == nil {
		t.Errorf("edit of missing id accepted")
	}
}

func TestDeleteClearsActiveAndEdge(t *testing.T) {
	pm, rec := newTestPM()
	mustUpsert(t, pm, Preset{Name: "Home", SSID: "s1", Passphrase: "pass1234", Security: "wpa2", Band: "5", Triggers: []string{"HomeAP"}})
	pm.SetAuto(true)
	pm.MaybeAutoSwitch([]string{"HomeAP"}) // active=home, lastMatch=home
	if rec.count() != 1 || pm.activeID != "home" {
		t.Fatalf("setup: calls=%d active=%q", rec.count(), pm.activeID)
	}
	if err := pm.Delete("home"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if pm.activeID != "" || pm.lastMatch != "" {
		t.Errorf("delete did not clear active/lastMatch: active=%q last=%q", pm.activeID, pm.lastMatch)
	}
	if err := pm.Delete("home"); err == nil {
		t.Errorf("double delete accepted")
	}
}

// The core behaviour: edge-triggered auto-switch that respects manual picks.
func TestMaybeAutoSwitchEdge(t *testing.T) {
	pm, rec := newTestPM()
	mustUpsert(t, pm, Preset{Name: "Home", SSID: "s1", Passphrase: "pass1234", Security: "wpa2", Band: "5", Triggers: []string{"HomeAP"}})
	mustUpsert(t, pm, Preset{Name: "Office", SSID: "s2", Passphrase: "pass1234", Security: "wpa2", Band: "5", Triggers: []string{"OfficeAP"}})

	// auto OFF: no switching
	pm.MaybeAutoSwitch([]string{"HomeAP"})
	if rec.count() != 0 {
		t.Fatalf("switched while auto off")
	}

	pm.SetAuto(true)
	pm.MaybeAutoSwitch([]string{"HomeAP"}) // -> home (1)
	pm.MaybeAutoSwitch([]string{"HomeAP"}) // same zone, no-op (1)
	if rec.count() != 1 || pm.activeID != "home" {
		t.Fatalf("home: calls=%d active=%q", rec.count(), pm.activeID)
	}
	pm.MaybeAutoSwitch([]string{"OfficeAP"}) // transition -> office (2)
	if rec.count() != 2 || pm.activeID != "office" {
		t.Fatalf("office: calls=%d active=%q", rec.count(), pm.activeID)
	}
	pm.MaybeAutoSwitch([]string{"Unknown"})  // no match, resets lastMatch (2)
	pm.MaybeAutoSwitch([]string{"OfficeAP"}) // back, but office already active -> no-op (2)
	if rec.count() != 2 {
		t.Fatalf("redundant re-apply: calls=%d", rec.count())
	}

	// manual override sticks while in the same zone
	if _, err := pm.Apply("home"); err != nil { // manual (3), active=home
		t.Fatalf("manual apply: %v", err)
	}
	if rec.count() != 3 || pm.activeID != "home" {
		t.Fatalf("manual: calls=%d active=%q", rec.count(), pm.activeID)
	}
	pm.MaybeAutoSwitch([]string{"OfficeAP"}) // physically still office (lastMatch=office) -> no-op
	if rec.count() != 3 {
		t.Fatalf("auto fought manual: calls=%d", rec.count())
	}
	// leaving to home zone resumes auto
	pm.MaybeAutoSwitch([]string{"HomeAP"}) // office->home transition, but home already active -> no-op, lastMatch=home
	pm.MaybeAutoSwitch([]string{"OfficeAP"})
	if pm.activeID != "office" || rec.count() != 4 {
		t.Fatalf("resume: calls=%d active=%q", rec.count(), pm.activeID)
	}
}

func TestMaybeAutoSwitchRetryOnFailure(t *testing.T) {
	pm, rec := newTestPM()
	mustUpsert(t, pm, Preset{Name: "Home", SSID: "s1", Passphrase: "pass1234", Security: "wpa2", Band: "5", Triggers: []string{"HomeAP"}})
	pm.SetAuto(true)
	rec.failNext = true
	pm.MaybeAutoSwitch([]string{"HomeAP"}) // fails, lastMatch NOT advanced
	if rec.count() != 0 || pm.activeID != "" || pm.lastMatch != "" {
		t.Fatalf("failed apply advanced state: calls=%d active=%q last=%q", rec.count(), pm.activeID, pm.lastMatch)
	}
	pm.MaybeAutoSwitch([]string{"HomeAP"}) // retry succeeds
	if rec.count() != 1 || pm.activeID != "home" {
		t.Fatalf("retry: calls=%d active=%q", rec.count(), pm.activeID)
	}
}

func TestStatusRedactedHidesPassphrase(t *testing.T) {
	pm, _ := newTestPM()
	mustUpsert(t, pm, Preset{Name: "Home", SSID: "s1", Passphrase: "secret-pass", Security: "wpa2", Band: "5"})
	st := pm.statusRedacted()
	b, _ := json.Marshal(st)
	if wantAbsent := "secret-pass"; contains(string(b), wantAbsent) {
		t.Fatalf("passphrase leaked in status: %s", b)
	}
	list := st["presets"].([]map[string]any)
	if hp, _ := list[0]["has_pass"].(bool); !hp {
		t.Errorf("has_pass should be true")
	}
	if _, ok := list[0]["passphrase"]; ok {
		t.Errorf("passphrase key present in redacted status")
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestPersistHotspotPresetsRoundTripPreservesKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// A config with a sibling key the presets writer must not drop.
	seed := map[string]any{
		"bind_host": "127.0.0.1",
		"hotspot":   map[string]any{"ssid_whitelist": []string{"Home"}},
	}
	b, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	hp := HotspotPresets{
		Active:     "home",
		AutoSwitch: true,
		Presets:    []Preset{{ID: "home", Name: "Home", SSID: "s", Passphrase: "pass1234", Security: "wpa2", Band: "5", Triggers: []string{"HomeAP"}}},
	}
	if err := persistHotspotPresets(path, hp); err != nil {
		t.Fatalf("persist: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if _, ok := m["bind_host"]; !ok {
		t.Errorf("sibling bind_host dropped")
	}
	if _, ok := m["hotspot"]; !ok {
		t.Errorf("sibling hotspot dropped")
	}
	hpm, ok := m["hotspot_presets"].(map[string]any)
	if !ok {
		t.Fatalf("hotspot_presets missing/wrong type")
	}
	if hpm["active"] != "home" || hpm["auto_switch"] != true {
		t.Errorf("preset state not round-tripped: %v", hpm)
	}
}
