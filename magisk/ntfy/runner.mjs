// Device-local ntfy alert runner for the Z Flip 5 modem daemon.
//
// Reads the daemon on loopback with the read-status token and posts
// edge-triggered alerts to one or more ntfy servers. launch.cjs sets the
// environment; nothing here writes to the daemon.

import { detect, plain, DEFAULTS } from './alerts.mjs';

const {
  DAEMON_BASE_URL,
  READ_STATUS,
  NTFY_URL,
  NTFY_TOPIC,
  NTFY_TOKEN,
  BATTERY_LOW_PCT = '20',
  DATA_CAP_GB = '0',
} = process.env;

if (!DAEMON_BASE_URL || !READ_STATUS || !NTFY_URL || !NTFY_TOPIC || !NTFY_TOKEN) {
  console.error('Set DAEMON_BASE_URL, READ_STATUS, NTFY_URL, NTFY_TOPIC and NTFY_TOKEN');
  process.exit(1);
}

const CAP_BYTES = Number(DATA_CAP_GB) * 1e9; // decimal GB, matching the daemon's *_human formatting

// ---- daemon calls -----------------------------------------------------------

async function callDaemon(path) {
  const res = await fetch(DAEMON_BASE_URL + path, {
    headers: { Authorization: `Bearer ${READ_STATUS}` },
    // A wedged phone can accept the TCP connection but never answer; without a
    // timeout the fetch hangs forever and defeats the unreachable-device alert.
    signal: AbortSignal.timeout(60000),
  });
  return { status: res.status, json: await res.json().catch(() => ({})) };
}
const getOk = async (path) => {
  const r = await callDaemon(path);
  return r.status === 200 ? r.json : undefined;
};

// ---- ntfy -------------------------------------------------------------------

// NTFY_URL may list several servers (comma-separated); each gets the same
// message. NTFY_TOKEN is one token for all, or one per URL in the same order.
const ntfyUrls = NTFY_URL.split(',').map((s) => s.trim().replace(/\/+$/, '')).filter(Boolean);
const ntfyTokens = NTFY_TOKEN.split(',').map((s) => s.trim());
function publishNtfy(message) {
  const body = plain(message);
  return Promise.all(ntfyUrls.map((url, i) => {
    const token = ntfyTokens.length > 1 ? ntfyTokens[i] : ntfyTokens[0];
    const headers = token ? { Authorization: `Bearer ${token}` } : {};
    // Timeout so a down homelab can't pile up hung sockets.
    return fetch(`${url}/${NTFY_TOPIC}`, { method: 'POST', headers, body, signal: AbortSignal.timeout(15000) })
      .then((r) => { if (!r.ok) throw new Error(`HTTP ${r.status}`); console.log(`ntfy ${url}: accepted`); return true; })
      .catch((e) => { console.error(`ntfy ${url}:`, e.message); return false; });
  }));
}

// ---- alerts -----------------------------------------------------------------

// Rules live in alerts.mjs (pure, unit-tested). Each source is fetched
// independently: one failing leaves that rule's state untouched.
const alertCfg = { ...DEFAULTS, batteryLow: Number(BATTERY_LOW_PCT), capBytes: CAP_BYTES };
let alertState = null, lastReachable = true, unreachableStreak = 0;

async function pollAlerts() {
  let status;
  try { status = await getOk('/v1/status'); } catch { /* counted below */ }
  if (!status) {
    unreachableStreak++;
    if (unreachableStreak >= 3 && lastReachable) { // ~3 min unreachable
      lastReachable = false;
      publishNtfy('⚠️ Modem daemon unreachable for ~3 min (device may be wedged).');
    }
    return;
  }
  if (!lastReachable) { publishNtfy('✅ Modem daemon reachable again.'); lastReachable = true; }
  unreachableStreak = 0;
  const [usage, summary, hotspot] = await Promise.all([
    getOk('/v1/usage'), getOk('/v1/summary'), getOk('/v1/hotspot'),
  ].map((p) => Promise.resolve(p).catch(() => undefined)));
  // The 3 s power loop owns battery, so it is left out here: two detectors
  // would each announce the same power change.
  const r = detect(alertState, { status, usage, hotspot, signal: summary?.signal ?? null }, alertCfg);
  alertState = r.state;
  r.messages.forEach(publishNtfy);
}

// Battery-only polling cannot be delayed by the slower routes.
async function pollPowerLoop() {
  let previous = null;
  for (;;) {
    try {
      const summary = await getOk('/v1/summary');
      const battery = summary?.battery;
      if (battery?.available !== false && battery) {
        if (previous?.plugged !== battery.plugged) {
          console.log(`${new Date().toISOString()} power: ${previous?.plugged ?? 'baseline'} -> ${battery.plugged}`);
        }
        const r = detect(previous, { battery }, alertCfg);
        previous = r.state;
        r.messages.forEach(publishNtfy);
      }
    } catch (e) { console.error('power poll:', e.message); }
    await new Promise((resolve) => setTimeout(resolve, 3000));
  }
}

// ---- boot -------------------------------------------------------------------

if (process.argv.includes('--test-notification')) {
  const results = await publishNtfy('Z Flip 5 modem: device-local ntfy integration test.');
  process.exit(results.every(Boolean) ? 0 : 1);
}
console.log('ntfy alerts ready; power checks every 3s');
pollPowerLoop();
// One poll at a time, even when a daemon route is slow.
for (;;) {
  await pollAlerts().catch((e) => console.error('alert poll:', e.message));
  await new Promise((r) => setTimeout(r, 60_000));
}
