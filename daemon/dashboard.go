package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.usage.Report())
}

// handleDashboard serves the admin dashboard HTML for exactly "/" and
// "/dashboard"; every other unmatched path is a 404 (ServeMux routes them here).
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/dashboard" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	// Never cache the HTML: the WebView/browser otherwise serves a stale page
	// after a daemon update (e.g. a new card wouldn't appear until cache expiry).
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

// handleQR returns a PNG QR of the URL a new device should open to onboard:
// "<origin>/?token=<read-status>". The origin comes from the request Host, so a
// dashboard opened over Tailscale QRs the shareable tailnet URL (not 127.0.0.1).
func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	scheme := "http"
	if r.Header.Get("X-Forwarded-Proto") == "https" || strings.Contains(host, ".ts.net") {
		scheme = "https"
	}
	url := scheme + "://" + host + "/?token=" + s.cfg.Tokens["read-status"]
	png, err := qrcode.Encode(url, qrcode.Medium, 480)
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

// handleIcon serves the home-screen app icon (a maskable signal glyph on the
// app's dark ground). SVG scales to any size iOS/Android asks for.
func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = w.Write([]byte(appIconSVG))
}

const appIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">
<rect width="512" height="512" rx="112" fill="#0b0d10"/>
<g transform="translate(150 300)">
<rect x="0" y="-40" width="34" height="40" rx="6" fill="#3fb8af"/>
<rect x="58" y="-80" width="34" height="80" rx="6" fill="#3fb8af"/>
<rect x="116" y="-128" width="34" height="128" rx="6" fill="#3fb8af"/>
<rect x="174" y="-184" width="34" height="184" rx="6" fill="#3fb8af"/>
</g></svg>`

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
  "name": "Z Flip 5 Modem",
  "short_name": "ZF5 Modem",
  "description": "Admin dashboard for the Z Flip 5 modem",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "orientation": "any",
  "background_color": "#0b0d10",
  "theme_color": "#0b0d10",
  "icons": [
    {"src": "/icon.svg", "sizes": "any", "type": "image/svg+xml", "purpose": "any maskable"}
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

// dashboardHTML is the Claude Design-built dashboard, integrated into the daemon
// (fonts dropped for the single-file self-contained build; the system font stack
// is the fallback). Restructured for the Z Flip 5 cover screen (~352×308 CSS px
// usable): five focused tabs instead of one long scroll, a global slim header
// with the NSA-aware tech indicator, ≥44px touch targets, and per-tab polling so
// a small screen never pays for data it isn't showing. Also the owner controls:
// thermal-gate adjust and the hotspot SSID-whitelist editor (radio-control
// token, stored locally, server-clamped/validated).
const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Z Flip 5 Modem</title>
<link rel="manifest" href="/manifest.webmanifest">
<meta name="theme-color" content="#0b0d10">
<link rel="apple-touch-icon" href="/icon.svg">
<link rel="icon" href="/icon.svg">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<meta name="apple-mobile-web-app-title" content="ZF5 Modem">
<style>
  :root{
    --bg:#0b0d10; --card:#161b22; --card-2:#1b212a; --line:#21262d; --line-soft:#1a1f27;
    --text:#e6edf3; --text-2:#9aa5b1; --text-3:#7d8794; /* 4.7:1 on card — small labels need AA */
    --teal:#3fb8af; --green:#3fb950; --amber:#e3a008; --red:#f0524e; --blue:#5b8dee; --violet:#8a7dff;
    --track:#242b34; --radius:12px; --tabbar-h:60px;
  }
  *{box-sizing:border-box;margin:0;padding:0}
  html,body{height:100%}
  body{
    background:var(--bg); color:var(--text);
    font-family:"Be Vietnam Pro",-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
    -webkit-font-smoothing:antialiased; font-feature-settings:"tnum" 1; line-height:1.4;
  }
  .num{font-variant-numeric:tabular-nums}
  .mono{font-family:ui-monospace,Menlo,monospace}
  .app{max-width:720px;margin:0 auto;min-height:100%;padding:0 12px calc(var(--tabbar-h) + 16px)}
  .screen{display:none}
  .screen.active{display:block}
  header{position:sticky;top:0;z-index:5;background:linear-gradient(var(--bg) 72%,rgba(11,13,16,0));padding:10px 0 8px;display:flex;align-items:center;justify-content:space-between;gap:10px}
  /* top-left signal indicator (global, all tabs) */
  .sigind{display:flex;align-items:center;gap:8px}
  .bars{display:inline-flex;align-items:flex-end;gap:2px;height:18px}
  .bars>i{width:3.5px;background:#30363d;border-radius:1px}
  .bars>i:nth-child(1){height:6px}.bars>i:nth-child(2){height:10px}.bars>i:nth-child(3){height:14px}.bars>i:nth-child(4){height:18px}
  .bars.g>i.on{background:var(--green)}.bars.a>i.on{background:var(--amber)}.bars.r>i.on{background:var(--red)}
  .sigind .lab{font-size:14px;font-weight:700}
  .sigind .op{font-size:10.5px;color:var(--text-3);font-weight:500}
  .netbadge{display:flex;align-items:center;gap:7px;background:var(--card);border:1px solid var(--line);border-radius:999px;padding:6px 11px 6px 10px;font-size:12.5px;font-weight:600;letter-spacing:.02em}
  .dot{width:8px;height:8px;border-radius:50%;flex:none}
  .dot.green{background:var(--green)}.dot.amber{background:var(--amber)}.dot.red{background:var(--red)}.dot.off{background:#30363d}
  .card{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:14px}
  .card+.card,.grid+.card,.card+.grid{margin-top:10px}
  .hero{display:flex;flex-direction:column;align-items:center;padding:14px 14px 12px;background:radial-gradient(120% 80% at 50% 0%,rgba(63,184,175,.06),transparent 60%),var(--card)}
  .ring-wrap{position:relative;width:min(44vw,180px);aspect-ratio:1}
  .ring-wrap svg{width:100%;height:100%;transform:rotate(-90deg)}
  .ring-center{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:2px;text-align:center}
  .ring-pct{font-size:clamp(34px,11vw,46px);font-weight:700;letter-spacing:-.03em;line-height:1}
  .ring-pct span{font-size:.5em;font-weight:600;color:var(--text-2);margin-left:1px}
  .ring-sub{font-size:12.5px;color:var(--text-2);font-weight:500}
  .ring-label{margin-top:10px;font-size:10.5px;font-weight:600;letter-spacing:.09em;text-transform:uppercase;color:var(--text-3)}
  .ring-caption{margin-top:4px;font-size:12.5px;color:var(--text-2)}
  .ring-caption b{color:var(--text);font-weight:600}
  .grid{display:grid;grid-template-columns:1fr;gap:10px}
  .duo{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:10px}
  .stat-head{display:flex;align-items:center;justify-content:space-between;font-size:10.5px;font-weight:600;letter-spacing:.07em;text-transform:uppercase;color:var(--text-3);margin-bottom:10px}
  .stat-head .accent{width:7px;height:7px;border-radius:50%}
  .stat-val{font-size:30px;font-weight:700;letter-spacing:-.02em;line-height:1}
  .stat-val small{font-size:.5em;font-weight:600;color:var(--text-2);margin-left:1px}
  .stat-sub{margin-top:6px;font-size:12px;color:var(--text-2)}
  .bar{margin-top:12px;height:6px;border-radius:3px;background:var(--track);overflow:hidden}
  .bar>i{display:block;height:100%;border-radius:3px;background:var(--teal);transition:width .5s ease}
  .cpu-head{display:flex;align-items:baseline;justify-content:space-between;gap:8px;margin-bottom:12px}
  .cpu-load{font-size:30px;font-weight:700;letter-spacing:-.02em;line-height:1}
  .cpu-load small{font-size:.4em;font-weight:600;color:var(--text-2);margin-left:3px}
  .cpu-meta{font-size:11.5px;color:var(--text-3);font-weight:600;text-align:right}
  .cores{display:flex;align-items:flex-end;gap:5px;height:64px}
  .core{flex:1;display:flex;flex-direction:column;align-items:center;gap:5px;height:100%;justify-content:flex-end}
  .core .track{position:relative;width:100%;flex:1;background:var(--track);border-radius:4px;overflow:hidden;display:flex;align-items:flex-end}
  .core .fill{width:100%;border-radius:4px;background:var(--violet);transition:height .5s ease}
  .core .idx{font-size:9.5px;color:var(--text-3);font-weight:600}
  .core.off{opacity:.32}
  .core.off .fill{background:#30363d!important}
  /* signal grid */
  .sg{display:grid;grid-template-columns:1fr 1fr;gap:5px 16px}
  .sgrow{display:flex;justify-content:space-between;font-size:13px;padding:2px 0}
  .sgrow span{color:var(--text-2)}
  .sgrow b{font-weight:600}
  .good{color:var(--green)}.mid{color:var(--amber)}.low{color:var(--red)}
  /* clients: roomy rows for a small touch screen */
  details.cli{border-top:1px solid var(--line-soft)}
  details.cli:first-of-type{border-top:0}
  details.cli summary{cursor:pointer;list-style:none;display:flex;align-items:center;gap:9px;font-size:13.5px;min-height:46px;padding:4px 0}
  details.cli summary::-webkit-details-marker{display:none}
  details.cli summary .chev{margin-left:auto;color:var(--text-3);transition:transform .15s}
  details.cli[open] summary .chev{transform:rotate(90deg)}
  .clibody{font-size:12px;color:var(--text-2);margin:2px 0 10px 17px;display:grid;gap:4px}
  .clibody .r{display:flex;justify-content:space-between;gap:12px}
  .clibody .r b{color:var(--text);font-weight:600;word-break:break-all;text-align:right}
  .state-row{display:flex;align-items:center;justify-content:space-between;min-height:46px;padding:4px 0}
  .state-row+.state-row{border-top:1px solid var(--line-soft)}
  .state-row .k{font-size:13.5px;color:var(--text-2);font-weight:500}
  .state-row .v{display:flex;align-items:center;gap:8px;font-size:13.5px;font-weight:600;letter-spacing:.01em;text-align:right}
  /* settings */
  .setgrid{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:4px}
  label.f{font-size:10.5px;color:var(--text-3);font-weight:600;text-transform:uppercase;letter-spacing:.05em;display:block;margin-bottom:4px}
  input,textarea{width:100%;background:var(--card-2);border:1px solid var(--line);border-radius:8px;color:var(--text);padding:11px 12px;font-size:14px;font-family:inherit;min-height:44px}
  textarea{min-height:76px;resize:vertical;line-height:1.5}
  .settok{margin-top:10px}
  .setbtn{margin-top:12px;width:100%;background:var(--teal);color:#04211f;border:0;border-radius:9px;padding:13px;font-size:14px;font-weight:700;cursor:pointer;font-family:inherit;min-height:46px}
  .setbtn:disabled{opacity:.5;cursor:default}
  /* secondary/trigger button: quiet, for occasional actions (speedtest, rotate) so
     they don't outshout the data. Filled teal stays for commit actions only. */
  .setbtn.sec{background:var(--card-2);color:var(--teal);border:1px solid var(--line);font-weight:600}
  .setbtn.sec:active{background:#232a33}
  .spd{display:grid;grid-template-columns:1fr 1fr 1fr;gap:8px;margin-bottom:4px}
  .spdcell{text-align:center;background:var(--card-2);border:1px solid var(--line);border-radius:9px;padding:12px 6px}
  .spdv{font-size:24px;font-weight:700;letter-spacing:-.02em;line-height:1}
  .spdl{font-size:10.5px;color:var(--text-3);font-weight:600;margin-top:5px;text-transform:uppercase;letter-spacing:.04em}
  .minibtn{background:var(--card-2);border:1px solid var(--line);color:var(--teal);border-radius:8px;padding:7px 13px;font-size:12.5px;font-weight:600;cursor:pointer;font-family:inherit;min-height:34px}
  .minibtn:disabled{opacity:.5;cursor:default}
  .apbtns{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:10px}
  .apbtns .minibtn{min-height:44px}
  /* nearby-networks list (tap a row to add/remove from the whitelist) */
  .nrow{display:flex;align-items:center;gap:11px;width:100%;background:none;border:0;border-top:1px solid var(--line-soft);padding:11px 2px;min-height:48px;cursor:pointer;color:var(--text);font-family:inherit;text-align:left}
  .nrow:first-child{border-top:0}
  .nname{flex:1;min-width:0;font-size:13.5px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .nchip{flex:none;font-size:11.5px;font-weight:600;color:var(--text-3)}
  .nrow.on .nchip{color:var(--teal)}
  .nbars{display:inline-flex;align-items:flex-end;gap:2px;height:15px;flex:none}
  .nbars>i{width:3px;background:#30363d;border-radius:1px}
  .nbars>i:nth-child(1){height:5px}.nbars>i:nth-child(2){height:8px}.nbars>i:nth-child(3){height:11px}.nbars>i:nth-child(4){height:15px}
  .nbars.b1>i:nth-child(-n+1),.nbars.b2>i:nth-child(-n+2),.nbars.b3>i:nth-child(-n+3),.nbars.b4>i:nth-child(-n+4){background:var(--teal)}
  .nrow:focus-visible{outline:2px solid var(--teal);outline-offset:-2px;border-radius:6px}
  /* currently-whitelisted list (removable chips) */
  .wlhead{font-size:10.5px;font-weight:600;letter-spacing:.05em;text-transform:uppercase;color:var(--text-3);margin:14px 0 8px}
  .wlchip{display:flex;align-items:center;gap:8px;background:var(--card-2);border:1px solid var(--line);border-radius:8px;padding:6px 6px 6px 12px;margin-bottom:6px;min-height:44px}
  .wlname{flex:1;min-width:0;font-size:13.5px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .wlx{flex:none;background:none;border:0;color:var(--text-3);font-size:22px;line-height:1;cursor:pointer;width:44px;height:44px;border-radius:7px;font-family:inherit}
  .wlx:active,.wlx:focus-visible{color:var(--red);outline:none;background:rgba(240,82,78,.12)}
  /* selects share the input look; native arrow hidden for a consistent field */
  select{width:100%;background:var(--card-2);border:1px solid var(--line);border-radius:8px;color:var(--text);padding:11px 34px 11px 12px;font-size:14px;font-family:inherit;min-height:44px;-webkit-appearance:none;appearance:none;background-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%237d8794' stroke-width='2.4' stroke-linecap='round'%3E%3Cpolyline points='6 9 12 15 18 9'/%3E%3C/svg%3E");background-repeat:no-repeat;background-position:right 12px center}
  select:focus{outline:none;border-color:var(--teal)}
  /* preset rows: name+meta open the editor, then Apply, then delete */
  .prow{display:flex;align-items:center;gap:9px;padding:9px 0;border-top:1px solid var(--line-soft)}
  .prow:first-child{border-top:0}
  .pmain{flex:1;min-width:0;background:none;border:0;text-align:left;color:var(--text);font-family:inherit;cursor:pointer;padding:2px 0}
  .pname{font-size:14px;font-weight:600;letter-spacing:-.01em;display:flex;align-items:center;gap:7px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .pname .tag{flex:none;font-size:9.5px;font-weight:700;letter-spacing:.04em;text-transform:uppercase;color:var(--teal);background:rgba(63,184,175,.12);border:1px solid rgba(63,184,175,.3);border-radius:999px;padding:1px 7px}
  .pmeta{font-size:11.5px;color:var(--text-3);margin-top:2px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .papply{flex:none}
  .pmain:focus-visible{outline:2px solid var(--teal);outline-offset:-2px;border-radius:6px}
  /* nearby add-chips in the trigger picker */
  .pchips{display:flex;flex-wrap:wrap;gap:7px;margin-top:9px}
  .pchip{background:var(--card-2);border:1px solid var(--line);color:var(--text-2);border-radius:999px;padding:7px 12px;font-size:12px;font-weight:600;cursor:pointer;font-family:inherit;min-height:36px;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .pchip.added{color:var(--teal);border-color:rgba(63,184,175,.4)}
  .setmsg{margin-top:8px;font-size:12px;color:var(--text-2);min-height:14px}
  .footer{margin-top:14px;text-align:center;font-size:11px;color:var(--text-3);font-weight:500}
  .errslot{margin-top:12px;display:none;background:rgba(240,82,78,.09);border:1px solid rgba(240,82,78,.32);color:#ff9b98;border-radius:10px;padding:10px 13px;font-size:12.5px;font-weight:500}
  .errslot.show{display:block}
  .sec-label{font-size:10.5px;font-weight:600;letter-spacing:.09em;text-transform:uppercase;color:var(--text-3);margin:14px 2px 8px}
  .intg{display:flex;align-items:flex-start;gap:12px}
  .intg .logo{width:40px;height:40px;border-radius:11px;flex:none;display:grid;place-items:center}
  .intg .body{flex:1;min-width:0}
  .intg .name{font-size:14.5px;font-weight:600;letter-spacing:-.01em}
  .intg .desc{font-size:12px;color:var(--text-2);margin-top:2px}
  .intg-foot{margin-top:12px;padding-top:11px;border-top:1px solid var(--line-soft);display:flex;align-items:center;gap:8px;font-size:12px;color:var(--text-2);font-weight:500}
  .managed{display:inline-flex;align-items:center;gap:6px;flex:none;font-size:11.5px;font-weight:600;background:rgba(63,185,80,.1);border:1px solid rgba(63,185,80,.3);color:#5ed36c;padding:6px 11px;border-radius:999px}
  nav{position:fixed;left:50%;transform:translateX(-50%);bottom:0;width:100%;max-width:720px;z-index:20;height:calc(var(--tabbar-h) + env(safe-area-inset-bottom));padding-bottom:env(safe-area-inset-bottom);background:rgba(13,16,20,.86);-webkit-backdrop-filter:blur(14px);backdrop-filter:blur(14px);border-top:1px solid var(--line);display:flex}
  nav .tab{flex:1;background:none;border:0;cursor:pointer;color:var(--text-3);display:flex;flex-direction:column;align-items:center;justify-content:center;gap:3px;font-size:10px;font-weight:600;transition:color .18s ease}
  nav .tab svg{width:21px;height:21px}
  nav .tab.active{color:var(--text)}nav .tab.active svg{color:var(--teal)}
  nav .tab:focus-visible,.setbtn:focus-visible,.minibtn:focus-visible,details.cli summary:focus-visible{outline:2px solid var(--teal);outline-offset:-2px;border-radius:8px}
  input:focus,textarea:focus{outline:none;border-color:var(--teal)}
  input::placeholder,textarea::placeholder{color:var(--text-3);opacity:1}
  /* press + hover feedback. Hover is gated so a tap on a touch screen doesn't
     leave a stuck hover state. */
  .setbtn:active:not(:disabled),.minibtn:active:not(:disabled){transform:translateY(1px)}
  .minibtn:active:not(:disabled){background:#232a33}
  @media (hover:hover){
    .setbtn:hover:not(:disabled){filter:brightness(1.06)}
    .minibtn:hover:not(:disabled){border-color:var(--teal)}
    nav .tab:hover{color:var(--text-2)}
  }
  @media (prefers-reduced-motion:reduce){
    *,*::before,*::after{transition-duration:.01ms!important;animation-duration:.01ms!important}
  }
  /* Cover screen (~352×308): trim chrome so each tab is at most a short scroll */
  @media (max-height:420px){
    header{padding:6px 0 6px}
    .hero{padding:10px 12px 8px}
    .ring-wrap{width:min(38vh,140px)}
    .ring-label{margin-top:6px}
    .card{padding:12px}
    .stat-val,.cpu-load{font-size:26px}
    .cores{height:52px}
    :root{--tabbar-h:54px}
  }
</style>
</head>
<body>
<div class="app">
  <header>
    <div class="sigind">
      <span class="bars" id="bars"><i></i><i></i><i></i><i></i></span>
      <div><div class="lab" id="tech">—</div><div class="op mono" id="wanip">—</div></div>
    </div>
    <div class="netbadge"><span class="dot amber" id="statusDot"></span><span id="netType">—</span></div>
    <!-- op kept for the operator name, shown on the Network signal card -->
  </header>
  <div class="errslot" id="errSlot" role="alert"></div>

  <section class="screen active" id="home">
    <div class="card hero">
      <div class="ring-wrap">
        <svg viewBox="0 0 120 120" aria-hidden="true">
          <circle cx="60" cy="60" r="52" fill="none" stroke="var(--track)" stroke-width="11"/>
          <circle id="ringFill" cx="60" cy="60" r="52" fill="none" stroke="var(--teal)" stroke-width="11" stroke-linecap="round" stroke-dasharray="326.7" stroke-dashoffset="326.7" style="transition:stroke-dashoffset .7s ease,stroke .4s ease"/>
        </svg>
        <div class="ring-center">
          <div class="ring-pct num"><span id="ringPct">0</span><span>%</span></div>
          <div class="ring-sub"><b class="num" id="ringUsed" style="color:var(--text)">—</b> of 512 GB</div>
        </div>
      </div>
      <div class="ring-label">Mobile data · this month</div>
      <div class="ring-caption">Today <b class="num" id="capToday">—</b> · Week <b class="num" id="capWeek">—</b></div>
    </div>
    <div class="duo">
      <div class="card">
        <div class="stat-head"><span>Battery</span><span class="accent" id="battAccent" style="background:var(--green)"></span></div>
        <div class="stat-val num"><span id="battLevel">—</span><small>%</small></div>
        <div class="stat-sub num" id="battSub">—</div>
      </div>
      <div class="card">
        <div class="stat-head"><span>Temp</span><span class="accent" id="tempAccent" style="background:var(--amber)"></span></div>
        <div class="stat-val num" id="tempVal" style="color:var(--amber)"><span id="tempMax">—</span><small>°C</small></div>
        <div class="stat-sub num" id="tempSub">—</div>
      </div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Hotspot preset</span><span id="hpActive" style="color:var(--text-2)">—</span></div>
      <div id="hpQuick" class="pchips"><div class="stat-sub">No presets — add them in the Presets tab.</div></div>
      <div class="setmsg" id="hpMsg">Tap to switch the hotspot. Clients drop briefly (~5s), then reconnect.</div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Speedtest</span></div>
      <div class="spd" id="spdRes">
        <div class="spdcell"><div class="spdv num" id="spdDown">—</div><div class="spdl">↓ Mbps</div></div>
        <div class="spdcell"><div class="spdv num" id="spdUp">—</div><div class="spdl">↑ Mbps</div></div>
        <div class="spdcell"><div class="spdv num" id="spdPing">—</div><div class="spdl">ping ms</div></div>
      </div>
      <button class="setbtn sec" id="spdBtn">Run speedtest</button>
      <div class="setmsg" id="spdMsg">Uses mobile data (tens–hundreds of MB) and heats the radio — run it deliberately. ~15–40s.</div>
    </div>
    <div class="footer">updated <span id="updated">—</span></div>
  </section>

  <section class="screen" id="net">
    <div class="card">
      <div class="stat-head"><span>Signal</span><span id="sigTech" style="color:var(--text-2)"></span></div>
      <div class="sg" id="sig"><div class="stat-sub">loading…</div></div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Hotspot</span><span class="accent" id="hsAccent" style="background:var(--green)"></span></div>
      <div class="state-row"><span class="k">State</span><span class="v"><span class="dot off" id="hsDot"></span><span id="hsState">—</span></span></div>
      <div class="state-row"><span class="k">Auto (SSID whitelist)</span><span class="v" id="hsAuto">off</span></div>
      <div class="state-row" id="hsMatchRow" style="display:none"><span class="k">Seen nearby</span><span class="v" id="hsMatch">—</span></div>
      <div class="stat-sub" id="hsSub"></div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Connectivity</span></div>
      <div class="state-row"><span class="k">WAN IP</span><span class="v mono" id="wanIp">—</span></div>
      <div class="state-row"><span class="k">Airplane</span><span class="v"><span class="dot off" id="apDot"></span><span id="apState">off</span></span></div>
      <button class="setbtn sec" id="rotateBtn">Rotate IP (airplane cycle)</button>
      <button class="setbtn" id="hotspotOnBtn" style="margin-top:8px;background:var(--green);color:#04211f">Turn hotspot on</button>
      <div class="apbtns">
        <button class="minibtn" id="apOnBtn">Airplane on</button>
        <button class="minibtn" id="apOffBtn">Airplane off</button>
      </div>
      <div class="setmsg" id="apMsg">Cycles airplane to pull a fresh carrier IP, then restarts the hotspot. ~15–30s; clients drop briefly.</div>
    </div>
    <div class="card">
      <div class="state-row"><span class="k">Bands</span><span class="v"><span class="dot off" id="bandDot"></span><span id="bandsv">—</span></span></div>
      <div class="state-row"><span class="k">Ingress</span><span class="v"><span class="dot green"></span><span>loopback/tailscale</span></span></div>
    </div>
  </section>

  <section class="screen" id="clientsScr">
    <div class="card">
      <div class="stat-head"><span>Clients</span><span id="clientsN" style="color:var(--text-2)">0</span></div>
      <div id="clients"><div class="stat-sub">no clients</div></div>
    </div>
  </section>

  <section class="screen" id="system">
    <div class="card">
      <div class="stat-head"><span>CPU · per core</span></div>
      <div class="cpu-head">
        <div class="cpu-load num"><span id="cpuLoad">—</span><small>load 5m</small></div>
        <div class="cpu-meta"><span id="cpuMode">—</span> · <span id="cpuCores">8</span> cores</div>
      </div>
      <div class="cores" id="cores"></div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Memory</span></div>
      <div class="stat-val num"><span id="memPct">—</span><small>%</small></div>
      <div class="bar"><i id="memBar" style="width:0%"></i></div>
    </div>
    <div class="card">
      <div class="state-row"><span class="k">Thermal policy</span><span class="v"><span class="dot amber" id="policyDot"></span><span id="policyState">—</span></span></div>
      <div class="state-row"><span class="k">CPU mode</span><span class="v"><span class="dot green" id="cpuDot"></span><span id="cpuModeRow">—</span></span></div>
    </div>
  </section>

  <section class="screen" id="presets">
    <div class="card">
      <div class="stat-head"><span>Auto-switch by location</span><span class="accent" id="paAccent" style="background:#30363d"></span></div>
      <div class="state-row"><span class="k">Status</span><span class="v"><span class="dot off" id="paDot"></span><span id="paState">off</span></span></div>
      <div class="setgrid" style="grid-template-columns:1fr 1fr">
        <button class="minibtn" id="paOnBtn" style="min-height:44px">Turn on</button>
        <button class="minibtn" id="paOffBtn" style="min-height:44px">Turn off</button>
      </div>
      <div class="setmsg">Switches the hotspot preset when a preset's trigger Wi-Fi comes into range. Rides the hotspot scan; needs location services ON.</div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Presets</span><span id="pCount" style="color:var(--text-2)"></span></div>
      <div id="pList"><div class="stat-sub">No presets yet — create one below.</div></div>
    </div>
    <div class="card">
      <div class="stat-head"><span id="pFormTitle">Create preset</span><button class="minibtn" id="pNewBtn" style="display:none">New</button></div>
      <label class="f" for="pName">Preset name</label>
      <input id="pName" type="text" placeholder="Home, Cafe, Event…" maxlength="32">
      <div class="settok"><label class="f" for="pSsid">Network name (SSID)</label>
      <input id="pSsid" type="text" placeholder="What devices see" maxlength="32"></div>
      <div class="settok"><label class="f" for="pPass">Password</label>
      <input id="pPass" type="password" placeholder="8–63 chars" autocomplete="new-password"></div>
      <div class="setgrid">
        <div><label class="f" for="pSec">Security</label><select id="pSec"><option value="wpa2">WPA2</option><option value="wpa3">WPA3</option><option value="open">Open (no password)</option></select></div>
        <div><label class="f" for="pBand">Band</label><select id="pBand"><option value="5">5 GHz · faster</option><option value="2">2.4 GHz · range</option><option value="6">6 GHz</option></select></div>
      </div>
      <div class="settok"><label class="f" for="pTrig">Trigger networks — seeing any one switches to this preset (one per line)</label>
      <textarea id="pTrig" placeholder="CafeWifi&#10;ACME-staff"></textarea></div>
      <button class="minibtn" id="pScanBtn">Scan nearby to add</button>
      <div id="pNearby"></div>
      <button class="setbtn" id="pSaveBtn">Save preset</button>
      <div class="setmsg" id="pMsg">Applying a preset briefly bounces the hotspot — connected devices reconnect to the new name.</div>
    </div>
  </section>

  <section class="screen" id="settings">
    <div class="card">
      <div class="stat-head"><span>Thermal gate · adjust</span></div>
      <div class="setgrid">
        <div><label class="f" for="setWarn">Warn °C</label><input id="setWarn" type="number" step="0.5" inputmode="decimal"></div>
        <div><label class="f" for="setGate">Gate °C</label><input id="setGate" type="number" step="0.5" inputmode="decimal"></div>
      </div>
      <button class="setbtn" id="setBtn">Apply thermal limits</button>
      <div class="setmsg" id="setMsg">Gate is hard-capped at 48°C; Samsung mitigation is unaffected.</div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Nearby networks</span><button class="minibtn" id="scanBtn">Scan now</button></div>
      <div id="nearby"><div class="stat-sub">Tap “Scan now” to list networks in range. A ✓ marks whitelisted ones; tap a row to add or remove it.</div></div>
      <div class="setmsg" id="nearbyMsg"></div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Hotspot auto-toggle</span></div>
      <label class="f" for="wlBox">SSID whitelist (one per line — hotspot turns OFF when seen, back ON when absent)</label>
      <textarea id="wlBox" placeholder="HomeWifi&#10;OfficeWifi"></textarea>
      <button class="setbtn" id="wlBtn">Save whitelist</button>
      <div class="setmsg" id="wlMsg">Empty list disables the auto-toggle. Needs location services ON to scan.</div>
      <div id="wlList"></div>
    </div>
    <div class="card">
      <label class="f" for="setTok">radio-control token (stored locally, used by both forms)</label>
      <input id="setTok" type="password" placeholder="paste once">
    </div>
    <div class="card">
      <div class="stat-head"><span>Add a device</span></div>
      <div id="qrWrap" style="display:flex;flex-direction:column;align-items:center;gap:10px">
        <img id="qrImg" alt="Scan to open on another device" width="200" height="200" style="border-radius:10px;background:#fff;padding:8px;display:none">
        <div class="stat-sub" id="qrHint">Point another phone's camera here — it opens the dashboard and remembers the token.</div>
      </div>
      <button class="setbtn" id="installBtn" style="display:none">Install app (add to home screen)</button>
      <div class="setmsg" id="qrMsg"></div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Open reads (no token on the tailnet)</span><span class="accent" id="orAccent" style="background:#30363d"></span></div>
      <div class="state-row"><span class="k">Status</span><span class="v"><span class="dot off" id="orDot"></span><span id="orState">off</span></span></div>
      <div class="setgrid" style="grid-template-columns:1fr 1fr">
        <button class="minibtn" id="orOnBtn" style="min-height:44px">Turn on</button>
        <button class="minibtn" id="orOffBtn" style="min-height:44px">Turn off</button>
      </div>
      <div class="setmsg" id="orMsg">On: any device on your tailnet opens the dashboard with no token — read-only. Off: a token (or the QR) is required. Needs the radio-control token to change.</div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Open control (no token for writes)</span><span class="accent" id="ocAccent" style="background:#30363d"></span></div>
      <div class="state-row"><span class="k">Status</span><span class="v"><span class="dot off" id="ocDot"></span><span id="ocState">off</span></span></div>
      <div class="setgrid" style="grid-template-columns:1fr 1fr">
        <button class="minibtn" id="ocOnBtn" style="min-height:44px">Turn on</button>
        <button class="minibtn" id="ocOffBtn" style="min-height:44px">Turn off</button>
      </div>
      <div class="setmsg" id="ocMsg">On: writes (airplane, thermal, whitelist, reboot…) need no token on your tailnet. SMS always keeps its token. Enabling needs the radio-control token once.</div>
    </div>
    <div class="sec-label">Integrations</div>
    <div class="card">
      <div class="intg">
        <div class="logo" style="background:#5865f2"><svg width="24" height="24" viewBox="0 0 24 24" fill="#fff"><path d="M19.6 5.3A18 18 0 0 0 15 3.9l-.24.47a13 13 0 0 1 4 .96 12.9 12.9 0 0 0-11.5 0 13 13 0 0 1 4-.96L11 3.9A18 18 0 0 0 6.4 5.3C3.5 9.6 2.7 13.8 3.1 17.9a18 18 0 0 0 5.5 2.8l.45-.98a12 12 0 0 1-1.9-.9l.35-.27a12.9 12.9 0 0 0 11 0l.35.27c-.6.36-1.24.66-1.9.9l.45.98a18 18 0 0 0 5.5-2.8c.47-4.77-.79-8.94-3.75-12.6ZM9.35 15.4c-.9 0-1.63-.82-1.63-1.83 0-1 .72-1.83 1.63-1.83.9 0 1.64.83 1.62 1.83 0 1-.72 1.83-1.62 1.83Zm5.3 0c-.9 0-1.63-.82-1.63-1.83 0-1 .72-1.83 1.63-1.83.9 0 1.64.83 1.62 1.83 0 1-.72 1.83-1.62 1.83Z"/></svg></div>
        <div class="body"><div class="name">Discord</div><div class="desc">Self-hosted Gateway bot · see selfhost/</div></div>
        <span class="managed">Bot</span>
      </div>
    </div>
    <div class="card">
      <div class="intg">
        <div class="logo" style="background:#111318;border:1px solid var(--line)"><svg width="22" height="22" viewBox="0 0 24 24" fill="#e6edf3"><circle cx="5" cy="5" r="2.1" opacity=".35"/><circle cx="12" cy="5" r="2.1" opacity=".35"/><circle cx="19" cy="5" r="2.1" opacity=".35"/><circle cx="5" cy="12" r="2.1"/><circle cx="12" cy="12" r="2.1"/><circle cx="19" cy="12" r="2.1"/><circle cx="5" cy="19" r="2.1" opacity=".35"/><circle cx="12" cy="19" r="2.1" opacity=".35"/><circle cx="19" cy="19" r="2.1" opacity=".35"/></svg></div>
        <div class="body"><div class="name">Tailscale</div><div class="desc">Secure ingress · wired through tailscaled</div></div>
        <span class="managed">Managed</span>
      </div>
    </div>
  </section>
</div>

<nav>
  <button class="tab active" data-screen="home" aria-current="page"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="8" height="8" rx="1.6"/><rect x="13" y="3" width="8" height="5" rx="1.6"/><rect x="13" y="10" width="8" height="11" rx="1.6"/><rect x="3" y="13" width="8" height="8" rx="1.6"/></svg>Home</button>
  <button class="tab" data-screen="net"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M2 20h.01M7 20v-4M12 20v-8M17 20V8M22 20V4"/></svg>Network</button>
  <button class="tab" data-screen="clientsScr"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75"/></svg>Clients</button>
  <button class="tab" data-screen="system"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3M15 1v3M9 20v3M15 20v3M1 9h3M1 15h3M20 9h3M20 15h3"/></svg>System</button>
  <button class="tab" data-screen="presets"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>Presets</button>
  <button class="tab" data-screen="settings"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>Settings</button>
</nav>

<script>
(function(){
  "use strict";
  // Same-origin (relative) so the page works whether served on 127.0.0.1:18080
  // in the kiosk WebView, on a non-default bind_port, or proxied over Tailscale
  // — and never fetches the viewer's own localhost when opened remotely.
  var API="", GIB=1024*1024*1024, CAP=512*GIB;
  var WARN_C=44, GATE_C=46;

  var token="";
  try{
    var url=new URL(window.location.href), t=url.searchParams.get("token"), rt=url.searchParams.get("rtoken");
    if(t){localStorage.setItem("zf5tok",t);}
    // radio-control token is seeded by the owner's kiosk (action.sh) so writes
    // auto-fill. Stored in this WebView's sandboxed localStorage; a plain
    // browser opening the dashboard without &rtoken keeps the paste-once flow.
    if(rt){localStorage.setItem("zf5rtok",rt);}
    if(t||rt){url.searchParams.delete("token");url.searchParams.delete("rtoken");window.history.replaceState({},"",url.pathname);}
    token=localStorage.getItem("zf5tok")||"";
  }catch(e){}

  var active="home", lastKick=0;
  var tabs=document.querySelectorAll("nav .tab");
  tabs.forEach(function(tab){tab.addEventListener("click",function(){
    var id=tab.getAttribute("data-screen");
    tabs.forEach(function(t){t.classList.toggle("active",t===tab);if(t===tab)t.setAttribute("aria-current","page");else t.removeAttribute("aria-current");});
    document.querySelectorAll(".screen").forEach(function(sc){sc.classList.toggle("active",sc.id===id);});
    window.scrollTo(0,0);
    active=id;
    if(id==="settings"&&typeof loadQR==="function")loadQR();
    // Refresh the newly shown tab, but throttle: rapid tab-hopping must not burst
    // past the read-status rate limit (each tick is 2-3 requests).
    var now=Date.now();
    if(now-lastKick>1500){lastKick=now;tick();}
  });});

  var coresEl=document.getElementById("cores"), coreEls=[];
  function buildCores(n){coresEl.innerHTML="";coreEls=[];for(var i=0;i<n;i++){var c=document.createElement("div");c.className="core";var tr=document.createElement("div");tr.className="track";var f=document.createElement("div");f.className="fill";f.style.height="0%";var idx=document.createElement("div");idx.className="idx num";idx.textContent=i;tr.appendChild(f);c.appendChild(tr);c.appendChild(idx);coresEl.appendChild(c);coreEls.push({core:c,fill:f});}}
  buildCores(8);

  function get(p){var h={};if(token)h.Authorization="Bearer "+token;return fetch(API+p,{headers:h}).then(function(r){if(!r.ok)throw new Error(p+" "+r.status);return r.json();});}
  var errEl=document.getElementById("errSlot");
  function showErr(m){errEl.textContent=m;errEl.classList.add("show");}
  function clearErr(){errEl.classList.remove("show");}
  function esc(s){return String(s==null?"":s).replace(/[&<>]/g,function(c){return {"&":"&amp;","<":"&lt;",">":"&gt;"}[c];});}
  function fmtBytes(b){var gb=b/GIB;if(gb>=1000)return (gb/1024).toFixed(2)+" TB";return (gb>=10?Math.round(gb):gb.toFixed(1))+" GB";}
  function tempColor(c){return c>=GATE_C?"var(--red)":(c>=WARN_C?"var(--amber)":"var(--teal)");}
  function usageColor(p){return p>0.9?"var(--red)":(p>=0.7?"var(--amber)":"var(--teal)");}
  var CIRC=2*Math.PI*52;

  function rsrpCls(v){return v>=-95?"good":(v>=-110?"mid":"low");}
  function sinrCls(v){return v>=13?"good":(v>=0?"mid":"low");}
  function stCls(s){return s=="REACHABLE"?"green":(s=="FAILED"?"red":(s=="DELAY"||s=="PROBE"?"amber":"off"));}

  // SAFE/RECOVERY are safe; WARM is caution; HOT/COOLDOWN are hot. RECOVERY is a
  // transitional-but-safe state (radio writes are allowed), so it must not read red.
  function polColor(p){return (p==="SAFE"||p==="RECOVERY")?"green":(p==="WARM"?"amber":"red");}
  function renderStatus(s){
    var net=s.network||{}, bat=s.battery||{}, th=s.thermal||{}, ip=s.wan_ip||{};
    if(typeof s.open_reads==="boolean"&&document.getElementById("orState"))renderOpenReads(s.open_reads);
    if(typeof s.open_control==="boolean"){openControl=s.open_control;if(document.getElementById("ocState"))renderOpenControl(s.open_control);}
    if(s.hotspot_presets)renderPresets(s.hotspot_presets);
    // Always show the WAN IP (header). Airplane on / no data -> explicit label.
    document.getElementById("wanip").textContent=s.airplane?"airplane ✈":(ip.available&&ip.ip?ip.ip:"no data");
    var ipEl=document.getElementById("wanIp"); if(ipEl){ipEl.textContent=ip.available&&ip.ip?ip.ip:(s.airplane?"— (airplane on)":"— (no data)");}
    var apEl=document.getElementById("apState"); if(apEl){apEl.textContent=s.airplane?"ON":"off";document.getElementById("apDot").className="dot "+(s.airplane?"amber":"off");}
    var pol=s.policy_state||"—", dc=polColor(pol);
    // Header badge shows thermal-policy health (the device's key risk), not a second
    // copy of the radio tech that already sits on the left.
    document.getElementById("netType").textContent=pol;
    document.getElementById("statusDot").className="dot "+dc;
    document.getElementById("policyDot").className="dot "+dc;
    document.getElementById("policyState").textContent=pol;
    if(th.warn_c)WARN_C=th.warn_c; if(th.gate_c)GATE_C=th.gate_c;

    // Collector unavailable -> show em-dash, not a fake 0% red battery.
    var battEl=document.getElementById("battLevel");
    if(bat.available===false||bat.level==null){
      battEl.textContent="—";document.getElementById("battSub").textContent="unavailable";
      document.getElementById("battAccent").style.background="#30363d";
    }else{
      var lvl=Math.round(bat.level);
      battEl.textContent=lvl;
      document.getElementById("battSub").textContent=(bat.plugged||"")+" · "+(bat.temp_c!=null?bat.temp_c.toFixed(1)+"°C":"");
      var bcol=lvl<=10?"var(--red)":(lvl<=20?"var(--amber)":"var(--green)");
      document.getElementById("battAccent").style.background=bcol;
    }

    var tmax=th.temp_max_c, hasT=tmax!=null&&tmax>0, tcol=hasT?tempColor(tmax):"#30363d";
    document.getElementById("tempMax").textContent=hasT?tmax.toFixed(1):"—";
    document.getElementById("tempVal").style.color=tcol;
    document.getElementById("tempAccent").style.background=tcol;
    document.getElementById("tempSub").textContent=hasT?("batt "+(th.battery_c!=null?th.battery_c.toFixed(1)+"°":"—")+" · "+(th.safe?"safe":"UNSAFE")+" · gate "+GATE_C+"°"):"sensor unavailable";

    var h=s.health||{};
    document.getElementById("cpuLoad").textContent=h.cpu_load5!=null?h.cpu_load5.toFixed(2):"—";
    var cores=h.cpu_cores||0;
    document.getElementById("cpuCores").textContent=cores||"—";
    var per=h.per_core_pct;
    if(!per||!per.length){per=[];for(var i=0;i<cores;i++)per.push(0);}
    if(per.length&&coreEls.length!==per.length)buildCores(per.length);
    per.forEach(function(p,i){if(!coreEls[i])return;p=Math.max(0,Math.min(100,p));coreEls[i].fill.style.height=p+"%";coreEls[i].fill.style.background=p>85?"var(--red)":(p>60?"var(--amber)":"var(--violet)");});

    var mp=h.mem_used_pct!=null?h.mem_used_pct:0;document.getElementById("memPct").textContent=h.mem_used_pct!=null?mp:"—";document.getElementById("memBar").style.width=mp+"%";
    document.getElementById("updated").textContent=new Date().toLocaleTimeString([], {hour:"2-digit",minute:"2-digit",second:"2-digit"});
  }

  function renderUsage(u){
    var pct=u.month_bytes/CAP, pc=Math.min(1,pct);
    document.getElementById("ringPct").textContent=Math.round(pct*100);
    document.getElementById("ringUsed").textContent=fmtBytes(u.month_bytes);
    var ring=document.getElementById("ringFill");ring.style.stroke=usageColor(pct);ring.style.strokeDashoffset=(CIRC*(1-pc)).toFixed(1);
    document.getElementById("capToday").textContent=u.today_human;document.getElementById("capWeek").textContent=u.week_human;
  }

  function renderSignal(sig){
    var lvl=sig.level||0, bars=document.getElementById("bars");
    var cls=sig.rsrp_dbm?(sig.rsrp_dbm>=-95?"g":(sig.rsrp_dbm>=-110?"a":"r")):"g";
    bars.className="bars "+cls;
    var ch=bars.children;for(var i=0;i<4;i++)ch[i].className=i<lvl?"on":"";
    document.getElementById("tech").textContent=sig.display||sig.tech||"—";
    var is5g=(sig.display||"").indexOf("5G")===0;
    var tag=is5g?"NR "+(sig.nr_state||""):(sig.carrier_aggregation?"LTE-CA":"");
    document.getElementById("sigTech").textContent=(sig.operator||"")+(tag?" · "+tag:"");
    var box=document.getElementById("sig");
    if(!sig.available){box.innerHTML='<div class="stat-sub">unavailable</div>';return;}
    function row(k,v,c){return '<div class="sgrow"><span>'+esc(k)+'</span><b class="'+(c||"")+'">'+esc(v)+'</b></div>';}
    // The daemon maps Android's Integer.MAX_VALUE "unavailable" sentinel to 0, so
    // a metric of exactly 0 (non-physical for dBm/dB) means "not reported" — the
    // rows below guard on truthiness and omit it rather than show a green "0 dBm".
    var h="";
    h+=row("Tech",(sig.display||sig.tech||"—")+(sig.carrier_aggregation?" · CA":""));
    if(sig.nr_state&&sig.nr_state!=="NONE")h+=row("NR state",sig.nr_state,sig.nr_state==="CONNECTED"?"good":"");
    if(sig.band)h+=row("Band","B"+sig.band);
    if(sig.earfcn)h+=row("EARFCN",sig.earfcn);
    if(sig.rsrp_dbm)h+=row("RSRP",sig.rsrp_dbm+" dBm",rsrpCls(sig.rsrp_dbm));
    if(sig.rsrq_db)h+=row("RSRQ",sig.rsrq_db+" dB");
    if(sig.sinr_db)h+=row("SINR",sig.sinr_db+" dB",sinrCls(sig.sinr_db));
    if(sig.rssi_dbm)h+=row("RSSI",sig.rssi_dbm+" dBm");
    if(sig.pci)h+=row("PCI",sig.pci);
    if(sig.tac)h+=row("TAC",sig.tac);
    if(sig.mcc)h+=row("PLMN",sig.mcc+"/"+sig.mnc);
    if(sig.nr_band)h+=row("NR band","n"+sig.nr_band);
    if(sig.nr_rsrp_dbm)h+=row("NR RSRP",sig.nr_rsrp_dbm+" dBm",rsrpCls(sig.nr_rsrp_dbm));
    if(sig.nr_sinr_db)h+=row("NR SINR",sig.nr_sinr_db+" dB",sinrCls(sig.nr_sinr_db));
    box.innerHTML=h;
  }

  function renderClients(cl){
    document.getElementById("clientsN").textContent=(cl.count||0)+(cl.count===1?" client":" clients");
    var box=document.getElementById("clients");
    if(!cl.clients||!cl.clients.length){box.innerHTML='<div class="stat-sub">no clients</div>';return;}
    var html="";
    cl.clients.forEach(function(c){
      var st=c.state||"?";
      var v6=(c.ipv6&&c.ipv6.length)?'<div class="r"><span>IPv6</span><b class="mono" style="font-size:10.5px;text-align:right">'+c.ipv6.map(esc).join("<br>")+'</b></div>':"";
      html+='<details class="cli"><summary><span class="dot '+stCls(st)+'"></span><b class="mono">'+esc(c.ipv4||"(no IPv4)")+'</b><span style="color:var(--text-3);font-size:11.5px">'+esc(st)+'</span><span class="chev">&#8250;</span></summary>'
        +'<div class="clibody"><div class="r"><span>MAC</span><b class="mono">'+esc(c.mac)+'</b></div><div class="r"><span>State</span><b>'+esc(st)+'</b></div>'+v6+'</div></details>';
    });
    box.innerHTML=html;
  }

  function renderCPU(c){
    if(!c)return;
    document.getElementById("cpuMode").textContent=c.mode||"—";
    document.getElementById("cpuModeRow").textContent=(c.mode||"—")+(c.requested&&c.requested!==c.mode?" ("+c.requested+")":"");
    document.getElementById("cpuDot").className="dot "+(c.mode==="performance"?"green":(c.mode==="eco"?"amber":(c.mode==="off"?"off":"green")));
    if(c.cores&&coreEls.length===c.cores.length){c.cores.forEach(function(ci,i){coreEls[i].core.classList.toggle("off",!ci.online);});}
  }

  function renderBands(b){
    if(!b||!b.available){document.getElementById("bandsv").textContent="—";return;}
    document.getElementById("bandsv").textContent=b.is_max?"all unlocked (NR+LTE)":(b.nr_enabled?"NR+…":"LTE only");
    document.getElementById("bandDot").className="dot "+(b.is_max?"green":(b.nr_enabled?"amber":"off"));
  }

  var wlLoaded=false;
  function renderHotspot(h){
    document.getElementById("hsState").textContent=h.active?"on":"off";
    document.getElementById("hsDot").className="dot "+(h.active?"green":"off");
    document.getElementById("hsAccent").style.background=h.active?"var(--green)":"#30363d";
    var auto=h.auto?(h.paused?"paused: "+h.paused.replace("_"," "):"watching "+h.whitelist.length+" SSID"+(h.whitelist.length===1?"":"s")):"off";
    document.getElementById("hsAuto").textContent=auto;
    var mr=document.getElementById("hsMatchRow");
    if(h.matched&&h.matched.length){mr.style.display="";document.getElementById("hsMatch").textContent=h.matched.join(", ");}
    else{mr.style.display="none";}
    var sub=[];
    if(h.last_action)sub.push(h.last_action);
    if(h.auto&&h.ap_count)sub.push(h.ap_count+" APs in last scan");
    document.getElementById("hsSub").textContent=sub.join(" · ");
    if(!wlLoaded&&h.whitelist){document.getElementById("wlBox").value=h.whitelist.join("\n");wlLoaded=true;}
    renderWhitelist(h.whitelist||[]);
    renderNearby(h);
    hsActive=!!h.active; renderHotspotBtn();
  }

  // Hotspot toggle button reflects the current state: press turns it on when off,
  // off when on.
  var hsActive=false;
  function renderHotspotBtn(){
    var b=document.getElementById("hotspotOnBtn"); if(!b)return;
    b.textContent=hsActive?"Turn hotspot off":"Turn hotspot on";
    b.style.background=hsActive?"var(--red)":"var(--green)";
    b.style.color=hsActive?"#fff":"#04211f";
  }

  // Read-only list of what's currently whitelisted, with a × to remove each.
  // Index-based handlers keep SSIDs (spaces, Vietnamese, quotes) out of markup.
  function renderWhitelist(wl){
    var box=document.getElementById("wlList");
    if(!wl.length){box.innerHTML='<div class="stat-sub">Nothing whitelisted yet.</div>';return;}
    var html='<div class="wlhead">Whitelisted ('+wl.length+')</div>';
    wl.forEach(function(s,i){
      html+='<div class="wlchip"><span class="wlname">'+esc(s)+'</span><button class="wlx" data-idx="'+i+'" aria-label="Remove '+esc(s)+' from whitelist">&times;</button></div>';
    });
    box.innerHTML=html;
    box.querySelectorAll(".wlx").forEach(function(b){b.addEventListener("click",function(){
      var s=wl[+b.getAttribute("data-idx")]; if(s!=null)toggleWhitelist(s);
    });});
  }

  // Nearby-networks list. Rows are indexed into nearbyList (not templated with
  // the SSID) so an SSID containing quotes can't break the markup or inject.
  var nearbyList=[];
  function rssiBars(r){return r>=-60?4:(r>=-70?3:(r>=-80?2:1));}
  function renderNearby(h){
    nearbyList=(h&&h.nearby)||[];
    var box=document.getElementById("nearby");
    if(!nearbyList.length){
      box.innerHTML='<div class="stat-sub">'+(h&&h.paused==="location_off"?"Turn on location services, then Scan.":"No scan yet — tap “Scan now”.")+'</div>';
      return;
    }
    var html="";
    nearbyList.forEach(function(ap,i){
      var b=rssiBars(ap.rssi);
      html+='<button class="nrow'+(ap.whitelisted?" on":"")+'" data-idx="'+i+'" aria-pressed="'+(ap.whitelisted?"true":"false")+'">'
        +'<span class="nbars b'+b+'"><i></i><i></i><i></i><i></i></span>'
        +'<span class="nname">'+esc(ap.ssid)+'</span>'
        +'<span class="nchip">'+(ap.whitelisted?"✓ whitelisted":"+ add")+'</span></button>';
    });
    box.innerHTML=html;
    box.querySelectorAll(".nrow").forEach(function(b){b.addEventListener("click",function(){
      var ap=nearbyList[+b.getAttribute("data-idx")]; if(ap)toggleWhitelist(ap.ssid);
    });});
  }

  // Owner controls (radio-control token shared by both forms). localStorage can
  // throw in a storage-blocked WebView context, so every access is guarded — an
  // exception here must not kill the settings buttons or the polling loop.
  function lsGet(k){try{return localStorage.getItem(k)||"";}catch(e){return "";}}
  function lsSet(k,v){try{localStorage.setItem(k,v);}catch(e){}}
  var setMsg=document.getElementById("setMsg"), setTok=document.getElementById("setTok");
  setTok.value=lsGet("zf5rtok");
  var openControl=false; // updated from /v1/status; when true writes need no token
  var OPEN_TOK="__open__"; // sentinel: proceed tokenless (truthy, so rtok callers run)
  function rtok(msgEl){
    var rt=setTok.value.trim();
    if(rt){lsSet("zf5rtok",rt);return rt;}
    if(openControl)return OPEN_TOK; // open-control on: no token needed
    msgEl.textContent="Paste the radio-control token below first.";
    return null;
  }
  function post(path,body,rt){
    var h={"Content-Type":"application/json"};
    if(rt&&rt!==OPEN_TOK)h.Authorization="Bearer "+rt;
    return fetch(API+path,{method:"POST",headers:h,body:JSON.stringify(body)})
      .then(function(r){return r.json().then(function(j){return {ok:r.ok,j:j};});});
  }

  // --- Add a device: QR of this dashboard's URL + token, fetched as a blob so
  // the token rides the auth header, not the <img> src.
  var qrLoaded=false;
  function loadQR(){
    if(qrLoaded)return;
    var h={};if(token)h.Authorization="Bearer "+token;
    fetch(API+"/v1/qr",{headers:h}).then(function(r){if(!r.ok)throw new Error(r.status);return r.blob();})
      .then(function(b){var img=document.getElementById("qrImg");img.src=URL.createObjectURL(b);img.style.display="";qrLoaded=true;})
      .catch(function(){document.getElementById("qrHint").textContent="QR needs a token or Open reads on. (You're seeing this because reads are open, or your token isn't set.)";});
  }

  // --- Install (Add to Home Screen). Chrome/Android fires beforeinstallprompt;
  // iOS has no event — show the manual hint there.
  var deferredPrompt=null, installBtn=document.getElementById("installBtn"), qrMsg=document.getElementById("qrMsg");
  window.addEventListener("beforeinstallprompt",function(e){e.preventDefault();deferredPrompt=e;installBtn.style.display="";});
  installBtn.addEventListener("click",function(){
    if(!deferredPrompt)return; deferredPrompt.prompt();
    deferredPrompt.userChoice.then(function(){deferredPrompt=null;installBtn.style.display="none";});
  });
  (function(){var ios=/iP(hone|ad|od)/.test(navigator.userAgent), standalone=window.navigator.standalone||matchMedia("(display-mode: standalone)").matches;
    if(ios&&!standalone)qrMsg.textContent="On iPhone: Share → Add to Home Screen for a one-tap app.";})();

  // --- Open reads toggle (radio-control).
  var orMsg=document.getElementById("orMsg");
  function setOpenReads(open){
    var rt=rtok(orMsg); if(!rt)return;
    orMsg.textContent="applying…";
    post("/v1/dashboard/open",{open:open},rt).then(function(res){
      if(!res.ok){orMsg.textContent="Error: "+(res.j.error||"failed")+(res.j.code===403?" (needs radio-control token)":"");return;}
      orMsg.textContent=res.j.open_reads?"Open reads ON — any tailnet device can view without a token (read-only).":"Open reads OFF — a token or the QR is required.";
      renderOpenReads(res.j.open_reads);
    }).catch(function(e){orMsg.textContent="Error: "+e.message;});
  }
  document.getElementById("orOnBtn").addEventListener("click",function(){setOpenReads(true);});
  document.getElementById("orOffBtn").addEventListener("click",function(){setOpenReads(false);});
  function renderOpenReads(on){
    document.getElementById("orState").textContent=on?"on":"off";
    document.getElementById("orDot").className="dot "+(on?"amber":"off");
    document.getElementById("orAccent").style.background=on?"var(--amber)":"#30363d";
  }

  // --- Open control toggle (tokenless radio-control writes). radio-control gated
  // to change (need the token once to enable, since it starts off).
  var ocMsg=document.getElementById("ocMsg");
  function setOpenControl(open){
    var rt=rtok(ocMsg); if(!rt)return;
    ocMsg.textContent="applying…";
    post("/v1/dashboard/control",{open:open},rt).then(function(res){
      if(!res.ok){ocMsg.textContent="Error: "+(res.j.error||"failed")+(res.j.code===403?" (needs radio-control token)":"");return;}
      ocMsg.textContent=res.j.open_control?"Open control ON — writes on your tailnet need no token. SMS still does.":"Open control OFF — writes require the radio-control token.";
      renderOpenControl(res.j.open_control);
    }).catch(function(e){ocMsg.textContent="Error: "+e.message;});
  }
  document.getElementById("ocOnBtn").addEventListener("click",function(){setOpenControl(true);});
  document.getElementById("ocOffBtn").addEventListener("click",function(){setOpenControl(false);});
  function renderOpenControl(on){
    openControl=on;
    document.getElementById("ocState").textContent=on?"on":"off";
    document.getElementById("ocDot").className="dot "+(on?"red":"off");
    document.getElementById("ocAccent").style.background=on?"var(--red)":"#30363d";
  }

  document.getElementById("setBtn").addEventListener("click",function(){
    var rt=rtok(setMsg); if(!rt)return;
    var warn=parseFloat(document.getElementById("setWarn").value), gate=parseFloat(document.getElementById("setGate").value);
    var body={};if(!isNaN(warn))body.warn_c=warn;if(!isNaN(gate))body.gate_c=gate;
    if(body.warn_c==null&&body.gate_c==null){setMsg.textContent="Enter a warn and/or gate value.";return;}
    setMsg.textContent="applying…";
    post("/v1/thermal/limits",body,rt).then(function(res){
      if(!res.ok){setMsg.textContent="Error: "+(res.j.error||"failed");return;}
      setMsg.textContent="Applied: warn "+res.j.warn_c+"° · gate "+res.j.gate_c+"°"+(res.j.clamped?" (clamped, max "+res.j.gate_ceiling+")":"");
    }).catch(function(e){setMsg.textContent="Error: "+e.message;});
  });
  var wlMsg=document.getElementById("wlMsg");
  document.getElementById("wlBtn").addEventListener("click",function(){
    var rt=rtok(wlMsg); if(!rt)return;
    var ssids=document.getElementById("wlBox").value.split("\n").map(function(s){return s.trim();}).filter(Boolean);
    wlMsg.textContent="saving…";
    post("/v1/hotspot/whitelist",{ssids:ssids},rt).then(function(res){
      if(!res.ok){wlMsg.textContent="Error: "+(res.j.error||"failed");return;}
      wlMsg.textContent=ssids.length?("Watching "+ssids.length+" SSID"+(ssids.length===1?"":"s")+"."):"Auto-toggle disabled.";
      renderHotspot(res.j);
    }).catch(function(e){wlMsg.textContent="Error: "+e.message;});
  });

  // Nearby list: "Scan now" triggers an on-demand scan; tapping a row adds or
  // removes that SSID from the whitelist and saves immediately.
  var nearbyMsg=document.getElementById("nearbyMsg"), scanBtn=document.getElementById("scanBtn");
  scanBtn.addEventListener("click",function(){
    var rt=rtok(nearbyMsg); if(!rt)return;
    nearbyMsg.textContent="scanning… (a few seconds)"; scanBtn.disabled=true;
    post("/v1/hotspot/scan",{},rt).then(function(res){
      scanBtn.disabled=false;
      if(!res.ok){nearbyMsg.textContent="Error: "+(res.j.error||"failed");return;}
      renderHotspot(res.j);
      nearbyMsg.textContent=res.j.paused==="location_off"?"Turn on location services to scan.":
        ((res.j.nearby&&res.j.nearby.length||0)+" network"+((res.j.nearby&&res.j.nearby.length)===1?"":"s")+" in range.");
    }).catch(function(e){scanBtn.disabled=false;nearbyMsg.textContent="Error: "+e.message;});
  });
  function toggleWhitelist(ssid){
    var rt=rtok(nearbyMsg); if(!rt)return;
    var lines=document.getElementById("wlBox").value.split("\n").map(function(s){return s.trim();}).filter(Boolean);
    var idx=lines.indexOf(ssid), removing=idx>=0;
    if(removing)lines.splice(idx,1); else lines.push(ssid);
    nearbyMsg.textContent="saving…";
    // Update the textarea only AFTER the server accepts, so a rejected SSID
    // (e.g. >32 bytes, or the 16-entry cap) never desyncs the editor from
    // what's actually saved. renderHotspot repaints the nearby list's ✓ flags.
    post("/v1/hotspot/whitelist",{ssids:lines},rt).then(function(res){
      if(!res.ok){nearbyMsg.textContent="Error: "+(res.j.error||"failed");return;}
      document.getElementById("wlBox").value=lines.join("\n");
      nearbyMsg.textContent=(removing?"Removed “":"Added “")+ssid+"” "+(removing?"from":"to")+" whitelist.";
      renderHotspot(res.j);
    }).catch(function(e){nearbyMsg.textContent="Error: "+e.message;});
  }

  // Airplane trigger + IP rotate. The cycle blocks ~15-30s (radio drop + PDP
  // re-attach + hotspot restart); disable all three buttons while it runs.
  var apMsg=document.getElementById("apMsg");
  var apBtns=[document.getElementById("rotateBtn"),document.getElementById("apOnBtn"),document.getElementById("apOffBtn")];
  function apBusy(on){apBtns.forEach(function(b){b.disabled=on;});}
  function apPost(mode,pending,done){
    var rt=rtok(apMsg); if(!rt)return;
    apMsg.textContent=pending; apBusy(true);
    post("/v1/airplane",{mode:mode},rt).then(function(res){
      apBusy(false);
      if(!res.ok){apMsg.textContent="Error: "+(res.j.error||"failed");return;}
      apMsg.textContent=done(res.j);
      tick(); // refresh header IP + airplane state now
    }).catch(function(e){apBusy(false);apMsg.textContent="Error: "+e.message;});
  }
  document.getElementById("rotateBtn").addEventListener("click",function(){
    apPost("cycle","rotating IP… (~20s, clients drop briefly)",function(j){
      var line=j.changed?("IP changed: "+j.old_ip+" → "+j.new_ip)
        :(j.data_back?("IP unchanged ("+(j.new_ip||"?")+") — carrier reused it")
        :"data did not come back — check the connection");
      return line+(j.hotspot_active?" · hotspot back up":(j.note||" · hotspot NOT up"));
    });
  });
  document.getElementById("apOnBtn").addEventListener("click",function(){
    apPost("on","enabling airplane…",function(){return "Airplane ON — radio + hotspot off.";});
  });
  document.getElementById("apOffBtn").addEventListener("click",function(){
    apPost("off","disabling airplane…",function(j){return "Airplane OFF"+(j.hotspot_active?" · hotspot back up":(j.note||"")); });
  });
  // Speedtest — deliberate (data + heat). Blocks ~15-40s.
  var spdMsg=document.getElementById("spdMsg");
  document.getElementById("spdBtn").addEventListener("click",function(){
    var rt=rtok(spdMsg); if(!rt)return;
    var b=document.getElementById("spdBtn");
    b.disabled=true; spdMsg.textContent="testing… (~15–40s, using data)";
    ["spdDown","spdUp","spdPing"].forEach(function(id){document.getElementById(id).textContent="…";});
    post("/v1/speedtest",{},rt).then(function(res){
      b.disabled=false;
      var j=res.j||{};
      if(!res.ok||!j.available){
        ["spdDown","spdUp","spdPing"].forEach(function(id){document.getElementById(id).textContent="—";});
        spdMsg.textContent="Speedtest failed: "+(j.error||("HTTP "+(res.j&&res.j.code||"?")));return;
      }
      function fmt(v){return (v==null||v<0)?"n/a":v;}
      document.getElementById("spdDown").textContent=fmt(j.download_mbps);
      document.getElementById("spdUp").textContent=fmt(j.upload_mbps);
      document.getElementById("spdPing").textContent=fmt(j.ping_ms);
      spdMsg.textContent="jitter "+fmt(j.jitter_ms)+" ms · "+esc(j.server||"");
    }).catch(function(e){b.disabled=false;["spdDown","spdUp","spdPing"].forEach(function(id){document.getElementById(id).textContent="—";});spdMsg.textContent="Error: "+e.message;});
  });

  // Hotspot toggle — turn it on when off, off when on (not thermal-gated).
  document.getElementById("hotspotOnBtn").addEventListener("click",function(){
    var rt=rtok(apMsg); if(!rt)return;
    var stopping=hsActive, action=stopping?"stop":"start", hb=document.getElementById("hotspotOnBtn");
    apMsg.textContent=stopping?"stopping hotspot…":"starting hotspot…"; apBtns.forEach(function(b){b.disabled=true;}); hb.disabled=true;
    post("/v1/tether?action="+action,{},rt).then(function(res){
      apBtns.forEach(function(b){b.disabled=false;}); hb.disabled=false;
      if(!res.ok){apMsg.textContent="Error: "+(res.j.error||"failed");return;}
      hsActive=!!res.j.active; renderHotspotBtn();
      apMsg.textContent=res.j.active?"📶 hotspot on":(stopping?"hotspot off":"⚠️ hotspot did not come up — retry");
      tick();
    }).catch(function(e){apBtns.forEach(function(b){b.disabled=false;});hb.disabled=false;apMsg.textContent="Error: "+e.message;});
  });

  // --- Hotspot presets: create/edit/apply + Wi-Fi-fingerprint auto-switch.
  // The list is rebuilt from /v1/status each tick and from mutation responses;
  // the create/edit FORM is never touched by a refresh, so editing is safe.
  var pMsg=document.getElementById("pMsg"), presetList=[], editingId="";
  function presetMeta(p){
    var sec=p.security==="open"?"Open":String(p.security||"").toUpperCase();
    var band=p.band==="2"?"2.4 GHz":(p.band==="6"?"6 GHz":"5 GHz");
    var n=(p.triggers&&p.triggers.length)||0;
    return esc(p.ssid)+" · "+band+" · "+sec+(n?(" · "+n+" trigger"+(n===1?"":"s")):"");
  }
  function renderPresets(hp){
    if(!hp)return;
    presetList=hp.presets||[];
    renderPresetShortcut(hp); // Home quick-switch (runs before the pList early-return)
    var on=!!hp.auto_switch;
    document.getElementById("paState").textContent=on?"on":"off";
    document.getElementById("paDot").className="dot "+(on?"green":"off");
    document.getElementById("paAccent").style.background=on?"var(--green)":"#30363d";
    document.getElementById("pCount").textContent=presetList.length?(presetList.length+"/12"):"";
    var box=document.getElementById("pList");
    if(!presetList.length){box.innerHTML='<div class="stat-sub">No presets yet — create one below.</div>';return;}
    var html="";
    presetList.forEach(function(p,i){
      var act=hp.active&&p.id===hp.active;
      html+='<div class="prow">'
        +'<button class="pmain" data-idx="'+i+'"><div class="pname">'+esc(p.name)+(act?'<span class="tag">active</span>':'')+'</div><div class="pmeta">'+presetMeta(p)+'</div></button>'
        +'<button class="minibtn papply" data-idx="'+i+'"'+(act?' disabled':'')+'>'+(act?'On':'Apply')+'</button>'
        +'<button class="wlx pdel" data-idx="'+i+'" aria-label="Delete preset">&times;</button>'
      +'</div>';
    });
    box.innerHTML=html;
    box.querySelectorAll(".pmain").forEach(function(b){b.addEventListener("click",function(){editPreset(presetList[+b.getAttribute("data-idx")]);});});
    box.querySelectorAll(".papply").forEach(function(b){b.addEventListener("click",function(){applyPreset(presetList[+b.getAttribute("data-idx")]);});});
    box.querySelectorAll(".pdel").forEach(function(b){b.addEventListener("click",function(){deletePreset(presetList[+b.getAttribute("data-idx")]);});});
  }
  // Home-tab shortcut: active preset + one-tap switch chips. Same data as the
  // Presets tab; applies via applyPreset with the Home message element.
  var hpMsg=document.getElementById("hpMsg");
  function renderPresetShortcut(hp){
    var box=document.getElementById("hpQuick"); if(!box)return;
    var list=hp.presets||[], act=hp.active;
    var actName=""; list.forEach(function(p){if(p.id===act)actName=p.name;});
    document.getElementById("hpActive").textContent=actName?("on “"+actName+"”"):(list.length?"none active":"");
    if(!list.length){box.innerHTML='<div class="stat-sub">No presets — add them in the Presets tab.</div>';return;}
    var html="";
    list.forEach(function(p,i){var o=p.id===act;
      html+='<button class="pchip hpq'+(o?" added":"")+'" data-idx="'+i+'"'+(o?' disabled':'')+'>'+(o?"● ":"")+esc(p.name)+'</button>';});
    box.innerHTML=html;
    box.querySelectorAll(".hpq").forEach(function(b){b.addEventListener("click",function(){applyPreset(list[+b.getAttribute("data-idx")],hpMsg);});});
  }
  function editPreset(p){
    if(!p)return;
    editingId=p.id;
    document.getElementById("pName").value=p.name||"";
    document.getElementById("pSsid").value=p.ssid||"";
    document.getElementById("pPass").value="";
    document.getElementById("pPass").placeholder=p.has_pass?"leave blank to keep current":"8–63 chars";
    document.getElementById("pSec").value=p.security||"wpa2";
    document.getElementById("pBand").value=p.band||"5";
    document.getElementById("pTrig").value=(p.triggers||[]).join("\n");
    document.getElementById("pFormTitle").textContent="Edit “"+(p.name||"preset")+"”";
    document.getElementById("pSaveBtn").textContent="Update preset";
    document.getElementById("pNewBtn").style.display="";
    document.getElementById("pNearby").innerHTML="";
    pMsg.textContent="";
    document.getElementById("pFormTitle").scrollIntoView({block:"nearest"});
  }
  function resetPresetForm(){
    editingId="";
    ["pName","pSsid","pPass","pTrig"].forEach(function(id){document.getElementById(id).value="";});
    document.getElementById("pPass").placeholder="8–63 chars";
    document.getElementById("pSec").value="wpa2"; document.getElementById("pBand").value="5";
    document.getElementById("pFormTitle").textContent="Create preset";
    document.getElementById("pSaveBtn").textContent="Save preset";
    document.getElementById("pNewBtn").style.display="none";
    document.getElementById("pNearby").innerHTML=""; pMsg.textContent="";
  }
  document.getElementById("pNewBtn").addEventListener("click",resetPresetForm);
  document.getElementById("pSaveBtn").addEventListener("click",function(){
    var rt=rtok(pMsg); if(!rt)return;
    var name=document.getElementById("pName").value.trim(), ssid=document.getElementById("pSsid").value.trim();
    var pass=document.getElementById("pPass").value, sec=document.getElementById("pSec").value, band=document.getElementById("pBand").value;
    var triggers=document.getElementById("pTrig").value.split("\n").map(function(s){return s.trim();}).filter(Boolean);
    if(!name){pMsg.textContent="Preset name required.";return;}
    if(!ssid){pMsg.textContent="Network name (SSID) required.";return;}
    if(sec!=="open"&&!pass&&!editingId){pMsg.textContent="Password (8–63 chars) required for a new "+sec.toUpperCase()+" preset.";return;}
    var body={id:editingId,name:name,ssid:ssid,passphrase:pass,security:sec,band:band,triggers:triggers};
    var b=document.getElementById("pSaveBtn"); b.disabled=true; pMsg.textContent=editingId?"updating…":"saving…";
    post("/v1/presets",body,rt).then(function(res){
      b.disabled=false;
      if(!res.ok){pMsg.textContent="Error: "+(res.j.error||"failed");return;}
      renderPresets(res.j.hotspot_presets); resetPresetForm();
      pMsg.textContent="Saved “"+name+"”.";
    }).catch(function(e){b.disabled=false;pMsg.textContent="Error: "+e.message;});
  });
  function applyPreset(p,msgEl){
    if(!p)return; msgEl=msgEl||pMsg; var rt=rtok(msgEl); if(!rt)return;
    msgEl.textContent="switching to “"+p.name+"”… clients drop briefly (~5s).";
    var sel=".papply,.hpq";
    document.querySelectorAll(sel).forEach(function(b){b.disabled=true;});
    post("/v1/presets/apply",{id:p.id},rt).then(function(res){
      document.querySelectorAll(sel).forEach(function(b){b.disabled=false;});
      if(!res.ok){msgEl.textContent="Error: "+(res.j.error||(res.j.code===409?"an apply is already running":"failed"));return;}
      renderPresets(res.j.hotspot_presets);
      msgEl.textContent="Now on “"+p.name+"” ("+esc(res.j.ssid||p.ssid)+").";
      tick();
    }).catch(function(e){document.querySelectorAll(sel).forEach(function(b){b.disabled=false;});msgEl.textContent="Error: "+e.message;});
  }
  function deletePreset(p){
    if(!p)return; var rt=rtok(pMsg); if(!rt)return;
    pMsg.textContent="deleting…";
    post("/v1/presets/delete",{id:p.id},rt).then(function(res){
      if(!res.ok){pMsg.textContent="Error: "+(res.j.error||"failed");return;}
      renderPresets(res.j); if(editingId===p.id)resetPresetForm();
      pMsg.textContent="Deleted “"+p.name+"”.";
    }).catch(function(e){pMsg.textContent="Error: "+e.message;});
  }
  function setPresetAuto(on){
    var rt=rtok(pMsg); if(!rt)return; pMsg.textContent="applying…";
    post("/v1/presets/auto",{on:on},rt).then(function(res){
      if(!res.ok){pMsg.textContent="Error: "+(res.j.error||"failed");return;}
      renderPresets(res.j);
      pMsg.textContent=on?"Auto-switch ON — presets follow the Wi-Fi in range.":"Auto-switch off.";
    }).catch(function(e){pMsg.textContent="Error: "+e.message;});
  }
  document.getElementById("paOnBtn").addEventListener("click",function(){setPresetAuto(true);});
  document.getElementById("paOffBtn").addEventListener("click",function(){setPresetAuto(false);});
  document.getElementById("pScanBtn").addEventListener("click",function(){
    var rt=rtok(pMsg); if(!rt)return;
    var sb=document.getElementById("pScanBtn"); sb.disabled=true; pMsg.textContent="scanning… (a few seconds)";
    post("/v1/hotspot/scan",{},rt).then(function(res){
      sb.disabled=false;
      if(!res.ok){pMsg.textContent="Error: "+(res.j.error||"failed");return;}
      if(res.j.paused==="location_off"){pMsg.textContent="Turn on location services to scan.";return;}
      var names=(res.j.nearby||[]).map(function(a){return a.ssid;});
      renderTriggerChips(names);
      pMsg.textContent=names.length?(names.length+" network"+(names.length===1?"":"s")+" in range — tap to add as a trigger."):"No networks in range.";
    }).catch(function(e){sb.disabled=false;pMsg.textContent="Error: "+e.message;});
  });
  function renderTriggerChips(ssids){
    var box=document.getElementById("pNearby");
    if(!ssids.length){box.innerHTML="";return;}
    var cur=document.getElementById("pTrig").value.split("\n").map(function(s){return s.trim();});
    var html='<div class="pchips">';
    ssids.forEach(function(s,i){html+='<button class="pchip'+(cur.indexOf(s)>=0?" added":"")+'" data-idx="'+i+'">'+esc(s)+'</button>';});
    box.innerHTML=html+'</div>';
    box.querySelectorAll(".pchip").forEach(function(b){b.addEventListener("click",function(){
      var s=ssids[+b.getAttribute("data-idx")], ta=document.getElementById("pTrig");
      var lines=ta.value.split("\n").map(function(x){return x.trim();}).filter(Boolean);
      if(lines.indexOf(s)<0){lines.push(s);ta.value=lines.join("\n");b.classList.add("added");}
    });});
  }

  // Per-tab polling: status+signal always (header indicator), plus only what
  // the visible tab shows. Small screen, small request budget (~36-48/min,
  // limit 120). allSettled so one failure renders what it can.
  var extras={
    home:[["/v1/usage",renderUsage]],
    net:[["/v1/bands",renderBands],["/v1/hotspot",renderHotspot]],
    clientsScr:[["/v1/clients",renderClients]],
    system:[["/v1/cpu",renderCPU]],
    settings:[["/v1/hotspot",renderHotspot]]
  };
  var inFlight=false, seq=0, gateSeeded=false;
  function tick(){
    // No early return without a token: try anyway. If "open reads" is on, the
    // daemon serves reads tokenless; only a real 401 means we need the token.
    if(inFlight)return; // don't stack overlapping ticks on a slow daemon
    inFlight=true;
    var mine=++seq, activeAtStart=active;
    var ex=extras[activeAtStart]||[];
    var reqs=[get("/v1/status"),get("/v1/signal")].concat(ex.map(function(e){return get(e[0]);}));
    Promise.allSettled(reqs).then(function(rs){
      inFlight=false;
      if(mine!==seq)return; // a newer tick finished first; don't overwrite it
      function val(i){return rs[i]&&rs[i].status==="fulfilled"?rs[i].value:null;}
      var st=val(0),sg=val(1);
      if(st)renderStatus(st); if(sg)renderSignal(sg);
      if(activeAtStart===active)ex.forEach(function(e,i){var v=val(2+i);if(v)e[1](v);});
      if(st){clearErr();}
      else{var e=rs[0].reason,m=e&&e.message||"";
        if(/ 401/.test(m))showErr("Needs a token. Scan the “Add a device” QR in Settings, open with ?token=…, or turn on Open reads.");
        else showErr("Live data unavailable — "+(m||"status failed"));}
      // Seed the adjust inputs once from live limits; don't clobber a field the
      // owner is editing on later ticks.
      if(!gateSeeded&&st){document.getElementById("setGate").value=GATE_C;document.getElementById("setWarn").value=WARN_C;gateSeeded=true;}
    }).catch(function(){inFlight=false;});
  }
  tick();setInterval(tick,5000);

  // No service worker: for a single-file loopback/tailnet app it only risked
  // serving a stale cached page (it survives Cache-Control: no-store). Actively
  // tear down any SW a previous version installed, and clear its caches, so the
  // page is always the freshly-served one.
  if("serviceWorker" in navigator){
    navigator.serviceWorker.getRegistrations().then(function(rs){rs.forEach(function(r){r.unregister();});}).catch(function(){});
    if(window.caches&&caches.keys){caches.keys().then(function(ks){ks.forEach(function(k){caches.delete(k);});}).catch(function(){});}
  }
})();
</script>
</body>
</html>`
