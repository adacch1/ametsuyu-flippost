import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

for (const failFirst of [false, true]) {
  test(`ntfy-only publishes independently, first fails=${failFirst}`, async () => {
    const requests = [];
    const server = http.createServer((req, res) => {
      let body = '';
      req.on('data', chunk => body += chunk);
      req.on('end', () => {
        requests.push({ path: req.url, auth: req.headers.authorization, body });
        res.writeHead(failFirst && req.url.startsWith('/first/') ? 503 : 200);
        res.end('{}');
      });
    });
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    try {
      const base = `http://127.0.0.1:${server.address().port}`;
      const child = spawn(process.execPath, [fileURLToPath(new URL('./index.js', import.meta.url)), '--test-notification'], {
        env: { ...process.env, TELEGRAM_TOKEN: '', NTFY_ONLY: 'true', DAEMON_BASE_URL: base,
          READ_STATUS: 'test', NTFY_URL: `${base}/first,${base}/second`, NTFY_TOKEN: 'token1,token2', NTFY_TOPIC: 'zf5-modem' },
        stdio: 'ignore',
      });
      const code = await new Promise(resolve => child.on('exit', resolve));
      assert.equal(code, failFirst ? 1 : 0);
      assert.equal(requests.length, 2);
      assert.deepEqual(requests.map(r => r.auth).sort(), ['Bearer token1', 'Bearer token2']);
      assert.ok(requests.every(r => r.path.endsWith('/zf5-modem') && r.body.includes('integration test')));
    } finally { await new Promise(resolve => server.close(resolve)); }
  });
}

test('device power changes publish while the main status request is stalled', { timeout: 15000 }, async () => {
  let polls = 0, child;
  let delivered;
  const arrival = new Promise(resolve => delivered = resolve);
  const server = http.createServer((req, res) => {
    if (req.url === '/v1/status') return; // slow general poll must not block power
    if (req.url === '/v1/battery') {
      res.end(JSON.stringify({available: true, level: 80, plugged: ++polls === 1 ? 'ac' : 'unplugged'}));
      return;
    }
    if (req.url === '/zf5-modem') {
      let body = '';
      req.on('data', b => body += b);
      req.on('end', () => { res.end('{}'); delivered(body); });
      return;
    }
    res.end('{}');
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const base = `http://127.0.0.1:${server.address().port}`;
    child = spawn(process.execPath, [fileURLToPath(new URL('./index.js', import.meta.url))], {
      env: {...process.env, TELEGRAM_TOKEN:'', NTFY_ONLY:'true', DAEMON_BASE_URL:base,
        READ_STATUS:'test', NTFY_URL:base, NTFY_TOKEN:'test', NTFY_TOPIC:'zf5-modem'}, stdio:'ignore',
    });
    const body = await arrival;
    assert.match(body, /Power lost/);
    assert.ok(polls >= 2);
  } finally {
    child?.kill();
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
});
