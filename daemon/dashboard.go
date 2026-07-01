package main

import "net/http"

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

// dashboardHTML is a self-contained, dependency-free page. It reads the bearer
// token from the URL (?token=) into localStorage (then strips it), polls the
// read endpoints, and renders battery/temp/CPU/RAM/data-usage/network/policy.
// Sized for the Z Flip 5 cover screen (compact, dark, auto-refresh).
const dashboardHTML = `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Z Flip 5 Modem</title>
<style>
:root{color-scheme:dark}
*{box-sizing:border-box;margin:0;font-family:-apple-system,system-ui,sans-serif}
body{background:#0b0d10;color:#e8edf2;padding:10px;font-size:14px}
h1{font-size:15px;font-weight:600;margin-bottom:8px;display:flex;justify-content:space-between;align-items:center}
.dot{width:9px;height:9px;border-radius:50%;display:inline-block;margin-right:5px}
.ok{background:#39d353}.bad{background:#f85149}.warn{background:#e3b341}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:8px}
.card{background:#161b22;border:1px solid #21262d;border-radius:10px;padding:10px}
.k{font-size:11px;color:#8b949e;text-transform:uppercase;letter-spacing:.04em}
.v{font-size:20px;font-weight:600;margin-top:2px}
.sub{font-size:11px;color:#8b949e;margin-top:2px}
.wide{grid-column:1/3}
.bar{height:6px;background:#21262d;border-radius:3px;overflow:hidden;margin-top:6px}
.bar>i{display:block;height:100%;background:#2f81f7}
.u{display:flex;justify-content:space-between;font-size:12px;margin-top:3px}
#err{color:#f85149;font-size:12px;margin-top:6px;min-height:14px}
.t{font-size:11px;color:#8b949e;text-align:center;margin-top:8px}
</style></head><body>
<h1><span>📶 Z Flip 5 Modem</span><span id="net" class="sub">—</span></h1>
<div class="grid">
  <div class="card"><div class="k">Battery</div><div class="v" id="bat">—</div><div class="sub" id="batx">—</div></div>
  <div class="card"><div class="k">Temperature</div><div class="v" id="temp">—</div><div class="sub" id="tempx">—</div></div>
  <div class="card"><div class="k">CPU load</div><div class="v" id="cpu">—</div><div class="sub" id="cpux">—</div></div>
  <div class="card"><div class="k">Memory</div><div class="v" id="mem">—</div><div class="bar"><i id="membar"></i></div></div>
  <div class="card wide"><div class="k">Mobile data</div>
    <div class="u"><span>Today</span><b id="uToday">—</b></div>
    <div class="u"><span>This week</span><b id="uWeek">—</b></div>
    <div class="u"><span>This month</span><b id="uMonth">—</b></div>
  </div>
  <div class="card wide"><div class="k">State</div>
    <div class="u"><span><span id="dPol" class="dot warn"></span>Thermal policy</span><b id="pol">—</b></div>
    <div class="u"><span><span id="dIng" class="dot warn"></span>Ingress</span><b id="ing">—</b></div>
  </div>
</div>
<div id="err"></div>
<div class="t" id="ts">connecting…</div>
<script>
(function(){
  var u=new URL(location.href), t=u.searchParams.get('token');
  if(t){localStorage.setItem('zf5tok',t);history.replaceState({},'',location.pathname);}
  var TOK=localStorage.getItem('zf5tok')||'';
  function g(p){return fetch(p,{headers:{Authorization:'Bearer '+TOK}}).then(function(r){if(!r.ok)throw new Error(p+' '+r.status);return r.json();});}
  function set(id,v){var e=document.getElementById(id);if(e)e.textContent=v;}
  function refresh(){
    if(!TOK){document.getElementById('err').textContent='No token. Open with ?token=YOUR_READ_STATUS_TOKEN once.';return;}
    Promise.all([g('/v1/status'),g('/v1/usage')]).then(function(a){
      var s=a[0],us=a[1];
      var b=s.battery||{}, h=s.health||{}, th=s.thermal||{}, n=s.network||{};
      set('bat',(b.level!=null?b.level+'%':'—'));
      set('batx',(b.plugged||'')+' · '+(b.temp_c!=null?b.temp_c+'°C':''));
      set('temp',(th.temp_max_c!=null?th.temp_max_c+'°C':'—'));
      set('tempx','battery '+(th.battery_c!=null?th.battery_c+'°C':'—')+(th.safe?' · safe':' · UNSAFE'));
      set('cpu',(h.cpu_load1!=null?h.cpu_load1:'—'));
      set('cpux',(h.cpu_cores||'?')+' cores · 5m '+(h.cpu_load5!=null?h.cpu_load5:'—'));
      set('mem',(h.mem_used_pct!=null?h.mem_used_pct+'%':'—'));
      var mb=document.getElementById('membar'); if(mb&&h.mem_used_pct!=null)mb.style.width=h.mem_used_pct+'%';
      set('uToday',us.today_human||'—'); set('uWeek',us.week_human||'—'); set('uMonth',us.month_human||'—');
      set('pol',s.policy_state||'—');
      set('net',(n.type||'—')+(n.override&&n.override!=n.type?'/'+n.override:'')+(n.nr_state&&n.nr_state!='NONE'?' 5G':''));
      var dp=document.getElementById('dPol'); dp.className='dot '+(s.policy_state=='SAFE'?'ok':(s.policy_state=='HOT'||s.policy_state=='COOLDOWN'?'bad':'warn'));
      document.getElementById('err').textContent='';
      document.getElementById('ts').textContent='updated '+new Date().toLocaleTimeString();
    }).catch(function(e){document.getElementById('err').textContent=e.message;});
    g('/v1/status').then(function(s){}).catch(function(){});
  }
  set('ing', 'loopback/tailscale'); document.getElementById('dIng').className='dot ok';
  refresh(); setInterval(refresh, 5000);
})();
</script>
</body></html>`
