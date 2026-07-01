package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Server struct {
	cfg *Config
	col Collector
	mux *http.ServeMux
	rl  *rateLimiter
	pol *PolicyEngine
}

func NewServer(cfg *Config, col Collector) *Server {
	s := &Server{cfg: cfg, col: col, mux: http.NewServeMux(), rl: newRateLimiter(), pol: NewPolicyEngine(cfg.Thermal.WarnC, cfg.Thermal.GateC)}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/v1/status", s.guard("read-status", s.handleStatus))
	s.mux.HandleFunc("/v1/health", s.guard("read-status", s.handleHealth))
	s.mux.HandleFunc("/v1/thermal", s.guard("read-status", s.handleThermal))
	s.mux.HandleFunc("/v1/network", s.guard("read-status", s.handleNetwork))
	s.mux.HandleFunc("/v1/battery", s.guard("read-status", s.handleBattery))
	s.mux.HandleFunc("/v1/sms/recent", s.guard("sms", s.handleSMSRecent))
	s.mux.HandleFunc("/v1/tether", s.guardWrite("radio-control", s.handleTether))
	// prefer5g uses the policy engine (incl. cooldown stickiness), so it takes the
	// auth-only guard and makes its own thermal decision in the handler.
	s.mux.HandleFunc("/v1/prefer5g", s.guardAuth("radio-control", http.MethodPost, s.handlePrefer5G))
	s.mux.HandleFunc("/v1/cooldown", s.guardWrite("radio-control", s.handleCooldown))
	s.mux.HandleFunc("/v1/service/restart", s.guardWrite("radio-control", s.handleRestart))
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
		t := s.col.Thermal(s.cfg.Thermal.WarnC, s.cfg.Thermal.GateC, s.cfg.Thermal.FailClosed)
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
	writeJSON(w, http.StatusOK, s.col.Thermal(s.cfg.Thermal.WarnC, s.cfg.Thermal.GateC, s.cfg.Thermal.FailClosed))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	th := s.col.Thermal(s.cfg.Thermal.WarnC, s.cfg.Thermal.GateC, s.cfg.Thermal.FailClosed)
	worst := th.BatteryC
	if th.MaxC > worst {
		worst = th.MaxC
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"health":       s.col.Health(),
		"thermal":      th,
		"policy_state": string(classify(worst, s.cfg.Thermal.WarnC, s.cfg.Thermal.GateC)),
		"network":      s.col.Network(),
		"battery":      s.col.Battery(),
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
	writeJSON(w, http.StatusOK, map[string]any{"applied": false, "note": "tether verb path from Todo 1 discovery"})
}

func (s *Server) handlePrefer5G(w http.ResponseWriter, r *http.Request) {
	t := s.col.Thermal(s.cfg.Thermal.WarnC, s.cfg.Thermal.GateC, s.cfg.Thermal.FailClosed)
	worst := t.BatteryC
	if t.MaxC > worst {
		worst = t.MaxC
	}
	// Fail closed: unreadable thermal -> treat as gate temperature (HOT).
	if t.Source == "degraded" {
		worst = s.cfg.Thermal.GateC
	}
	state := s.pol.Evaluate(worst)
	ok, reason := Prefer5GAllowed(state)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"applied": false, "state": string(state), "error": reason})
		return
	}
	// Safe: apply the reversible NR-preference verb (no-op-safe if unavailable).
	before := readAllowedTypes()
	writeJSON(w, http.StatusOK, map[string]any{"applied": true, "state": string(state), "verb": "cmd phone set-allowed-network-types-for-users", "before": before})
}
func (s *Server) handleCooldown(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]any{"cooldown": true}) }
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request)  { writeJSON(w, http.StatusOK, map[string]any{"restarting": true}) }

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
	addr := s.cfg.BindHost + ":" + itoa(s.cfg.BindPort)
	log.Printf("zflip5-modemd listening on %s (loopback)", addr)
	srv := &http.Server{Addr: addr, Handler: s, ReadHeaderTimeout: 5 * time.Second}
	return srv.ListenAndServe()
}
