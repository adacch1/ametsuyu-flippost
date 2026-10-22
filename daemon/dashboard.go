package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.usage.Report())
}

// handleUsageQuota edits the data-cap limit and/or reset schedule. Body
// fields are pointers so an omitted one keeps its current value. Persist
// happens FIRST (same pattern as handleThermalLimits): a write that can't reach
// disk must not take effect in memory either, or a restart would silently
// revert it.
func (s *Server) handleUsageQuota(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LimitBytes *int64  `json:"limit_bytes"`
		Period     *string `json:"period"`
		ResetTime  *string `json:"reset_time"`
		ResetDay   *int    `json:"reset_day"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	q := s.cfg.Quota
	if body.LimitBytes != nil {
		q.LimitBytes = *body.LimitBytes
	}
	if body.Period != nil {
		q.Period = *body.Period
	}
	if body.ResetTime != nil {
		q.ResetTime = *body.ResetTime
	}
	if body.ResetDay != nil {
		q.ResetDay = *body.ResetDay
	}
	q, err := normalizeQuota(q)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.cfgPath != "" {
		if err := persistQuota(s.cfgPath, q); err != nil {
			writeErr(w, http.StatusInternalServerError, "persist failed: "+err.Error())
			return
		}
	}
	s.cfg.Quota = q
	s.usage.SetQuota(q)
	writeJSON(w, http.StatusOK, s.usage.Report())
}

// handleUsageReset zeroes the period meter right now (manual "Reset data
// usage"). today/week/month buckets are untouched.
func (s *Server) handleUsageReset(w http.ResponseWriter, r *http.Request) {
	log.Printf("audit: usage period reset via API")
	s.usage.Reset()
	writeJSON(w, http.StatusOK, s.usage.Report())
}

// pairURL builds the address a new device opens to onboard. A loopback host
// means the request came through the app, so it prefers the Tailscale web URL.
// The token rides along only when the caller passes one.
func pairURL(r *http.Request, webURL *string, token string) string {
	host := r.Host
	scheme := "http"
	if r.Header.Get("X-Forwarded-Proto") == "https" || strings.Contains(host, ".ts.net") {
		scheme = "https"
	}
	u := scheme + "://" + host + "/"
	if webURL != nil && isLoopbackHost(host) {
		u = *webURL
	}
	if token != "" {
		u += "?token=" + token
	}
	return u
}

func isLoopbackHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.Trim(h, "[]")
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// pairTarget returns the QR payload. The token is empty when open reads is on.
// Only a loopback host asks Tailscale for its web URL.
func (s *Server) pairTarget(r *http.Request) string {
	token := ""
	if !s.openReads.Load() {
		token = s.cfg.Tokens["read-status"]
	}
	var web *string
	if isLoopbackHost(r.Host) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		web = s.ts.Status(ctx).WebURL
	}
	return pairURL(r, web, token)
}

// handleQR returns a PNG QR of the pairing address.
func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	png, err := qrcode.Encode(s.pairTarget(r), qrcode.Medium, 480)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "qr encode failed")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// handleDashboardOpen toggles open-reads (tokenless read-status over the tailnet).
// Body: {"open": true|false}. radio-control, so only the owner flips it.
func (s *Server) handleDashboardOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Open bool `json:"open"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	s.cfgMu.Lock()
	s.openReads.Store(body.Open)
	s.cfg.Dashboard.OpenReads = body.Open
	if s.cfgPath != "" {
		if err := persistDashboardFlag(s.cfgPath, "open_reads", body.Open); err != nil {
			log.Printf("open_reads: persist failed: %v", err)
		}
	}
	s.cfgMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"open_reads": body.Open})
}

// handleDashboardControl toggles open-control (tokenless radio-control WRITES
// over the tailnet). Body: {"open": true|false}. SMS is never affected.
func (s *Server) handleDashboardControl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Open bool `json:"open"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	s.cfgMu.Lock()
	s.openControl.Store(body.Open)
	s.cfg.Dashboard.OpenControl = body.Open
	if s.cfgPath != "" {
		if err := persistDashboardFlag(s.cfgPath, "open_control", body.Open); err != nil {
			log.Printf("open_control: persist failed: %v", err)
		}
	}
	s.cfgMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"open_control": body.Open})
}

//go:embed logo.png
var flippostLogoPNG []byte

// handleIcon serves the approved Flippost logo as the app's home-screen icon.
func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(flippostLogoPNG)
}

// handleManifest serves the PWA manifest so the dashboard installs as an app.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write([]byte(webManifest))
}

// handleServiceWorker serves a minimal service worker (installability + a cached
// app shell so the UI opens instantly and offline shows the last shell).
func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write([]byte(serviceWorkerJS))
}

const webManifest = `{
  "name": "Ametsuyu Flippost",
  "short_name": "Flippost",
  "description": "Hotspot and modem controls for the Z Flip 5",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "orientation": "any",
  "background_color": "#1b2436",
  "theme_color": "#1b2436",
  "icons": [
    {"src": "/logo.png", "sizes": "1254x1254", "type": "image/png", "purpose": "any maskable"}
  ]
}`

// Kill switch. A previous version shipped a caching SW that could serve a stale
// page. This one unregisters itself and wipes all caches, then reloads open
// windows — so any client still running the old SW self-heals on the next update
// check. New clients never register a SW at all (see the page script).
const serviceWorkerJS = `self.addEventListener('install',function(){self.skipWaiting();});
self.addEventListener('activate',function(e){e.waitUntil((async function(){
  try{var ks=await caches.keys();await Promise.all(ks.map(function(k){return caches.delete(k);}));}catch(err){}
  try{await self.registration.unregister();}catch(err){}
  try{var cs=await self.clients.matchAll({type:'window'});cs.forEach(function(c){c.navigate(c.url);});}catch(err){}
})());});`
