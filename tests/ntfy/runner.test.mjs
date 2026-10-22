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
      const child = spawn(process.execPath, [fileURLToPath(new URL('../../magisk/ntfy/runner.mjs', import.meta.url)), '--test-notification'], {
        env: { ...process.env, DAEMON_BASE_URL: base,
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

test('runner polls summary and never the removed routes', { timeout: 15000 }, async () => {
  const paths = [];
  const server = http.createServer((req, res) => {
    paths.push(req.url);
    res.end(req.url === '/v1/summary' ? '{"signal":null,"battery":{"level":80,"plugged":"ac","temp_c":30}}' : '{}');
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  let child;
  try {
    const base = `http://127.0.0.1:${server.address().port}`;
    child = spawn(process.execPath, [fileURLToPath(new URL('../../magisk/ntfy/runner.mjs', import.meta.url))], {
      env: { ...process.env, DAEMON_BASE_URL: base, READ_STATUS: 'test', NTFY_URL: base, NTFY_TOKEN: 't', NTFY_TOPIC: 'zf5-modem' },
      stdio: 'ignore',
    });
    await new Promise(resolve => setTimeout(resolve, 1500));
    assert.ok(paths.includes('/v1/summary'));
    assert.ok(!paths.some(p => /^\/v1\/(signal|battery|sms\/recent)/.test(p)), paths.join(' '));
  } finally {
    child?.kill();
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
});
