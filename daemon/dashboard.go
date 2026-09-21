package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.usage.Report())
}

// handleUsageQuota edits the data-cap limit and/or reset schedule. Body
// fields are pointers so an omitted one keeps its current value. Persist
// happens FIRST (same pattern as handleCoverAccent): a write that can't reach
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

// handleDashboard serves the full control panel at "/dashboard". The cover
// screen sits at "/" (handleCover), which is also the 404 catch-all.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Never cache the HTML: the WebView/browser otherwise serves a stale page
	// after a daemon update (e.g. a new card wouldn't appear until cache expiry).
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(s.htmlWithTokens(dashboardHTML)))
}

// htmlWithTokens embeds the daemon's own read-status/radio-control
// tokens straight into the served page when open_reads/open_control are on,
// so a hardwired/public install never needs manual token entry on any access
// path (kiosk, tailnet browser, ...). Each token is embedded ONLY when its
// own open flag is set — flipping a flag off still requires the paste-once
// fallback, so the toggle stays a real security control, not decorative.
// The sms token rides open-reads too (owner's call on this donor phone): the
// Inbox is meant to work with no token anywhere, and guard() opens the same
// scope for tokenless reads. Content stays redacted and pull-only.
func (s *Server) htmlWithTokens(html string) string {
	var js strings.Builder
	if s.openReads.Load() && isHexToken(s.cfg.Tokens["read-status"]) {
		js.WriteString(`localStorage.setItem("zf5tok","` + s.cfg.Tokens["read-status"] + `");`)
	}
	if s.openReads.Load() && isHexToken(s.cfg.Tokens["sms"]) {
		js.WriteString(`localStorage.setItem("zf5smstok","` + s.cfg.Tokens["sms"] + `");`)
	}
	if s.openControl.Load() && isHexToken(s.cfg.Tokens["radio-control"]) {
		js.WriteString(`localStorage.setItem("zf5rtok","` + s.cfg.Tokens["radio-control"] + `");`)
	}
	snippet := ""
	if js.Len() > 0 {
		snippet = "<script>try{" + js.String() + "}catch(e){}</script>"
	}
	return strings.Replace(html, "<!--EMBEDDED_TOKENS-->", snippet, 1)
}

// isHexToken guards the string-concat embed above: config.json is root-only
// (0600) and self-generated, but this keeps a hand-edited/corrupted token
// from ever landing unescaped inside an inline <script>.
func isHexToken(s string) bool {
	if len(s) < 32 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
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
	url := scheme + "://" + host + "/dashboard?token=" + s.cfg.Tokens["read-status"]
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
  "description": "Cover-screen widget deck for the Z Flip 5 modem",
  "start_url": "/dashboard",
  "scope": "/",
  "display": "standalone",
  "orientation": "any",
  "background_color": "#07090d",
  "theme_color": "#07090d",
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

// dashboardHTML is the control panel served at "/dashboard": a self-contained
// dark instrument page built on DESIGN.md ("The Instrument Panel"). One teal
// accent plus a strict semantic status set (green/amber/red; violet is the CPU
// lane only); flat tonal depth — three surface tones and hairline borders, no
// gradients, bevels or shadows; the loudest type on any screen is always a
// datum, never chrome. System font stack only: no webfont, no CDN, nothing to
// fetch before first paint. Structured for the Z Flip 5 cover screen (~352×308
// CSS px usable): seven focused swipe pages + a bottom tab bar, ≥44px touch
// targets, and per-endpoint poll cadence so a small screen never pays for data
// it isn't showing. The owner controls: thermal-gate adjust, SSID-whitelist
// editor, quota, presets, the Inbox, and the admin-gated bench/CPU/danger
// cards. The :root token block is shared verbatim with coverHTML —
// TestCoverTokensMatchDashboard exists to keep the two from drifting.
const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Ametsuyu Flippost</title>
<link rel="manifest" href="/manifest.webmanifest">
<meta name="theme-color" content="#0b0d10">
<link rel="apple-touch-icon" href="/logo.png">
<link rel="icon" href="/logo.png">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<meta name="apple-mobile-web-app-title" content="ZF5 Modem">
<style>
  /* "Midnight glass" — a modern dark instrument skin. The :root block below is
     the shared design-token vocabulary — coverHTML carries the same names and
     values, so a token can never mean one thing here and another on the cover
     screen. Depth = layered translucency + blur + soft shadows; one gradient
     accent (teal→emerald) carries interactive state, green/amber/red are
     reserved for live status, violet is the CPU lane. */
  :root{
    /* surfaces: translucent glass over a near-black aurora ground */
    --ground:#07090d;
    --surface:rgba(255,255,255,.045);
    --surface-inset:rgba(5,8,12,.55);
    --line:rgba(255,255,255,.09);
    --line-soft:rgba(255,255,255,.055);

    /* ink: three levels */
    --ink:#f2f5f9;
    --ink-2:#a8b3c2;
    --ink-3:#8a94a3;

    /* the accent pair + the semantic status set + the CPU lane */
    --teal:#2dd4bf;
    --teal-2:#34d399;
    --green:#4ade80;
    --amber:#fbbf24;
    --red:#fb7185;
    --violet:#8a7dff;
    --track:rgba(255,255,255,.08); /* unfilled rings and bars */
    --on-teal:#04211f;             /* label colour on the accent fill */
    --accent-grad:linear-gradient(135deg,var(--teal) 0%,var(--teal-2) 100%);

    /* the cover screen's seven owner-picked accents (Settings swatches) */
    --dawn-start:#FFC2A2;      --dawn-end:#FF8820;
    --sunflower-start:#FED6AD; --sunflower-end:#F3B817;
    --coral-start:#F890B6;     --coral-end:#FF5757;
    --breeze-start:#90CCF8;    --breeze-end:#57A0FF;
    --ocean-start:#8389FA;     --ocean-end:#3140E4;
    --wisteria-start:#86A7FD;  --wisteria-end:#8037FF;
    --slate-start:#848C98;     --slate-end:#565E69;

    /* metrics: 4/8/12/16/32/64 (the scale skips 24 and 48) */
    --space-xs:4px; --space-sm:8px; --space-md:12px; --space-lg:16px; --space-xl:32px; --space-2xl:64px;
    --radius-sm:10px; --radius-md:18px; --radius-lg:28px; --radius-pill:999px;
    --radius-nav:22px; --radius-btn:14px;
    --tabbar-h:58px;

    /* one sans doing every job — roles separate by weight and size */
    --font-display:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
    --font-body:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;

    --smooth:cubic-bezier(0.4,0,0.2,1);
    --pop:cubic-bezier(0.34,1.56,0.64,1);
  }

  @keyframes acPop{from{opacity:0;transform:scale(.97)}to{opacity:1;transform:scale(1)}}
  @keyframes acPulse{0%,100%{opacity:.45}50%{opacity:.75}}
  *{box-sizing:border-box;margin:0;padding:0}
  button{border:0;background:none;color:inherit;font:inherit;cursor:pointer;-webkit-appearance:none;appearance:none;-webkit-tap-highlight-color:transparent}
  html,body{height:100%}
  body{
    background:var(--ground); color:var(--ink);
    font-family:var(--font-body); font-size:13.5px;
    -webkit-font-smoothing:antialiased; line-height:1.4; letter-spacing:normal;
    display:flex; flex-direction:column;
  }
  /* the aurora: two fixed colour washes the glass cards blur over. Purely
     ambient — nothing interactive sits on it. */
  body::before{content:"";position:fixed;inset:0;z-index:-1;pointer-events:none;background:
    radial-gradient(60% 46% at 14% -4%,rgba(45,212,191,.11),transparent 62%),
    radial-gradient(52% 40% at 88% 4%,rgba(138,125,255,.09),transparent 62%),
    radial-gradient(70% 34% at 50% 108%,rgba(52,211,153,.06),transparent 64%)}
  /* every column of numbers is tabular, so digits don't jitter as values tick */
  .num{font-variant-numeric:tabular-nums}
  .mono{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}

  /* App shell: quiet header, a horizontal snap deck of screens, a bottom tab
     bar. The deck swipes page to page the way a cover widget carousel does;
     the nav bar is still the tablist. */
  .app{max-width:720px;width:100%;margin:0 auto;flex:1 1 auto;min-height:0;display:flex;flex-direction:column;overflow:hidden}
  .app>.errslot{margin-left:var(--space-md);margin-right:var(--space-md)}
  main{flex:1 1 auto;min-height:0;display:flex;overflow-x:auto;overflow-y:hidden;scroll-snap-type:x mandatory;overscroll-behavior-x:contain;-webkit-overflow-scrolling:touch;scrollbar-width:none;-ms-overflow-style:none}
  main::-webkit-scrollbar{display:none}
  /* Each page scrolls vertically on its own; the final 32px fade away so a cut
     row reads as "scrolls further", not "broken". */
  .screen{flex:0 0 100%;scroll-snap-align:start;scroll-snap-stop:always;overflow-y:auto;padding:0 var(--space-md) var(--space-xl);scrollbar-width:none;-ms-overflow-style:none;
    -webkit-mask-image:linear-gradient(180deg,#000 calc(100% - var(--space-xl)),transparent 100%);
    mask-image:linear-gradient(180deg,#000 calc(100% - var(--space-xl)),transparent 100%)}
  .screen::-webkit-scrollbar{display:none}

  /* Header: quiet glass chrome — it carries the live reading (signal + tech +
     WAN IP) and the thermal-policy badge, and nothing else. */
  header{flex:none;display:flex;align-items:center;justify-content:space-between;gap:var(--space-sm);padding:var(--space-sm) var(--space-md);background:color-mix(in srgb,var(--ground) 55%,transparent);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px);border-bottom:1px solid var(--line-soft)}
  .sigind{display:flex;align-items:center;gap:var(--space-sm);min-width:0}
  .bars{display:inline-flex;align-items:flex-end;gap:3px;height:18px;flex:none}
  .bars>i{width:4px;background:var(--track);border-radius:1px}
  .bars>i:nth-child(1){height:6px}.bars>i:nth-child(2){height:10px}.bars>i:nth-child(3){height:14px}.bars>i:nth-child(4){height:18px}
  .bars.g>i.on{background:var(--teal)}.bars.a>i.on{background:var(--amber)}.bars.r>i.on{background:var(--red)}
  .sigind .lab{font-family:var(--font-display);font-size:15px;font-weight:700;letter-spacing:-.01em}
  .sigind .op{font-size:11px;color:var(--ink-3)}
  .clockcard{padding:var(--space-md)}
  .clocktime{font-family:var(--font-display);font-size:clamp(30px,9vw,40px);font-weight:700;line-height:1.05;letter-spacing:-.02em;background:var(--accent-grad);-webkit-background-clip:text;background-clip:text;color:transparent}
  .clockdate{margin-top:var(--space-xs);font-size:12.5px;font-weight:600;color:var(--ink-2)}
  .clocklunar{margin-top:2px;font-size:11.5px;color:var(--ink-3)}
  .hdrbtn{flex:none;width:34px;height:34px;display:grid;place-items:center;border-radius:var(--radius-sm);background:var(--surface-inset);border:1px solid var(--line);color:var(--ink-3)}
  .hdrbtn svg{width:16px;height:16px}
  .netbadge{display:flex;align-items:center;gap:var(--space-sm);background:var(--surface-inset);border:1px solid var(--line);border-radius:var(--radius-pill);padding:var(--space-xs) var(--space-md);font-size:12px;font-weight:700}
  .dot{width:8px;height:8px;border-radius:50%;flex:none;background:var(--track)}
  .dot.green{background:var(--green);box-shadow:0 0 10px color-mix(in srgb,var(--green) 55%,transparent)}
  .dot.amber{background:var(--amber);box-shadow:0 0 10px color-mix(in srgb,var(--amber) 55%,transparent)}
  .dot.red{background:var(--red);box-shadow:0 0 10px color-mix(in srgb,var(--red) 55%,transparent)}

  /* Card: frosted glass — translucent surface, hairline border, a soft drop
     shadow and a 1px top inner highlight so light reads as coming from above. */
  .card{position:relative;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-md);padding:var(--space-lg);box-shadow:0 10px 30px rgba(0,0,0,.35),inset 0 1px 0 rgba(255,255,255,.06);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px)}
  .card+.card,.grid+.card,.card+.grid,.duo+.card,.duo+.grid{margin-top:var(--space-md)}
  .duo>.card{margin-top:0}
  .hero{display:flex;flex-direction:column;align-items:center;padding:var(--space-lg)}
  .ring-wrap{position:relative;width:min(44vw,180px);aspect-ratio:1}
  .ring-wrap svg{width:100%;height:100%;transform:rotate(-90deg)}
  .ring-center{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:var(--space-xs);text-align:center}
  .ring-pct{font-family:var(--font-display);font-size:clamp(34px,11vw,46px);font-weight:700;line-height:1;letter-spacing:-.03em}
  .ring-pct span{font-size:.5em;font-weight:600;color:var(--ink-2)}
  .ring-sub{font-size:12px;color:var(--ink-2);font-weight:400}
  .ring-label{margin-top:var(--space-sm);font-size:11px;font-weight:600;color:var(--ink-3);text-transform:uppercase;letter-spacing:.07em}
  .ring-caption{margin-top:var(--space-xs);font-size:12px;color:var(--ink-2)}
  .ring-caption b{color:var(--ink);font-weight:600}

  .grid{display:grid;grid-template-columns:1fr;gap:var(--space-md)}
  .duo{display:grid;grid-template-columns:1fr 1fr;gap:var(--space-md);margin-top:var(--space-md)}
  .stat-head{display:flex;align-items:center;justify-content:space-between;font-size:10.5px;font-weight:600;color:var(--ink-3);text-transform:uppercase;letter-spacing:.07em;margin-bottom:var(--space-md)}
  .stat-head h2{font:inherit;margin:0;color:inherit;letter-spacing:inherit}
  .stat-head .accent{width:8px;height:8px;border-radius:50%;background:var(--track)}
  .stat-val{font-family:var(--font-display);font-size:30px;font-weight:700;line-height:1;letter-spacing:-.02em}
  .stat-val small{font-size:.5em;font-weight:600;color:var(--ink-2)}
  .stat-sub{margin-top:var(--space-xs);font-size:12px;color:var(--ink-2)}
  /* meters animate transform, never layout */
  .bar{margin-top:var(--space-md);height:8px;border-radius:4px;background:var(--track);overflow:hidden}
  .bar>i{display:block;height:100%;width:100%;transform-origin:left center;background:var(--accent-grad);transition:transform .4s var(--smooth)}

  .cpu-head{display:flex;align-items:baseline;justify-content:space-between;gap:var(--space-sm);margin-bottom:var(--space-md)}
  .cpu-load{font-family:var(--font-display);font-size:30px;font-weight:700;line-height:1;letter-spacing:-.02em}
  .cpu-load small{font-size:.4em;font-weight:600;color:var(--ink-2);margin-left:var(--space-xs)}
  .cpu-meta{font-size:11px;font-weight:600;color:var(--ink-3);text-align:right}
  .cores{display:flex;align-items:flex-end;gap:var(--space-xs);height:60px}
  .core{flex:1;display:flex;flex-direction:column;align-items:center;gap:var(--space-xs);height:100%;justify-content:flex-end}
  .core .track{position:relative;width:100%;flex:1;background:var(--track);border-radius:4px;overflow:hidden;display:flex;align-items:flex-end}
  /* the CPU lane is the one place violet appears */
  .core .fill{width:100%;height:100%;transform-origin:bottom center;background:linear-gradient(180deg,var(--violet),#6d5ae0);transition:transform .4s var(--smooth)}
  .core .idx{font-size:11px;color:var(--ink-3);font-weight:600}
  .core.off{opacity:.45}
  .core.off .fill{background:var(--track)}

  /* signal readouts are numbers acted on at density: status colour is a
     correctness signal here */
  .sg{display:grid;grid-template-columns:1fr 1fr;gap:var(--space-xs) var(--space-lg)}
  .sgrow{display:flex;justify-content:space-between;font-size:13px;padding:var(--space-xs) 0;font-variant-numeric:tabular-nums}
  .sgrow span{color:var(--ink-2)}
  .sgrow b{font-weight:600;color:var(--ink)}
  .good{color:var(--green)}.mid{color:var(--amber)}.low{color:var(--red)}

  /* rows: hairline rules, no cell borders */
  details.cli{border-top:1px solid var(--line-soft)}
  details.cli:first-of-type{border-top:0}
  details.cli summary{cursor:pointer;list-style:none;display:flex;align-items:center;gap:var(--space-sm);font-size:13px;min-height:48px;padding:var(--space-xs) 0}
  details.cli summary::-webkit-details-marker{display:none}
  details.cli summary .chev{margin-left:auto;color:var(--ink-3);transition:transform .14s var(--smooth)}
  details.cli[open] summary .chev{transform:rotate(90deg)}
  .clibody{font-size:12px;color:var(--ink-2);margin:0 0 var(--space-md) var(--space-lg);display:grid;gap:var(--space-xs)}
  .clibody .r{display:flex;justify-content:space-between;gap:var(--space-md)}
  .clibody .r b{color:var(--ink);font-weight:600;word-break:break-all;text-align:right}
  .state-row{display:flex;align-items:center;justify-content:space-between;min-height:48px;padding:var(--space-xs) 0}
  .state-row+.state-row{border-top:1px solid var(--line-soft)}
  .state-row .k{font-size:13px;color:var(--ink-2)}
  .state-row .v{display:flex;align-items:center;gap:var(--space-sm);font-size:13px;font-weight:600;text-align:right}

  /* inputs: recessed into the card */
  .setgrid{display:grid;grid-template-columns:1fr 1fr;gap:var(--space-md);margin-top:var(--space-sm)}
  label.f{font-size:10.5px;color:var(--ink-3);font-weight:600;display:block;margin-bottom:var(--space-xs);text-transform:uppercase;letter-spacing:.07em}
  input,textarea,select{width:100%;background:var(--surface-inset);border:1px solid var(--line);border-radius:var(--radius-sm);color:var(--ink);padding:var(--space-md);font-size:14px;font-family:var(--font-body);min-height:44px}
  textarea{min-height:76px;resize:vertical;line-height:1.5}
  select{padding-right:var(--space-xl);-webkit-appearance:none;appearance:none;background-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%23a8b3c2' stroke-width='2.5' stroke-linecap='round' opacity='.9'%3E%3Cpolyline points='6 9 12 15 18 9'/%3E%3C/svg%3E"),linear-gradient(var(--surface-inset),var(--surface-inset));background-repeat:no-repeat;background-position:right var(--space-md) center,0 0}
  .settok{margin-top:var(--space-lg)}

  /* buttons: one gradient commit per card; ghosts and minis around it. Fill IS
     the state for on/off actions — the label says what a press does. */
  .setbtn{margin-top:var(--space-md);width:100%;background:var(--accent-grad);color:var(--on-teal);border:0;border-radius:var(--radius-btn);padding:13px;font-size:14px;font-weight:700;font-family:var(--font-body);cursor:pointer;min-height:48px;box-shadow:0 6px 20px color-mix(in srgb,var(--teal) 28%,transparent);transition:transform .15s var(--pop),filter .15s var(--smooth),box-shadow .15s var(--smooth)}
  .setbtn:disabled{opacity:.55;cursor:default}
  .setbtn:active:not(:disabled){filter:brightness(1.1);transform:scale(.98)}
  .setbtn.on{background:linear-gradient(135deg,#34d399,#10b981);color:#032015;box-shadow:0 6px 20px color-mix(in srgb,#34d399 28%,transparent)}
  .setbtn.danger{background:linear-gradient(135deg,#fb7185,#f43f5e);color:#2b0409;box-shadow:0 6px 20px color-mix(in srgb,#f43f5e 26%,transparent)}
  .setbtn.sec{background:var(--surface-inset);color:var(--teal);border:1px solid var(--line);font-weight:600;box-shadow:none}
  .minibtn{background:var(--surface-inset);color:var(--ink);border:1px solid var(--line);border-radius:var(--radius-sm);padding:var(--space-sm) var(--space-md);font-size:12.5px;font-weight:600;font-family:var(--font-body);cursor:pointer;min-height:44px;transition:border-color .15s var(--smooth),color .15s var(--smooth),transform .15s var(--pop)}
  /* the affirmative half of a paired control carries the accent */
  .minibtn.primary{background:var(--accent-grad);color:var(--on-teal);border-color:transparent;box-shadow:0 4px 14px color-mix(in srgb,var(--teal) 24%,transparent)}
  .minibtn:disabled{opacity:.55;cursor:default}
  .minibtn:active:not(:disabled){transform:scale(.97)}

  .spd{display:grid;grid-template-columns:1fr 1fr 1fr;gap:var(--space-sm);margin-bottom:var(--space-xs)}
  /* a well carved into the card, not another card stacked on it */
  .spdcell{text-align:center;background:var(--surface-inset);border-radius:var(--radius-sm);padding:var(--space-md) var(--space-sm)}
  .spdv{font-family:var(--font-display);font-size:24px;font-weight:700;line-height:1;font-variant-numeric:tabular-nums;transition:color .2s var(--smooth)}
  .spdv:not(.has-value){color:var(--ink-3);font-size:20px}
  .spdl{font-size:11px;font-weight:600;color:var(--ink-3);margin-top:var(--space-xs);text-transform:uppercase;letter-spacing:.06em}
  .apbtns{display:grid;grid-template-columns:1fr 1fr;gap:var(--space-md);margin-top:var(--space-md)}
  .apbtns .minibtn{min-height:44px}

  .thChart{display:block;width:100%;height:90px;margin-top:var(--space-md);border-radius:var(--radius-sm);background:rgba(255,255,255,.03);border:1px solid var(--line-soft)}
  .thChart polyline{stroke:var(--teal);stroke-width:2;vector-effect:non-scaling-stroke;stroke-linejoin:round;filter:drop-shadow(0 0 4px color-mix(in srgb,var(--teal) 45%,transparent))}
  .thChart line{stroke:var(--red);stroke-width:1;stroke-dasharray:4 4;vector-effect:non-scaling-stroke}

  .nrow{display:flex;align-items:center;gap:var(--space-md);width:100%;border-top:1px solid var(--line-soft);padding:var(--space-md) 2px;min-height:48px;cursor:pointer;color:var(--ink);font-family:var(--font-body);text-align:left;animation:acPop .24s var(--smooth)}
  .nrow:first-child{border-top:0}
  .nname{flex:1;min-width:0;font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .nchip{flex:none;font-size:12px;font-weight:600;color:var(--ink-3)}
  .nrow.on .nchip{color:var(--teal)}
  .nbars{display:inline-flex;align-items:flex-end;gap:3px;height:16px;flex:none}
  .nbars>i{width:3px;background:var(--track);border-radius:1px}
  .nbars>i:nth-child(1){height:5px}.nbars>i:nth-child(2){height:8px}.nbars>i:nth-child(3){height:12px}.nbars>i:nth-child(4){height:16px}
  .nbars.b1>i:nth-child(-n+1),.nbars.b2>i:nth-child(-n+2),.nbars.b3>i:nth-child(-n+3),.nbars.b4>i:nth-child(-n+4){background:var(--teal)}

  /* inbox rows: a message is text to read, not a control — hairline rules only */
  details.msg{border-top:1px solid var(--line-soft);animation:acPop .24s var(--smooth)}
  details.msg:first-of-type{border-top:0}
  details.msg summary{cursor:pointer;list-style:none;padding:var(--space-md) 0}
  details.msg summary::-webkit-details-marker{display:none}
  .msghead{display:flex;align-items:baseline;gap:var(--space-sm)}
  .msgfrom{flex:1;min-width:0;font-size:14px;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .msgwhen{flex:none;font-size:12px;color:var(--ink-3);font-variant-numeric:tabular-nums}
  .msgbody{margin-top:var(--space-xs);font-size:13px;color:var(--ink-2);line-height:1.45;overflow-wrap:anywhere;white-space:pre-wrap;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden}
  details.msg[open] .msgbody{-webkit-line-clamp:unset;overflow:visible}
  .msgapp{margin-top:var(--space-xs);font-size:12px;color:var(--ink-3)}
  /* the chevron is only drawn on rows whose text is actually clipped */
  .msgmore{flex:none;display:none;color:var(--ink-3);transition:transform .14s var(--smooth)}
  details.msg.can .msgmore{display:inline-flex}
  details.msg:not(.can) summary{cursor:default}
  details.msg[open] .msgmore{transform:rotate(90deg)}
  /* skeleton: matches the row it replaces, so nothing jumps on arrival */
  .sk{height:12px;border-radius:var(--radius-sm);background:rgba(255,255,255,.07);animation:acPulse 1.2s var(--smooth) infinite}
  .skrow{padding:var(--space-md) 0;border-top:1px solid var(--line-soft)}
  .skrow:first-child{border-top:0}
  .skrow .sk+.sk{margin-top:var(--space-sm)}

  .wlhead{font-size:10.5px;font-weight:600;color:var(--ink-3);text-transform:uppercase;letter-spacing:.07em;margin:var(--space-lg) 0 var(--space-sm)}
  .wlchip{display:flex;align-items:center;gap:var(--space-sm);background:var(--surface-inset);border:1px solid var(--line);border-radius:var(--radius-sm);padding:var(--space-xs) var(--space-xs) var(--space-xs) var(--space-md);margin-bottom:var(--space-sm);min-height:44px;animation:acPop .24s var(--smooth)}
  .wlname{flex:1;min-width:0;font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .wlx{flex:none;display:flex;align-items:center;justify-content:center;background:none;border:0;color:var(--ink-3);cursor:pointer;width:44px;height:44px;border-radius:var(--radius-sm);font-family:var(--font-body)}
  .wlx:active,.wlx:focus-visible{color:var(--red)}

  .prow{display:flex;align-items:center;gap:var(--space-md);padding:var(--space-sm) 0;border-top:1px solid var(--line-soft);animation:acPop .24s var(--smooth)}
  .prow:first-child{border-top:0}
  .pmain{flex:1;min-width:0;background:none;border:0;text-align:left;color:var(--ink);font-family:var(--font-body);cursor:pointer;padding:var(--space-xs) 0}
  .pname{font-size:14px;font-weight:600;display:flex;align-items:center;gap:var(--space-sm);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .pname .tag{flex:none;font-size:11px;font-weight:700;color:var(--teal);background:color-mix(in srgb,var(--teal) 16%,transparent);border-radius:var(--radius-pill);padding:3px var(--space-md)}
  .pmeta{font-size:12px;color:var(--ink-3);margin-top:var(--space-xs);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .papply{flex:none}
  .pchips{display:flex;flex-wrap:wrap;gap:var(--space-sm);margin-top:var(--space-sm)}
  .pchip{background:var(--surface-inset);border:1px solid var(--line);color:var(--ink-2);border-radius:var(--radius-pill);padding:var(--space-sm) var(--space-md);font-size:12px;font-weight:600;cursor:pointer;font-family:var(--font-body);min-height:36px;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;animation:acPop .24s var(--smooth)}
  .pchip.added{color:var(--teal);border-color:color-mix(in srgb,var(--teal) 35%,var(--line))}

  .visually-hidden{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
  .setmsg{margin-top:var(--space-sm);font-size:12px;color:var(--ink-2);min-height:14px}
  .footer{margin-top:var(--space-lg);text-align:center;font-size:11px;color:var(--ink-3)}
  /* errors and empty-state notes: quiet glass; an alert gets a red left rule */
  .errslot,.bub{position:relative;margin-top:var(--space-md);background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-md);padding:var(--space-md) var(--space-lg);font-size:13px;color:var(--ink-2);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px)}
  .errslot{display:none;border-left:3px solid var(--red);color:var(--ink)}
  .errslot.show,.bub{display:block;animation:acPop .24s var(--smooth)}
  .sec-label{font-size:10.5px;font-weight:600;color:var(--ink-3);text-transform:uppercase;letter-spacing:.07em;margin:var(--space-lg) var(--space-xs) var(--space-sm)}
  .intg{display:flex;align-items:flex-start;gap:var(--space-md)}
  .intg .logo{width:40px;height:40px;border-radius:var(--radius-sm);flex:none;display:grid;place-items:center;background:var(--surface-inset)}
  .intg .body{flex:1;min-width:0}
  .intg .name{font-size:14px;font-weight:600}
  .intg .desc{font-size:12px;color:var(--ink-2);margin-top:var(--space-xs)}
  .intg-foot{margin-top:var(--space-md);padding-top:var(--space-md);border-top:1px solid var(--line-soft);display:flex;align-items:center;gap:var(--space-sm);font-size:12px;color:var(--ink-2)}
  .managed{display:inline-flex;align-items:center;gap:var(--space-sm);flex:none;font-size:12px;font-weight:700;background:color-mix(in srgb,var(--green) 16%,transparent);color:var(--green);padding:3px var(--space-md);border-radius:var(--radius-pill)}

  /* tab bar: a floating glass dock — frosted, hairline border, soft shadow;
     the active tab earns ink text on a light pill and a gradient glyph */
  nav{flex:none;width:calc(100% - var(--space-lg));max-width:688px;margin:0 auto calc(var(--space-sm) + env(safe-area-inset-bottom));height:var(--tabbar-h);display:flex;align-items:stretch;justify-content:space-between;background:color-mix(in srgb,var(--ground) 72%,transparent);backdrop-filter:blur(18px);-webkit-backdrop-filter:blur(18px);border:1px solid var(--line);border-radius:var(--radius-nav);box-shadow:0 12px 32px rgba(0,0,0,.45),inset 0 1px 0 rgba(255,255,255,.05);padding:var(--space-xs) var(--space-sm)}
  nav .tab{flex:0 1 auto;min-width:0;background:none;border:0;cursor:pointer;color:var(--ink-3);display:flex;flex-direction:column;align-items:center;justify-content:center;gap:3px;font-size:10px;font-weight:600;border-radius:14px;padding:0 var(--space-md);transition:color .15s var(--smooth),background .15s var(--smooth)}
  nav .tab svg{width:18px;height:18px;color:currentColor}
  nav .tab.active{color:var(--ink);background:rgba(255,255,255,.07)}
  nav .tab.active svg{color:var(--teal)}

  :focus-visible{outline:2px solid var(--teal);outline-offset:2px}
  input:focus,textarea:focus,select:focus{outline:none;border-color:var(--teal);box-shadow:0 0 0 3px color-mix(in srgb,var(--teal) 20%,transparent)}
  input::placeholder,textarea::placeholder{color:var(--ink-3);opacity:1}

  @media (hover:hover){
    .setbtn:hover:not(:disabled){filter:brightness(1.08)}
    .minibtn:hover:not(:disabled){border-color:var(--teal);color:var(--teal)}
    .minibtn.primary:hover:not(:disabled){color:var(--on-teal);border-color:transparent}
    nav .tab:hover{color:var(--ink)}
    .nrow:hover{color:var(--ink)}
  }
  @media (prefers-reduced-motion:reduce){
    *,*::before,*::after{transition-duration:.01ms!important;animation-duration:.01ms!important}
  }
  /* Narrow screens (cover screen, small phones): no room left to share, so
     tabs go back to equal cells and the dock hugs the edges. */
  @media (max-width:480px){
    nav{width:calc(100% - var(--space-sm))}
    nav .tab{flex:1;padding:0}
  }
  /* Cover-screen accent picker. The swatch IS the colour; selected is a ring. */
  .swatches{display:flex;flex-wrap:wrap;gap:var(--space-sm)}
  .sw{position:relative;width:42px;height:42px;border-radius:12px;border:1px solid var(--line);transition:transform .15s var(--pop)}
  .sw:hover:not(.on){transform:translateY(-2px) scale(1.04)}
  .sw.on::after{content:"";position:absolute;inset:0;border-radius:inherit;box-shadow:inset 0 0 0 2px var(--ink)}

  /* Floating "refresh status" action, above the nav dock on every screen. */
  .fab{position:fixed;right:var(--space-lg);bottom:calc(var(--tabbar-h) + 24px + env(safe-area-inset-bottom));z-index:30;width:50px;height:50px;display:grid;place-items:center;border-radius:var(--radius-pill);background:var(--accent-grad);color:var(--on-teal);border:0;box-shadow:0 8px 24px color-mix(in srgb,var(--teal) 32%,transparent),0 2px 8px rgba(0,0,0,.4);transition:transform .15s var(--pop),filter .15s var(--smooth)}
  .fab:active{filter:brightness(1.1);transform:scale(.92)}
  .fab:disabled{opacity:.55}
  .fab svg{width:22px;height:22px}
  .fab.spin svg{animation:acSpin .9s linear infinite}
  @keyframes acSpin{to{transform:rotate(360deg)}}
  /* Cover screen (~352x308): trim chrome so each tab is at most a short scroll */
  @media (max-height:420px){
    header{padding:6px var(--space-md)}
    .hero{padding:var(--space-md)}
    .ring-wrap{width:min(38vh,140px)}
    .ring-label{margin-top:var(--space-xs)}
    .card{padding:var(--space-md)}
    .stat-val,.cpu-load{font-size:26px}
    .cores{height:52px}
    :root{--tabbar-h:52px}
  }
</style>
</head>
<body>
<!--EMBEDDED_TOKENS-->
<div class="app">
  <header>
    <div class="sigind">
      <span class="bars" id="bars"><i></i><i></i><i></i><i></i></span>
      <div><div class="lab" id="tech">—</div><div class="op mono" id="wanip">—</div></div>
    </div>
    <div class="netbadge"><span class="dot amber" id="statusDot"></span><span id="netType">—</span></div>
    <a class="hdrbtn" href="/" aria-label="Back to the cover screen" title="Cover screen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="2" width="14" height="20" rx="2.5"/><line x1="12" y1="18" x2="12.01" y2="18"/></svg></a>
    <!-- op kept for the operator name, shown on the Network signal card -->
  </header>
  <div class="errslot" id="errSlot" role="alert"></div>

  <main>
  <h1 class="visually-hidden">Ametsuyu Flippost</h1>

  <section class="screen" id="home" role="tabpanel" aria-labelledby="tab-home" tabindex="-1">
    <div class="card clockcard">
      <div class="clocktime num" id="clkTime">--:--:--</div>
      <div class="clockdate" id="clkDate">—</div>
      <div class="clocklunar" id="clkLunar">—</div>
    </div>
    <div class="card hero">
      <div class="ring-wrap">
        <svg viewBox="0 0 120 120" aria-hidden="true">
          <defs>
            <linearGradient id="ringGrad" x1="0" y1="0" x2="120" y2="120" gradientUnits="userSpaceOnUse">
              <stop offset="0" stop-color="#2dd4bf"/><stop offset="1" stop-color="#34d399"/>
            </linearGradient>
          </defs>
          <circle cx="60" cy="60" r="52" fill="none" stroke="var(--track)" stroke-width="11"/>
          <circle id="ringFill" cx="60" cy="60" r="52" fill="none" stroke="url(#ringGrad)" stroke-width="11" stroke-linecap="round" stroke-dasharray="326.7" stroke-dashoffset="326.7" style="transition:stroke-dashoffset .4s cubic-bezier(0.4,0,0.2,1),stroke .4s cubic-bezier(0.4,0,0.2,1)"/>
        </svg>
        <div class="ring-center">
          <div class="ring-pct num"><span id="ringPct">0</span><span>%</span></div>
          <div class="ring-sub"><b class="num" id="ringUsed" style="color:var(--text)">—</b> <span id="ringCap">of —</span></div>
        </div>
      </div>
      <div class="ring-label" id="ringLbl">Mobile data · this month</div>
      <div class="ring-caption">Today <b class="num" id="capToday">—</b> · Week <b class="num" id="capWeek">—</b></div>
      <button class="minibtn" id="quotaSetBtn" style="display:none;margin-top:var(--space-xs)">Set a data limit</button>
    </div>
    <div class="duo">
      <div class="card">
        <div class="stat-head"><h2>Battery</h2><span class="accent" id="battAccent" style="background:var(--green)"></span></div>
        <div class="stat-val num"><span id="battLevel">—</span><small>%</small></div>
        <div class="stat-sub num" id="battSub">—</div>
      </div>
      <div class="card" id="tempCard">
        <div class="stat-head"><h2>Temp</h2><span class="accent" id="tempAccent" style="background:var(--amber)"></span></div>
        <div class="stat-val num" id="tempVal" style="color:var(--amber)"><span id="tempMax">—</span><small>°C</small></div>
        <div class="stat-sub num" id="tempSub">—</div>
      </div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Hotspot preset</h2><span id="hpActive" style="color:var(--ink-2)">—</span></div>
      <div id="hpQuick" class="pchips"><div class="stat-sub">No presets — add them in the Presets tab.</div></div>
      <div class="setmsg" id="hpMsg">Tap to switch the hotspot. Clients drop briefly (~5s), then reconnect.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Speedtest</h2></div>
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

  <section class="screen" id="net" role="tabpanel" aria-labelledby="tab-net" tabindex="-1">
    <div class="card">
      <div class="stat-head"><h2>Signal</h2><span id="sigTech" style="color:var(--ink-2)"></span></div>
      <div class="sg" id="sig"><div class="stat-sub">loading…</div></div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Hotspot</h2><span class="accent" id="hsAccent" style="background:var(--green)"></span></div>
      <div class="state-row"><span class="k">State</span><span class="v"><span class="dot off" id="hsDot"></span><span id="hsState">—</span></span></div>
      <div class="state-row"><span class="k">Auto (SSID whitelist)</span><span class="v" id="hsAuto">off</span></div>
      <div class="state-row" id="hsMatchRow" style="display:none"><span class="k">Seen nearby</span><span class="v" id="hsMatch">—</span></div>
      <div class="stat-sub" id="hsSub"></div>
      <div class="setmsg" id="hsPause" style="display:none"></div>
      <div class="state-row" id="hsOvRow" style="display:none"><span class="k">Forced on</span><span class="v" id="hsOvLeft">—</span></div>
      <div class="apbtns" id="hsOvBtns">
        <button class="minibtn" id="hsOv2" data-hours="2">2h</button>
        <button class="minibtn" id="hsOv4" data-hours="4">4h</button>
        <button class="minibtn" id="hsOv8" data-hours="8">8h</button>
        <button class="minibtn" id="hsOv12" data-hours="12">12h</button>
        <button class="minibtn" id="hsOv24" data-hours="24">24h</button>
      </div>
      <button class="minibtn" id="hsOvCancel" style="display:none;width:100%">Cancel forced-on</button>
      <div class="setmsg" id="hsOvMsg">Keeps the hotspot on for the chosen time even when a whitelisted network is in range; survives daemon restarts.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>USB tethering</h2></div>
      <div class="state-row"><span class="k">State</span><span class="v"><span class="dot off" id="usbDot"></span><span id="usbState">—</span></span></div>
      <div class="stat-sub" id="usbSub"></div>
      <button class="setbtn on" id="usbTetherBtn">Turn USB tethering on</button>
      <div class="setmsg" id="usbMsg">Needs a USB cable to a host computer; not thermal-gated.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Connectivity</h2></div>
      <div class="state-row"><span class="k">WAN IP</span><span class="v mono" id="wanIp">—</span></div>
      <div class="state-row"><span class="k">Airplane</span><span class="v"><span class="dot off" id="apDot"></span><span id="apState">off</span></span></div>
      <button class="setbtn sec" id="rotateBtn">Rotate IP (airplane cycle)</button>
      <button class="setbtn on" id="hotspotOnBtn">Turn hotspot on</button>
      <div class="apbtns">
        <button class="minibtn primary" id="apOnBtn">Airplane on</button>
        <button class="minibtn" id="apOffBtn">Airplane off</button>
      </div>
      <div class="setmsg" id="apMsg">Cycles airplane to pull a fresh carrier IP, then restarts the hotspot. ~15–30s; clients drop briefly.</div>
    </div>
    <div class="card">
      <div class="state-row"><span class="k">Bands</span><span class="v"><span class="dot off" id="bandDot"></span><span id="bandsv">—</span></span></div>
      <div class="state-row"><span class="k">Ingress</span><span class="v"><span class="dot green"></span><span>loopback/tailscale</span></span></div>
    </div>
  </section>

  <section class="screen" id="clientsScr" role="tabpanel" aria-labelledby="tab-clientsScr" tabindex="-1">
    <div class="card">
      <div class="stat-head"><h2>Clients</h2><span id="clientsN" style="color:var(--ink-2)">0</span></div>
      <div id="clients"><div class="stat-sub">no clients</div></div>
    </div>
  </section>

  <section class="screen" id="inbox" role="tabpanel" aria-labelledby="tab-inbox" tabindex="-1">
    <div class="card">
      <div class="stat-head"><h2>Messages</h2><button class="minibtn" id="inboxBtn">Refresh</button></div>
      <div id="smsList" aria-busy="true"><div class="skrow"><div class="sk" style="width:38%"></div><div class="sk" style="width:92%"></div></div><div class="skrow"><div class="sk" style="width:30%"></div><div class="sk" style="width:80%"></div></div><div class="skrow"><div class="sk" style="width:44%"></div><div class="sk" style="width:88%"></div></div></div>
      <div id="smsNote"></div>
      <div class="setmsg">Full message text, nothing masked. Tap a row to read the rest. Read-only — nothing here sends, replies or deletes.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Notifications</h2><span id="notifN" style="color:var(--ink-3)">—</span></div>
      <div id="notifList" aria-busy="true"><div class="skrow"><div class="sk" style="width:38%"></div><div class="sk" style="width:92%"></div></div><div class="skrow"><div class="sk" style="width:30%"></div><div class="sk" style="width:80%"></div></div><div class="skrow"><div class="sk" style="width:44%"></div><div class="sk" style="width:88%"></div></div></div>
      <div id="notifNote"></div>
      <div class="setmsg">What's on the phone's shade right now. Tap a row for the full text. Read-only — dismissing or acting on one has to happen on the phone.</div>
    </div>
    <div class="card" id="smsTokCard">
      <label class="f" for="setSmsTok">sms token (stored locally)</label>
      <input id="setSmsTok" type="password" placeholder="paste once">
      <div class="setmsg">Only needed when Open reads is off — with it on, the inbox opens with no token at all.</div>
    </div>
  </section>

  <section class="screen" id="system" role="tabpanel" aria-labelledby="tab-system" tabindex="-1">
    <div class="card">
      <div class="stat-head"><h2>CPU · per core</h2></div>
      <div class="cpu-head">
        <div class="cpu-load num"><span id="cpuLoad">—</span><small>load 5m</small></div>
        <div class="cpu-meta"><span id="cpuMode">—</span> · <span id="cpuCores">8</span> cores</div>
      </div>
      <div class="cores" id="cores"></div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Memory</h2></div>
      <div class="stat-val num"><span id="memPct">—</span><small>%</small></div>
      <div class="bar"><i id="memBar" style="transform:scaleX(0)"></i></div>
    </div>
    <div class="card" id="tempHist">
      <div class="stat-head"><h2>Temperature · history</h2><span id="thN" style="color:var(--ink-3)">—</span></div>
      <div class="apbtns" id="thRanges" style="grid-template-columns:repeat(4,1fr);margin-top:0">
        <button class="minibtn primary" data-range="1h">1h</button>
        <button class="minibtn" data-range="24h">24h</button>
        <button class="minibtn" data-range="7d">7d</button>
        <button class="minibtn" data-range="40d">40d</button>
      </div>
      <svg class="thChart" viewBox="0 0 300 90" preserveAspectRatio="none" aria-hidden="true">
        <line id="thGate" x1="0" x2="300" style="display:none"/>
        <polyline id="thLine" fill="none" points=""/>
      </svg>
      <div class="sg">
        <div class="sgrow"><span>Min</span><b class="num" id="thMin">—</b></div>
        <div class="sgrow"><span>Max</span><b class="num" id="thMax">—</b></div>
        <div class="sgrow"><span>Average</span><b class="num" id="thAvg">—</b></div>
        <div class="sgrow"><span>Latest</span><b class="num" id="thLast">—</b></div>
      </div>
      <div class="setmsg" id="thMsg">One sample a minute (the 14 s display median). Hour and day points are means of those samples.</div>
    </div>
    <div class="card">
      <div class="state-row"><span class="k">Thermal policy</span><span class="v"><span class="dot amber" id="policyDot"></span><span id="policyState">—</span></span></div>
      <div class="state-row"><span class="k">CPU mode</span><span class="v"><span class="dot green" id="cpuDot"></span><span id="cpuModeRow">—</span></span></div>
    </div>
  </section>

  <section class="screen" id="presets" role="tabpanel" aria-labelledby="tab-presets" tabindex="-1">
    <div class="card">
      <div class="stat-head"><h2>Auto-switch by location</h2><span class="accent" id="paAccent" style="background:var(--ink-3)"></span></div>
      <div class="state-row"><span class="k">Status</span><span class="v"><span class="dot off" id="paDot"></span><span id="paState">off</span></span></div>
      <div class="setgrid" style="grid-template-columns:1fr 1fr">
        <button class="minibtn" id="paOnBtn" style="min-height:44px">Turn on</button>
        <button class="minibtn" id="paOffBtn" style="min-height:44px">Turn off</button>
      </div>
      <div class="setmsg">Switches the hotspot preset when a preset's trigger Wi-Fi comes into range. Rides the hotspot scan; needs location services ON.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Presets</h2><span id="pCount" style="color:var(--ink-2)"></span></div>
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

  <section class="screen" id="settings" role="tabpanel" aria-labelledby="tab-settings" tabindex="-1">
    <div class="card">
      <div class="stat-head"><h2>Data limit</h2></div>
      <div class="setgrid">
        <div><label class="f" for="qLimit">Limit (GB)</label><input id="qLimit" type="number" min="0" step="0.1" inputmode="decimal" placeholder="0 = none"></div>
        <div><label class="f" for="qPeriod">Resets</label><select id="qPeriod">
          <option value="monthly">Monthly</option>
          <option value="weekly">Weekly (Mon)</option>
          <option value="daily">Daily</option>
          <option value="manual">Manual only</option>
        </select></div>
      </div>
      <div class="setgrid">
        <div><label class="f" for="qTime">Reset time</label><input id="qTime" type="time"></div>
        <div><label class="f" for="qDay">Day of month (monthly)</label><input id="qDay" type="number" min="1" max="28" step="1" inputmode="numeric"></div>
      </div>
      <button class="setbtn" id="qBtn">Save data limit</button>
      <div class="setmsg" id="qMsg">Enter your plan's cap to start tracking a period. 0 = no limit.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Cover screen</h2></div>
      <button class="setbtn sec" id="coverHomeBtn">Exit to cover screen</button>
      <div class="setmsg" id="coverHomeMsg">Hands the cover screen back to the Samsung clock. Reopen this app to take it back.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Cover screen buttons</h2></div>
      <div class="swatches" id="accentRow"></div>
      <div class="setmsg" id="accentMsg">Paints the cover screen's buttons in one of the seven accents. The kiosk picks it up within ten seconds.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Thermal gate · adjust</h2></div>
      <div class="setgrid">
        <div><label class="f" for="setWarn">Warn °C</label><input id="setWarn" type="number" step="0.5" inputmode="decimal"></div>
        <div><label class="f" for="setGate">Gate °C</label><input id="setGate" type="number" step="0.5" inputmode="decimal"></div>
      </div>
      <button class="setbtn" id="setBtn">Apply thermal limits</button>
      <div class="setmsg" id="setMsg">Gate is hard-capped at 48°C; Samsung mitigation is unaffected.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Nearby networks</h2><button class="minibtn" id="scanBtn">Scan now</button></div>
      <div id="nearby"><div class="stat-sub">Tap “Scan now” to list networks in range. A <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:-1px"><polyline points="20 6 9 17 4 12"/></svg> marks whitelisted ones; tap a row to add or remove it.</div></div>
      <div class="setmsg" id="nearbyMsg"></div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Hotspot auto-toggle</h2></div>
      <label class="f" for="wlBox">SSID whitelist (one per line — hotspot turns OFF when seen, back ON when absent)</label>
      <textarea id="wlBox" placeholder="HomeWifi&#10;OfficeWifi"></textarea>
      <button class="setbtn" id="wlBtn">Save whitelist</button>
      <div class="setmsg" id="wlMsg">Empty list disables the auto-toggle. Needs location services ON to scan.</div>
      <div id="wlList"></div>
    </div>
    <div class="card" id="tokCard">
      <label class="f" for="setTok">radio-control token (stored locally, used by both forms)</label>
      <input id="setTok" type="password" placeholder="paste once">
    </div>
    <div class="card">
      <div class="stat-head"><h2>Add a device</h2></div>
      <div id="qrWrap" style="display:flex;flex-direction:column;align-items:center;gap:10px">
        <img id="qrImg" alt="Scan to open on another device" width="200" height="200" style="border-radius:10px;background:#fff;padding:8px;display:none">
        <div class="stat-sub" id="qrHint">Point another phone's camera here — it opens the dashboard and remembers the token.</div>
      </div>
      <button class="setbtn" id="installBtn" style="display:none">Install app (add to home screen)</button>
      <div class="setmsg" id="qrMsg"></div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Open reads (no token on the tailnet)</h2><span class="accent" id="orAccent" style="background:var(--ink-3)"></span></div>
      <div class="state-row"><span class="k">Status</span><span class="v"><span class="dot off" id="orDot"></span><span id="orState">off</span></span></div>
      <div class="setgrid" style="grid-template-columns:1fr 1fr">
        <button class="minibtn primary" id="orOnBtn" style="min-height:44px">Turn on</button>
        <button class="minibtn" id="orOffBtn" style="min-height:44px">Turn off</button>
      </div>
      <div class="setmsg" id="orMsg">On: any device on your tailnet opens the dashboard with no token — read-only, inbox included. Off: a token (or the QR) is required. Needs the radio-control token to change.</div>
    </div>
    <div class="card">
      <div class="stat-head"><h2>Open control (no token for writes)</h2><span class="accent" id="ocAccent" style="background:var(--ink-3)"></span></div>
      <div class="state-row"><span class="k">Status</span><span class="v"><span class="dot off" id="ocDot"></span><span id="ocState">off</span></span></div>
      <div class="setgrid" style="grid-template-columns:1fr 1fr">
        <button class="minibtn primary" id="ocOnBtn" style="min-height:44px">Turn on</button>
        <button class="minibtn" id="ocOffBtn" style="min-height:44px">Turn off</button>
      </div>
      <div class="setmsg" id="ocMsg">On: writes (airplane, thermal, whitelist, reboot…) need no token on your tailnet. Enabling needs the radio-control token once.</div>
    </div>
    <div id="adminCard" style="display:none">
      <div class="sec-label">Admin · unlocked</div>
      <div class="card">
        <div class="stat-head"><h2>Bench mode · battery-less donor</h2><span class="accent" id="bmAccent" style="background:var(--ink-3)"></span></div>
        <div class="state-row"><span class="k">Status</span><span class="v"><span class="dot off" id="bmDot"></span><span id="bmState">off</span></span></div>
        <div class="state-row"><span class="k">Zones disabled</span><span class="v" id="bmZones">—</span></div>
        <div class="setgrid" style="grid-template-columns:1fr 1fr">
          <button class="minibtn primary" id="bmOnBtn" style="min-height:44px">Turn on</button>
          <button class="minibtn" id="bmOffBtn" style="min-height:44px">Turn off</button>
        </div>
        <div class="setmsg" id="bmMsg">No battery + bench supply only. Gate 70°C, auto re-arm ≤55°C; a trip restores all stock mitigation.</div>
      </div>
      <div class="card">
        <div class="stat-head"><h2>CPU policy</h2></div>
        <div class="setgrid">
          <div><label class="f" for="cpuModeSel">Mode</label>
          <select id="cpuModeSel">
            <option value="auto">auto</option>
            <option value="performance">performance</option>
            <option value="balanced">balanced</option>
            <option value="eco">eco</option>
            <option value="off">off</option>
          </select></div>
        </div>
        <button class="setbtn" id="cpuModeBtn">Apply CPU mode</button>
        <div class="setmsg" id="cpuModeMsg">auto: performance with clients, eco idle; hot always reduces. Persists across reboots.</div>
      </div>
      <div class="card">
        <div class="stat-head"><h2>Danger</h2></div>
        <div class="setgrid" style="grid-template-columns:1fr 1fr">
          <button class="setbtn sec" id="coolBtn" style="min-height:44px">Force CPU cooldown</button>
          <button class="setbtn sec" id="rebootBtn" style="min-height:44px">Reboot device</button>
          <button class="setbtn sec" id="usageResetBtn" style="min-height:44px;grid-column:1/-1">Reset data usage</button>
        </div>
        <div class="setmsg" id="dangerMsg">Cooldown parks the prime core + caps the mid cluster. Reboot restarts the whole phone; the module comes back by itself.</div>
      </div>
    </div>
    <div class="sec-label">Integrations</div>
    <div class="card">
      <div class="intg">
        <div class="logo" style="background:linear-gradient(180deg,#707cfd 0%,#5865f2 100%)"><svg width="24" height="24" viewBox="0 0 24 24" fill="#fafafa"><path d="M19.6 5.3A18 18 0 0 0 15 3.9l-.24.47a13 13 0 0 1 4 .96 12.9 12.9 0 0 0-11.5 0 13 13 0 0 1 4-.96L11 3.9A18 18 0 0 0 6.4 5.3C3.5 9.6 2.7 13.8 3.1 17.9a18 18 0 0 0 5.5 2.8l.45-.98a12 12 0 0 1-1.9-.9l.35-.27a12.9 12.9 0 0 0 11 0l.35.27c-.6.36-1.24.66-1.9.9l.45.98a18 18 0 0 0 5.5-2.8c.47-4.77-.79-8.94-3.75-12.6ZM9.35 15.4c-.9 0-1.63-.82-1.63-1.83 0-1 .72-1.83 1.63-1.83.9 0 1.64.83 1.62 1.83 0 1-.72 1.83-1.62 1.83Zm5.3 0c-.9 0-1.63-.82-1.63-1.83 0-1 .72-1.83 1.63-1.83.9 0 1.64.83 1.62 1.83 0 1-.72 1.83-1.62 1.83Z"/></svg></div>
        <div class="body"><div class="name">Discord</div><div class="desc">Self-hosted Gateway bot · see selfhost/</div></div>
        <span class="managed">Bot</span>
      </div>
    </div>
    <div class="card">
      <div class="intg">
        <div class="logo" style="background:var(--gradient-surface);box-shadow:var(--shadow-card)"><svg width="22" height="22" viewBox="0 0 24 24" fill="#fafafa"><circle cx="5" cy="5" r="2.1" opacity=".35"/><circle cx="12" cy="5" r="2.1" opacity=".35"/><circle cx="19" cy="5" r="2.1" opacity=".35"/><circle cx="5" cy="12" r="2.1"/><circle cx="12" cy="12" r="2.1"/><circle cx="19" cy="12" r="2.1"/><circle cx="5" cy="19" r="2.1" opacity=".35"/><circle cx="12" cy="19" r="2.1" opacity=".35"/><circle cx="19" cy="19" r="2.1" opacity=".35"/></svg></div>
        <div class="body"><div class="name">Tailscale</div><div class="desc">Secure ingress · wired through tailscaled</div></div>
        <span class="managed">Managed</span>
      </div>
    </div>
  </section>
  </main>
</div>

<nav role="tablist" aria-label="Dashboard sections">
  <button class="tab active" data-screen="home" aria-current="page" role="tab" id="tab-home" aria-controls="home" aria-selected="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="8" height="8" rx="1.6"/><rect x="13" y="3" width="8" height="5" rx="1.6"/><rect x="13" y="10" width="8" height="11" rx="1.6"/><rect x="3" y="13" width="8" height="8" rx="1.6"/></svg>Home</button>
  <button class="tab" data-screen="net" role="tab" id="tab-net" aria-controls="net" aria-selected="false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 20h.01M7 20v-4M12 20v-8M17 20V8M22 20V4"/></svg>Network</button>
  <button class="tab" data-screen="clientsScr" role="tab" id="tab-clientsScr" aria-controls="clientsScr" aria-selected="false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75"/></svg>Clients</button>
  <button class="tab" data-screen="inbox" role="tab" id="tab-inbox" aria-controls="inbox" aria-selected="false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 16 12 14 15 10 15 8 12 2 12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/></svg>Inbox</button>
  <button class="tab" data-screen="system" role="tab" id="tab-system" aria-controls="system" aria-selected="false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3M15 1v3M9 20v3M15 20v3M1 9h3M1 15h3M20 9h3M20 15h3"/></svg>System</button>
  <button class="tab" data-screen="presets" role="tab" id="tab-presets" aria-controls="presets" aria-selected="false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>Presets</button>
  <button class="tab" data-screen="settings" role="tab" id="tab-settings" aria-controls="settings" aria-selected="false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>Settings</button>
</nav>
<button class="fab" id="refreshFab" type="button" aria-label="Refresh status" title="Refresh status"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 0 1 15.36-6.36L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-15.36 6.36L3 16"/><path d="M3 21v-5h5"/></svg></button>

<script>
(function(){
  "use strict";
  // Same-origin (relative) so the page works whether served on 127.0.0.1:18080
  // in the kiosk WebView, on a non-default bind_port, or proxied over Tailscale
  // — and never fetches the viewer's own localhost when opened remotely.
  var API="", GB=1e9, CAP=0, quotaSeeded=false;
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

  // Hand-authored inline Lucide-geometry icons (no CDN/icon font). Static
  // markup only — never interpolated with user-controlled data.
  var ICN_WIFI='<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:-3px;margin-right:4px"><path d="M5 13a10 10 0 0 1 14 0"/><path d="M8.5 16.5a5 5 0 0 1 7 0"/><path d="M2 8.82a15 15 0 0 1 20 0"/><line x1="12" y1="20" x2="12.01" y2="20"/></svg>';
  var ICN_WARN='<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:-3px;margin-right:4px"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>';
  var ICN_USB='<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:-3px;margin-right:4px"><circle cx="10" cy="7" r="1"/><circle cx="4" cy="20" r="1"/><path d="M4.7 19.3 19 5"/><path d="m21 3-3 1 2 2Z"/><path d="M9.26 7.68 5 12l2 5"/><path d="m10 14 5 2 3.5-3.5"/><path d="m18 12 1-1 1 1-1 1Z"/></svg>';
  var ICN_PLANE='<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:-2px;margin-right:3px"><path d="M17.8 19.2 16 11l3.5-3.5C21 6 21.5 4 21 3c-1-.5-3 0-4.5 1.5L13 8 4.8 6.2c-.5-.1-.9.1-1.1.5l-.3.5c-.2.5-.1 1 .3 1.3L9 12l-2 3H4l-1 1 3 2 2 3 1-1v-3l3-2 3.5 5.3c.3.4.8.5 1.3.3l.5-.2c.4-.3.6-.7.5-1.2z"/></svg>';
  var ICN_CHECK='<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:-1px;margin-right:3px"><polyline points="20 6 9 17 4 12"/></svg>';
  var ICN_X='<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>';
  var ICN_CHEVRON='<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>';
  var ICN_DOT='<svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" stroke="none" style="vertical-align:-1px;margin-right:4px;color:var(--teal)"><circle cx="12" cy="12" r="9"/></svg>';

  var active="home", lastKick=0;
  var tabs=document.querySelectorAll("nav .tab");
  var deck=document.querySelector("main");
  var screens=[].slice.call(document.querySelectorAll(".screen"));

  // Both routes into a page land here: a nav tap (which scrolls the deck) and a
  // swipe (which settles on it). Idempotent, so the scroll ending on the page a
  // tap already selected costs nothing.
  function selectScreen(id,moveFocus){
    if(id===active&&!moveFocus)return;
    tabs.forEach(function(t){var on=t.getAttribute("data-screen")===id;t.classList.toggle("active",on);t.setAttribute("aria-selected",on?"true":"false");if(on)t.setAttribute("aria-current","page");else t.removeAttribute("aria-current");});
    var panel=document.getElementById(id);
    // Move focus into the page a tap or keypress opened: without it the view
    // changes silently for screen-reader and keyboard users. A swipe already
    // has the owner looking at the page, and taking focus mid-gesture fights it.
    if(panel){panel.scrollTop=0;if(moveFocus)panel.focus({preventScroll:true});}
    var was=active; active=id;
    try{history.replaceState(null,"",location.pathname+location.search+"#"+id);}catch(e){}
    if(id==="settings"&&typeof loadQR==="function")loadQR();
    if(id==="inbox"&&typeof loadInbox==="function")loadInbox();
    // Refresh the newly shown page, but throttle: rapid page-hopping must not
    // burst past the read-status rate limit (each tick is 2-3 requests).
    if(was!==id){var now=Date.now();if(now-lastKick>1500){lastKick=now;tick();}}
  }

  tabs.forEach(function(tab){tab.addEventListener("click",function(){
    // Jump, don't glide: scroll-behavior:smooth is a no-op wherever the browser
    // has smooth scrolling switched off, which would leave the nav bar selecting
    // a page the deck never moved to. Swipes animate themselves.
    var id=tab.getAttribute("data-screen"), i=screens.indexOf(document.getElementById(id));
    if(i>=0)deck.scrollLeft=i*deck.clientWidth;
    selectScreen(id,true);
  });});

  // Swipe: the deck is a snap scroller, so whichever page it comes to rest on
  // is the selection. Debounced rather than a scrollend listener — that event stayed
  // silent for programmatic scrolls in testing, and a missed one would leave the
  // nav bar pointing at a page the owner already swiped away from.
  var settleT=0;
  function deckSettled(){
    if(!deck.clientWidth)return;
    var i=Math.round(deck.scrollLeft/deck.clientWidth);
    var sc=screens[Math.max(0,Math.min(screens.length-1,i))];
    if(sc)selectScreen(sc.id,false);
  }
  deck.addEventListener("scroll",function(){clearTimeout(settleT);settleT=setTimeout(deckSettled,120);});

  // Vietnamese lunar calendar (Ho Ngoc Duc's algorithm, the reference one every
  // Vietnamese calendar uses). Same astronomy as the Chinese calendar but
  // evaluated at UTC+7, not UTC+8 — that offset is the whole difference, and it
  // is what decides which day Tet falls on in the years the two disagree. The
  // browser's own Intl "chinese" calendar would silently give the UTC+8 answer.
  var LUNAR_TZ=7;
  function jdFromDate(dd,mm,yy){
    var a=Math.floor((14-mm)/12), y=yy+4800-a, m=mm+12*a-3;
    var jd=dd+Math.floor((153*m+2)/5)+365*y+Math.floor(y/4)-Math.floor(y/100)+Math.floor(y/400)-32045;
    if(jd<2299161)jd=dd+Math.floor((153*m+2)/5)+365*y+Math.floor(y/4)-32083;
    return jd;
  }
  // Julian day of the k-th new moon since 1900-01-01 (Meeus, Astronomical Algorithms ch.49).
  function newMoon(k){
    var T=k/1236.85, T2=T*T, T3=T2*T, dr=Math.PI/180;
    var jd1=2415020.75933+29.53058868*k+0.0001178*T2-0.000000155*T3;
    jd1=jd1+0.00033*Math.sin((166.56+132.87*T-0.009173*T2)*dr);
    var M=359.2242+29.10535608*k-0.0000333*T2-0.00000347*T3;
    var Mpr=306.0253+385.81691806*k+0.0107306*T2+0.00001236*T3;
    var F=21.2964+390.67050646*k-0.0016528*T2-0.00000239*T3;
    var c1=(0.1734-0.000393*T)*Math.sin(M*dr)+0.0021*Math.sin(2*dr*M);
    c1=c1-0.4068*Math.sin(Mpr*dr)+0.0161*Math.sin(dr*2*Mpr);
    c1=c1-0.0004*Math.sin(dr*3*Mpr);
    c1=c1+0.0104*Math.sin(dr*2*F)-0.0051*Math.sin(dr*(M+Mpr));
    c1=c1-0.0074*Math.sin(dr*(M-Mpr))+0.0004*Math.sin(dr*(2*F+M));
    c1=c1-0.0004*Math.sin(dr*(2*F-M))-0.0006*Math.sin(dr*(2*F+Mpr));
    c1=c1+0.0010*Math.sin(dr*(2*F-Mpr))+0.0005*Math.sin(dr*(2*Mpr+M));
    var deltat=T<-11?(0.001+0.000839*T+0.0002261*T2-0.00000845*T3-0.000000081*T*T3)
                    :(-0.000278+0.000265*T+0.000262*T2);
    return jd1+c1-deltat;
  }
  function sunLongitude(jdn){
    var T=(jdn-2451545.0)/36525, T2=T*T, dr=Math.PI/180;
    var M=357.52910+35999.05030*T-0.0001559*T2-0.00000048*T*T2;
    var L0=280.46645+36000.76983*T+0.0003032*T2;
    var DL=(1.914600-0.004817*T-0.000014*T2)*Math.sin(dr*M);
    DL=DL+(0.019993-0.000101*T)*Math.sin(dr*2*M)+0.000290*Math.sin(dr*3*M);
    var L=(L0+DL)*dr;
    return L-Math.PI*2*Math.floor(L/(Math.PI*2));
  }
  function sunZodiac(dayNumber,tz){return Math.floor(sunLongitude(dayNumber-0.5-tz/24)/Math.PI*6);}
  function newMoonDay(k,tz){return Math.floor(newMoon(k)+0.5+tz/24);}
  // The 11th lunar month is the one containing the winter solstice.
  function lunarMonth11(yy,tz){
    var k=Math.floor((jdFromDate(31,12,yy)-2415021)/29.530588853);
    var nm=newMoonDay(k,tz);
    if(sunZodiac(nm,tz)>=9)nm=newMoonDay(k-1,tz);
    return nm;
  }
  // In a 13-month year the leap month is the first that holds no principal term.
  function leapMonthOffset(a11,tz){
    var k=Math.floor((a11-2415021.076998695)/29.530588853+0.5), last=0, i=1;
    var arc=sunZodiac(newMoonDay(k+i,tz),tz);
    do{last=arc;i++;arc=sunZodiac(newMoonDay(k+i,tz),tz);}while(arc!==last&&i<14);
    return i-1;
  }
  // -> [day, month, year, isLeapMonth]
  function solarToLunar(dd,mm,yy,tz){
    var dayNumber=jdFromDate(dd,mm,yy);
    var k=Math.floor((dayNumber-2415021.076998695)/29.530588853);
    var monthStart=newMoonDay(k+1,tz);
    if(monthStart>dayNumber)monthStart=newMoonDay(k,tz);
    var a11=lunarMonth11(yy,tz), b11=a11, lunarYear;
    if(a11>=monthStart){lunarYear=yy;a11=lunarMonth11(yy-1,tz);}
    else{lunarYear=yy+1;b11=lunarMonth11(yy+1,tz);}
    var lunarDay=dayNumber-monthStart+1;
    var diff=Math.floor((monthStart-a11)/29);
    var lunarLeap=0, lunarMonth=diff+11;
    if(b11-a11>365){
      var off=leapMonthOffset(a11,tz);
      if(diff>=off){lunarMonth=diff+10;if(diff===off)lunarLeap=1;}
    }
    if(lunarMonth>12)lunarMonth=lunarMonth-12;
    if(lunarMonth>=11&&diff<4)lunarYear-=1;
    return [lunarDay,lunarMonth,lunarYear,lunarLeap];
  }

  var clkTime=document.getElementById("clkTime"), clkDate=document.getElementById("clkDate"), clkLunar=document.getElementById("clkLunar");
  var DOW=["Sunday","Monday","Tuesday","Wednesday","Thursday","Friday","Saturday"];
  var MON=["January","February","March","April","May","June","July","August","September","October","November","December"];
  var clkDay=-1;
  function tickClock(){
    var d=new Date();
    clkTime.textContent=d.toTimeString().slice(0,8);
    renderOvLeft(); // local hotspot-override countdown; zero requests
    // Date and lunar conversion only change at midnight; rerunning the astronomy
    // every second would burn the cover screen's battery for nothing.
    if(clkDay===d.getDate())return;
    clkDay=d.getDate();
    clkDate.textContent=DOW[d.getDay()]+", "+d.getDate()+" "+MON[d.getMonth()]+" "+d.getFullYear();
    var l=solarToLunar(d.getDate(),d.getMonth()+1,d.getFullYear(),LUNAR_TZ);
    clkLunar.textContent="Lunar "+l[0]+"/"+l[1]+(l[3]?" (leap)":"")+" · "+l[2];
  }
  tickClock(); setInterval(tickClock,1000);

  var coresEl=document.getElementById("cores"), coreEls=[];
  function buildCores(n){coresEl.innerHTML="";coreEls=[];for(var i=0;i<n;i++){var c=document.createElement("div");c.className="core";var tr=document.createElement("div");tr.className="track";var f=document.createElement("div");f.className="fill";f.style.transform="scaleY(0)";var idx=document.createElement("div");idx.className="idx num";idx.textContent=i;tr.appendChild(f);c.appendChild(tr);c.appendChild(idx);coresEl.appendChild(c);coreEls.push({core:c,fill:f});}}
  buildCores(8);

  function get(p){var h={};if(token)h.Authorization="Bearer "+token;return fetch(API+p,{headers:h}).then(function(r){if(!r.ok)throw new Error(p+" "+r.status);return r.json();});}
  var errEl=document.getElementById("errSlot");
  function showErr(m){errEl.textContent=m;errEl.classList.add("show");}
  function clearErr(){errEl.classList.remove("show");}
  function esc(s){return String(s==null?"":s).replace(/[&<>]/g,function(c){return {"&":"&amp;","<":"&lt;",">":"&gt;"}[c];});}
  // Skips the innerHTML write (and its DOM-recreate, which would replay the
  // acPop entrance animation on every poll tick) when the markup hasn't changed.
  function setListHTML(box,html){if(box._lastHtml===html)return false;box._lastHtml=html;box.innerHTML=html;return true;}
  function fmtBytes(b){var gb=b/GB;if(gb>=1000)return (gb/1000).toFixed(2)+" TB";return (gb>=10?Math.round(gb):gb.toFixed(1))+" GB";}
  function tempColor(c){return c>=GATE_C?"var(--red)":(c>=WARN_C?"var(--amber)":"var(--teal)");}
  var CIRC=2*Math.PI*52;

  function rsrpCls(v){return v>=-95?"good":(v>=-110?"mid":"low");}
  function sinrCls(v){return v>=13?"good":(v>=0?"mid":"low");}
  function stCls(s){return s=="REACHABLE"?"green":(s=="FAILED"?"red":(s=="DELAY"||s=="PROBE"?"amber":"off"));}

  // SAFE/RECOVERY are safe; WARM is caution; HOT/COOLDOWN are hot. RECOVERY is a
  // transitional-but-safe state (radio writes are allowed), so it must not read red.
  function polColor(p){return (p==="SAFE"||p==="RECOVERY")?"green":(p==="WARM"?"amber":"red");}
  function renderStatus(s){
    var net=s.network||{}, bat=s.battery||{}, th=s.thermal||{}, ip=s.wan_ip||{};
    if(typeof s.cover_home==="boolean"&&typeof renderCoverHome==="function")renderCoverHome(s.cover_home);
    if(s.cover_accent&&typeof renderAccent==="function")renderAccent(s.cover_accent);
    if(typeof s.open_reads==="boolean"&&document.getElementById("orState"))renderOpenReads(s.open_reads);
    if(typeof s.open_control==="boolean"){openControl=s.open_control;if(document.getElementById("ocState"))renderOpenControl(s.open_control);}
    if(s.hotspot_presets)renderPresets(s.hotspot_presets);
    if(s.bench)renderBench(s.bench);
    // Always show the WAN IP (header). Airplane on / no data -> explicit label.
    setListHTML(document.getElementById("wanip"),s.airplane?(ICN_PLANE+"airplane"):esc(ip.available&&ip.ip?ip.ip:"no data"));
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
      document.getElementById("battAccent").style.background="var(--ink-3)";
    }else{
      var lvl=Math.round(bat.level);
      battEl.textContent=lvl;
      document.getElementById("battSub").textContent=(bat.plugged||"")+" · "+(bat.temp_c!=null?bat.temp_c.toFixed(1)+"°C":"");
      var bcol=lvl<=10?"var(--red)":(lvl<=20?"var(--amber)":"var(--green)");
      document.getElementById("battAccent").style.background=bcol;
    }

    var tmax=th.temp_max_c, hasT=tmax!=null&&tmax>0, tcol=hasT?tempColor(tmax):"var(--ink-3)";
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
    per.forEach(function(p,i){if(!coreEls[i])return;p=Math.max(0,Math.min(100,p));coreEls[i].fill.style.transform="scaleY("+(p/100)+")";coreEls[i].fill.style.background=p>85?"var(--red)":(p>60?"var(--amber)":"var(--teal)");});

    var mp=h.mem_used_pct!=null?h.mem_used_pct:0;document.getElementById("memPct").textContent=h.mem_used_pct!=null?mp:"—";document.getElementById("memBar").style.transform="scaleX("+(mp/100)+")";
    document.getElementById("updated").textContent=new Date().toLocaleTimeString([], {hour:"2-digit",minute:"2-digit",second:"2-digit"});
  }

  // Local, human string for an RFC3339 instant (next_reset/last_reset); "" for
  // a manual period (the daemon sends no next_reset then).
  function fmtWhenIso(iso){
    if(!iso)return "";
    var d=new Date(iso);
    return isNaN(d.getTime())?"":d.toLocaleString([],{month:"short",day:"numeric",hour:"2-digit",minute:"2-digit"});
  }
  var RING_LABEL={daily:"Mobile data · today",weekly:"Mobile data · this week",monthly:"Mobile data · this cycle",manual:"Mobile data · since reset"};
  function renderUsage(u){
    CAP=u.limit_bytes||0;
    var used=u.period_bytes||0, ring=document.getElementById("ringFill"), ringCap=document.getElementById("ringCap");
    if(CAP>0){
      var pct=used/CAP, pc=Math.min(1,pct);
      document.getElementById("ringPct").textContent=Math.round(pct*100);
      // healthy period rides the gradient; amber/red take over past the
      // warning and over-cap marks so status stays louder than decoration
      ring.style.stroke=pct>0.9?"var(--red)":(pct>=0.7?"var(--amber)":"url(#ringGrad)");
      ring.style.strokeDashoffset=(CIRC*(1-pc)).toFixed(1);
      ringCap.textContent="of "+fmtBytes(CAP);
    }else{
      document.getElementById("ringPct").textContent="—";
      ring.style.stroke="var(--track)";ring.style.strokeDashoffset=CIRC.toFixed(1);
      ringCap.textContent="· no limit";
    }
    // period_human is the daemon's own SI string (MB/KB below 1 GB); fmtBytes
    // only ever emits GB/TB and would show "0.0 GB" for a sub-GB period.
    document.getElementById("ringUsed").textContent=u.period_human||fmtBytes(used);
    document.getElementById("ringLbl").textContent=RING_LABEL[u.period]||RING_LABEL.monthly;
    document.getElementById("quotaSetBtn").style.display=CAP>0?"none":"";
    document.getElementById("capToday").textContent=u.today_human;document.getElementById("capWeek").textContent=u.week_human;

    // Seed the settings form once from live values; don't clobber a field the
    // owner is mid-edit on a later poll (same rule as gateSeeded).
    if(!quotaSeeded){
      document.getElementById("qLimit").value=CAP?Math.round(CAP/GB*10)/10:"";
      document.getElementById("qPeriod").value=u.period||"monthly";
      document.getElementById("qTime").value=u.reset_time||"00:00";
      document.getElementById("qDay").value=u.reset_day||1;
      qSyncDisabled();
      quotaSeeded=true;
    }
    var qMsg=document.getElementById("qMsg");
    if(qMsg)qMsg.textContent=CAP?("Next reset "+fmtWhenIso(u.next_reset)):"No limit set yet — enter your plan's cap.";
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
    if(!sig.available){setListHTML(box,'<div class="stat-sub">unavailable</div>');return;}
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
    setListHTML(box,h);
  }

  function renderClients(cl){
    document.getElementById("clientsN").textContent=(cl.count||0)+(cl.count===1?" client":" clients");
    var box=document.getElementById("clients");
    // A tick can land mid-read: remember which rows the owner had expanded so
    // a data change (a client joins/leaves, the list reorders) doesn't collapse
    // them. Keyed by MAC, not index — the list reorders as neighbours age.
    var openMacs=[];
    box.querySelectorAll("details.cli[open]").forEach(function(d){openMacs.push(d.getAttribute("data-mac"));});
    if(!cl.clients||!cl.clients.length){setListHTML(box,'<div class="stat-sub">no clients</div>');return;}
    var html="";
    cl.clients.forEach(function(c){
      var st=c.state||"?";
      var v6=(c.ipv6&&c.ipv6.length)?'<div class="r"><span>IPv6</span><b class="mono" style="font-size:10.5px;text-align:right">'+c.ipv6.map(esc).join("<br>")+'</b></div>':"";
      html+='<details class="cli" data-mac="'+esc(c.mac)+'"><summary><span class="dot '+stCls(st)+'"></span><b class="mono">'+esc(c.ipv4||"(no IPv4)")+'</b><span style="color:var(--ink-3);font-size:11.5px">'+esc(st)+'</span><span class="chev">'+ICN_CHEVRON+'</span></summary>'
        +'<div class="clibody"><div class="r"><span>MAC</span><b class="mono">'+esc(c.mac)+'</b></div><div class="r"><span>State</span><b>'+esc(st)+'</b></div>'+v6+'</div></details>';
    });
    if(setListHTML(box,html)&&openMacs.length){
      box.querySelectorAll("details.cli").forEach(function(d){
        if(openMacs.indexOf(d.getAttribute("data-mac"))>=0)d.open=true;
      });
    }
  }

  function renderCPU(c){
    if(!c)return;
    document.getElementById("cpuMode").textContent=c.mode||"—";
    document.getElementById("cpuModeRow").textContent=(c.mode||"—")+(c.requested&&c.requested!==c.mode?" ("+c.requested+")":"");
    document.getElementById("cpuDot").className="dot "+(c.mode==="performance"?"green":(c.mode==="eco"?"amber":(c.mode==="off"?"off":"green")));
    var sel=document.getElementById("cpuModeSel");
    if(sel&&c.requested&&!sel._seeded){sel.value=c.requested;sel._seeded=true;}
    if(c.cores&&coreEls.length===c.cores.length){c.cores.forEach(function(ci,i){coreEls[i].core.classList.toggle("off",!ci.online);});}
  }

  // Temperature history (System tab). thData is the last /v1/thermal/history
  // fetch, refetched at the SLOW cadence only while that tab is open (see the
  // polls table); range switches just re-render already-fetched data, no refetch.
  var thRange="1h", thData=null;
  var THMSG_DEFAULT=document.getElementById("thMsg").textContent;
  function thPick(){
    if(!thData)return[];
    if(thRange==="1h")return thData.minutes.slice(-60).map(function(m){return[m[0],m[1],1];});
    if(thRange==="24h")return thData.minutes.map(function(m){return[m[0],m[1],1];});
    var b=thRange==="7d"?thData.hours:thData.days;
    return b.map(function(p){return[p.t,p.c,p.n];});
  }
  function renderTempHist(){
    var pts=thPick(), line=document.getElementById("thLine"), gate=document.getElementById("thGate");
    if(!pts.length){
      line.setAttribute("points","");gate.style.display="none";
      document.getElementById("thN").textContent="—";
      document.getElementById("thMin").textContent=document.getElementById("thMax").textContent=
        document.getElementById("thAvg").textContent=document.getElementById("thLast").textContent="—";
      document.getElementById("thMsg").textContent="Collecting — first point in about a minute.";
      return;
    }
    document.getElementById("thMsg").textContent=THMSG_DEFAULT;
    var vals=pts.map(function(p){return p[1];});
    var lo=Math.min.apply(null,vals), hi=Math.max.apply(null,vals);
    var y0=Math.floor(lo)-1, y1=Math.ceil(hi)+1;
    if(y1-y0<4){var mid=(y0+y1)/2;y0=mid-2;y1=mid+2;}
    line.setAttribute("points",pts.map(function(p,i){
      var x=pts.length>1?i/(pts.length-1)*300:150, y=90-(p[1]-y0)/(y1-y0)*90;
      return x.toFixed(1)+","+y.toFixed(1);
    }).join(" "));
    if(GATE_C>y0&&GATE_C<y1){
      var gy=(90-(GATE_C-y0)/(y1-y0)*90).toFixed(1);
      gate.setAttribute("y1",gy);gate.setAttribute("y2",gy);gate.style.display="";
    }else{gate.style.display="none";}
    document.getElementById("thMin").textContent=lo.toFixed(1)+"°";
    document.getElementById("thMax").textContent=hi.toFixed(1)+"°";
    document.getElementById("thAvg").textContent=(vals.reduce(function(a,v){return a+v;},0)/vals.length).toFixed(1)+"°";
    document.getElementById("thLast").textContent=vals[vals.length-1].toFixed(1)+"°";
    var unit=thRange==="7d"?"h":thRange==="40d"?"d":"pts";
    document.getElementById("thN").textContent=unit==="pts"?(pts.length+" pts"):
      (pts.length+" "+unit+" · last "+pts[pts.length-1][2]+" samples");
  }
  document.querySelectorAll("#thRanges .minibtn").forEach(function(b){
    b.addEventListener("click",function(){
      thRange=b.getAttribute("data-range");
      document.querySelectorAll("#thRanges .minibtn").forEach(function(x){x.classList.toggle("primary",x===b);});
      renderTempHist();
    });
  });

  function renderBands(b){
    if(!b||!b.available){document.getElementById("bandsv").textContent="—";return;}
    document.getElementById("bandsv").textContent=b.is_max?"all unlocked (NR+LTE)":(b.nr_enabled?"NR+…":"LTE only");
    document.getElementById("bandDot").className="dot "+(b.is_max?"green":(b.nr_enabled?"amber":"off"));
  }

  // Hours+minutes left, e.g. "3h 12m"; under an hour, minutes+seconds so the
  // countdown visibly runs, e.g. "45m 09s".
  function fmtHM(totalSec){
    totalSec=Math.max(0,totalSec);
    var h=Math.floor(totalSec/3600), m=Math.floor((totalSec%3600)/60), s=Math.floor(totalSec%60);
    return h>0?(h+"h "+m+"m"):(m+"m "+("0"+s).slice(-2)+"s");
  }

  // Local countdown for the hotspot "forced on" override: ovEnd is a local ms
  // timestamp set from receive-time + override_left_s (see renderHotspot), so
  // it ticks down with zero requests and is immune to clock skew between this
  // browser and the phone. Driven by tickClock's 1s timer, not a new interval.
  var ovEnd=0;
  function renderOvLeft(){
    if(!ovEnd)return;
    var left=Math.ceil((ovEnd-Date.now())/1000);
    if(left<=0){
      ovEnd=0;
      document.getElementById("hsOvRow").style.display="none";
      document.getElementById("hsOvBtns").style.display="";
      document.getElementById("hsOvCancel").style.display="none";
      lastAt["/v1/hotspot"]=0; // confirm from the daemon on the next heartbeat
      return;
    }
    var until=lastHs&&lastHs.override_until?new Date(lastHs.override_until):null;
    var untilOk=until&&!isNaN(until.getTime());
    document.getElementById("hsOvLeft").textContent=fmtHM(left)+" left"
      +(untilOk?(" · until "+until.toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"})):"");
  }

  var wlLoaded=false, lastHs=null;
  function renderHotspot(h){
    lastHs=h;
    document.getElementById("hsState").textContent=h.active?"on":"off";
    document.getElementById("hsDot").className="dot "+(h.active?"green":"off");
    document.getElementById("hsAccent").style.background=h.active?"var(--green)":"var(--ink-3)";
    // Short here on purpose: the hsPause banner below carries the full hint,
    // so this row never says "paused: location off" twice.
    var auto=h.auto?(h.paused?"paused":"watching "+h.whitelist.length+" SSID"+(h.whitelist.length===1?"":"s")):"off";
    document.getElementById("hsAuto").textContent=auto;
    var mr=document.getElementById("hsMatchRow");
    if(h.matched&&h.matched.length){mr.style.display="";document.getElementById("hsMatch").textContent=h.matched.join(", ");}
    else{mr.style.display="none";}
    var sub=[];
    if(h.last_action)sub.push(h.last_action);
    if(h.auto&&h.ap_count)sub.push(h.ap_count+" APs in last scan");
    document.getElementById("hsSub").textContent=sub.join(" · ");

    var pauseEl=document.getElementById("hsPause");
    if(h.auto&&h.paused){
      pauseEl.style.display="";
      setListHTML(pauseEl,h.paused==="location_off"
        ?(ICN_WARN+"Scanning paused: turn on Location on the phone. Retries automatically.")
        :(ICN_WARN+"Scanning paused: scan failed ("+esc(h.paused_detail||"unknown")+"). Retrying automatically."));
    }else{
      pauseEl.style.display="none";
    }

    var ovActive=!!h.override_until;
    document.getElementById("hsOvRow").style.display=ovActive?"":"none";
    document.getElementById("hsOvBtns").style.display=ovActive?"none":"";
    document.getElementById("hsOvCancel").style.display=ovActive?"":"none";
    // Anchor on receive-time + the daemon's own left_s, not on override_until
    // vs the browser clock — a tailnet laptop's clock can be minutes off a
    // donor phone's. renderOvLeft() (driven by the 1s clock tick) recomputes
    // the remaining seconds locally between polls, so the countdown runs with
    // zero extra requests.
    ovEnd=ovActive?Date.now()+(h.override_left_s||0)*1000:0;
    if(ovActive)renderOvLeft();

    if(!wlLoaded&&h.whitelist){document.getElementById("wlBox").value=h.whitelist.join("\n");wlLoaded=true;}
    renderWhitelist(h.whitelist||[]);
    renderNearby(h);
    hsActive=!!h.active; renderHotspotBtn();
  }

  var usbActive=false;
  function renderUsbTether(u){
    document.getElementById("usbState").textContent=u.active?"on":"off";
    document.getElementById("usbDot").className="dot "+(u.active?"green":"off");
    document.getElementById("usbSub").textContent=(u.ifaces&&u.ifaces.length)?u.ifaces.join(", "):"";
    usbActive=!!u.active; renderUsbTetherBtn();
  }
  function renderUsbTetherBtn(){
    var b=document.getElementById("usbTetherBtn"); if(!b)return;
    b.textContent=usbActive?"Turn USB tethering off":"Turn USB tethering on";
    b.classList.toggle("danger",usbActive);
    b.classList.toggle("on",!usbActive);
  }

  // Hotspot toggle button reflects the current state: press turns it on when off,
  // off when on.
  var hsActive=false;
  function renderHotspotBtn(){
    var b=document.getElementById("hotspotOnBtn"); if(!b)return;
    b.textContent=hsActive?"Turn hotspot off":"Turn hotspot on";
    b.classList.toggle("danger",hsActive);
    b.classList.toggle("on",!hsActive);
  }

  // Read-only list of what's currently whitelisted, with a × to remove each.
  // Index-based handlers keep SSIDs (spaces, Vietnamese, quotes) out of markup.
  function renderWhitelist(wl){
    var box=document.getElementById("wlList");
    var html;
    if(!wl.length){html='<div class="stat-sub">Nothing whitelisted yet.</div>';}
    else{
      html='<div class="wlhead">Whitelisted ('+wl.length+')</div>';
      wl.forEach(function(s,i){
        html+='<div class="wlchip"><span class="wlname">'+esc(s)+'</span><button class="wlx" data-idx="'+i+'" aria-label="Remove '+esc(s)+' from whitelist">'+ICN_X+'</button></div>';
      });
    }
    if(!setListHTML(box,html))return;
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
    var html;
    if(!nearbyList.length){
      html='<div class="stat-sub">'+(h&&h.paused==="location_off"?"Turn on location services, then Scan.":"No scan yet — tap “Scan now”.")+'</div>';
    }else{
      html="";
      nearbyList.forEach(function(ap,i){
        var b=rssiBars(ap.rssi);
        html+='<button class="nrow'+(ap.whitelisted?" on":"")+'" data-idx="'+i+'" aria-pressed="'+(ap.whitelisted?"true":"false")+'">'
          +'<span class="nbars b'+b+'"><i></i><i></i><i></i><i></i></span>'
          +'<span class="nname">'+esc(ap.ssid)+'</span>'
          +'<span class="nchip">'+(ap.whitelisted?(ICN_CHECK+"whitelisted"):"+ add")+'</span></button>';
      });
    }
    if(!setListHTML(box,html))return;
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
  // Hardwired install: the daemon already embedded a working radio-control
  // token (open_control on) — no manual paste needed, so hide the field. It
  // reappears automatically if open_control is ever turned off and the
  // owner clears/loses the stored token.
  if(setTok.value){var tc=document.getElementById("tokCard");if(tc)tc.style.display="none";}
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

  // --- Inbox: messages + notifications. Both ride the "sms" scope, which
  // follows Open reads on this phone: with it on the daemon embeds the token and
  // serves tokenless, with it off this paste-once field is the way in.
  var smsTokEl=document.getElementById("setSmsTok");
  smsTokEl.value=lsGet("zf5smstok");
  // Token already seeded (open reads embedded it, or it was pasted once): the
  // field is just clutter until it's needed again.
  if(smsTokEl.value){var stc=document.getElementById("smsTokCard");if(stc)stc.style.display="none";}
  smsTokEl.addEventListener("change",function(){lsSet("zf5smstok",smsTokEl.value.trim());loadInbox(true);});
  document.getElementById("inboxBtn").addEventListener("click",function(){loadInbox(true);});

  function getPrivate(p){
    var t=smsTokEl.value.trim(), h={};
    if(t)h.Authorization="Bearer "+t;
    return fetch(API+p,{headers:h}).then(function(r){
      return r.json().then(function(j){return {ok:r.ok,code:r.status,j:j};},function(){return {ok:false,code:r.status,j:{}};});
    });
  }
  // §1.7: an empty or blocked list is a speech bubble with one sentence, never
  // a bare "no data" line.
  function bubble(box,text){setListHTML(box,'<div class="bub">'+esc(text)+"</div>");box.removeAttribute("aria-busy");}
  // A row only gets the chevron (and a pointer cursor) when its two-line clamp
  // is actually hiding something — otherwise tapping it would do nothing.
  function markExpandable(box){
    box.removeAttribute("aria-busy");
    box.querySelectorAll("details.msg").forEach(function(d){
      var b=d.querySelector(".msgbody");
      if(b&&b.scrollHeight-b.clientHeight>1)d.classList.add("can");
    });
  }
  // A transient failure (above all a 429 from the tiny sms budget) must not wipe
  // the messages you were mid-way through reading: keep the rows, put the
  // message in a note under them, and only take the list over when it's empty.
  function note(id,text){var el=document.getElementById(id);if(el)el.innerHTML=text?'<div class="bub">'+esc(text)+"</div>":"";}
  function failInto(box,noteId,text){
    if(box.querySelector("details.msg"))note(noteId,text);
    else bubble(box,text);
  }
  function privateErr(res){
    if(res.code===401||res.code===403){
      // The field hides itself once a token is stored; a rejected token is
      // exactly when it has to come back, or there is no way to fix it.
      var stc=document.getElementById("smsTokCard"); if(stc)stc.style.display="";
      return "The sms token was rejected — paste a working one below, or turn Open reads on in Settings.";
    }
    if(res.code===429){inboxAt=Date.now()+20000;return "Rate limit hit — the phone only serves a few private reads a minute. This list is still the last one it sent; Refresh works again in ~20s.";}
    return "Couldn't read that right now — "+((res.j&&res.j.error)||("HTTP "+res.code))+".";
  }
  function fmtWhen(ms){
    var n=+ms; if(!n)return "";
    var d=new Date(n<1e12?n*1000:n), hm=("0"+d.getHours()).slice(-2)+":"+("0"+d.getMinutes()).slice(-2);
    return d.toDateString()===new Date().toDateString()?hm:(d.getDate()+"/"+(d.getMonth()+1)+" "+hm);
  }
  // com.google.android.apps.messaging -> "messaging": the package tail is the
  // only app name available without querying the package manager.
  function appLabel(p){var s=String(p||"").split(".");return s[s.length-1]||"app";}

  var inboxAt=0, inboxBtn=document.getElementById("inboxBtn"), inboxPending=0;
  // Refresh has to say something happened even when the two lists come back
  // identical — otherwise a tap reads as a dead button.
  function inboxBusy(on){
    inboxPending+=on?1:-1;
    var busy=inboxPending>0;
    inboxBtn.disabled=busy;
    inboxBtn.textContent=busy?"Refreshing…":"Refresh";
  }
  function loadInbox(force){
    var now=Date.now();
    // sms_per_min is deliberately small (3 by default) and one load spends two
    // of it — don't burn the budget on tab-hopping.
    // inboxAt sits in the future after a 429, which holds off the forced path too.
    if(now<inboxAt)return;
    if(!force&&now-inboxAt<20000)return;
    if(inboxPending>0)return;
    inboxAt=now;
    var smsBox=document.getElementById("smsList"), ntBox=document.getElementById("notifList"), ntN=document.getElementById("notifN");
    inboxBusy(true); inboxBusy(true);

    getPrivate("/v1/sms/recent?limit=10").then(function(res){
      if(!res.ok)return failInto(smsBox,"smsNote",privateErr(res));
      if(res.j.available===false)return failInto(smsBox,"smsNote","No inbox access yet — the daemon needs READ_SMS granted, then refresh.");
      note("smsNote","");
      var ms=res.j.messages||[];
      if(!ms.length)return bubble(smsBox,"No messages yet. New ones show up here in full.");
      var h="";
      ms.forEach(function(m){
        h+='<details class="msg"><summary><div class="msghead"><span class="msgfrom">'+esc(m.address||"unknown")+'</span>'
          +'<span class="msgwhen">'+esc(fmtWhen(m.date))+'</span><span class="msgmore">'+ICN_CHEVRON+'</span></div>'
          +'<div class="msgbody">'+esc(m.body)+'</div></summary></details>';
      });
      if(setListHTML(smsBox,h))markExpandable(smsBox);
    }).catch(function(){failInto(smsBox,"smsNote","Couldn't reach the daemon.");}).then(function(){inboxBusy(false);});

    getPrivate("/v1/notifications/recent?limit=12").then(function(res){
      if(!res.ok)return failInto(ntBox,"notifNote",privateErr(res));
      if(res.j.available===false)return failInto(ntBox,"notifNote","Notifications need root — the daemon couldn't read the shade.");
      note("notifNote","");
      var ns=res.j.notifications||[];
      ntN.textContent=ns.length?(ns.length+" active"):"none";
      if(!ns.length)return bubble(ntBox,"Nothing on the shade right now.");
      var h="";
      ns.forEach(function(n){
        h+='<details class="msg"><summary><div class="msghead"><span class="msgfrom">'+esc(n.title||appLabel(n.pkg))+'</span>'
          +'<span class="msgwhen">'+esc(fmtWhen(n.when))+'</span><span class="msgmore">'+ICN_CHEVRON+'</span></div>'
          +(n.text?'<div class="msgbody">'+esc(n.text)+'</div>':"")
          +'<div class="msgapp">'+esc(appLabel(n.pkg))+'</div></summary></details>';
      });
      if(setListHTML(ntBox,h))markExpandable(ntBox);
    }).catch(function(){failInto(ntBox,"notifNote","Couldn't reach the daemon.");}).then(function(){inboxBusy(false);});
  }

  // --- Open reads toggle (radio-control).
  var orMsg=document.getElementById("orMsg");
  function setOpenReads(open){
    var rt=rtok(orMsg); if(!rt)return;
    orMsg.textContent="applying…";
    post("/v1/dashboard/open",{open:open},rt).then(function(res){
      if(!res.ok){orMsg.textContent="Error: "+(res.j.error||"failed")+(res.j.code===403?" (needs radio-control token)":"");return;}
      orMsg.textContent=res.j.open_reads?"Open reads ON — any tailnet device can view without a token, inbox included (read-only).":"Open reads OFF — a token or the QR is required.";
      renderOpenReads(res.j.open_reads);
    }).catch(function(e){orMsg.textContent="Error: "+e.message;});
  }
  document.getElementById("orOnBtn").addEventListener("click",function(){setOpenReads(true);});
  document.getElementById("orOffBtn").addEventListener("click",function(){setOpenReads(false);});
  function renderOpenReads(on){
    document.getElementById("orState").textContent=on?"on":"off";
    document.getElementById("orDot").className="dot "+(on?"amber":"off");
    document.getElementById("orAccent").style.background=on?"var(--amber)":"var(--ink-3)";
  }

  // --- Open control toggle (tokenless radio-control writes). radio-control gated
  // to change (need the token once to enable, since it starts off).
  var ocMsg=document.getElementById("ocMsg");
  function setOpenControl(open){
    var rt=rtok(ocMsg); if(!rt)return;
    ocMsg.textContent="applying…";
    post("/v1/dashboard/control",{open:open},rt).then(function(res){
      if(!res.ok){ocMsg.textContent="Error: "+(res.j.error||"failed")+(res.j.code===403?" (needs radio-control token)":"");return;}
      ocMsg.textContent=res.j.open_control?"Open control ON — writes on your tailnet need no token.":"Open control OFF — writes require the radio-control token.";
      renderOpenControl(res.j.open_control);
    }).catch(function(e){ocMsg.textContent="Error: "+e.message;});
  }
  document.getElementById("ocOnBtn").addEventListener("click",function(){setOpenControl(true);});
  document.getElementById("ocOffBtn").addEventListener("click",function(){setOpenControl(false);});
  function renderOpenControl(on){
    openControl=on;
    document.getElementById("ocState").textContent=on?"on":"off";
    document.getElementById("ocDot").className="dot "+(on?"red":"off");
    document.getElementById("ocAccent").style.background=on?"var(--red)":"var(--ink-3)";
  }

  // --- Secret admin unlock: 8 taps on the Home temp card. Locked by default;
  // survives in localStorage for this browser/kiosk.
  var adminUnlocked=localStorage.getItem("zf5admin")==="1";
  var adminTaps=0, adminTapUntil=0;
  document.getElementById("tempCard").addEventListener("click",function(){
    var now=Date.now();
    if(now>adminTapUntil)adminTaps=0;
    adminTaps++;adminTapUntil=now+5000;
    if(adminTaps>=8){adminUnlocked=true;localStorage.setItem("zf5admin","1");}
    renderAdmin();
  });
  function renderAdmin(){
    var el=document.getElementById("adminCard"); if(!el)return;
    el.style.display=adminUnlocked?"":"none";
  }

  // --- Bench mode (battery-less donor only). Hidden behind admin unlock.
  var bmMsg=document.getElementById("bmMsg");
  function renderBench(b){
    if(!b)return;
    document.getElementById("bmState").textContent=b.enabled?(b.tripped?"tripped":"on"):"off";
    document.getElementById("bmDot").className="dot "+(b.enabled&&!b.tripped?"red":(b.enabled?"amber":"off"));
    document.getElementById("bmZones").textContent=b.enabled?(b.zones_disabled||0)+"/95":"—";
    document.getElementById("bmAccent").style.background=(b.enabled&&!b.tripped)?"var(--red)":"var(--ink-3)";
  }
  function setBench(on){
    var rt=rtok(bmMsg); if(!rt)return;
    bmMsg.textContent="applying…";
    post("/v1/thermal/bench",{enabled:on},rt).then(function(res){
      if(!res.ok){bmMsg.textContent="Error: "+(res.j.error||"failed");return;}
      renderBench(res.j);
      bmMsg.textContent=res.j.enabled
        ?(res.j.tripped?"Bench tripped — mitigation restored; auto re-arms at ≤"+res.j.rearm_c+"°C.":"Bench ON — zones/HALs suspended, gate "+res.j.trip_c+"°C.")
        :"Bench OFF — stock mitigation restored.";
    }).catch(function(e){bmMsg.textContent="Error: "+e.message;});
  }
  document.getElementById("bmOnBtn").addEventListener("click",function(){setBench(true);});
  document.getElementById("bmOffBtn").addEventListener("click",function(){setBench(false);});

  // --- CPU policy mode. Hidden behind admin unlock.
  var cpuModeMsg=document.getElementById("cpuModeMsg");
  document.getElementById("cpuModeBtn").addEventListener("click",function(){
    var rt=rtok(cpuModeMsg); if(!rt)return;
    var mode=document.getElementById("cpuModeSel").value;
    cpuModeMsg.textContent="applying…";
    post("/v1/cpu/mode",{mode:mode},rt).then(function(res){
      if(!res.ok){cpuModeMsg.textContent="Error: "+(res.j.error||"failed");return;}
      cpuModeMsg.textContent="CPU mode: "+res.j.mode+" (persisted).";
    }).catch(function(e){cpuModeMsg.textContent="Error: "+e.message;});
  });

  // --- Admin danger actions: cooldown + reboot. Hidden behind admin unlock.
  var dangerMsg=document.getElementById("dangerMsg");
  document.getElementById("coolBtn").addEventListener("click",function(){
    var rt=rtok(dangerMsg); if(!rt)return;
    post("/v1/cooldown",{},rt).then(function(res){
      dangerMsg.textContent=res.ok?(res.j.note||"CPU forced to eco."):("Error: "+(res.j.error||"failed"));
    }).catch(function(e){dangerMsg.textContent="Error: "+e.message;});
  });
  var rebootBtn=document.getElementById("rebootBtn"), rebootArmed=0;
  rebootBtn.addEventListener("click",function(){
    var now=Date.now();
    if(now>rebootArmed){
      rebootArmed=now+5000; rebootBtn.textContent="Tap again to confirm reboot";
      return;
    }
    rebootBtn.textContent="Reboot device";
    var rt=rtok(dangerMsg); if(!rt)return;
    post("/v1/device/reboot",{},rt).then(function(res){
      dangerMsg.textContent=res.ok?(res.j.note||"Rebooting…"):("Error: "+(res.j.error||"failed"));
    }).catch(function(e){dangerMsg.textContent="Error: "+e.message;});
  });
  var usageResetBtn=document.getElementById("usageResetBtn"), usageResetArmed=0;
  usageResetBtn.addEventListener("click",function(){
    var now=Date.now();
    if(now>usageResetArmed){
      usageResetArmed=now+5000; usageResetBtn.textContent="Tap again to confirm reset";
      return;
    }
    usageResetBtn.textContent="Reset data usage";
    var rt=rtok(dangerMsg); if(!rt)return;
    post("/v1/usage/reset",{},rt).then(function(res){
      if(!res.ok){dangerMsg.textContent="Error: "+(res.j.error||"failed");return;}
      renderUsage(res.j); // limit/period/schedule are unchanged; only period_bytes/last_reset moved
      dangerMsg.textContent="Data usage reset — period starts now.";
    }).catch(function(e){dangerMsg.textContent="Error: "+e.message;});
  });

  // Cover-screen accent. The kiosk has no settings screen of its own (352px),
  // so its one appearance control lives here and rides /v1/status back down.
  var ACCENTS=["dawn","sunflower","coral","breeze","ocean","wisteria","slate"];
  var accentRow=document.getElementById("accentRow"), accentMsg=document.getElementById("accentMsg");
  ACCENTS.forEach(function(a){
    var b=document.createElement("button");
    b.type="button"; b.className="sw"; b.title=a;
    b.setAttribute("data-a",a);
    b.setAttribute("aria-label","Cover screen accent: "+a);
    b.style.background="linear-gradient(180deg,var(--"+a+"-start) 0%,var(--"+a+"-end) 100%)";
    b.addEventListener("click",function(){setAccent(a);});
    accentRow.appendChild(b);
  });
  function renderAccent(a){
    accentRow.querySelectorAll(".sw").forEach(function(b){
      var on=b.getAttribute("data-a")===a;
      b.classList.toggle("on",on);
      b.setAttribute("aria-pressed",on?"true":"false");
    });
  }
  function setAccent(a){
    var rt=rtok(accentMsg); if(!rt)return;
    accentMsg.textContent="applying…";
    post("/v1/cover/accent",{accent:a},rt).then(function(res){
      if(!res.ok){accentMsg.textContent="Error: "+(res.j.error||"failed");return;}
      renderAccent(a);
      accentMsg.textContent="Cover screen buttons are now "+a+".";
    }).catch(function(e){accentMsg.textContent="Error: "+e.message;});
  }

  // Cover home. The label follows the live state, so the one button reads as
  // whichever move is available rather than as a toggle you have to decode.
  var coverHomeBtn=document.getElementById("coverHomeBtn"), coverHomeMsg=document.getElementById("coverHomeMsg"), coverHomeState=true;
  function renderCoverHome(on){
    coverHomeState=on;
    coverHomeBtn.textContent=on?"Exit to cover screen":"Take over cover screen";
  }
  coverHomeBtn.addEventListener("click",function(){
    var rt=rtok(coverHomeMsg); if(!rt)return;
    var want=!coverHomeState;
    post("/v1/cover/home",{enabled:want},rt).then(function(res){
      if(!res.ok){coverHomeMsg.textContent="Error: "+(res.j.error||"failed");return;}
      renderCoverHome(want);
      coverHomeMsg.textContent=res.j.note||"";
    }).catch(function(e){coverHomeMsg.textContent="Error: "+e.message;});
  });
  renderAdmin();

  // First-run prompt: the ring's "Set a data limit" button just jumps to the
  // Settings tab where the Data limit card lives.
  document.getElementById("quotaSetBtn").addEventListener("click",function(){
    document.getElementById("tab-settings").click();
  });

  // Weekly always resets Monday (reset_day is monthly-only) and manual never
  // resets on a schedule (reset_time is meaningless there) — grey out the
  // field that doesn't apply to the picked period.
  function qSyncDisabled(){
    var p=document.getElementById("qPeriod").value;
    document.getElementById("qDay").disabled=p!=="monthly";
    document.getElementById("qTime").disabled=p==="manual";
  }
  document.getElementById("qPeriod").addEventListener("change",qSyncDisabled);

  var qMsg=document.getElementById("qMsg");
  document.getElementById("qBtn").addEventListener("click",function(){
    var rt=rtok(qMsg); if(!rt)return;
    var gb=parseFloat(document.getElementById("qLimit").value);
    var body={
      limit_bytes: isNaN(gb)||gb<=0 ? 0 : Math.round(gb*GB),
      period: document.getElementById("qPeriod").value,
      reset_time: document.getElementById("qTime").value||"00:00",
      reset_day: parseInt(document.getElementById("qDay").value,10)||1
    };
    qMsg.textContent="saving…";
    post("/v1/usage/quota",body,rt).then(function(res){
      if(!res.ok){qMsg.textContent="Error: "+(res.j.error||"failed");return;}
      quotaSeeded=false; // re-seed the form from the (possibly clamped) saved values
      renderUsage(res.j);
    }).catch(function(e){qMsg.textContent="Error: "+e.message;});
  });

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

  // Timed force-on override: pins the hotspot on for the chosen window
  // regardless of whitelist matches (persists across a daemon restart).
  var hsOvMsg=document.getElementById("hsOvMsg"), hsOvBtnList=document.querySelectorAll("#hsOvBtns .minibtn");
  hsOvBtnList.forEach(function(b){
    b.addEventListener("click",function(){
      var rt=rtok(hsOvMsg); if(!rt)return;
      var hours=+b.getAttribute("data-hours");
      hsOvMsg.textContent="forcing on for "+hours+"h…";
      hsOvBtnList.forEach(function(x){x.disabled=true;});
      post("/v1/hotspot/override",{hours:hours},rt).then(function(res){
        hsOvBtnList.forEach(function(x){x.disabled=false;});
        if(!res.ok){hsOvMsg.textContent="Error: "+(res.j.error||"failed");return;}
        hsOvMsg.textContent="Forced on for "+hours+"h.";
        renderHotspot(res.j);
      }).catch(function(e){hsOvBtnList.forEach(function(x){x.disabled=false;});hsOvMsg.textContent="Error: "+e.message;});
    });
  });
  document.getElementById("hsOvCancel").addEventListener("click",function(){
    var rt=rtok(hsOvMsg); if(!rt)return;
    var cb=document.getElementById("hsOvCancel"); cb.disabled=true;
    hsOvMsg.textContent="cancelling…";
    post("/v1/hotspot/override",{hours:0},rt).then(function(res){
      cb.disabled=false;
      if(!res.ok){hsOvMsg.textContent="Error: "+(res.j.error||"failed");return;}
      hsOvMsg.textContent="Forced-on cancelled.";
      renderHotspot(res.j);
    }).catch(function(e){cb.disabled=false;hsOvMsg.textContent="Error: "+e.message;});
  });

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
      tick(true); // refresh header IP + airplane state now, ignoring cadence
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
    ["spdDown","spdUp","spdPing"].forEach(function(id){var el=document.getElementById(id);el.textContent="…";el.classList.remove("has-value");});
    post("/v1/speedtest",{},rt).then(function(res){
      b.disabled=false;
      var j=res.j||{};
      if(!res.ok||!j.available){
        ["spdDown","spdUp","spdPing"].forEach(function(id){document.getElementById(id).textContent="—";});
        spdMsg.textContent="Speedtest failed: "+(j.error||("HTTP "+(res.j&&res.j.code||"?")));return;
      }
      function fmt(v){return (v==null||v<0)?"n/a":v;}
      function setStat(id,v){var el=document.getElementById(id);el.textContent=fmt(v);el.classList.add("has-value");}
      setStat("spdDown",j.download_mbps);
      setStat("spdUp",j.upload_mbps);
      setStat("spdPing",j.ping_ms);
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
      var msg=res.j.active?(ICN_WIFI+"hotspot on"):(stopping?"hotspot off":(ICN_WARN+"hotspot did not come up — retry"));
      // A manual start is undone by the auto-toggle within one hotspot-on tick
      // (≤3 min) if a whitelisted network is still in range — say so, and point
      // at the fix, instead of leaving the owner to rediscover it themselves.
      if(res.j.active&&!stopping&&lastHs&&lastHs.auto&&lastHs.matched&&lastHs.matched.length){
        msg+=" · auto-toggle will turn it off again within 3 min (whitelisted network in range) — use Force on to keep it";
      }
      apMsg.innerHTML=msg;
      tick(true);
    }).catch(function(e){apBtns.forEach(function(b){b.disabled=false;});hb.disabled=false;apMsg.textContent="Error: "+e.message;});
  });

  // USB tether toggle — turn it on when off, off when on (not thermal-gated).
  var usbMsg=document.getElementById("usbMsg");
  document.getElementById("usbTetherBtn").addEventListener("click",function(){
    var rt=rtok(usbMsg); if(!rt)return;
    var stopping=usbActive, action=stopping?"stop":"start", ub=document.getElementById("usbTetherBtn");
    usbMsg.textContent=stopping?"stopping USB tethering…":"starting USB tethering…"; ub.disabled=true;
    post("/v1/usbtether/toggle?action="+action,{},rt).then(function(res){
      ub.disabled=false;
      if(!res.ok){usbMsg.textContent="Error: "+(res.j.error||"failed");return;}
      renderUsbTether(res.j.status||{});
      usbMsg.innerHTML=usbActive?(ICN_USB+"USB tethering on"):(stopping?"USB tethering off":(ICN_WARN+"USB tethering did not come up — retry"));
    }).catch(function(e){ub.disabled=false;usbMsg.textContent="Error: "+e.message;});
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
    document.getElementById("paAccent").style.background=on?"var(--green)":"var(--ink-3)";
    document.getElementById("pCount").textContent=presetList.length?(presetList.length+"/12"):"";
    var box=document.getElementById("pList");
    var html;
    if(!presetList.length){html='<div class="stat-sub">No presets yet — create one below.</div>';}
    else{
      html="";
      presetList.forEach(function(p,i){
        var act=hp.active&&p.id===hp.active;
        html+='<div class="prow">'
          +'<button class="pmain" data-idx="'+i+'"><div class="pname">'+esc(p.name)+(act?'<span class="tag">active</span>':'')+'</div><div class="pmeta">'+presetMeta(p)+'</div></button>'
          +'<button class="minibtn papply" data-idx="'+i+'"'+(act?' disabled':'')+'>'+(act?'On':'Apply')+'</button>'
          +'<button class="wlx pdel" data-idx="'+i+'" aria-label="Delete preset">'+ICN_X+'</button>'
        +'</div>';
      });
    }
    if(!setListHTML(box,html))return;
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
    var html;
    if(!list.length){html='<div class="stat-sub">No presets — add them in the Presets tab.</div>';}
    else{
      html="";
      list.forEach(function(p,i){var o=p.id===act;
        html+='<button class="pchip hpq'+(o?" added":"")+'" data-idx="'+i+'"'+(o?' disabled':'')+'>'+(o?ICN_DOT:"")+esc(p.name)+'</button>';});
    }
    if(!setListHTML(box,html))return;
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
      tick(true);
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

  // Per-endpoint refresh cadence, not one fixed poll-everything tick: rows
  // that change on their own (status/signal/usage/hotspot/clients) stay LIVE
  // (5s); rows a background loop nudges every 30s+ (cpu) go MID (15s); rows
  // that never change on their own and are expensive on the phone (bands and
  // usbtether each cost a fresh dumpsys/ART-VM boot; history is a ~26KB read)
  // go SLOW (60s). "*" rows run on every tab (the header + Home cards).
  var LIVE=5000, MID=15000, SLOW=60000;
  var polls={
    "*":       [["/v1/status",renderStatus,LIVE],["/v1/signal",renderSignal,LIVE]],
    home:      [["/v1/usage",renderUsage,LIVE]],
    net:       [["/v1/hotspot",renderHotspot,LIVE],["/v1/bands",renderBands,SLOW],["/v1/usbtether",renderUsbTether,SLOW]],
    clientsScr:[["/v1/clients",renderClients,LIVE]],
    system:    [["/v1/cpu",renderCPU,MID],["/v1/thermal/history",function(d){thData=d;renderTempHist();},SLOW]],
    settings:  [["/v1/hotspot",renderHotspot,LIVE],["/v1/usage",renderUsage,MID]]
  };
  var inFlight=false, seq=0, gateSeeded=false, lastAt={}, holdUntil=0;
  function tick(force){
    // A backgrounded/minimized tab polls for nothing; visibilitychange below
    // fires a tick the instant it's shown again.
    if(document.hidden)return Promise.resolve();
    // A burst of 429s (shared bucket across dashboards + the kiosk) backs off
    // for 20s; the FAB (force=true) still gets through.
    if(!force&&Date.now()<holdUntil)return Promise.resolve();
    // No early return without a token: try anyway. If "open reads" is on, the
    // daemon serves reads tokenless; only a real 401 means we need the token.
    if(inFlight)return Promise.resolve(); // don't stack overlapping ticks on a slow daemon
    inFlight=true;
    var mine=++seq, activeAtStart=active, now=Date.now();
    function due(row){return force||!lastAt[row[0]]||now-lastAt[row[0]]>=row[2]-250;}
    var star=polls["*"].filter(due), tab=(polls[activeAtStart]||[]).filter(due);
    var rows=star.concat(tab);
    if(!rows.length){inFlight=false;return Promise.resolve();}
    return Promise.allSettled(rows.map(function(row){return get(row[0]);})).then(function(rs){
      inFlight=false;
      if(mine!==seq)return; // a newer tick finished first; don't overwrite it
      var stOk=false, rateLimited=false;
      rs.forEach(function(r,i){
        var row=rows[i], isStar=i<star.length;
        if(r.status==="fulfilled"){
          // Mark handled (and render) only when this row's owner tab is still
          // showing — if the tab changed mid-flight, leave it unmarked so
          // reopening that tab refetches instead of showing a stale card.
          // "*" rows (header/Home cards) always render/mark.
          if(isStar||activeAtStart===active){row[1](r.value);lastAt[row[0]]=now;}
          if(row[0]==="/v1/status")stOk=true;
        }else{
          lastAt[row[0]]=now; // handled (failed); retry follows its normal cadence
          var m=r.reason&&r.reason.message||"";
          if(/ 429/.test(m))rateLimited=true;
          if(row[0]==="/v1/status"){
            if(/ 401/.test(m))showErr("Needs a token. Scan the “Add a device” QR in Settings, open with ?token=…, or turn on Open reads.");
            else showErr("Live data unavailable — "+(m||"status failed"));
          }
        }
      });
      if(stOk)clearErr();
      if(rateLimited){holdUntil=Date.now()+20000;showErr("Rate limited — retrying in 20 s.");}
      // Seed the adjust inputs once from live limits; don't clobber a field the
      // owner is editing on later ticks.
      if(!gateSeeded&&stOk){document.getElementById("setGate").value=GATE_C;document.getElementById("setWarn").value=WARN_C;gateSeeded=true;}
    }).catch(function(){inFlight=false;});
  }
  tick();setInterval(tick,LIVE);
  document.addEventListener("visibilitychange",function(){if(!document.hidden)tick();});

  // Deep link: /dashboard#system (etc.) opens that page directly. Runs after
  // everything above is wired (selectScreen kicks a tick, which reads the
  // cadence table), and selectScreen keeps the hash in sync afterwards — so a
  // refresh, or pasting the URL into another browser on the tailnet, lands on
  // the page that was meant.
  (function(){
    var h=location.hash.replace("#","");
    if(!h||h===active)return;
    var el=document.getElementById(h);
    if(!el)return;
    var i=screens.indexOf(el);
    if(i<0)return;
    deck.scrollLeft=i*deck.clientWidth;
    selectScreen(h,true);
  })();

  // Refresh now. Usage rides along even off the Home tab (its elements are in
  // the DOM either way), so one press lands live usage, battery and IP. Forces
  // every row of the open tab regardless of cadence (on Net that's bands +
  // usbtether too) and bypasses the 429 hold.
  var fab=document.getElementById("refreshFab");
  fab.addEventListener("click",function(){
    var t0=new Date().getTime();
    fab.disabled=true;fab.classList.add("spin");
    Promise.allSettled([tick(true),get("/v1/usage").then(renderUsage)]).then(function(){
      // Hold the spinner long enough to read as an action even on a fast reply.
      setTimeout(function(){fab.disabled=false;fab.classList.remove("spin");},Math.max(0,450-(new Date().getTime()-t0)));
    });
  });

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
