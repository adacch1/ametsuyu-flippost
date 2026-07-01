'use strict';
// Discord interaction relay. Verifies Ed25519 + timestamp replay window, answers
// PING, enforces an owner user-ID allowlist (default-deny), routes /modem
// subcommands to the phone daemon over the private ingress, and marks sensitive
// replies ephemeral. SMS is NEVER served over Discord (locked decision): the
// relay holds no sms token and refuses the subcommand.
const { verify } = require('./verify');

const EPHEMERAL = 64; // Discord message flag
const PING = 1;
const APPLICATION_COMMAND = 2;
const PONG = 1;
const CHANNEL_MESSAGE = 4;

// read = GET on daemon; write = POST. sms is intentionally absent.
const ROUTES = {
  status:   { path: '/v1/status',  method: 'GET',  scope: 'read-status',   ephemeral: false },
  thermal:  { path: '/v1/thermal', method: 'GET',  scope: 'read-status',   ephemeral: false },
  battery:  { path: '/v1/battery', method: 'GET',  scope: 'read-status',   ephemeral: false },
  network:  { path: '/v1/network', method: 'GET',  scope: 'read-status',   ephemeral: true },
  tether:   { path: '/v1/tether',  method: 'POST', scope: 'radio-control', ephemeral: true },
  prefer5g: { path: '/v1/prefer5g', method: 'POST', scope: 'radio-control', ephemeral: true },
  cooldown: { path: '/v1/cooldown', method: 'POST', scope: 'radio-control', ephemeral: true },
};

function ephemeralMsg(content) {
  return { status: 200, body: { type: CHANNEL_MESSAGE, data: { flags: EPHEMERAL, content } } };
}

// handleInteraction(config, headers, rawBody, deps) -> { status, body }
// deps: { fetch, now } injectable for tests. fetch(url, opts) -> {status, json()}.
async function handleInteraction(config, headers, rawBody, deps) {
  const now = (deps && deps.now) || (() => Math.floor(Date.now() / 1000));
  const fetchFn = deps && deps.fetch;

  const sig = headers['x-signature-ed25519'];
  const ts = headers['x-signature-timestamp'];
  if (!sig || !ts) return { status: 401, body: { error: 'missing signature headers' } };

  // Replay window (<=5s) before crypto, cheap reject of stale/replayed requests.
  const window = config.replay_window_s || 5;
  if (Math.abs(now() - Number(ts)) > window) {
    return { status: 401, body: { error: 'stale timestamp' } };
  }
  if (!verify(config.discord_public_key, ts, rawBody, sig)) {
    return { status: 401, body: { error: 'bad signature' } };
  }

  let interaction;
  try { interaction = JSON.parse(rawBody); } catch (_e) {
    return { status: 400, body: { error: 'bad body' } };
  }

  if (interaction.type === PING) return { status: 200, body: { type: PONG } };
  if (interaction.type !== APPLICATION_COMMAND) {
    return { status: 200, body: { type: CHANNEL_MESSAGE, data: { content: 'unsupported' } } };
  }

  // Owner allowlist (default-deny).
  const userId = (interaction.member && interaction.member.user && interaction.member.user.id)
    || (interaction.user && interaction.user.id) || '';
  if (!Array.isArray(config.owner_user_ids) || !config.owner_user_ids.includes(userId)) {
    return ephemeralMsg('Not authorized.');
  }

  const opt = (interaction.data && interaction.data.options && interaction.data.options[0]) || {};
  const sub = opt.name || '';

  if (sub === 'sms') {
    // Locked decision: SMS never traverses Discord.
    return ephemeralMsg('SMS is not available over Discord. Use the iPhone/Tailscale path.');
  }

  const route = ROUTES[sub];
  if (!route) return ephemeralMsg(`Unknown command: ${sub}`);

  const token = config.daemon_tokens && config.daemon_tokens[route.scope];
  if (!token) return ephemeralMsg(`Relay not provisioned for ${route.scope}.`);

  try {
    const res = await fetchFn(config.daemon_base_url + route.path, {
      method: route.method,
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: route.method === 'POST' ? '{}' : undefined,
    });
    const payload = await res.json();
    const content = res.status === 200
      ? '```json\n' + JSON.stringify(payload).slice(0, 1800) + '\n```'
      : `daemon returned ${res.status}: ${payload.error || ''}`;
    return { status: 200, body: { type: CHANNEL_MESSAGE, data: { content, flags: route.ephemeral ? EPHEMERAL : 0 } } };
  } catch (_e) {
    return ephemeralMsg('Daemon unreachable over private ingress.');
  }
}

module.exports = { handleInteraction, ROUTES, EPHEMERAL };
