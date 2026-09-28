package main

import (
	"net/http"
	"strings"
)

// coverAccents is the closed set the cover screen may be painted with — the
// seven named accent gradients of the design system. The chosen name is
// substituted into the served HTML, so nothing outside this list is ever
// accepted (see handleCoverAccent).
var coverAccents = []string{"dawn", "sunflower", "coral", "breeze", "ocean", "wisteria", "slate"}

func validCoverAccent(a string) bool {
	for _, c := range coverAccents {
		if c == a {
			return true
		}
	}
	return false
}

// normalizeCoverAccent falls back to sunflower for an empty or hand-edited
// value, so a bad config.json repaints the kiosk rather than breaking it.
func normalizeCoverAccent(a string) string {
	if validCoverAccent(a) {
		return a
	}
	return "sunflower"
}

// handleCover serves the cover-screen page, and is also the catch-all: the
// kiosk WebView loads "/" with no path of its own, so the cover page lives
// there and the full control panel moved to "/dashboard". Every other
// unmatched path is still a 404 (ServeMux routes them here).
func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	// Never cache the HTML: the WebView would otherwise keep serving the page
	// from before a daemon update.
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	accent, _ := s.coverAccent.Load().(string)
	html := strings.Replace(coverHTML, "COVER_ACCENT", normalizeCoverAccent(accent), 1)
	_, _ = w.Write([]byte(s.htmlWithTokens(html)))
}

// coverHTML is the Flex Window page: clock, data usage, battery, WAN IP, and
// the three actions the owner runs from the cover screen (hotspot, IP rotate,
// refresh). Deliberately NOT the control panel — the panel's seven tabs do not
// fit 352x308 without becoming a scroll hunt, and everything it drops is one
// tap away at /dashboard.
//
// Styled in the same "midnight glass" system as the dashboard, with one
// OLED-specific override: the ground is true black (#000) because this panel
// is lit for as long as the phone is closed, and black pixels cost nothing —
// so no aurora washes and no backdrop blur here, just hairline glass. Token
// values are the dashboard's shared block and are asserted identical by
// TestCoverTokensMatchDashboard, so the two pages cannot drift.
const coverHTML = `<!DOCTYPE html>
<html lang="en" data-accent="COVER_ACCENT">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Flippost</title>
<meta name="theme-color" content="#1b2436">
<link rel="icon" href="/icon.svg">
<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Nunito:wght@500;600;700;800&display=swap">
<style>
  :root{
    /* surfaces: icy-hair glass over the Flippost rain-sky ground */
    --ground:#1b2436;
    --surface:rgba(213,231,253,.07);
    --surface-inset:rgba(14,20,33,.45);
    --line:rgba(213,231,253,.15);
    --line-soft:rgba(213,231,253,.08);

    /* ink: three levels */
    --ink:#fef7ee;
    --ink-2:#c9d7ea;
    --ink-3:#9eb1cb;

    /* the accent pair (hair → clip blue; names kept from the old teal skin) +
       the pastel status set + the CPU lane + the logo's blush */
    --teal:#a9cdfb;
    --teal-2:#6fa4f0;
    --green:#9ee6c3;
    --amber:#ffd49a;
    --red:#ff9aa6;
    --violet:#c4b5ff;
    --blush:#fbd3d0;
    --track:rgba(213,231,253,.12); /* unfilled rings and bars */
    --on-teal:#1b2436;             /* label colour on the accent fill */
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
    --radius-sm:12px; --radius-md:22px; --radius-lg:30px; --radius-pill:999px;
    --radius-nav:26px; --radius-btn:999px;
    --tabbar-h:58px;

    /* one rounded sans doing every job — roles separate by weight and size */
    --font-display:"Nunito",ui-rounded,"SF Pro Rounded",system-ui,-apple-system,Roboto,sans-serif;
    --font-body:"Nunito",ui-rounded,"SF Pro Rounded",system-ui,-apple-system,Roboto,sans-serif;

    --smooth:cubic-bezier(0.4,0,0.2,1);
    --pop:cubic-bezier(0.34,1.56,0.64,1);
  }
  /* accent-filled controls bind through --primary-fill/--on-primary so the
     three accents that can't carry dark text fall back to glass. */
  :root{--primary-fill:var(--accent-grad);--on-primary:var(--on-teal)}
  /* One accent for the whole page, bound once, and picked by the owner from
     the control panel (POST /v1/cover/accent) — the kiosk has no room for a
     settings screen of its own. Picking coral costs the over-cap bar its
     warning meaning; that is the owner's call to make. */
  [data-accent="dawn"]     {--primary-fill:linear-gradient(135deg,var(--dawn-start),var(--dawn-end))}
  [data-accent="sunflower"]{--primary-fill:linear-gradient(135deg,var(--sunflower-start),var(--sunflower-end))}
  [data-accent="coral"]    {--primary-fill:linear-gradient(135deg,var(--coral-start),var(--coral-end))}
  [data-accent="breeze"]   {--primary-fill:linear-gradient(135deg,var(--breeze-start),var(--breeze-end))}
  [data-accent="ocean"]    {--primary-fill:linear-gradient(135deg,var(--ocean-start),var(--ocean-end))}
  [data-accent="wisteria"] {--primary-fill:linear-gradient(135deg,var(--wisteria-start),var(--wisteria-end))}
  [data-accent="slate"]    {--primary-fill:linear-gradient(135deg,var(--slate-start),var(--slate-end))}
  /* ocean, wisteria and slate don't clear AA with dark ink, so accent-filled
     controls fall back to the glass surface treatment on those accents. */
  [data-accent="ocean"],[data-accent="wisteria"],[data-accent="slate"]{--primary-fill:linear-gradient(180deg,rgba(255,255,255,.09),rgba(255,255,255,.04));--on-primary:var(--ink)}

  *{box-sizing:border-box;margin:0;padding:0}
  button{border:0;background:none;color:inherit;font:inherit;cursor:pointer;-webkit-appearance:none;appearance:none;-webkit-tap-highlight-color:transparent}
  a{text-decoration:none;color:inherit}
  /* The deck is sized to the panel and the burn-in shift moves it a few pixels;
     without this that shift would raise scrollbars on a screen that has nothing
     to scroll to. */
  html,body{height:100%;overflow:hidden}
  /* Rainy dusk, kept dim for the always-on OLED panel: deep slate with a
     faint static drizzle (low enough that burn-in has nothing to hold). */
  body{
    background:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='64' height='64'%3E%3Cg stroke='%23d5e7fd' stroke-opacity='.07' stroke-width='1.3' stroke-linecap='round'%3E%3Cpath d='M13 4l-3 9M42 17l-3 9M27 38l-3 9M56 47l-3 9'/%3E%3C/g%3E%3C/svg%3E"),linear-gradient(180deg,#222d43 0%,#141b29 100%);
    color:var(--ink);
    font-family:var(--font-body); font-size:13.5px; line-height:1.35; letter-spacing:normal;
    -webkit-font-smoothing:antialiased;
  }
  .num{font-variant-numeric:tabular-nums}
  /* transition, not a jump: the burn-in shift below moves the whole deck a few
     pixels each minute and a slow slide reads as nothing at all. */
  .wrap{min-height:100%;max-width:440px;margin:0 auto;padding:var(--space-sm);display:flex;flex-direction:column;gap:var(--space-xs);transition:transform 1.2s var(--smooth)}

  /* Header: the clock is the page title, the way the cover clock it replaced
     was. Gradient numerals, same as the dashboard's clock. */
  .hdr{display:flex;align-items:center;gap:var(--space-sm)}
  .hmain{flex:1;min-width:0}
  .ava{flex:none;width:34px;height:34px;border-radius:50%;object-fit:cover;border:2px solid var(--hair,#d5e7fd);box-shadow:0 0 0 3px color-mix(in srgb,var(--blush) 35%,transparent)}
  .time{font-family:var(--font-display);font-weight:700;font-size:24px;line-height:1;letter-spacing:-.02em;background:var(--accent-grad);-webkit-background-clip:text;background-clip:text;color:transparent}
  .hsub{margin-top:2px;font-size:11px;color:var(--ink-3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .badge{flex:none;display:inline-flex;align-items:center;gap:5px;padding:3px 10px;border-radius:var(--radius-pill);background:var(--surface-inset);border:1px solid var(--line);font-family:var(--font-display);font-size:13px;font-weight:700;color:var(--ink)}
  .dot{width:6px;height:6px;border-radius:50%;background:var(--track)}
  .dot.green{background:var(--green);box-shadow:0 0 8px color-mix(in srgb,var(--green) 55%,transparent)}
  .dot.amber{background:var(--amber);box-shadow:0 0 8px color-mix(in srgb,var(--amber) 55%,transparent)}
  .dot.red{background:var(--red);box-shadow:0 0 8px color-mix(in srgb,var(--red) 55%,transparent)}
  .iconbtn{width:30px;height:30px;flex:none;border-radius:10px;background:var(--surface-inset);border:1px solid var(--line);display:grid;place-items:center;color:var(--ink-3)}
  .iconbtn svg{width:15px;height:15px}
  .iconbtn:focus-visible{outline:2px solid var(--teal);outline-offset:2px}

  .card{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-md);padding:var(--space-xs) var(--space-md);box-shadow:inset 0 1px 0 rgba(213,231,253,.08)}
  .lbl{font-size:10px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:var(--ink-3)}

  /* Usage: one bar carrying the period against the cap, with today and the
     remainder on the caption row. */
  .urow{display:flex;align-items:baseline;gap:var(--space-sm)}
  .urow .lbl{flex:1}
  .uval{font-family:var(--font-display);font-weight:700;font-size:15px}
  .upct{font-size:12px;color:var(--ink-3)}
  .well{height:9px;margin:6px 0;border-radius:var(--radius-pill);background:var(--track);overflow:hidden}
  .well i{position:relative;display:block;height:100%;width:0;border-radius:var(--radius-pill);background:var(--primary-fill);transition:width .4s var(--smooth)}
  .well i::after{content:"";position:absolute;inset:0;background:repeating-linear-gradient(45deg,rgba(27,36,54,.16) 0 4px,transparent 4px 9px)}
  .ufoot{display:flex;justify-content:space-between;gap:var(--space-sm);font-size:11px;color:var(--ink-3)}
  .ufoot b{font-weight:600;color:var(--ink-2)}

  .duo{display:grid;grid-template-columns:1fr 1fr;gap:var(--space-xs)}
  .gauge{display:flex;flex-direction:column;align-items:center;gap:var(--space-xs);padding:var(--space-xs) var(--space-sm)}
  .ring{position:relative;width:64px;height:64px}
  .ring svg{width:100%;height:100%;transform:rotate(-90deg)}
  .ring .tick{fill:none;stroke:rgba(255,255,255,.14);stroke-width:1;stroke-dasharray:1 4}
  .ring .trk{fill:none;stroke:var(--track);stroke-width:7}
  .ring .fil{fill:none;stroke:url(#bGrad);stroke-width:7;stroke-linecap:round;transition:stroke-dashoffset .4s var(--smooth),stroke .4s var(--smooth)}
  .rcen{position:absolute;inset:0;display:grid;place-content:center;text-align:center}
  .rcen b{font-family:var(--font-display);font-weight:700;font-size:22px;line-height:1}
  .chip{max-width:100%;padding:2px 8px;border-radius:var(--radius-pill);background:var(--surface-inset);border:1px solid var(--line-soft);font-size:10px;color:var(--ink-3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}

  .ipc{display:flex;flex-direction:column;align-items:flex-start;gap:var(--space-xs);padding:var(--space-xs) var(--space-sm)}
  .ipv{font-family:var(--font-display);font-weight:700;font-size:16px;line-height:1.15;word-break:break-all}
  /* The network tech is what gets read at arm's length, so it is the one thing
     on this card that carries the accent fill rather than a muted chip. */
  .iprow{display:flex;align-items:center;gap:var(--space-xs);width:100%;min-width:0}
  .tech{flex:none;padding:2px 9px;border-radius:var(--radius-pill);background:var(--primary-fill);color:var(--on-primary);font-family:var(--font-display);font-weight:700;font-size:14px;line-height:1.3}
  .iface{min-width:0;font-size:10px;color:var(--ink-3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}

  .btn{display:inline-flex;align-items:center;justify-content:center;gap:3px;padding:10px 4px;border-radius:var(--radius-btn);font-size:11.5px;font-weight:800;white-space:nowrap;letter-spacing:-.01em;transition:transform .14s var(--pop),filter .14s var(--smooth)}
  .btn.pri{background:var(--primary-fill);color:var(--on-primary)}
  .btn.sec{background:var(--surface-inset);border:1px solid var(--line);color:var(--ink)}
  .btn:active{transform:scale(.97);filter:brightness(1.1)}
  .btn:disabled{opacity:.55}
  .btn:focus-visible{outline:2px solid var(--teal);outline-offset:2px}
  .btn svg{width:14px;height:14px}
  .btn.spin svg{animation:acSpin .9s linear infinite}
  @keyframes acSpin{to{transform:rotate(360deg)}}
  .acts{display:grid;grid-template-columns:repeat(4,1fr);gap:var(--space-xs)}
  .msg{min-height:13px;padding:0 2px;font-size:11px;color:var(--ink-3)}
  .msg.err{color:var(--red)}
  @media (prefers-reduced-motion:reduce){
    *,*::before,*::after{transition-duration:.01ms!important;animation-duration:.01ms!important}
  }
</style>
</head>
<body>
<!--EMBEDDED_TOKENS-->
<div class="wrap">
  <header class="hdr">
    <img class="ava" src="/logo.png" alt="">
    <div class="hmain">
      <div class="time num" id="cTime">--:--</div>
      <div class="hsub" id="cDate">&mdash;</div>
    </div>
    <span class="badge"><i class="dot" id="cDot"></i><span class="num" id="cTemp">&mdash;</span></span>
    <a class="iconbtn" href="/dashboard" aria-label="Open the full control panel" title="Full control panel"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></svg></a>
  </header>

  <section class="card">
    <div class="urow">
      <span class="lbl" id="uLbl">Mobile data &middot; cycle</span>
      <b class="uval num" id="uMonth">&mdash;</b>
      <span class="upct num" id="uPct">&mdash;</span>
    </div>
    <div class="well"><i id="uFill"></i></div>
    <div class="ufoot">
      <span>Today <b class="num" id="uToday">&mdash;</b></span>
      <span id="uLeftWrap"><b class="num" id="uLeft">&mdash;</b> left</span>
    </div>
  </section>

  <div class="duo">
    <section class="card gauge">
      <span class="lbl">Battery</span>
      <div class="ring">
        <svg viewBox="0 0 100 100" aria-hidden="true">
          <defs>
            <linearGradient id="bGrad" x1="0" y1="0" x2="100" y2="100" gradientUnits="userSpaceOnUse">
              <stop offset="0" stop-color="#d5e7fd"/><stop offset="1" stop-color="#6fa4f0"/>
            </linearGradient>
          </defs>
          <circle class="tick" cx="50" cy="50" r="45"/>
          <circle class="trk" cx="50" cy="50" r="38"/>
          <circle class="fil" id="bFill" cx="50" cy="50" r="38" stroke-dasharray="238.8" stroke-dashoffset="238.8"/>
        </svg>
        <div class="rcen"><b class="num" id="bLvl">&mdash;</b></div>
      </div>
      <span class="chip" id="bSub">&mdash;</span>
    </section>
    <section class="card ipc">
      <span class="lbl">WAN IP</span>
      <div class="ipv" id="cIp">&mdash;</div>
      <div class="iprow"><span class="tech" id="cTech">&mdash;</span><span class="iface" id="cIface">&mdash;</span></div>
    </section>
  </div>

  <div class="acts">
    <button class="btn sec" id="hsBtn" type="button">Hotspot</button>
    <button class="btn sec" id="dualBtn" type="button">Dual band</button>
    <button class="btn sec" id="rotBtn" type="button">Rotate IP</button>
    <button class="btn sec" id="refBtn" type="button"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 0 1 15.36-6.36L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-15.36 6.36L3 16"/><path d="M3 21v-5h5"/></svg>Refresh</button>
  </div>
  <p class="msg" id="cMsg"></p>
</div>

<script>
(function(){
  "use strict";
  // Same-origin so the page works on the kiosk's 127.0.0.1:18080, on a custom
  // bind_port, and proxied over Tailscale.
  var API="", GB=1e9, CAP=0, CIRC=2*Math.PI*38;

  var token="";
  try{
    var url=new URL(window.location.href), t=url.searchParams.get("token"), rt=url.searchParams.get("rtoken");
    if(t)localStorage.setItem("zf5tok",t);
    if(rt)localStorage.setItem("zf5rtok",rt);
    if(t||rt){url.searchParams.delete("token");url.searchParams.delete("rtoken");window.history.replaceState({},"",url.pathname);}
    token=localStorage.getItem("zf5tok")||"";
  }catch(e){}

  function $(id){return document.getElementById(id);}
  function ls(k){try{return localStorage.getItem(k)||"";}catch(e){return "";}}
  function get(p){var h={};if(token)h.Authorization="Bearer "+token;return fetch(API+p,{headers:h}).then(function(r){if(!r.ok)throw new Error(p+" "+r.status);return r.json();});}

  var msgEl=$("cMsg"), errShown=false;
  function msg(s,isErr){msgEl.textContent=s;msgEl.className="msg"+(isErr?" err":"");errShown=!!isErr;}

  // Writes need the radio-control token. The kiosk gets it seeded by action.sh
  // (-e rtoken), and an open-control install needs none; anything else is sent
  // to the full panel rather than growing a token field on a 352px screen.
  var openControl=false, OPEN="__open__";
  function rtok(){
    var rt=ls("zf5rtok");
    if(rt)return rt;
    if(openControl)return OPEN;
    msg("Needs the radio-control token — open the full panel once.",true);
    return null;
  }
  function post(p,body,rt){
    var h={"Content-Type":"application/json"};
    if(rt&&rt!==OPEN)h.Authorization="Bearer "+rt;
    return fetch(API+p,{method:"POST",headers:h,body:JSON.stringify(body)})
      .then(function(r){return r.json().then(function(j){return {ok:r.ok,j:j};});});
  }

  var DOW=["Sunday","Monday","Tuesday","Wednesday","Thursday","Friday","Saturday"];
  var MON=["January","February","March","April","May","June","July","August","September","October","November","December"];
  function p2(n){return (n<10?"0":"")+n;}
  // Burn-in shift. This panel is now always on with a layout that never moves,
  // so the deck walks a few pixels every minute the way Samsung's own AOD does.
  // The two cycles have different lengths (5 and 4), so the pattern takes 20
  // minutes to repeat instead of tracing the same short diagonal.
  var SHIFT_X=[0,2,4,2,0], SHIFT_Y=[0,2,4,2], wrapEl=document.querySelector(".wrap");
  var lastDay=-1;
  function tickClock(){
    var d=new Date();
    $("cTime").textContent=p2(d.getHours())+":"+p2(d.getMinutes());
    var m=d.getHours()*60+d.getMinutes();
    wrapEl.style.transform="translate("+SHIFT_X[m%5]+"px,"+SHIFT_Y[m%4]+"px)";
    if(lastDay!==d.getDate()){
      lastDay=d.getDate();
      $("cDate").textContent=DOW[d.getDay()]+", "+d.getDate()+" "+MON[d.getMonth()]+" "+d.getFullYear();
    }
  }
  // One wake-up a minute, aligned to the minute boundary. With seconds gone
  // there is nothing to repaint in between, and this page is on screen for as
  // long as the phone is closed.
  function scheduleClock(){
    var d=new Date();
    setTimeout(function(){tickClock();scheduleClock();},(60-d.getSeconds())*1000-d.getMilliseconds()+50);
  }
  tickClock(); scheduleClock();

  function fmtBytes(b){var gb=b/GB;if(gb>=1000)return (gb/1000).toFixed(2)+" TB";return (gb>=10?Math.round(gb):gb.toFixed(1))+" GB";}

  function renderStatus(s){
    var bat=s.battery||{}, ip=s.wan_ip||{}, net=s.network||{};
    openControl=!!s.open_control;
    // The owner repaints this page from the control panel; the served HTML is
    // already stamped with the accent, so this only catches a change made while
    // the kiosk is up.
    if(s.cover_accent)document.documentElement.setAttribute("data-accent",s.cover_accent);

    // Temperature against the LIVE thresholds: /v1/status already reports the
    // effective warn/gate, so bench mode's 70° ceiling colours this correctly
    // without the page knowing bench mode exists.
    var th=s.thermal||{}, t=th.temp_max_c!=null&&th.temp_max_c>0?th.temp_max_c:th.battery_c;
    if(t==null||!(t>0)){
      $("cTemp").textContent="—";
      $("cDot").className="dot";
    }else{
      var warn=th.warn_c||44, gate=th.gate_c||46;
      $("cTemp").textContent=t.toFixed(1)+"°";
      $("cDot").className="dot "+(t>=gate?"red":(t>=warn?"amber":"green"));
    }

    var fil=$("bFill");
    if(bat.available===false||bat.level==null){
      $("bLvl").textContent="—"; $("bSub").textContent="unavailable";
      fil.style.stroke="var(--ink-3)"; fil.style.strokeDashoffset=CIRC.toFixed(1);
    }else{
      var lvl=Math.round(bat.level);
      $("bLvl").textContent=lvl;
      // healthy battery rides the gradient; amber/red take over when it runs low
      fil.style.stroke=lvl<=10?"var(--red)":(lvl<=20?"var(--amber)":"url(#bGrad)");
      fil.style.strokeDashoffset=(CIRC*(1-Math.min(1,Math.max(0,lvl)/100))).toFixed(1);
      $("bSub").textContent=(bat.plugged||"")+(bat.temp_c!=null?" · "+bat.temp_c.toFixed(1)+"°":"");
    }

    $("cIp").textContent=s.airplane?"airplane":(ip.available&&ip.ip?ip.ip:"no data");
    $("cTech").textContent=net.display||net.type||"—";
    $("cIface").textContent=ip.iface||net.operator||"";
  }

  var U_LBL={daily:"Mobile data · today",weekly:"Mobile data · week",monthly:"Mobile data · cycle",manual:"Mobile data · since reset"};
  function renderUsage(u){
    CAP=u.limit_bytes||0;
    var used=u.period_bytes||0, f=$("uFill");
    $("uLbl").textContent=U_LBL[u.period]||U_LBL.monthly;
    if(CAP>0){
      var pct=used/CAP, pc=Math.min(1,pct);
      $("uPct").textContent=Math.round(pct*100)+"%";
      f.style.width=(pc*100).toFixed(1)+"%";
      // Over cap is the one place coral appears on this page (hard rule 13);
      // otherwise the bar follows the bound accent like every other fill.
      f.style.background=pct>0.9?"linear-gradient(180deg,var(--coral-start) 0%,var(--coral-end) 100%)":"var(--primary-fill)";
      $("uLeftWrap").innerHTML='<b class="num">'+fmtBytes(Math.max(0,CAP-used))+"</b> left";
    }else{
      $("uPct").textContent="—";
      f.style.width="0%";
      $("uLeftWrap").textContent="no limit";
    }
    // today_bytes is raw bytes; formatting it here (instead of using the
    // daemon's today_human) keeps every number in this card in the same GB
    // unit as the ring, computed from the same CAP/used values.
    $("uMonth").textContent=fmtBytes(used);
    $("uToday").textContent=fmtBytes(u.today_bytes);
  }

  var hsOn=false, dualOn=false;
  function renderHotspot(h){
    hsOn=!!h.active;
    var b=$("hsBtn");
    b.textContent=hsOn?"Hotspot on":"Hotspot off";
    b.className="btn "+(hsOn?"pri":"sec");
    dualOn=!!h.dual;
    $("dualBtn").className="btn "+(dualOn?"pri":"sec");
  }

  // 10s, not the panel's 5s: this page is on screen whenever the cover panel is
  // lit, so it pays the poll cost far more often than the panel does.
  var inFlight=false;
  function tick(){
    if(inFlight)return Promise.resolve();
    inFlight=true;
    return Promise.allSettled([get("/v1/status"),get("/v1/usage"),get("/v1/hotspot")]).then(function(rs){
      inFlight=false;
      if(rs[0].status==="fulfilled"){
        renderStatus(rs[0].value);
        if(errShown)msg("");
      }else{
        var m=(rs[0].reason&&rs[0].reason.message)||"status failed";
        msg(/ 401/.test(m)?"Needs a token — open the full panel to pair this device.":("Live data unavailable — "+m),true);
      }
      if(rs[1].status==="fulfilled")renderUsage(rs[1].value);
      if(rs[2].status==="fulfilled")renderHotspot(rs[2].value);
    }).catch(function(){inFlight=false;});
  }

  var actBtns=[$("rotBtn"),$("hsBtn"),$("dualBtn"),$("refBtn")];
  function busy(on){actBtns.forEach(function(b){b.disabled=on;});}

  // The cycle blocks ~15-30s (radio drop + PDP re-attach + hotspot restart).
  $("rotBtn").addEventListener("click",function(){
    var rt=rtok(); if(!rt)return;
    busy(true); msg("rotating IP… ~20s, clients drop briefly");
    post("/v1/airplane",{mode:"cycle"},rt).then(function(res){
      busy(false);
      if(!res.ok){msg("Error: "+(res.j.error||"failed"),true);return;}
      var j=res.j;
      msg(j.changed?("IP changed → "+j.new_ip)
        :(j.data_back?("IP unchanged ("+(j.new_ip||"?")+") — carrier reused it")
        :"data did not come back — check the connection"));
      tick();
    }).catch(function(e){busy(false);msg("Error: "+e.message,true);});
  });

  $("hsBtn").addEventListener("click",function(){
    var rt=rtok(); if(!rt)return;
    var stopping=hsOn, action=stopping?"stop":"start";
    busy(true); msg(stopping?"stopping hotspot…":"starting hotspot…");
    post("/v1/tether?action="+action,{},rt).then(function(res){
      busy(false);
      if(!res.ok){msg("Error: "+(res.j.error||"failed"),true);return;}
      renderHotspot(res.j);
      msg(res.j.active?"hotspot on":(stopping?"hotspot off":"hotspot did not come up — retry"));
    }).catch(function(e){busy(false);msg("Error: "+e.message,true);});
  });

  // Dual band: one SSID on 2.4 + 5GHz. Pauses the whitelist auto-toggle
  // (the phone can't scan beside two APs); tapping again goes back to 5GHz.
  $("dualBtn").addEventListener("click",function(){
    var rt=rtok(); if(!rt)return;
    var band=dualOn?"5":"dual";
    busy(true); msg(band==="dual"?"switching to 2.4 + 5 GHz… clients drop briefly":"switching to 5 GHz… clients drop briefly");
    post("/v1/hotspot/band",{band:band},rt).then(function(res){
      busy(false);
      if(!res.ok){msg("Error: "+(res.j.error||"failed"),true);return;}
      renderHotspot(res.j);
      msg(!res.j.active?"band saved — applies when the hotspot starts":(res.j.dual?"dual band on":"5 GHz only"));
    }).catch(function(){
      // Viewed over the hotspot itself, the restart drops this very request
      // though the switch lands. Re-read the real state once it's back.
      msg("reconnecting…");
      setTimeout(function(){
        get("/v1/hotspot").then(function(h){
          busy(false); renderHotspot(h);
          msg(h.dual===(band==="dual")?(h.dual?"dual band on":"5 GHz only"):"switch failed — retry",h.dual!==(band==="dual"));
        }).catch(function(e){busy(false);msg("Error: "+e.message,true);});
      },12000);
    });
  });

  // Refresh now. The poll is fine for watching, but after unlocking the panel
  // the owner wants this second's numbers, not the last tick's.
  $("refBtn").addEventListener("click",function(){
    var b=$("refBtn"), t0=new Date().getTime();
    b.classList.add("spin"); busy(true); msg("refreshing…");
    tick().then(function(){
      // Hold the spinner long enough to read as an action even on a fast reply.
      setTimeout(function(){
        b.classList.remove("spin"); busy(false);
        if(!errShown)msg("updated "+p2(new Date().getHours())+":"+p2(new Date().getMinutes())+":"+p2(new Date().getSeconds()));
      },Math.max(0,450-(new Date().getTime()-t0)));
    });
  });

  tick(); setInterval(tick,10000);

  // No service worker here either: tear down any the old page at this path
  // registered, so the kiosk always runs the freshly-served HTML.
  if("serviceWorker" in navigator){
    navigator.serviceWorker.getRegistrations().then(function(rs){rs.forEach(function(r){r.unregister();});}).catch(function(){});
    if(window.caches&&caches.keys){caches.keys().then(function(ks){ks.forEach(function(k){caches.delete(k);});}).catch(function(){});}
  }
})();
</script>
</body>
</html>`
