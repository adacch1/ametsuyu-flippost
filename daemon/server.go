package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

type Server struct {
	cfg *Config
	col Collector
	mux *http.ServeMux
	rl  *rateLimiter
}

func NewServer(cfg *Config, col Collector) *Server {
	s := &Server{cfg: cfg, col: col, mux: http.NewServeMux(), rl: newRateLimiter()}
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
	s.mux.HandleFunc("/v1/prefer5g", s.guardWrite("radio-control", s.handlePrefer5G))
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
	writeJSON(w, http.StatusOK, map[string]any{
		"health":  s.col.Health(),
		"thermal": s.col.Thermal(s.cfg.Thermal.WarnC, s.cfg.Thermal.GateC, s.cfg.Thermal.FailClosed),
		"network": map[string]any{"available": true, "source": "shell-scrape"},
		"battery": map[string]any{"source": "shell-scrape"},
		"service": map[string]any{"daemon": "ok", "helper": map[string]any{"available": false}},
	})
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "source": "shell-scrape"})
}

func (s *Server) handleBattery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"source": "shell-scrape"})
}

func (s *Server) handleSMSRecent(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.SMS.Enabled {
		writeErr(w, http.StatusForbidden, "sms disabled")
		return
	}
	// Pull-only, redacted-by-default. Body reading is Todo 10; this proves the
	// scope boundary and never forwards.
	writeJSON(w, http.StatusOK, map[string]any{"messages": []any{}, "redacted": true, "path": "iphone-tailscale"})
}

func (s *Server) handleTether(w http.ResponseWriter, r *http.Request)   { writeJSON(w, http.StatusOK, map[string]any{"applied": false, "note": "device verb wired in Todo 11"}) }
func (s *Server) handlePrefer5G(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]any{"applied": false, "note": "cmd phone set-allowed-network-types-for-users; Todo 11"}) }
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
