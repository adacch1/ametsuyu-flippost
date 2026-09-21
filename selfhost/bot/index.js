// Self-hosted Discord Gateway bot for the Z Flip 5 modem daemon.
//
// - Registers nothing here (run `node register.js` once); this process is the
//   long-lived Gateway connection (outbound WebSocket — no public inbound).
// - /modem <sub> -> calls the daemon over Tailscale with a scope-appropriate
//   token. Reads use READ_STATUS; tether/prefer5g/cooldown use RADIO_CONTROL.
//   SMS is refused (no token, locked decision). Thermal gating is enforced by
//   the DAEMON, not here.
// - Alert poller: watches thermal + monthly data cap, publishes to self-hosted
//   ntfy on state change (edge-triggered, no spam).
// - ntfy -> Discord bridge: streams the ntfy topic and mirrors alerts into a
//   Discord channel, so one alert reaches phones (ntfy app) and Discord.
import { Client, GatewayIntentBits, Events } from 'discord.js';

const {
  DISCORD_TOKEN,
  OWNER_USER_IDS = '',
  DAEMON_BASE_URL,
  READ_STATUS,
  RADIO_CONTROL,
  NTFY_URL,
  NTFY_TOPIC,
  NTFY_TOKEN,
  ALERT_CHANNEL_ID,
  DATA_CAP_GB = '512',
} = process.env;

const owners = new Set(OWNER_USER_IDS.split(',').map((s) => s.trim()).filter(Boolean));
const CAP_BYTES = Number(DATA_CAP_GB) * 1e9; // decimal GB, matching the daemon's *_human formatting

// Route table mirrors the daemon's read/write scopes. sms is intentionally absent.
const ROUTES = {
  status:   { path: '/v1/status',   method: 'GET',  scope: 'read',  ephemeral: false },
  thermal:  { path: '/v1/thermal',  method: 'GET',  scope: 'read',  ephemeral: false },
  battery:  { path: '/v1/battery',  method: 'GET',  scope: 'read',  ephemeral: false },
  signal:   { path: '/v1/signal',   method: 'GET',  scope: 'read',  ephemeral: false },
  clients:  { path: '/v1/clients',  method: 'GET',  scope: 'read',  ephemeral: false },
  bands:    { path: '/v1/bands',    method: 'GET',  scope: 'read',  ephemeral: false },
  usage:    { path: '/v1/usage',    method: 'GET',  scope: 'read',  ephemeral: false },
  network:  { path: '/v1/network',  method: 'GET',  scope: 'read',  ephemeral: true },
  prefer5g: { path: '/v1/prefer5g', method: 'POST', scope: 'radio', ephemeral: true },
  tether:   { path: '/v1/tether',   method: 'POST', scope: 'radio', ephemeral: true },
  cooldown: { path: '/v1/cooldown', method: 'POST', scope: 'radio', ephemeral: true },
};

const tokenFor = (scope) => (scope === 'radio' ? RADIO_CONTROL : READ_STATUS);

async function callDaemon(route) {
  const res = await fetch(DAEMON_BASE_URL + route.path, {
    method: route.method,
    headers: {
      Authorization: `Bearer ${tokenFor(route.scope)}`,
      'Content-Type': 'application/json',
    },
    body: route.method === 'POST' ? '{}' : undefined,
  });
  return { status: res.status, text: await res.text() };
}

async function publishNtfy(title, message, { priority, tags } = {}) {
  if (!NTFY_URL || !NTFY_TOPIC) return;
  const headers = { Title: title };
  if (priority) headers.Priority = priority;
  if (tags) headers.Tags = tags;
  if (NTFY_TOKEN) headers.Authorization = `Bearer ${NTFY_TOKEN}`;
  try {
    await fetch(`${NTFY_URL}/${NTFY_TOPIC}`, { method: 'POST', headers, body: message });
  } catch (e) {
    console.error('ntfy publish:', e.message);
  }
}

const client = new Client({ intents: [GatewayIntentBits.Guilds] });

client.once(Events.ClientReady, (c) => {
  console.log(`bot ready as ${c.user.tag}; owners=${owners.size}`);
  startAlertPoller();
  if (NTFY_URL && NTFY_TOPIC && ALERT_CHANNEL_ID) startNtfyBridge(c);
});

client.on(Events.InteractionCreate, async (i) => {
  if (!i.isChatInputCommand() || i.commandName !== 'modem') return;
  if (!owners.has(i.user.id)) return i.reply({ content: 'Not authorized.', ephemeral: true });

  const sub = i.options.getSubcommand();
  if (sub === 'sms') {
    return i.reply({ content: 'SMS is not available over Discord. Use the iPhone/Tailscale path.', ephemeral: true });
  }
  const route = ROUTES[sub];
  if (!route) return i.reply({ content: `Unknown command: ${sub}`, ephemeral: true });

  await i.deferReply({ ephemeral: route.ephemeral });
  try {
    const { status, text } = await callDaemon(route);
    const body = status === 200
      ? '```json\n' + text.slice(0, 1800) + '\n```'
      : `daemon returned ${status}`;
    await i.editReply(body);
  } catch (_e) {
    await i.editReply('Daemon unreachable over Tailscale.');
  }
});

// Edge-triggered alert poller: only fires when a condition flips, so no spam.
let lastThermalSafe = true;
let lastCapNear = false;
async function pollAlerts() {
  try {
    const st = JSON.parse((await callDaemon(ROUTES.status)).text);
    const safe = st.thermal?.safe !== false && st.policy_state !== 'HOT' && st.policy_state !== 'COOLDOWN';
    if (!safe && lastThermalSafe) {
      publishNtfy('🔥 Modem thermal', `policy ${st.policy_state}, ${st.thermal?.temp_max_c}°C`, { priority: 'high', tags: 'fire' });
    } else if (safe && !lastThermalSafe) {
      publishNtfy('✅ Modem cooled', `policy ${st.policy_state}`, { tags: 'white_check_mark' });
    }
    lastThermalSafe = safe;

    const us = JSON.parse((await callDaemon(ROUTES.usage)).text);
    // Prefer the daemon's own configured cap/period meter; fall back to
    // DATA_CAP_GB against month-to-date only when no limit is set yet.
    const cap = us.limit_bytes > 0 ? us.limit_bytes : CAP_BYTES;
    const used = us.limit_bytes > 0 ? us.period_bytes : us.month_bytes;
    const usedHuman = us.limit_bytes > 0 ? us.period_human : us.month_human;
    const near = cap > 0 && used > 0.9 * cap;
    if (near && !lastCapNear) {
      publishNtfy('📊 Data cap near', `${usedHuman} of ${Math.round(cap / 1e9)} GB used this period`, { priority: 'high', tags: 'chart_with_upwards_trend' });
    }
    lastCapNear = near;
  } catch (e) {
    console.error('alert poll:', e.message);
  }
}
function startAlertPoller() {
  pollAlerts();
  setInterval(pollAlerts, 60_000);
}

// Stream the ntfy topic (JSON SSE) and mirror messages into a Discord channel.
async function startNtfyBridge(c) {
  const url = `${NTFY_URL}/${NTFY_TOPIC}/json`;
  const headers = NTFY_TOKEN ? { Authorization: `Bearer ${NTFY_TOKEN}` } : {};
  for (;;) {
    try {
      const res = await fetch(url, { headers });
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        let nl;
        while ((nl = buf.indexOf('\n')) >= 0) {
          const line = buf.slice(0, nl).trim();
          buf = buf.slice(nl + 1);
          if (!line) continue;
          try {
            const m = JSON.parse(line);
            if (m.event === 'message') {
              const ch = await c.channels.fetch(ALERT_CHANNEL_ID);
              await ch.send(`**${m.title || 'alert'}** — ${m.message || ''}`);
            }
          } catch { /* keepalive / non-JSON line */ }
        }
      }
    } catch (e) {
      console.error('ntfy bridge:', e.message);
    }
    await new Promise((r) => setTimeout(r, 5000)); // reconnect backoff
  }
}

client.login(DISCORD_TOKEN);
