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
	// Adjustable thermal limits (atomic float bits) so the owner can retune the
	// gate at runtime; seeded from config, clamped to a safe range on write.
	warnBits atomic.Uint64
	gateBits atomic.Uint64
	// airplaneBusy serializes /v1/airplane so overlapping cycles/toggles can't
	// interleave airplane on/off.
	airplaneBusy atomic.Bool
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
	s.warnBits.Store(math.Float64bits(warn))
	s.gateBits.Store(math.Float64bits(gate))
	s.routes()
	return s
}

func (s *Server) warnC() float64 { return math.Float64frombits(s.warnBits.Load()) }
func (s *Server) gateC() float64 { return math.Float64frombits(s.gateBits.Load()) }

// setThermalLimits clamps, applies, and persists new thermal thresholds. Returns
// the effective (possibly clamped) values. gate is capped at gateCeilingC and
// must stay above warn; warn floored at warnFloorC and kept below gate.
func (s *Server) setThermalLimits(warn, gate float64) (float64, float64) {
	warn, gate = clampThermal(warn, gate)
	s.warnBits.Store(math.Float64bits(warn))
	s.gateBits.Store(math.Float64bits(gate))
	s.cfg.Thermal.WarnC, s.cfg.Thermal.GateC = warn, gate
	s.pol.SetLimits(warn, gate)
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
		requested := s.cfg.CPUMode()
		if requested == "off" {
			s.cpu.restoreAll()
		} else {
			t := s.col.Thermal(s.warnC(), s.gateC(), s.cfg.Thermal.FailClosed)
			clients := deviceClients().Count
			mode, _ := decideMode(requested, clients, t.Safe)
			s.cpu.apply(mode)
		}
		time.Sleep(30 * time.Second)
	}
}

// runHotspotAuto is the background SSID-whitelist loop; the thermal check only
// gates auto-START (stopping while hot is always allowed).
func (s *Server) runHotspotAuto() {
	for {
		t := s.col.Thermal(s.warnC(), s.gateC(), s.cfg.Thermal.FailClosed)
		s.hs.step(t.Safe)
		time.Sleep(hotspotScanInterval)
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("/v1/status", s.guard("read-status", s.handleStatus))
	s.mux.HandleFunc("/v1/health", s.guard("read-status", s.handleHealth))
	s.mux.HandleFunc("/v1/thermal", s.guard("read-status", s.handleThermal))
	s.mux.HandleFunc("/v1/network", s.guard("read-status", s.handleNetwork))
	s.mux.HandleFunc("/v1/battery", s.guard("read-status", s.handleBattery))
	s.mux.HandleFunc("/v1/usage", s.guard("read-status", s.handleUsage))
	s.mux.HandleFunc("/v1/signal", s.guard("read-status", s.handleSignal))
	s.mux.HandleFunc("/v1/clients", s.guard("read-status", s.handleClients))
	s.mux.HandleFunc("/v1/bands", s.guard("read-status", s.handleBands))
	s.mux.HandleFunc("/v1/cpu", s.guard("read-status", s.handleCPU))
	s.mux.HandleFunc("/v1/hotspot", s.guard("read-status", s.handleHotspot))
	// Whitelist edits are config writes, not radio actions: no thermal gate
	// (and clearing the list must work while hot to stop auto-starts).
	s.mux.HandleFunc("/v1/hotspot/whitelist", s.guardAuth("radio-control", http.MethodPost, s.handleHotspotWhitelist))
	// On-demand scan-only refresh of the nearby-networks list (radio-control:
	// it drives the radio off-channel briefly and is rate-limited like a write).
	s.mux.HandleFunc("/v1/hotspot/scan", s.guardAuth("radio-control", http.MethodPost, s.handleHotspotScan))
	// Airplane trigger + IP-rotation cycle. Airplane on/off always works (turning
	// the radio OFF must work while hot); a fresh hotspot start ("off" mode) is
	// thermal-gated like /v1/tether, while the cycle restores a pre-existing
	// hotspot regardless (status quo). Serialized via airplaneBusy.
	s.mux.HandleFunc("/v1/airplane", s.guardAuth("radio-control", http.MethodPost, s.handleAirplane))
	// Retune the thermal gate: radio-control write, but NOT thermal-gated itself
	// (you must be able to adjust limits while hot), and hard-clamped server-side.
	s.mux.HandleFunc("/v1/thermal/limits", s.guardAuth("radio-control", http.MethodPost, s.handleThermalLimits))
	// Dashboard HTML (no data without a token; the page fetches /v1/* itself).
	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc("/dashboard", s.handleDashboard)
	s.mux.HandleFunc("/v1/sms/recent", s.guard("sms", s.handleSMSRecent))
	// tether/cooldown/restart make their own thermal decision (stopping the
	// hotspot or cooling down must work WHILE hot), so they take the auth-only
	// guard instead of guardWrite's blanket "refuse when unsafe" gate.
	s.mux.HandleFunc("/v1/tether", s.guardAuth("radio-control", http.MethodPost, s.handleTether))
	// prefer5g uses the policy engine (incl. cooldown stickiness), so it takes the
	// auth-only guard and makes its own thermal decision in the handler.
	s.mux.HandleFunc("/v1/prefer5g", s.guardAuth("radio-control", http.MethodPost, s.handlePrefer5G))
	s.mux.HandleFunc("/v1/cooldown", s.guardAuth("radio-control", http.MethodPost, s.handleCooldown))
	s.mux.HandleFunc("/v1/service/restart", s.guardAuth("radio-control", http.MethodPost, s.handleRestart))
	// Full DEVICE reboot (not just the daemon). radio-control; the module's
	// service.sh brings the daemon + hotspot back on boot.
	s.mux.HandleFunc("/v1/device/reboot", s.guardAuth("radio-control", http.MethodPost, s.handleDeviceReboot))
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
		t := s.col.Thermal(s.warnC(), s.gateC(), s.cfg.Thermal.FailClosed)
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
	writeJSON(w, http.StatusOK, s.col.Thermal(s.warnC(), s.gateC(), s.cfg.Thermal.FailClosed))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	th := s.col.Thermal(s.warnC(), s.gateC(), s.cfg.Thermal.FailClosed)
	worst := th.BatteryC
	if th.MaxC > worst {
		worst = th.MaxC
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"health":       s.col.Health(),
		"thermal":      th,
		"policy_state": string(classify(worst, s.warnC(), s.gateC())),
		"network":      s.col.Network(),
		"battery":      s.col.Battery(),
		"wan_ip":       deviceWanIP(),
		"airplane":     airplaneOn(),
		"service":      map[string]any{"daemon": "ok", "helper": map[string]any{"available": false}},
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
		"messages": msgs, "redacted": true, "available": true, "path": s.cfg.SMS.Path,
	})
}

func (s *Server) handleTether(w http.ResponseWriter, r *http.Request) {
	// Toggle the data-sharing hotspot via the root helper. ?action=start|stop
	// (default start). Starting is thermal-gated (never bring the radio up while
	// hot); stopping is always allowed since it reduces load.
	action := r.URL.Query().Get("action")
	if action == "" {
		action = "start"
	}
	switch action {
	case "stop":
		ok := stopHotspot()
		writeJSON(w, http.StatusOK, map[string]any{"applied": ok, "action": "stop", "active": hotspotActive()})
	case "start":
		t := s.col.Thermal(s.warnC(), s.gateC(), s.cfg.Thermal.FailClosed)
		if !t.Safe {
			writeErr(w, http.StatusConflict, "refused: unsafe thermal state")
			return
		}
		ok := startHotspot()
		writeJSON(w, http.StatusOK, map[string]any{"applied": ok, "action": "start", "active": hotspotActive()})
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
	t := s.col.Thermal(s.warnC(), s.gateC(), s.cfg.Thermal.FailClosed)
	clients := deviceClients().Count
	eff, reason := decideMode(requested, clients, t.Safe)
	writeJSON(w, http.StatusOK, s.cpu.report(requested, eff, reason, clients, t.Safe))
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
	safe := s.col.Thermal(s.warnC(), s.gateC(), s.cfg.Thermal.FailClosed).Safe
	switch body.Mode {
	case "on":
		airplaneSet(true)
		writeJSON(w, http.StatusOK, map[string]any{"airplane": true, "hotspot_active": hotspotActive(), "wan_ip": deviceWanIP()})
	case "off":
		airplaneSet(false)
		res := map[string]any{"airplane": false}
		// A standalone "off" is an explicit fresh hotspot start (not a status-quo
		// restore like the cycle), so it IS thermal-gated, same as /v1/tether start.
		if safe {
			res["hotspot_active"] = restartHotspotRetry()
		} else {
			res["hotspot_active"] = hotspotActive()
			res["note"] = "hotspot not started (thermal gate) — device too warm"
		}
		res["wan_ip"] = deviceWanIP()
		writeJSON(w, http.StatusOK, res)
	case "cycle":
		writeJSON(w, http.StatusOK, airplaneCycle(safe))
	default:
		writeErr(w, http.StatusBadRequest, "mode must be on, off, or cycle")
	}
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

func (s *Server) handlePrefer5G(w http.ResponseWriter, r *http.Request) {
	t := s.col.Thermal(s.warnC(), s.gateC(), s.cfg.Thermal.FailClosed)
	worst := t.BatteryC
	if t.MaxC > worst {
		worst = t.MaxC
	}
	// Fail closed: unreadable thermal -> treat as gate temperature (HOT).
	if t.Source == "degraded" {
		worst = s.gateC()
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

// handleDeviceReboot reboots the whole phone after replying. The module's
// late-start service restores the daemon + hotspot on boot (verified). Used by
// the Telegram /reboot command and scheduled auto-reboot.
func (s *Server) handleDeviceReboot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"rebooting": true, "note": "device reboot in ~2s; back in ~60s."})
	go func() {
		time.Sleep(2 * time.Second) // let the response flush before the radio drops
		log.Printf("device reboot requested via API")
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
	// Background data-usage sampler so day/week/month accrue even without hits.
	go func() {
		for {
			time.Sleep(60 * time.Second)
			s.usage.Sample()
		}
	}()
	// Background CPU power/perf policy (skips itself when mode is off).
	go s.runCPUPolicy()
	// Background SSID-whitelist hotspot auto-toggle (idle when list is empty).
	go s.runHotspotAuto()
	addr := s.cfg.BindHost + ":" + itoa(s.cfg.BindPort)
	log.Printf("zflip5-modemd listening on %s (loopback)", addr)
	srv := &http.Server{Addr: addr, Handler: s, ReadHeaderTimeout: 5 * time.Second}
	return srv.ListenAndServe()
}
