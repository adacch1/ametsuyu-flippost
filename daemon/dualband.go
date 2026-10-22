package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// bounceHotspot restarts a live AP so a new band or SoftAP config takes effect.
// It is a variable so tests can replace it.
var bounceHotspot = func() {
	stopHotspot()
	time.Sleep(1500 * time.Millisecond) // let the teardown land before restart
	restartHotspotRetry()
}

// dualBandBlock reports why dual band cannot turn on. Bridged dual band stops
// Wi-Fi scanning, so it conflicts with the allowlist and preset auto-switch.
// The caller holds s.cfgMu.
func (s *Server) dualBandBlock() (msg, reason string) {
	if len(s.hs.Whitelist()) > 0 {
		return "clear the allowlist first: dual band stops Wi-Fi scanning", "whitelist"
	}
	if s.presets.AutoOn() {
		return "turn off preset auto-switch first: dual band stops Wi-Fi scanning", "auto_switch"
	}
	return "", ""
}

// applyDualBand sets the dual-band setting. It returns the hotspot status, how
// the change applies ("now" or "next_start"), an HTTP status code, and on a
// non-200 result a machine-readable reason. Disabling skips the scan checks.
func (s *Server) applyDualBand(enabled, applyNow bool) (HotspotStatus, string, int, string, error) {
	if s.airplaneBusy.Load() || s.presets.ApplyBusy() {
		return HotspotStatus{}, "", http.StatusConflict, "busy", errBusy
	}
	s.cfgMu.Lock()
	if enabled {
		if msg, reason := s.dualBandBlock(); reason != "" {
			s.cfgMu.Unlock()
			return HotspotStatus{}, "", http.StatusConflict, reason, errString(msg)
		}
	}
	prev := s.hs.DualBand()
	if s.cfgPath != "" {
		if err := persistHotspotDualBand(s.cfgPath, enabled); err != nil {
			log.Printf("hotspot dual band: persist failed: %v", err)
		}
	}
	s.cfg.Hotspot.DualBand = enabled
	s.hs.SetDualBand(enabled)
	band := "5"
	if enabled {
		band = "dual"
	}
	if _, err := s.hs.SetBand(band); err != nil {
		s.cfg.Hotspot.DualBand = prev
		s.hs.SetDualBand(prev)
		if s.cfgPath != "" {
			if perr := persistHotspotDualBand(s.cfgPath, prev); perr != nil {
				log.Printf("hotspot dual band: revert persist failed: %v", perr)
			}
		}
		s.cfgMu.Unlock()
		return s.hs.Status(), "", http.StatusBadGateway, "helper", err
	}
	s.cfgMu.Unlock()
	s.hs.RefreshAPConfig()
	applied := "next_start"
	if applyNow && s.hs.apUp() {
		bounceHotspot()
		applied = "now"
	}
	return s.hs.Status(), applied, http.StatusOK, "", nil
}

type errString string

func (e errString) Error() string { return string(e) }

const errBusy = errString("a hotspot change is already in progress")

// handleHotspotDual is POST /v1/hotspot/dual. Body: {"enabled":bool,"apply_now":bool}.
func (s *Server) handleHotspotDual(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled  bool `json:"enabled"`
		ApplyNow bool `json:"apply_now"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	st, applied, code, reason, err := s.applyDualBand(body.Enabled, body.ApplyNow)
	if err != nil {
		writeErrReason(w, code, err.Error(), reason)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dual_band": body.Enabled, "applied": applied, "hotspot": st})
}
