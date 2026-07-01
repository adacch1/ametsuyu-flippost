'use strict';
// Self-contained relay tests. Generates an Ed25519 keypair, writes signed +
// tampered fixtures under fixtures/discord/, and asserts: PING->PONG, good
// command routes to daemon, bad signature 401 (no daemon call), stale timestamp
// 401, unauthorized user denied, SMS refused over Discord (no daemon call),
// sensitive replies ephemeral. Prints DISCORD_RELAY_OK=1 on success.
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { handleInteraction, EPHEMERAL } = require('./relay');

const FIXDIR = process.env.FIXDIR || path.join(__dirname, '..', 'fixtures', 'discord');
fs.mkdirSync(FIXDIR, { recursive: true });

const { publicKey, privateKey } = crypto.generateKeyPairSync('ed25519');
const pubHex = Buffer.from(publicKey.export({ format: 'jwk' }).x, 'base64url').toString('hex');

const OWNER = '111111111111111111';
const config = {
  discord_public_key: pubHex,
  owner_user_ids: [OWNER],
  replay_window_s: 5,
  daemon_base_url: 'http://127.0.0.1:18080',
  daemon_tokens: { 'read-status': 'r'.repeat(64), 'radio-control': 'c'.repeat(64) },
};

const NOW = 1_700_000_000;
const now = () => NOW;

function sign(ts, body) {
  return crypto.sign(null, Buffer.from(String(ts) + body, 'utf8'), privateKey).toString('hex');
}
function headers(ts, body) {
  return { 'x-signature-ed25519': sign(ts, body), 'x-signature-timestamp': String(ts) };
}

function mockDaemon() {
  const calls = [];
  const fetch = async (url, opts) => {
    calls.push({ url, method: opts.method });
    return { status: 200, json: async () => ({ ok: true, url }) };
  };
  return { fetch, calls };
}

(async () => {
  // 1. PING -> PONG
  {
    const body = JSON.stringify({ type: 1 });
    const d = mockDaemon();
    const r = await handleInteraction(config, headers(NOW, body), body, { fetch: d.fetch, now });
    assert.strictEqual(r.status, 200);
    assert.strictEqual(r.body.type, 1, 'PING must PONG');
    fs.writeFileSync(path.join(FIXDIR, 'ping.signed.json'), body);
  }

  // 2. good /modem status (owner) -> routes to daemon
  {
    const body = JSON.stringify({ type: 2, member: { user: { id: OWNER } }, data: { name: 'modem', options: [{ name: 'status' }] } });
    const d = mockDaemon();
    const r = await handleInteraction(config, headers(NOW, body), body, { fetch: d.fetch, now });
    assert.strictEqual(r.status, 200);
    assert.strictEqual(r.body.type, 4);
    assert.strictEqual(d.calls.length, 1, 'status should call daemon once');
    assert.ok(d.calls[0].url.endsWith('/v1/status'));
    fs.writeFileSync(path.join(FIXDIR, 'status.signed.json'), body);
  }

  // 3. bad signature -> 401, NO daemon call
  {
    const body = JSON.stringify({ type: 2, member: { user: { id: OWNER } }, data: { name: 'modem', options: [{ name: 'status' }] } });
    const h = headers(NOW, body);
    h['x-signature-ed25519'] = 'ab'.repeat(32); // wrong 64-byte sig
    const d = mockDaemon();
    const r = await handleInteraction(config, h, body, { fetch: d.fetch, now });
    assert.strictEqual(r.status, 401, 'bad signature must 401');
    assert.strictEqual(d.calls.length, 0, 'no daemon call on bad signature');
    fs.writeFileSync(path.join(FIXDIR, 'status.tampered.json'), body);
  }

  // 4. tampered body (signature valid for original, body changed) -> 401
  {
    const orig = JSON.stringify({ type: 2, member: { user: { id: OWNER } }, data: { name: 'modem', options: [{ name: 'status' }] } });
    const h = headers(NOW, orig);
    const changed = JSON.stringify({ type: 2, member: { user: { id: OWNER } }, data: { name: 'modem', options: [{ name: 'cooldown' }] } });
    const d = mockDaemon();
    const r = await handleInteraction(config, h, changed, { fetch: d.fetch, now });
    assert.strictEqual(r.status, 401, 'tampered body must 401');
    assert.strictEqual(d.calls.length, 0);
  }

  // 5. stale timestamp -> 401
  {
    const body = JSON.stringify({ type: 1 });
    const staleTs = NOW - 100;
    const r = await handleInteraction(config, headers(staleTs, body), body, { fetch: mockDaemon().fetch, now });
    assert.strictEqual(r.status, 401, 'stale timestamp must 401');
  }

  // 6. unauthorized user -> ephemeral denial, no daemon call
  {
    const body = JSON.stringify({ type: 2, member: { user: { id: '999' } }, data: { name: 'modem', options: [{ name: 'status' }] } });
    const d = mockDaemon();
    const r = await handleInteraction(config, headers(NOW, body), body, { fetch: d.fetch, now });
    assert.strictEqual(r.body.data.flags, EPHEMERAL, 'denial ephemeral');
    assert.match(r.body.data.content, /not authorized/i);
    assert.strictEqual(d.calls.length, 0);
  }

  // 7. SMS over Discord refused, NO daemon call
  {
    const body = JSON.stringify({ type: 2, member: { user: { id: OWNER } }, data: { name: 'modem', options: [{ name: 'sms' }] } });
    const d = mockDaemon();
    const r = await handleInteraction(config, headers(NOW, body), body, { fetch: d.fetch, now });
    assert.strictEqual(r.body.data.flags, EPHEMERAL);
    assert.match(r.body.data.content, /not available over discord/i);
    assert.strictEqual(d.calls.length, 0, 'SMS must never call the daemon over Discord');
  }

  // 8. network reply is ephemeral
  {
    const body = JSON.stringify({ type: 2, member: { user: { id: OWNER } }, data: { name: 'modem', options: [{ name: 'network' }] } });
    const d = mockDaemon();
    const r = await handleInteraction(config, headers(NOW, body), body, { fetch: d.fetch, now });
    assert.strictEqual(r.body.data.flags, EPHEMERAL, 'network sensitive -> ephemeral');
  }

  console.log('DISCORD_RELAY_OK=1');
})().catch((e) => { console.error('FAIL:', e.message); process.exit(1); });
