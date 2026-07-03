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
		if err := persistOpenReads(s.cfgPath, body.Open); err != nil {
			log.Printf("open_reads: persist failed: %v", err)
		}
	}
	s.cfgMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"open_reads": body.Open})
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

// Minimal SW: takes control, and network-first for navigations with a cached
// shell fallback. It never caches /v1/* (live data must not be stale).
const serviceWorkerJS = `self.addEventListener('install',e=>self.skipWaiting());
self.addEventListener('activate',e=>e.waitUntil(self.clients.claim()));
self.addEventListener('fetch',e=>{
  var u=new URL(e.request.url);
  if(u.pathname.startsWith('/v1/'))return;               // never cache live data
  if(e.request.mode==='navigate'){
    e.respondWith(fetch(e.request).then(r=>{caches.open('zf5').then(c=>c.put('/',r.clone()));return r})
      .catch(()=>caches.open('zf5').then(c=>c.match('/'))));
  }
});`

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
  .bar>i{display:block;height:100%;border-radius:3px;background:var(--blue);transition:width .5s ease}
  .stat-flex{display:flex;align-items:center;justify-content:space-between;gap:10px}
  .mini-ring{position:relative;width:52px;height:52px;flex:none}
  .mini-ring svg{width:100%;height:100%;transform:rotate(-90deg)}
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
  .setgrid input,.settok input,textarea{width:100%;background:var(--card-2);border:1px solid var(--line);border-radius:8px;color:var(--text);padding:11px 12px;font-size:14px;font-family:inherit;min-height:44px}
  textarea{min-height:76px;resize:vertical;line-height:1.5}
  .settok{margin-top:10px}
  .setbtn{margin-top:12px;width:100%;background:var(--teal);color:#04211f;border:0;border-radius:9px;padding:13px;font-size:14px;font-weight:700;cursor:pointer;font-family:inherit;min-height:46px}
  .setbtn:disabled{opacity:.5;cursor:default}
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
  .wlx{flex:none;background:none;border:0;color:var(--text-3);font-size:22px;line-height:1;cursor:pointer;width:40px;height:40px;border-radius:7px;font-family:inherit}
  .wlx:active,.wlx:focus-visible{color:var(--red);outline:none;background:rgba(240,82,78,.12)}
  .setmsg{margin-top:9px;font-size:12px;color:var(--text-2);min-height:14px}
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
  nav .tab:focus-visible,.setbtn:focus-visible,details.cli summary:focus-visible{outline:2px solid var(--teal);outline-offset:-2px;border-radius:8px}
  input:focus,textarea:focus{outline:none;border-color:var(--teal)}
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
        <div class="stat-flex">
          <div><div class="stat-val num"><span id="battLevel">—</span><small>%</small></div><div class="stat-sub num" id="battSub">—</div></div>
          <div class="mini-ring"><svg viewBox="0 0 44 44"><circle cx="22" cy="22" r="18" fill="none" stroke="var(--track)" stroke-width="5"/><circle id="battRing" cx="22" cy="22" r="18" fill="none" stroke="var(--green)" stroke-width="5" stroke-linecap="round" stroke-dasharray="113.1" stroke-dashoffset="0" style="transition:stroke-dashoffset .6s ease,stroke .4s ease"/></svg></div>
        </div>
      </div>
      <div class="card">
        <div class="stat-head"><span>Temp</span><span class="accent" id="tempAccent" style="background:var(--amber)"></span></div>
        <div class="stat-val num" id="tempVal" style="color:var(--amber)"><span id="tempMax">—</span><small>°C</small></div>
        <div class="stat-sub num" id="tempSub">—</div>
      </div>
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
      <div class="stat-head"><span>Connectivity</span><span class="accent" id="apAccent" style="background:var(--blue)"></span></div>
      <div class="state-row"><span class="k">WAN IP</span><span class="v mono" id="wanIp">—</span></div>
      <div class="state-row"><span class="k">Airplane</span><span class="v"><span class="dot off" id="apDot"></span><span id="apState">off</span></span></div>
      <button class="setbtn" id="rotateBtn">Rotate IP (airplane cycle)</button>
      <button class="setbtn" id="hotspotOnBtn" style="margin-top:8px;background:var(--green);color:#04211f">Hotspot On</button>
      <div class="apbtns">
        <button class="minibtn" id="apOnBtn">Airplane On</button>
        <button class="minibtn" id="apOffBtn">Airplane Off + hotspot</button>
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
      <div class="stat-head"><span>CPU · per core</span><span class="accent" style="background:var(--violet)"></span></div>
      <div class="cpu-head">
        <div class="cpu-load num"><span id="cpuLoad">—</span><small>load 5m</small></div>
        <div class="cpu-meta"><span id="cpuMode">—</span> · <span id="cpuCores">8</span> cores</div>
      </div>
      <div class="cores" id="cores"></div>
    </div>
    <div class="card">
      <div class="stat-head"><span>Memory</span><span class="accent" style="background:var(--blue)"></span></div>
      <div class="stat-val num"><span id="memPct">—</span><small>%</small></div>
      <div class="bar"><i id="memBar" style="width:0%"></i></div>
    </div>
    <div class="card">
      <div class="state-row"><span class="k">Thermal policy</span><span class="v"><span class="dot amber" id="policyDot"></span><span id="policyState">—</span></span></div>
      <div class="state-row"><span class="k">CPU mode</span><span class="v"><span class="dot green" id="cpuDot"></span><span id="cpuModeRow">—</span></span></div>
    </div>
  </section>

  <section class="screen" id="settings">
    <div class="card">
      <div class="stat-head"><span>Thermal gate · adjust</span><span class="accent" style="background:var(--red)"></span></div>
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
      <div class="stat-head"><span>Hotspot auto-toggle</span><span class="accent" style="background:var(--teal)"></span></div>
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
      <div class="stat-head"><span>Add a device</span><span class="accent" style="background:var(--teal)"></span></div>
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
  var CIRC=2*Math.PI*52, BCIRC=2*Math.PI*18;

  function rsrpCls(v){return v>=-95?"good":(v>=-110?"mid":"low");}
  function sinrCls(v){return v>=13?"good":(v>=0?"mid":"low");}
  function stCls(s){return s=="REACHABLE"?"green":(s=="FAILED"?"red":(s=="DELAY"||s=="PROBE"?"amber":"off"));}

  // SAFE/RECOVERY are safe; WARM is caution; HOT/COOLDOWN are hot. RECOVERY is a
  // transitional-but-safe state (radio writes are allowed), so it must not read red.
  function polColor(p){return (p==="SAFE"||p==="RECOVERY")?"green":(p==="WARM"?"amber":"red");}
  function renderStatus(s){
    var net=s.network||{}, bat=s.battery||{}, th=s.thermal||{}, ip=s.wan_ip||{};
    if(typeof s.open_reads==="boolean"&&document.getElementById("orState"))renderOpenReads(s.open_reads);
    document.getElementById("netType").textContent=net.display||net.type||"—";
    // Always show the WAN IP (header). Airplane on / no data -> explicit label.
    document.getElementById("wanip").textContent=s.airplane?"airplane ✈":(ip.available&&ip.ip?ip.ip:"no data");
    var ipEl=document.getElementById("wanIp"); if(ipEl){ipEl.textContent=ip.available&&ip.ip?ip.ip:(s.airplane?"— (airplane on)":"— (no data)");}
    var apEl=document.getElementById("apState"); if(apEl){apEl.textContent=s.airplane?"ON":"off";document.getElementById("apDot").className="dot "+(s.airplane?"amber":"off");}
    var pol=s.policy_state||"—", dc=polColor(pol);
    document.getElementById("statusDot").className="dot "+dc;
    document.getElementById("policyDot").className="dot "+dc;
    document.getElementById("policyState").textContent=pol;
    if(th.warn_c)WARN_C=th.warn_c; if(th.gate_c)GATE_C=th.gate_c;

    // Collector unavailable -> show em-dash, not a fake 0% red battery.
    var battEl=document.getElementById("battLevel"), bRing=document.getElementById("battRing");
    if(bat.available===false||bat.level==null){
      battEl.textContent="—";document.getElementById("battSub").textContent="unavailable";
      bRing.style.strokeDashoffset=BCIRC.toFixed(1);document.getElementById("battAccent").style.background="#30363d";
    }else{
      var lvl=Math.round(bat.level);
      battEl.textContent=lvl;
      document.getElementById("battSub").textContent=(bat.plugged||"")+" · "+(bat.temp_c!=null?bat.temp_c.toFixed(1)+"°C":"");
      var bcol=lvl<=10?"var(--red)":(lvl<=20?"var(--amber)":"var(--green)");
      bRing.style.stroke=bcol;bRing.style.strokeDashoffset=(BCIRC*(1-Math.max(0,Math.min(100,lvl))/100)).toFixed(1);
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
  function rtok(msgEl){
    var rt=setTok.value.trim();
    if(!rt){msgEl.textContent="Paste the radio-control token below first.";return null;}
    lsSet("zf5rtok",rt);
    return rt;
  }
  function post(path,body,rt){
    return fetch(API+path,{method:"POST",headers:{Authorization:"Bearer "+rt,"Content-Type":"application/json"},body:JSON.stringify(body)})
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
  // Manual "Hotspot On" — turn the data hotspot on now (not thermal-gated).
  document.getElementById("hotspotOnBtn").addEventListener("click",function(){
    var rt=rtok(apMsg); if(!rt)return;
    apMsg.textContent="starting hotspot…"; apBtns.forEach(function(b){b.disabled=true;});
    document.getElementById("hotspotOnBtn").disabled=true;
    post("/v1/tether?action=start",{},rt).then(function(res){
      apBtns.forEach(function(b){b.disabled=false;}); document.getElementById("hotspotOnBtn").disabled=false;
      if(!res.ok){apMsg.textContent="Error: "+(res.j.error||"failed");return;}
      apMsg.textContent=res.j.active?"📶 hotspot on":"⚠️ hotspot did not come up — retry";
      tick();
    }).catch(function(e){apBtns.forEach(function(b){b.disabled=false;});document.getElementById("hotspotOnBtn").disabled=false;apMsg.textContent="Error: "+e.message;});
  });

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

  // PWA: register the service worker (installability + instant app-shell). Safe
  // to fail — the dashboard works without it.
  if("serviceWorker" in navigator){navigator.serviceWorker.register("/sw.js").catch(function(){});}
})();
</script>
</body>
</html>`
