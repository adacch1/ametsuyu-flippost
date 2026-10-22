// Device-local alert runner: credentials never appear in argv or logs.
const fs = require('node:fs');
const { pathToFileURL } = require('node:url');
const cfg = JSON.parse(fs.readFileSync('/data/adb/zflip5-modem/config.json', 'utf8'));
const ntfy = JSON.parse(fs.readFileSync('/data/adb/zflip5-modem/ntfy.json', 'utf8'));
Object.assign(process.env, {
  DAEMON_BASE_URL: `http://127.0.0.1:${cfg.bind_port || 18080}`,
  READ_STATUS: cfg.tokens['read-status'],
  NTFY_URL: ntfy.servers.map(s => s.url).join(','),
  NTFY_TOKEN: ntfy.servers.map(s => s.token).join(','),
  NTFY_TOPIC: 'zf5-modem',
  DATA_CAP_GB: '0',
});
import(pathToFileURL(__dirname + '/runner.mjs').href).catch(() => {
  console.error('ntfy runner failed to start'); process.exitCode = 1;
});
