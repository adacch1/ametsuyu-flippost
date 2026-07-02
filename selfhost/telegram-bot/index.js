// Self-hosted Telegram bot for the Z Flip 5 modem daemon.
//
// Long-polling (getUpdates) — outbound only, no public inbound, same posture as
// the Discord Gateway bot it replaces. Talks to the daemon over Tailscale with
// scope-appropriate tokens (reads = READ_STATUS, writes = RADIO_CONTROL). SMS is
// refused here (no token; owner-only over the iPhone/Tailscale path).
//
// Adds over the Discord bot: /airplane on|off|cycle (quick radio toggle + IP
// rotate), /ip, /hotspot, /scan, /cpu, /reboot (whole device), and a scheduled
// auto-reboot. Edge-triggered thermal + data-cap alerts are pushed straight to
// the owner chat (and optionally still mirrored to ntfy).
//
// No SDK: plain Bot API over fetch (Node 18+ global fetch).

const {
  TELEGRAM_TOKEN,
  OWNER_CHAT_IDS = '',
  DAEMON_BASE_URL,
  READ_STATUS,
  RADIO_CONTROL,
  NTFY_URL,
  NTFY_TOPIC,
  NTFY_TOKEN,
  DATA_CAP_GB = '512',
  AUTOREBOOT_HHMM = '', // e.g. "04:30" local server time; empty = disabled
} = process.env;

if (!TELEGRAM_TOKEN || !DAEMON_BASE_URL || !READ_STATUS) {
  console.error('Set TELEGRAM_TOKEN, DAEMON_BASE_URL, READ_STATUS (see .env.example)');
  process.exit(1);
}

const owners = new Set(OWNER_CHAT_IDS.split(',').map((s) => s.trim()).filter(Boolean));
const CAP_BYTES = Number(DATA_CAP_GB) * 1024 ** 3;
const API = `https://api.telegram.org/bot${TELEGRAM_TOKEN}`;

// ---- daemon calls -----------------------------------------------------------

// Command table. read = READ_STATUS; radio = RADIO_CONTROL. body is sent as-is
// on POST (airplane needs {"mode":...}). Thermal gating is enforced by the DAEMON.
const CMDS = {
  status:   { path: '/v1/status',        method: 'GET',  scope: 'read' },
  signal:   { path: '/v1/signal',        method: 'GET',  scope: 'read' },
  thermal:  { path: '/v1/thermal',       method: 'GET',  scope: 'read' },
  battery:  { path: '/v1/battery',       method: 'GET',  scope: 'read' },
  clients:  { path: '/v1/clients',       method: 'GET',  scope: 'read' },
  bands:    { path: '/v1/bands',         method: 'GET',  scope: 'read' },
  usage:    { path: '/v1/usage',         method: 'GET',  scope: 'read' },
  network:  { path: '/v1/network',       method: 'GET',  scope: 'read' },
  cpu:      { path: '/v1/cpu',           method: 'GET',  scope: 'read' },
  hotspot:  { path: '/v1/hotspot',       method: 'GET',  scope: 'read' },
  ip:       { path: '/v1/status',        method: 'GET',  scope: 'read' },  // formatted below
  scan:     { path: '/v1/hotspot/scan',  method: 'POST', scope: 'radio' },
  prefer5g: { path: '/v1/prefer5g',      method: 'POST', scope: 'radio' },
  cooldown: { path: '/v1/cooldown',      method: 'POST', scope: 'radio' },
};

const tokenFor = (scope) => (scope === 'radio' ? RADIO_CONTROL : READ_STATUS);

// radio-control write with an explicit path (query strings, POST bodies).
const CMDS_WRITE = (path) => ({ path, method: 'POST', scope: 'radio' });

async function callDaemon({ path, method, scope }, body) {
  const res = await fetch(DAEMON_BASE_URL + path, {
    method,
    headers: { Authorization: `Bearer ${tokenFor(scope)}`, 'Content-Type': 'application/json' },
    body: method === 'POST' ? JSON.stringify(body || {}) : undefined,
    // A wedged phone can accept the TCP connection but never answer; without a
    // timeout the fetch hangs forever, leaking sockets and defeating the
    // unreachable-device alert. Airplane cycle can take ~30s, so allow 60s.
    signal: AbortSignal.timeout(60000),
  });
  return { status: res.status, json: await res.json().catch(() => ({})) };
}

// ---- Telegram helpers -------------------------------------------------------

async function tg(method, payload) {
  const res = await fetch(`${API}/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  return res.json();
}

function send(chatId, text, extra = {}) {
  return tg('sendMessage', { chat_id: chatId, text, parse_mode: 'HTML', ...extra });
}

const esc = (s) => String(s).replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
// Truncate the RAW json first, then escape, so slicing can't cut a mid-entity
// (e.g. "&am") and 400 the Telegram message.
const code = (obj) => `<pre>${esc(JSON.stringify(obj, null, 2).slice(0, 3500))}</pre>`;

// Text progress bar (10 cells). Renders the same in every Telegram client.
function bar(pct) {
  const n = 10;
  const p = Math.max(0, Math.min(100, Number(pct) || 0));
  const f = Math.round((p / 100) * n);
  return '▰'.repeat(f) + '▱'.repeat(n - f);
}
const battIcon = (lvl, plugged) =>
  (plugged === 'ac' || plugged === 'usb') ? '🔌' : lvl <= 15 ? '🪫' : '🔋';

// ---- formatters (compact human summaries for the common commands) -----------

const fmt = {
  // /status — the full dashboard as text: signal, IP, battery (with bar), thermal,
  // CPU, data meter (with bar), hotspot + clients. Aggregates several endpoints.
  statusFull(j, u, s, cl, hs) {
    const t = j.thermal || {}, b = j.battery || {}, n = j.network || {}, ip = j.wan_ip || {}, h = j.health || {};
    const battPct = Math.round(b.level ?? 0);
    const capPct = u.month_bytes && CAP_BYTES ? Math.min(100, Math.round((100 * u.month_bytes) / CAP_BYTES)) : 0;
    const L = [];
    L.push('📊 <b>Z Flip 5 — status</b>');
    L.push(`📶 <b>${esc(n.display || n.type || '—')}</b>${n.operator ? ' · ' + esc(n.operator) : ''}${j.airplane ? ' ✈️ airplane' : ''}`);
    if (s && s.available) L.push(`📡 RSRP ${esc(s.rsrp_dbm)} dBm · SINR ${esc(s.sinr_db)} dB · B${esc(s.band ?? '?')}${s.nr_state && s.nr_state !== 'NONE' ? ' · NR ' + esc(s.nr_state) : ''}`);
    L.push(`🌐 IP <code>${esc(ip.ip || (j.airplane ? 'airplane' : 'no data'))}</code>`);
    L.push(`${battIcon(battPct, b.plugged)} Battery ${battPct}% · ${esc(b.temp_c ?? '—')}°C ${esc(b.plugged || '')}`);
    L.push(`   ${bar(battPct)} ${battPct}%`);
    L.push(`🌡 Temp ${esc(t.temp_max_c ?? '—')}°C · policy <b>${esc(j.policy_state || '—')}</b>${t.safe === false ? ' ⚠️' : ''}`);
    L.push(`💻 CPU load ${esc(h.cpu_load5 ?? '—')} · 🧠 mem ${esc(h.mem_used_pct ?? '—')}%`);
    L.push(`📈 Data ${esc(u.month_human || '—')} / ${DATA_CAP_GB} GB (${capPct}%)`);
    L.push(`   ${bar(capPct)} ${capPct}%`);
    L.push(`   📅 today ${esc(u.today_human || '—')} · week ${esc(u.week_human || '—')}`);
    L.push(`📡 Hotspot <b>${hs.active ? 'on' : 'off'}</b> · 👥 ${esc(cl.count ?? 0)} client(s)${hs.auto ? ' · auto(' + (hs.whitelist || []).length + ')' : ''}${hs.paused ? ' · ⏸ ' + esc(hs.paused) : ''}`);
    return L.join('\n');
  },
  ip(j) {
    const ip = j.wan_ip || {};
    return j.airplane ? '✈️ airplane on — no data' : `🌐 WAN IP <code>${esc(ip.ip || 'no data')}</code> (${esc(ip.iface || '?')})`;
  },
  signal(j) {
    if (!j.available) return 'signal unavailable';
    return [
      `📡 <b>${esc(j.display || j.tech)}</b> ${esc(j.operator || '')}`,
      `Band B${j.band ?? '?'} · RSRP ${j.rsrp_dbm} dBm · SINR ${j.sinr_db} dB`,
      j.nr_state && j.nr_state !== 'NONE' ? `NR ${esc(j.nr_state)}` : '',
    ].filter(Boolean).join('\n');
  },
  clients(j) {
    const c = j.clients || [];
    if (!c.length) return '👥 no clients';
    return `👥 <b>${c.length}</b> client(s):\n` + c.map((x) => `• <code>${esc(x.ipv4 || '?')}</code> ${esc(x.state || '')}`).join('\n');
  },
  usage(j) {
    return `📊 ${esc(j.month_human || '?')} of ${DATA_CAP_GB} GB this month\nToday ${esc(j.today_human || '?')} · Week ${esc(j.week_human || '?')}`;
  },
  hotspot(j) {
    return `📶 hotspot <b>${j.active ? 'on' : 'off'}</b>` + (j.auto ? ` · auto (${(j.whitelist || []).length} SSID)` : '') + (j.paused ? ` · paused: ${esc(j.paused)}` : '');
  },
};

// ---- command handling -------------------------------------------------------

const HELP = [
  '<b>Z Flip 5 modem</b>',
  '/status /ip /signal /battery /thermal /clients /usage /network /cpu /bands /hotspot',
  '/scan — rescan nearby Wi-Fi',
  '/airplane on|off|cycle — radio toggle / rotate IP',
  '/tether start|stop — data hotspot',
  '/cooldown — force CPU eco',
  '/prefer5g — report allowed types',
  '/reboot — reboot the phone',
  '/raw &lt;cmd&gt; — full JSON of any read',
].join('\n');

async function handle(chatId, text) {
  const [rawCmd, ...args] = text.trim().replace(/^\//, '').split(/\s+/);
  const cmd = (rawCmd || '').toLowerCase().split('@')[0]; // strip /cmd@botname in groups
  const arg = (args[0] || '').toLowerCase();
  const lookup = (key) => (Object.hasOwn(CMDS, key) ? CMDS[key] : null); // no prototype keys

  if (cmd === 'start' || cmd === 'help') return send(chatId, HELP);
  if (cmd === 'sms') return send(chatId, 'SMS is not available over Telegram. Use the iPhone/Tailscale path.');

  // /status: the full picture from several endpoints. allSettled so one slow/
  // failed sub-read still renders the rest (status itself must succeed).
  if (cmd === 'status') {
    const [rst, rus, rsg, rcl, rhs] = await Promise.allSettled([
      callDaemon(CMDS.status, {}), callDaemon(CMDS.usage, {}), callDaemon(CMDS.signal, {}),
      callDaemon(CMDS.clients, {}), callDaemon(CMDS.hotspot, {}),
    ]);
    const okj = (r) => (r.status === 'fulfilled' && r.value.status === 200 ? r.value.json : {});
    if (rst.status !== 'fulfilled' || rst.value.status !== 200) {
      return send(chatId, `daemon unreachable${rst.status === 'fulfilled' ? ` (HTTP ${rst.value.status})` : ''}`);
    }
    return send(chatId, fmt.statusFull(rst.value.json, okj(rus), okj(rsg), okj(rcl), okj(rhs)));
  }

  // Airplane: quick on/off switch + IP-rotation cycle.
  if (cmd === 'airplane') {
    const mode = ['on', 'off', 'cycle'].includes(arg) ? arg : null;
    if (!mode) return send(chatId, 'Usage: /airplane on | off | cycle');
    await send(chatId, mode === 'cycle' ? '✈️ rotating IP… (~20s, clients drop briefly)' : `✈️ airplane ${mode}…`);
    const { status, json } = await callDaemon(CMDS_WRITE('/v1/airplane'), { mode });
    if (status !== 200) return send(chatId, `daemon returned ${status}`);
    if (mode === 'cycle') {
      const line = json.changed ? `✅ IP changed: <code>${esc(json.old_ip)}</code> → <code>${esc(json.new_ip)}</code>`
        : json.data_back ? `↔️ IP unchanged (<code>${esc(json.new_ip || '?')}</code>) — carrier reused it`
        : '⚠️ data did not come back';
      return send(chatId, line + (json.hotspot_active ? '\n📶 hotspot back up' : `\n⚠️ ${esc(json.note || 'hotspot not up')}`));
    }
    return send(chatId, `✈️ airplane ${json.airplane ? 'ON' : 'off'}` + (json.hotspot_active ? ' · hotspot up' : (json.note ? ` · ${esc(json.note)}` : '')));
  }

  if (cmd === 'tether') {
    const action = arg === 'stop' ? 'stop' : 'start';
    const { status, json } = await callDaemon(CMDS_WRITE(`/v1/tether?action=${action}`), {});
    return send(chatId, status !== 200 ? `daemon returned ${status}` : `📶 tether ${action}: ${json.applied ? 'ok' : 'failed'} · hotspot ${json.active ? 'on' : 'off'}`);
  }

  if (cmd === 'reboot') {
    const { status } = await callDaemon(CMDS_WRITE('/v1/device/reboot'), {});
    return send(chatId, status === 200 ? '🔄 rebooting the phone — back in ~60s.' : `daemon returned ${status}`);
  }

  if (cmd === 'raw') {
    const c = lookup(arg);
    if (!c) return send(chatId, `Unknown: ${esc(arg)}`);
    const { status, json } = await callDaemon(c, {});
    return send(chatId, status === 200 ? code(json) : `daemon returned ${status}`);
  }

  const c = lookup(cmd);
  if (!c) return send(chatId, `Unknown command. /help`);
  const { status, json } = await callDaemon(c, {});
  if (status !== 200) return send(chatId, `daemon returned ${status}`);
  return send(chatId, fmt[cmd] ? fmt[cmd](json) : code(json));
}

// ---- long-poll loop ---------------------------------------------------------

let offset = 0;
async function poll() {
  try {
    const res = await fetch(`${API}/getUpdates?timeout=50&offset=${offset}`, { signal: AbortSignal.timeout(60000) });
    const data = await res.json();
    for (const u of data.result || []) {
      offset = u.update_id + 1;
      const msg = u.message;
      if (!msg || !msg.text) continue;
      const chatId = String(msg.chat.id);
      // Fail closed: with no owners configured the bot runs NO commands (it can
      // reboot the phone), but still tells you your chat id so you can set
      // OWNER_CHAT_IDS. Non-owners get the same id hint, never command access.
      if (!owners.size) {
        await send(chatId, `Bot is unconfigured. Your chat id is <code>${chatId}</code> — add it to OWNER_CHAT_IDS and restart.`);
        continue;
      }
      if (!owners.has(chatId)) {
        await send(chatId, `Not authorized. Your chat id is <code>${chatId}</code>.`);
        continue;
      }
      handle(chatId, msg.text).catch((e) => send(chatId, `error: ${esc(e.message)}`).catch(() => {}));
    }
  } catch (e) {
    if (e.name !== 'TimeoutError') console.error('poll:', e.message);
  }
  setTimeout(poll, 500);
}

// ---- edge-triggered alerts + scheduled auto-reboot --------------------------

function broadcast(text) {
  for (const id of owners) send(id, text).catch(() => {});
  publishNtfy(text);
}
async function publishNtfy(message) {
  if (!NTFY_URL || !NTFY_TOPIC) return;
  const headers = {};
  if (NTFY_TOKEN) headers.Authorization = `Bearer ${NTFY_TOKEN}`;
  try { await fetch(`${NTFY_URL}/${NTFY_TOPIC}`, { method: 'POST', headers, body: message }); } catch { /* ignore */ }
}

let lastSafe = true, lastCapNear = false, lastReachable = true, unreachableStreak = 0;
async function pollAlerts() {
  try {
    const r = await callDaemon(CMDS.status, {});
    if (r.status !== 200) throw new Error(`status HTTP ${r.status}`); // 401/500 = not healthy
    const st = r.json;
    if (!lastReachable) { broadcast('✅ Modem daemon reachable again.'); lastReachable = true; }
    unreachableStreak = 0;
    const safe = st.thermal?.safe !== false && st.policy_state !== 'HOT' && st.policy_state !== 'COOLDOWN';
    if (!safe && lastSafe) broadcast(`🔥 Modem hot: policy ${esc(st.policy_state)}, ${esc(st.thermal?.temp_max_c)}°C`);
    else if (safe && !lastSafe) broadcast('✅ Modem cooled.');
    lastSafe = safe;

    const ru = await callDaemon(CMDS.usage, {});
    const us = ru.status === 200 ? ru.json : {};
    const near = us.month_bytes > 0.9 * CAP_BYTES;
    if (near && !lastCapNear) broadcast(`📊 Data cap near: ${esc(us.month_human)} of ${DATA_CAP_GB} GB used.`);
    lastCapNear = near;
  } catch (e) {
    unreachableStreak++;
    if (unreachableStreak >= 3 && lastReachable) { // ~3 min unreachable
      lastReachable = false;
      broadcast('⚠️ Modem daemon unreachable for ~3 min (device may be wedged).');
    }
  }
}

// Scheduled daily auto-reboot at AUTOREBOOT_HHMM (server local time). Checked
// once a minute; fires once when the minute matches.
let lastRebootMinute = '';
async function checkAutoReboot() {
  // Reject impossible times (e.g. 27:80) so a typo doesn't silently disable it.
  if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(AUTOREBOOT_HHMM)) return;
  const now = new Date();
  const hhmm = String(now.getHours()).padStart(2, '0') + ':' + String(now.getMinutes()).padStart(2, '0');
  if (hhmm === AUTOREBOOT_HHMM && lastRebootMinute !== hhmm) {
    lastRebootMinute = hhmm;
    broadcast(`🔄 Scheduled auto-reboot (${AUTOREBOOT_HHMM}).`);
    await callDaemon(CMDS_WRITE('/v1/device/reboot'), {}).catch(() => {});
  } else if (hhmm !== AUTOREBOOT_HHMM) {
    lastRebootMinute = '';
  }
}

// ---- boot -------------------------------------------------------------------

(async () => {
  // Retry boot so a transient network blip at startup doesn't crash the process.
  // drop_pending_updates: don't replay commands (e.g. a queued /reboot) that
  // piled up while the bot was down — a control surface must not act on stale input.
  for (;;) {
    try {
      await tg('deleteWebhook', { drop_pending_updates: true });
      const me = await tg('getMe', {});
      if (!owners.size) console.warn('WARNING: OWNER_CHAT_IDS is empty — bot runs no commands until set.');
      console.log(`bot ready: @${me.result?.username}; owners=${owners.size}; autoreboot=${AUTOREBOOT_HHMM || 'off'}`);
      break;
    } catch (e) {
      console.error('boot retry in 5s:', e.message);
      await new Promise((r) => setTimeout(r, 5000));
    }
  }
  poll();
  pollAlerts();
  setInterval(pollAlerts, 60_000);
  setInterval(checkAutoReboot, 60_000);
})();
