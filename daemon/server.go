package main

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// usagePath is where the data-usage buckets persist; main points it at the
// config dir. Tests override it to a temp file.
var usagePath = "/data/adb/zflip5-modem/usage.json"

type Server struct {
	cfg     *Config
	cfgPath string
	col     Collector
	mux     *http.ServeMux
	rl      *rateLimiter
	pol     *PolicyEngine
	usage   *UsageTracker
	cpu     *CPUController
	hs      *HotspotController
	presets *PresetManager
	bench   *BenchThermalController
	// smooth serves a median-of-N display value for handleStatus only; the
	// gate (col.Thermal) never reads it. Zero value is usable (empty ring ->
	// median() reports !ok -> handleStatus falls back to raw), so NewServer
	// needs no change and the test suite never spawns a sysfs sampler.
	smooth thermalSmoother
	// temps records s.smooth.median (display path) once a minute into
	// persisted hour/day averages for the dashboard's history chart. It
	// reads ONLY the smoother — never col.Thermal / readThermalZones (the
	// gate path), which this task must not touch.
	temps *TempHistory
	// Adjustable thermal limits (atomic float bits) so the owner can retune the
	// gate at runtime; seeded from config, clamped to a safe range on write.
	warnBits atomic.Uint64
	gateBits atomic.Uint64
	// airplaneBusy serializes /v1/airplane so overlapping cycles/toggles can't
	// interleave airplane on/off.
	airplaneBusy atomic.Bool
	// openReads: when true, read-status AND sms GETs need no token (tailnet
	// convenience; the owner opted the inbox in on this donor phone).
	// Seeded from config, toggled at runtime. Reads only; never radio writes.
	openReads atomic.Bool
	// openControl: when true, radio-control WRITES need no token either (owner's
	// tailnet-only, app-less device). Writes only. Off by default.
	openControl atomic.Bool
	// speedtestBusy: only one speedtest at a time (each is heavy on data + heat).
	speedtestBusy atomic.Bool
	// coverAccent: the cover screen's accent name, seeded from config and set
	// from the control panel. atomic.Value because the kiosk reads it on every
	// page load while a POST can rewrite it.
	coverAccent atomic.Value
	// cfgMu serializes the two config-mutating handlers (thermal limits, hotspot
	// whitelist): they read-modify-write both s.cfg fields and config.json, so
	// concurrent POSTs would otherwise race and lose updates.
	cfgMu sync.Mutex
}

// clampThermal enforces the hard safety envelope on the gate: it can be retuned
// but never disabled. gate is capped at gateCeilingC and kept above warn; warn
// is floored at warnFloorC and kept below gate. Used both at config load and on
// every runtime retune so a hand-edited config.json can't widen the gate.
func clampThermal(warn, gate float64) (float64, float64) {
	if gate > gateCeilingC {
		gate = gateCeilingC
	}
	if gate < warnFloorC+1 {
		gate = warnFloorC + 1
	}
	if warn < warnFloorC {
		warn = warnFloorC
	}
	if warn >= gate {
		warn = gate - 1
	}
	return warn, gate
}

// Safety clamps for the adjustable thermal gate. The gate can be RETUNED but
// never disabled: it is hard-capped so the app-level protection stays real, and
// Samsung's own thermal mitigation runs regardless.
const (
	gateCeilingC = 48.0 // absolute max battery/skin gate we will ever accept
	warnFloorC   = 30.0
)

func NewServer(cfg *Config, col Collector) *Server {
	// Clamp at load: the gateCeilingC envelope must hold even if config.json was
	// hand-edited or restored with a wide/disabled gate — not only on the
	// runtime retune path.
	warn, gate := clampThermal(cfg.Thermal.WarnC, cfg.Thermal.GateC)
	cfg.Thermal.WarnC, cfg.Thermal.GateC = warn, gate
	s := &Server{cfg: cfg, col: col, mux: http.NewServeMux(), rl: newRateLimiter(),
		pol: NewPolicyEngine(warn, gate), usage: NewUsageTracker(usagePath),
		cpu: NewCPUController(), hs: NewHotspotController(cfg.Hotspot.SSIDWhitelist)}
	// s.smooth is addressable (s is a pointer), so this method value binds to
	// &s.smooth: temps.go's only reader, forever the display path.
	s.temps = NewTempHistory(tempsPath, s.smooth.median)
	// Quota: clamp at load exactly like the thermal gate above — a bad
	// hand-edited value must never fail Validate (headless device).
	q, err := normalizeQuota(cfg.Quota)
	if err != nil {
		log.Printf("quota: %v (clamped)", err)
	}
	cfg.Quota = q
	s.usage.SetQuota(q)
	s.warnBits.Store(math.Float64bits(warn))
	s.gateBits.Store(math.Float64bits(gate))
	s.coverAccent.Store(normalizeCoverAccent(cfg.Cover.Accent))
	s.openReads.Store(cfg.Dashboard.OpenReads)
	s.openControl.Store(cfg.Dashboard.OpenControl)
	// cfgPath is set on s AFTER NewServer (see main), so resolve it lazily.
	s.presets = NewPresetManager(cfg.HotspotPresets, func() string { return s.cfgPath })
	s.bench = NewBenchThermalController()
	if cfg.Thermal.Bench {
		s.bench.Enable()
	}
	s.syncPolicyLimits()
	s.routes()
	return s
}

func (s *Server) warnC() float64 { return math.Float64frombits(s.warnBits.Load()) }
func (s *Server) gateC() float64 { return math.Float64frombits(s.gateBits.Load()) }

// Effective limits: bench mode (explicit opt-in, battery-less donor hardware)
// lifts the app gate to benchTripC while active. The stored clamp is untouched,
// so the moment bench drops (disable or critical trip) the normal 48C ceiling
// returns.
func (s *Server) effectiveWarnC() float64 {
	if s.bench != nil && s.bench.Active() {
		return benchWarnC
	}
	return s.warnC()
}

func (s *Server) effectiveGateC() float64 {
	if s.bench != nil && s.bench.Active() {
		return benchTripC
	}
	return s.gateC()
}

// syncPolicyLimits keeps the sticky policy engine aligned with the effective
// limits (bench lifts them; disable/trip restores the configured values).
func (s *Server) syncPolicyLimits() {
	s.pol.SetLimits(s.effectiveWarnC(), s.effectiveGateC())
}

// setThermalLimits clamps, applies, and persists new thermal thresholds. Returns
// the effective (possibly clamped) values. gate is capped at gateCeilingC and
// must stay above warn; warn floored at warnFloorC and kept below gate.
func (s *Server) setThermalLimits(warn, gate float64) (float64, float64) {
	warn, gate = clampThermal(warn, gate)
	s.warnBits.Store(math.Float64bits(warn))
	s.gateBits.Store(math.Float64bits(gate))
	s.cfg.Thermal.WarnC, s.cfg.Thermal.GateC = warn, gate
	s.pol.SetLimits(warn, gate)
	s.syncPolicyLimits()
	if s.cfgPath != "" {
		if err := persistThermalLimits(s.cfgPath, warn, gate); err != nil {
			log.Printf("thermal limits: persist failed: %v", err)
		}
	}
	return warn, gate
}

// runCPUPolicy is the background loop that retunes cores every 30s from live
// state (client count + thermal). Safe: when hot it only ever reduces.
func (s *Server) runCPUPolicy() {
	for {
		s.applyCPUPolicyOnce()
		time.Sleep(30 * time.Second)
	}
}

// applyCPUPolicyOnce enforces the configured CPU mode against live thermal
// state. Shared by the background loop and the /v1/cpu/mode write handler so a
// manual mode change takes effect immediately.
func (s *Server) applyCPUPolicyOnce() {
	requested := s.cfg.CPUMode()
	if requested == "off" {
		s.cpu.restoreAll()
		return
	}
	t := s.col.Thermal(s.effectiveWarnC(), s.effectiveGateC(), s.cfg.Thermal.FailClosed)
	clients := deviceClients().Count
	mode, _ := decideMode(requested, clients, t.Safe)
	s.cpu.apply(mode)
}

// runHotspotAuto is the background SSID-whitelist loop. Not thermal-gated: the
// hotspot is the modem's primary function (see decideHotspot). The cadence is
// adaptive — fast when the hotspot is off, gentle when it's on.
func (s *Server) runHotspotAuto() {
	for {
		s.hs.step()
		// Preset auto-switch (Wi-Fi fingerprint) rides the same scan. step() only
		// scans when the whitelist is non-empty, so when presets need a scan and
		// the whitelist is empty, do a scan-only refresh first.
		if s.presets.AutoOn() {
			if len(s.hs.Whitelist()) == 0 {
				s.hs.Scan(time.Now().Format(time.RFC3339))
			}
			s.presets.MaybeAutoSwitch(s.hs.LastSeenSSIDs())
		}
		d := hotspotScanIdle
		if hotspotActive() {
			d = hotspotScanActive
		}
		// Wake early at an override's deadline so "exactly that long" holds: the
		// tick right after expiry runs normal whitelist logic (stop, if a
		// whitelisted SSID is in range) instead of waiting out the full cadence.
		if left := s.hs.OverrideLeft(); left > 0 && left < d {
			d = left
		}
		time.Sleep(d)
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("/v1/status", s.guard("read-status", s.handleStatus))
	s.mux.HandleFunc("/v1/health", s.guard("read-status", s.handleHealth))
	s.mux.HandleFunc("/v1/thermal", s.guard("read-status", s.handleThermal))
	// Minute/hour/day temperature history, sourced from s.smooth (display
	// path) only — see TempHistory's doc comment. Never gates anything.
	s.mux.HandleFunc("/v1/thermal/history", s.guard("read-status", s.handleThermalHistory))
	s.mux.HandleFunc("/v1/network", s.guard("read-status", s.handleNetwork))
	s.mux.HandleFunc("/v1/battery", s.guard("read-status", s.handleBattery))
	s.mux.HandleFunc("/v1/usage", s.guard("read-status", s.handleUsage))
	// Quota config + manual reset: config/meter writes, not radio actions, so no
	// thermal gate — but still radio-control, and rate-limited like every other
	// write (radio_per_min).
	s.mux.HandleFunc("/v1/usage/quota", s.guardAuth("radio-control", http.MethodPost, s.handleUsageQuota))
	s.mux.HandleFunc("/v1/usage/reset", s.guardAuth("radio-control", http.MethodPost, s.handleUsageReset))
	s.mux.HandleFunc("/v1/signal", s.guard("read-status", s.handleSignal))
	s.mux.HandleFunc("/v1/clients", s.guard("read-status", s.handleClients))
	s.mux.HandleFunc("/v1/bands", s.guard("read-status", s.handleBands))
	s.mux.HandleFunc("/v1/cpu", s.guard("read-status", s.handleCPU))
	// CPU policy mode: radio-control write, not thermal-gated (eco/off must
	// work while hot; performance is only raise-if-cool via decideMode).
	s.mux.HandleFunc("/v1/cpu/mode", s.guardAuth("radio-control", http.MethodPost, s.handleCPUMode))
	s.mux.HandleFunc("/v1/hotspot", s.guard("read-status", s.handleHotspot))
	s.mux.HandleFunc("/v1/usbtether", s.guard("read-status", s.handleUsbTether))
	// Whitelist edits are config writes, not radio actions: no thermal gate
	// (and clearing the list must work while hot to stop auto-starts).
	s.mux.HandleFunc("/v1/hotspot/whitelist", s.guardAuth("radio-control", http.MethodPost, s.handleHotspotWhitelist))
	// On-demand scan-only refresh of the nearby-networks list (radio-control:
	// it drives the radio off-channel briefly and is rate-limited like a write).
	s.mux.HandleFunc("/v1/hotspot/scan", s.guardAuth("radio-control", http.MethodPost, s.handleHotspotScan))
	// Timed force-on override: a policy write like the whitelist above — not
	// thermal-gated. The start it can trigger is the auto loop's existing
	// ungated startHotspot() path (same one /v1/tether uses), so this adds no
	// new bypass, only a bounded (<=24h) window where the whitelist is ignored.
	s.mux.HandleFunc("/v1/hotspot/override", s.guardAuth("radio-control", http.MethodPost, s.handleHotspotOverride))
	// Hotspot presets: GET lists (read-status, passphrases redacted), POST upserts
	// (radio-control). Method-dispatched so the two scopes coexist on one path.
	s.mux.HandleFunc("/v1/presets", s.handlePresets)
	s.mux.HandleFunc("/v1/presets/apply", s.guardAuth("radio-control", http.MethodPost, s.handlePresetApply))
	s.mux.HandleFunc("/v1/presets/delete", s.guardAuth("radio-control", http.MethodPost, s.handlePresetDelete))
	s.mux.HandleFunc("/v1/presets/auto", s.guardAuth("radio-control", http.MethodPost, s.handlePresetAuto))
	// Speedtest: in-process Ookla test. radio-control (heavy: data + heat); one
	// at a time; blocks ~15-40s.
	s.mux.HandleFunc("/v1/speedtest", s.guardAuth("radio-control", http.MethodPost, s.handleSpeedtest))
	// Airplane trigger + IP-rotation cycle. Airplane on/off always works (turning
	// the radio OFF must work while hot); a fresh hotspot start ("off" mode) is
	// thermal-gated like /v1/tether, while the cycle restores a pre-existing
	// hotspot regardless (status quo). Serialized via airplaneBusy.
	s.mux.HandleFunc("/v1/airplane", s.guardAuth("radio-control", http.MethodPost, s.handleAirplane))
	// Retune the thermal gate: radio-control write, but NOT thermal-gated itself
	// (you must be able to adjust limits while hot), and hard-clamped server-side.
	s.mux.HandleFunc("/v1/thermal/limits", s.guardAuth("radio-control", http.MethodPost, s.handleThermalLimits))
	// Bench thermal mode: GET (read-status) reports state; POST (radio-control)
	// enables/disables the battery-less-donor bypass. Never thermally gated —
	// enabling it while hot is the point.
	s.mux.HandleFunc("/v1/thermal/bench", s.dispatchThermalBench)
	// HTML (no data without a token; the pages fetch /v1/* themselves). The
	// kiosk WebView loads "/" with no path of its own, so the cover screen owns
	// the root and the full control panel moved one path down. handleCover is
	// also the 404 catch-all for every unmatched path.
	s.mux.HandleFunc("/", s.handleCover)
	s.mux.HandleFunc("/dashboard", s.handleDashboard)
	// PWA: manifest + service worker so the dashboard installs to the home screen.
	// Static, no token (they carry no device data).
	s.mux.HandleFunc("/manifest.webmanifest", s.handleManifest)
	s.mux.HandleFunc("/sw.js", s.handleServiceWorker)
	s.mux.HandleFunc("/logo.png", s.handleIcon)
	// QR of "<this dashboard's origin>/?token=<read-status>" so a new device
	// scans instead of typing the token. read-status guarded (you must already
	// be able to read to share access).
	s.mux.HandleFunc("/v1/qr", s.guard("read-status", s.handleQR))
	// Toggle open-reads (tokenless read-status over the tailnet). radio-control.
	s.mux.HandleFunc("/v1/dashboard/open", s.guardAuth("radio-control", http.MethodPost, s.handleDashboardOpen))
	// Toggle open-control (tokenless radio-control writes). radio-control gated —
	// to ENABLE it you still need the token once (it starts off).
	s.mux.HandleFunc("/v1/dashboard/control", s.guardAuth("radio-control", http.MethodPost, s.handleDashboardControl))
	s.mux.HandleFunc("/v1/sms/recent", s.guard("sms", s.handleSMSRecent))
	// Active notifications. Same scope, gate and rate limit as SMS: identical
	// asset class (private content, one-time codes), so it never gets a cheaper
	// door than the inbox it mirrors.
	s.mux.HandleFunc("/v1/notifications/recent", s.guard("sms", s.handleNotificationsRecent))
	// tether/cooldown/restart make their own thermal decision (stopping the
	// hotspot or cooling down must work WHILE hot), so they take the auth-only
	// guard instead of guardWrite's blanket "refuse when unsafe" gate.
	s.mux.HandleFunc("/v1/tether", s.guardAuth("radio-control", http.MethodPost, s.handleTether))
	// Same shape as /v1/tether but for USB tethering: no thermal gate (a USB
	// cable's data path isn't a heat source), auth-only guard.
	s.mux.HandleFunc("/v1/usbtether/toggle", s.guardAuth("radio-control", http.MethodPost, s.handleUsbTetherToggle))
	// prefer5g uses the policy engine (incl. cooldown stickiness), so it takes the
	// auth-only guard and makes its own thermal decision in the handler.
	s.mux.HandleFunc("/v1/prefer5g", s.guardAuth("radio-control", http.MethodPost, s.handlePrefer5G))
	s.mux.HandleFunc("/v1/cooldown", s.guardAuth("radio-control", http.MethodPost, s.handleCooldown))
	s.mux.HandleFunc("/v1/service/restart", s.guardAuth("radio-control", http.MethodPost, s.handleRestart))
	// Full DEVICE reboot (not just the daemon). radio-control; the module's
	// service.sh brings the daemon + hotspot back on boot.
	s.mux.HandleFunc("/v1/device/reboot", s.guardAuth("radio-control", http.MethodPost, s.handleDeviceReboot))
	// Cover home: kiosk on the Flex Window, or hand it back to the stock clock.
	s.mux.HandleFunc("/v1/cover/home", s.guardAuth("radio-control", http.MethodPost, s.handleCoverHome))
	// Cover accent: repaints the kiosk's buttons. A config write, not a radio
	// action, so it carries no thermal gate — but it is still radio-control,
	// because anything that rewrites config.json is an owner action.
	s.mux.HandleFunc("/v1/cover/accent", s.guardAuth("radio-control", http.MethodPost, s.handleCoverAccent))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg, "code": code})
}

// guard wraps a read handler: auth -> scope -> rate limit.
func (s *Server) guard(need string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		have := s.cfg.authScope(r)
		if have == "" {
			// Open-reads mode: read-status GETs allowed WITHOUT a token (tailnet
			// convenience). On this donor phone the owner put sms-scope READS on
			// the same switch, so the Inbox needs no token either. Still reads
			// only (radio-control writes have their own switch), still redacted,
			// still pull-only — and turning open reads off closes both again.
			if (need == "read-status" || need == "sms") && s.openReads.Load() {
				if !s.rl.allow("open-reads", s.cfg.RateLimits.DefaultPerMin) {
					writeErr(w, http.StatusTooManyRequests, "rate limited")
					return
				}
				h(w, r)
				return
			}
			writeErr(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		if !scopeAllows(have, need) {
			writeErr(w, http.StatusForbidden, "token scope lacks "+need)
			return
		}
		if !s.rl.allow(have, s.limitFor(have)) {
			writeErr(w, http.StatusTooManyRequests, "rate limited")
			return
		}
		h(w, r)
	}
}

// guardWrite wraps a write handler: POST + auth + scope + rate limit + thermal gate.
func (s *Server) guardWrite(need string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		have := s.cfg.authScope(r)
		if have == "" {
			writeErr(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		if !scopeAllows(have, need) {
			writeErr(w, http.StatusForbidden, "token scope lacks "+need)
			return
		}
		if !s.rl.allow(have, s.limitFor(have)) {
			writeErr(w, http.StatusTooManyRequests, "rate limited")
			return
		}
		// Thermal gate: radio/tether writes refused unless safe. Fails closed.
		t := s.col.Thermal(s.effectiveWarnC(), s.effectiveGateC(), s.cfg.Thermal.FailClosed)
		if !t.Safe {
			writeErr(w, http.StatusConflict, "refused: unsafe thermal state")
			return
		}
		h(w, r)
	}
}

// guardAuth wraps a handler with method + auth + scope + rate limit, but no
// thermal gate — the handler makes its own policy decision.
func (s *Server) guardAuth(need, method string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		have := s.cfg.authScope(r)
		if have == "" {
			// Open-control mode: radio-control WRITES allowed WITHOUT a token
			// (owner's tailnet-only, app-less device). NEVER for sms — that
			// scope always requires its token.
			if need == "radio-control" && s.openControl.Load() {
				if !s.rl.allow("open-control", s.cfg.RateLimits.RadioPerMin) {
					writeErr(w, http.StatusTooManyRequests, "rate limited")
					return
				}
				h(w, r)
				return
			}
			writeErr(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		if !scopeAllows(have, need) {
			writeErr(w, http.StatusForbidden, "token scope lacks "+need)
			return
		}
		if !s.rl.allow(have, s.limitFor(have)) {
			writeErr(w, http.StatusTooManyRequests, "rate limited")
			return
		}
		h(w, r)
	}
}

func (s *Server) limitFor(scope string) int {
	switch scope {
	case "sms":
		return s.cfg.RateLimits.SMSPerMin
	case "radio-control":
		return s.cfg.RateLimits.RadioPerMin
	default:
		return s.cfg.RateLimits.DefaultPerMin
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.col.Health())
}

func (s *Server) handleThermal(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.col.Thermal(s.effectiveWarnC(), s.effectiveGateC(), s.cfg.Thermal.FailClosed))
}

func (s *Server) handleThermalHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.temps.Report())
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	th := s.col.Thermal(s.effectiveWarnC(), s.effectiveGateC(), s.cfg.Thermal.FailClosed)
	worst := th.BatteryC
	if th.MaxC > worst {
		worst = th.MaxC
	}
	// disp is a display-only copy: the safety-relevant fields (safe, source,
	// policy_state below) stay computed from the raw th so a smoothed number
	// can never make an UNSAFE gate look SAFE (or vice versa). Only the
	// headline number is swapped for the 14 s median.
	disp := th
	if m, ok := s.smooth.median(); ok {
		disp.MaxRawC = th.MaxC
		disp.MaxC = m
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"health":          s.col.Health(),
		"thermal":         disp,
		"policy_state":    string(classify(worst, s.effectiveWarnC(), s.effectiveGateC())),
		"network":         s.col.Network(),
		"battery":         s.col.Battery(),
		"wan_ip":          deviceWanIP(),
		"airplane":        airplaneOn(),
		"cover_home":      coverHomeOn(),
		"cover_accent":    s.coverAccent.Load(),
		"open_reads":      s.openReads.Load(),
		"open_control":    s.openControl.Load(),
		"bench":           s.bench.Status(),
		"hotspot_presets": s.presets.statusRedacted(),
		"service":         map[string]any{"daemon": "ok", "helper": map[string]any{"available": false}},
	})
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.col.Network())
}

func (s *Server) handleBattery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.col.Battery())
}

func (s *Server) handleSMSRecent(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.SMS.Enabled {
		writeErr(w, http.StatusForbidden, "sms disabled")
		return
	}
	limit := 5
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > smsMaxLimit {
			writeErr(w, http.StatusBadRequest, "limit out of range (1.."+itoa(smsMaxLimit)+")")
			return
		}
		limit = n
	}
	// Audit every read (never logs bodies).
	log.Printf("audit: sms.recent limit=%d path=%s", limit, s.cfg.SMS.Path)
	msgs, err := recentSMS(limit)
	if err != nil {
		// Fail safe: no permission / no provider -> empty, actionable, no crash.
		writeJSON(w, http.StatusOK, map[string]any{
			"messages": []SMSMessage{}, "redacted": true, "available": false,
			"reason": "sms provider unavailable or READ_SMS not granted", "path": s.cfg.SMS.Path,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages": msgs, "redacted": redactBodies, "available": true, "path": s.cfg.SMS.Path,
	})
}

func (s *Server) handleNotificationsRecent(w http.ResponseWriter, r *http.Request) {
	// sms.enabled is the one switch for private on-device content; turning it
	// off must silence notifications too.
	if !s.cfg.SMS.Enabled {
		writeErr(w, http.StatusForbidden, "sms disabled")
		return
	}
	limit := 8
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > notifMaxLimit {
			writeErr(w, http.StatusBadRequest, "limit out of range (1.."+itoa(notifMaxLimit)+")")
			return
		}
		limit = n
	}
	// Audit every read (never logs titles or bodies).
	log.Printf("audit: notifications.recent limit=%d", limit)
	ns, err := recentNotifications(limit)
	if err != nil {
		// Fail safe: no root / no notification service -> empty, actionable.
		writeJSON(w, http.StatusOK, map[string]any{
			"notifications": []Notification{}, "redacted": true, "available": false,
			"reason": "notification service unavailable (dumpsys needs root)",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": ns, "redacted": redactBodies, "available": true,
	})
}

func (s *Server) handleTether(w http.ResponseWriter, r *http.Request) {
	// Toggle the data-sharing hotspot via the root helper. ?action=start|stop
	// (default start). Not app-thermal-gated: the hotspot is the modem's primary
	// function and the app heat source is CPU compute (throttled by the eco CPU
	// policy), not the Wi-Fi radio; Samsung's own mitigation shuts the AP at
	// genuinely dangerous temperatures regardless.
	action := r.URL.Query().Get("action")
	if action == "" {
		action = "start"
	}
	switch action {
	case "stop":
		ok := stopHotspot()
		writeJSON(w, http.StatusOK, map[string]any{"applied": ok, "action": "stop", "active": hotspotActive()})
	case "start":
		ok := startHotspot()
		writeJSON(w, http.StatusOK, map[string]any{"applied": ok, "action": "start", "active": hotspotActive()})
	default:
		writeErr(w, http.StatusBadRequest, "action must be start or stop")
	}
}

func (s *Server) handleUsbTether(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, usbTetherStatus())
}

// handleUsbTetherToggle toggles USB tethering via the root helper.
// ?action=start|stop (default start). Not thermal-gated: the USB data path
// isn't a heat source, and being able to fall back to a wired connection when
// the radio is throttled is the point.
func (s *Server) handleUsbTetherToggle(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	if action == "" {
		action = "start"
	}
	switch action {
	case "stop":
		ok := stopUsbTether()
		writeJSON(w, http.StatusOK, map[string]any{"applied": ok, "action": "stop", "status": usbTetherStatus()})
	case "start":
		ok := startUsbTether()
		writeJSON(w, http.StatusOK, map[string]any{"applied": ok, "action": "start", "status": usbTetherStatus()})
	default:
		writeErr(w, http.StatusBadRequest, "action must be start or stop")
	}
}

func (s *Server) handleSignal(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, deviceSignal())
}
func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, deviceClients())
}
func (s *Server) handleBands(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, deviceBands())
}

func (s *Server) handleCPU(w http.ResponseWriter, r *http.Request) {
	requested := s.cfg.CPUMode()
	t := s.col.Thermal(s.effectiveWarnC(), s.effectiveGateC(), s.cfg.Thermal.FailClosed)
	clients := deviceClients().Count
	eff, reason := decideMode(requested, clients, t.Safe)
	writeJSON(w, http.StatusOK, s.cpu.report(requested, eff, reason, clients, t.Safe))
}

// handleCPUMode sets cpu.mode (auto/performance/balanced/eco/off), persists it,
// and applies it immediately. Body: {"mode":"performance"}.
func (s *Server) handleCPUMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil || !validCPUMode(body.Mode) {
		writeErr(w, http.StatusBadRequest, "mode must be auto, performance, balanced, eco, or off")
		return
	}
	s.cfgMu.Lock()
	s.cfg.CPU.Mode = body.Mode
	if s.cfgPath != "" {
		if err := persistCPUMode(s.cfgPath, body.Mode); err != nil {
			log.Printf("cpu mode: persist failed: %v", err)
		}
	}
	s.cfgMu.Unlock()
	s.applyCPUPolicyOnce()
	writeJSON(w, http.StatusOK, map[string]any{"mode": body.Mode, "applied": true})
}

func validCPUMode(m string) bool {
	switch m {
	case "auto", "performance", "balanced", "eco", "off":
		return true
	}
	return false
}

func (s *Server) handleHotspot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.hs.Status())
}

// handleAirplane toggles airplane mode or runs an IP-rotation cycle.
// Body: {"mode":"on"|"off"|"cycle"}.
//
//	on    - airplane on (drops radio + hotspot).
//	off   - airplane off, then re-enable the hotspot (thermal-gated).
//	cycle - on, wait, off, wait for data, re-enable hotspot; confirms IP change.
//
// The cycle blocks ~10-30s while the PDP context re-establishes.
func (s *Server) handleAirplane(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	// Serialize airplane operations: a cycle blocks for tens of seconds, and
	// overlapping toggles would interleave airplane on/off messily.
	if !s.airplaneBusy.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "an airplane operation is already in progress")
		return
	}
	defer s.airplaneBusy.Store(false)
	safe := s.col.Thermal(s.effectiveWarnC(), s.effectiveGateC(), s.cfg.Thermal.FailClosed).Safe
	switch body.Mode {
	case "on":
		s.usage.Sample() // the toggle can recreate rmnet_data*; fold the unsampled tail into today first
		airplaneSet(true)
		writeJSON(w, http.StatusOK, map[string]any{"airplane": true, "hotspot_active": hotspotActive(), "wan_ip": deviceWanIP()})
	case "off":
		airplaneSet(false)
		// Restore the hotspot (not app-thermal-gated; the hotspot is the modem's
		// primary function — see handleTether). Retry: the Wi-Fi stack needs a
		// moment to settle after airplane-off.
		res := map[string]any{"airplane": false, "hotspot_active": restartHotspotRetry(), "wan_ip": deviceWanIP()}
		writeJSON(w, http.StatusOK, res)
	case "cycle":
		s.usage.Sample() // same as "on": the cycle also recreates rmnet_data*
		writeJSON(w, http.StatusOK, airplaneCycle(safe))
	default:
		writeErr(w, http.StatusBadRequest, "mode must be on, off, or cycle")
	}
}

// handleSpeedtest runs one in-process speedtest (nearest server) and returns
// the result. Serialized; 409 if one is already running.
func (s *Server) handleSpeedtest(w http.ResponseWriter, r *http.Request) {
	if !s.speedtestBusy.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "a speedtest is already running")
		return
	}
	defer s.speedtestBusy.Store(false)
	writeJSON(w, http.StatusOK, runSpeedtest())
}

// handleHotspotScan runs an immediate scan and returns the refreshed status
// (including the nearby list). Scan-only: it never toggles the hotspot.
func (s *Server) handleHotspotScan(w http.ResponseWriter, r *http.Request) {
	paused := s.hs.Scan(time.Now().Format(time.RFC3339))
	st := s.hs.Status()
	// Reflect THIS scan's outcome, overriding any stale reason left by the
	// background loop: "" on success clears it (Paused is omitempty), so the
	// enable-location-then-Scan flow doesn't show "paused" next to fresh results.
	st.Paused = paused
	writeJSON(w, http.StatusOK, st)
}

// handleHotspotOverride forces the hotspot on for exactly N hours regardless
// of whitelist matches, then reverts to normal auto-toggle logic. Body:
// {"hours":N}, 0 cancels an active override.
func (s *Server) handleHotspotOverride(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hours int `json:"hours"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	if body.Hours < 0 || body.Hours > hotspotOverrideMaxH {
		writeErr(w, http.StatusBadRequest, "hours must be 0..24")
		return
	}
	writeJSON(w, http.StatusOK, s.hs.SetOverride(time.Duration(body.Hours)*time.Hour))
}

// handlePresets dispatches by method: GET lists presets (read-status), POST
// creates/edits one (radio-control). Two scopes on one path.
func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.guard("read-status", s.handlePresetList)(w, r)
	case http.MethodPost:
		s.guardAuth("radio-control", http.MethodPost, s.handlePresetUpsert)(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePresetList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.presets.statusRedacted())
}

// handlePresetUpsert creates a preset (no id) or edits one (id set). On edit an
// empty passphrase keeps the stored one.
func (s *Server) handlePresetUpsert(w http.ResponseWriter, r *http.Request) {
	var p Preset
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	saved, err := s.presets.Upsert(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": saved.ID, "hotspot_presets": s.presets.statusRedacted()})
}

// handlePresetDelete removes a preset. Body: {"id":"home"}.
func (s *Server) handlePresetDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	if err := s.presets.Delete(body.ID); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.presets.statusRedacted())
}

// handlePresetApply switches the live SoftAP to a preset now. Body: {"id":"home"}.
// Blocks a few seconds when the hotspot is up (it bounces the AP). Not
// thermal-gated: reconfiguring the hotspot is the modem's job, and Samsung's own
// mitigation still applies.
func (s *Server) handlePresetApply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	p, err := s.presets.Apply(body.ID)
	if err != nil {
		if err == errApplyBusy {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"applied": p.ID, "ssid": p.SSID,
		"hotspot": s.hs.Status(), "hotspot_presets": s.presets.statusRedacted(),
	})
}

// handlePresetAuto toggles Wi-Fi-fingerprint auto-switch. Body: {"on":true}.
func (s *Server) handlePresetAuto(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	s.presets.SetAuto(body.On)
	writeJSON(w, http.StatusOK, s.presets.statusRedacted())
}

// handleHotspotWhitelist replaces the SSID whitelist. Body: {"ssids":["home"]}.
// An empty list disables the auto-toggle.
func (s *Server) handleHotspotWhitelist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SSIDs []string `json:"ssids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	if len(body.SSIDs) > 16 {
		writeErr(w, http.StatusBadRequest, "too many ssids (max 16)")
		return
	}
	for _, ss := range body.SSIDs {
		if ss == "" || len(ss) > 32 || strings.ContainsAny(ss, "\t\r\n") {
			writeErr(w, http.StatusBadRequest, "invalid ssid")
			return
		}
	}
	s.cfgMu.Lock()
	s.hs.SetWhitelist(body.SSIDs)
	s.cfg.Hotspot.SSIDWhitelist = body.SSIDs
	if s.cfgPath != "" {
		if err := persistHotspotWhitelist(s.cfgPath, body.SSIDs); err != nil {
			log.Printf("hotspot whitelist: persist failed: %v", err)
		}
	}
	s.cfgMu.Unlock()
	writeJSON(w, http.StatusOK, s.hs.Status())
}

// handleThermalLimits retunes warn_c/gate_c. Body: {"warn_c":N,"gate_c":N}.
// Server clamps to the safe range and reports the effective values.
func (s *Server) handleThermalLimits(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WarnC *float64 `json:"warn_c"`
		GateC *float64 `json:"gate_c"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	if (body.WarnC != nil && !isFinite(*body.WarnC)) || (body.GateC != nil && !isFinite(*body.GateC)) {
		writeErr(w, http.StatusBadRequest, "warn_c/gate_c must be finite numbers")
		return
	}
	s.cfgMu.Lock()
	warn, gate := s.warnC(), s.gateC()
	if body.WarnC != nil {
		warn = *body.WarnC
	}
	if body.GateC != nil {
		gate = *body.GateC
	}
	ewarn, egate := s.setThermalLimits(warn, gate)
	s.cfgMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"warn_c": ewarn, "gate_c": egate,
		"clamped":      ewarn != warn || egate != gate,
		"gate_ceiling": gateCeilingC,
		"note":         "Samsung thermal mitigation is unaffected; this is the app-level gate.",
	})
}

// dispatchThermalBench splits GET (read-status) from POST (radio-control) on
// one path — ServeMux panics on duplicate registrations, so the method
// dispatch happens here.
func (s *Server) dispatchThermalBench(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.guard("read-status", s.handleThermalBenchGet)(w, r)
	case http.MethodPost:
		s.guardAuth("radio-control", http.MethodPost, s.handleThermalBenchPost)(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleThermalBenchGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.bench.Status())
}

// handleThermalBenchPost toggles bench mode. Body: {"enabled":true|false}.
func (s *Server) handleThermalBenchPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil || body.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "body must include enabled")
		return
	}
	s.cfgMu.Lock()
	s.bench.SetEnabled(*body.Enabled)
	s.cfg.Thermal.Bench = *body.Enabled
	if s.cfgPath != "" {
		if err := persistThermalBench(s.cfgPath, *body.Enabled); err != nil {
			log.Printf("thermal bench: persist failed: %v", err)
		}
	}
	s.cfgMu.Unlock()
	s.syncPolicyLimits()
	writeJSON(w, http.StatusOK, s.bench.Status())
}

func (s *Server) handlePrefer5G(w http.ResponseWriter, r *http.Request) {
	t := s.col.Thermal(s.effectiveWarnC(), s.effectiveGateC(), s.cfg.Thermal.FailClosed)
	worst := t.BatteryC
	if t.MaxC > worst {
		worst = t.MaxC
	}
	// Fail closed: unreadable thermal -> treat as gate temperature (HOT).
	if t.Source == "degraded" {
		worst = s.effectiveGateC()
	}
	state := s.pol.Evaluate(worst)
	ok, reason := Prefer5GAllowed(state)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"applied": false, "state": string(state), "error": reason})
		return
	}
	// Report the current allowed-network-types. The NR-preference WRITE
	// (`cmd phone set-allowed-network-types-for-users`) is intentionally not
	// executed here: it is baseband-adjacent and needs careful on-device
	// validation before enabling, so this endpoint is honest read-only rather
	// than claiming a change it does not make.
	writeJSON(w, http.StatusOK, map[string]any{
		"applied": false, "state": string(state),
		"current_allowed_types": readAllowedTypes(),
		"note":                  "NR-preference write not enabled; reporting current state only.",
	})
}

// handleCooldown forces an immediate CPU eco pass (park the prime core, cap the
// mid cluster). Reduce-only and reversible; the background policy will restore
// higher modes once cool. Unlike a radio write it must work while hot, which is
// why it is not behind guardWrite.
func (s *Server) handleCooldown(w http.ResponseWriter, r *http.Request) {
	s.cpu.apply("eco")
	writeJSON(w, http.StatusOK, map[string]any{"applied": true, "mode": "eco",
		"note": "CPU forced to eco; auto policy resumes on the next cycle."})
}

// handleRestart exits the daemon after replying; the Magisk watchdog respawns it
// within ~10s. This is the intended "restart" — the process has no in-place
// reload path.
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"restarting": true, "note": "exiting; watchdog will respawn."})
	go func() {
		time.Sleep(300 * time.Millisecond) // let the response flush
		log.Printf("restart requested via API; exiting for watchdog respawn")
		os.Exit(0)
	}()
}

// handleCoverHome puts the kiosk on the cover screen, or hands the panel back to
// the stock Samsung clock. Same scope as the other device controls.
func (s *Server) handleCoverHome(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil || body.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "enabled must be true or false")
		return
	}
	if err := setCoverHome(*body.Enabled, s.cfg.CoverDim()); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	note := "cover screen handed back to the Samsung clock"
	if *body.Enabled {
		note = "kiosk returns to the cover screen in a few seconds"
	}
	writeJSON(w, http.StatusOK, map[string]any{"cover_home": *body.Enabled, "note": note})
}

// handleCoverAccent repaints the cover screen. Body: {"accent":"coral"}.
// Rejects anything outside the seven named accents: the value is substituted
// into the served HTML, so an unvalidated string would be an injection point.
func (s *Server) handleCoverAccent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Accent string `json:"accent"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	if !validCoverAccent(body.Accent) {
		writeErr(w, http.StatusBadRequest, "accent must be one of "+strings.Join(coverAccents, ", "))
		return
	}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.cfgPath != "" {
		if err := persistCoverAccent(s.cfgPath, body.Accent); err != nil {
			writeErr(w, http.StatusInternalServerError, "persist failed: "+err.Error())
			return
		}
	}
	s.cfg.Cover.Accent = body.Accent
	s.coverAccent.Store(body.Accent)
	writeJSON(w, http.StatusOK, map[string]any{"cover_accent": body.Accent})
}

// handleDeviceReboot reboots the whole phone after replying. The module's
// late-start service restores the daemon + hotspot on boot (verified). Used by
// the Telegram /reboot command and scheduled auto-reboot.
func (s *Server) handleDeviceReboot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"rebooting": true, "note": "device reboot in ~2s; back in ~60s."})
	go func() {
		time.Sleep(2 * time.Second) // let the response flush before the radio drops
		log.Printf("device reboot requested via API")
		s.usage.Flush() // persist the current sample; the reboot resets the counter to 0
		s.temps.Flush()
		runCmd("reboot")
	}()
}

// --- rate limiter: fixed-window per scope ---

type rateLimiter struct {
	mu     sync.Mutex
	window map[string]*windowState
	now    func() time.Time
}

type windowState struct {
	start time.Time
	count int
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{window: map[string]*windowState{}, now: time.Now}
}

func (rl *rateLimiter) allow(key string, perMin int) bool {
	if perMin <= 0 {
		perMin = 1
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	ws := rl.window[key]
	if ws == nil || now.Sub(ws.start) >= time.Minute {
		rl.window[key] = &windowState{start: now, count: 1}
		return true
	}
	if ws.count >= perMin {
		return false
	}
	ws.count++
	return true
}

func (s *Server) ListenAndServe() error {
	// Background data-usage sampler so day/week/month accrue even without
	// hits. 15 s cadence (nextUsageWake), shortened so one sample lands just
	// after local midnight instead of up to one interval late.
	go func() {
		for {
			time.Sleep(s.usage.NextWake(time.Now()))
			s.usage.Sample()
		}
	}()
	// Background CPU power/perf policy (skips itself when mode is off).
	go s.runCPUPolicy()
	// Background SSID-whitelist hotspot auto-toggle (idle when list is empty).
	go s.runHotspotAuto()
	// Bench thermal watcher: re-asserts the bypass and arms the 95C critical trip.
	go s.bench.Watch()
	// Display-only thermal median sampler (see thermalSmoother); the gate
	// path never reads it.
	go s.smooth.run()
	// Minute-by-minute temperature history, sampled from the display path
	// above (s.smooth), never the gate.
	go s.temps.run()
	addr := s.cfg.BindHost + ":" + itoa(s.cfg.BindPort)
	log.Printf("zflip5-modemd listening on %s (loopback)", addr)
	srv := &http.Server{Addr: addr, Handler: s, ReadHeaderTimeout: 5 * time.Second}
	return srv.ListenAndServe()
}
