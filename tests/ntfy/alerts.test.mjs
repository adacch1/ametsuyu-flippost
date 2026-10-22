import { test } from 'node:test';
import assert from 'node:assert/strict';
import { detect, plain, DEFAULTS } from '../../magisk/ntfy/alerts.mjs';

// Feed snapshots in order, return every message per step.
function run(snaps, cfg = DEFAULTS) {
  let state = null;
  return snaps.map((snap) => {
    const r = detect(state, snap, cfg);
    state = r.state;
    return r.messages;
  });
}
const bat = (level, plugged = 'unplugged') => ({ status: { thermal: { safe: true }, policy_state: 'OK' }, battery: { available: true, level, plugged } });

test('first poll only seeds, even when conditions are already bad', () => {
  const [m] = run([{
    status: { thermal: { safe: false, temp_max_c: 50 }, policy_state: 'HOT' },
    battery: { available: true, level: 5, plugged: 'unplugged' },
    usage: { limit_bytes: 100, period_bytes: 99 },
    signal: { bars: 0 },
  }]);
  assert.deepEqual(m, []);
});

test('temperature edges both ways', () => {
  const hot = { status: { thermal: { safe: false, temp_max_c: 47 }, policy_state: 'HOT' } };
  const ok = { status: { thermal: { safe: true }, policy_state: 'OK' } };
  const m = run([ok, hot, hot, ok]);
  assert.match(m[1][0], /hot.*47/);
  assert.deepEqual(m[2], []);
  assert.match(m[3][0], /cooled/);
});

test('battery low fires once with hysteresis, power loss/restore announced', () => {
  const m = run([bat(30), bat(20), bat(21), bat(19), bat(26), bat(18), bat(18, 'ac')]);
  assert.match(m[1][0], /Battery low: 20%/);
  assert.deepEqual(m[2], []); // 21 is inside the rearm band
  assert.deepEqual(m[3], []);
  assert.deepEqual(m[4], []); // 26 clears
  assert.match(m[5][0], /Battery low: 18%/);
  assert.match(m[6][0], /Power restored \(ac\)/);
  assert.match(run([bat(50, 'usb'), bat(50)])[1][0], /Power lost/);
});

test('signal changes must settle for 2 polls; flaps are silent', () => {
  const sig = (display, bars = 3) => ({ signal: { display, bars, operator: 'Viettel' } });
  const m = run([sig('5G'), sig('4G'), sig('5G'), sig('4G'), sig('4G'), sig('x', 0), sig('x', 0), sig('5G'), sig('5G')]);
  assert.deepEqual(m.slice(0, 4).flat(), []);
  assert.match(m[4][0], /5G → 4G/);
  assert.deepEqual(m[5], []);
  assert.match(m[6][0], /Signal lost/);
  assert.match(m[8][0], /Signal back: 5G.*Viettel/);
});

test('whitelist hotspot actions and pauses', () => {
  const hs = (last_action, active, paused) => ({ hotspot: { last_action, active, paused } });
  const m = run([hs('', false), hs('started: whitelist not in range', true), hs('started: whitelist not in range', true),
    hs('stopped: saw HomeWifi', false), hs('stopped: saw HomeWifi', false, 'location_off'), hs('stopped: saw HomeWifi', false)]);
  assert.match(m[1][0], /📡 Hotspot started/);
  assert.deepEqual(m[2], []);
  assert.match(m[3][0], /💤 Hotspot stopped: saw HomeWifi/);
  assert.match(m[4][0], /paused: location_off/);
  assert.match(m[5][0], /resumed/);
});

test('data cap near fires once', () => {
  const u = (b) => ({ usage: { limit_bytes: 100e9, period_bytes: b, period_human: `${b / 1e9} GB` } });
  const m = run([u(10e9), u(91e9), u(95e9)]);
  assert.match(m[1][0], /91 GB of 100 GB/);
  assert.deepEqual(m[2], []);
});

test('a failed endpoint keeps previous state', () => {
  const m = run([bat(50, 'ac'), {}, bat(50, 'ac')]);
  assert.deepEqual(m.flat(), []);
});

test('plain strips Telegram HTML for ntfy, unescaping once', () => {
  assert.equal(plain('Hot <b>x</b>\ncode &lt;1234&gt; &amp;amp;'), 'Hot x\ncode <1234> &amp;');
});

test('battery from summary: power lost and restored', () => {
  const m = run([{ battery: { level: 50, plugged: 'ac', temp_c: 30 } }, { battery: { level: 50, plugged: 'unplugged', temp_c: 30 } }, { battery: { level: 49, plugged: 'usb', temp_c: 30 } }]);
  assert.match(m[1][0], /Power lost: on battery at 50%/);
  assert.match(m[2][0], /Power restored \(usb\), 49%/);
});
test('5G SA with bars null is not signal lost', () => {
  const sa = { signal: { display: '5G', tech: 'NR', operator: 'Viettel', bars: null } };
  assert.deepEqual(run([sa, sa, sa, sa]).flat(), []);
});
test('signal back with bars null omits the bar count', () => {
  const lost = { signal: { display: '5G', bars: 0, operator: 'Viettel' } };
  const sa = { signal: { display: '5G', tech: 'NR', operator: 'Viettel', bars: null } };
  assert.match(run([sa, lost, lost, sa, sa])[4][0], /^📶 Signal back: 5G, Viettel\.$/);
});
test('signal null keeps previous state', () => {
  const lte = { signal: { display: '4G', tech: 'LTE', operator: 'Viettel', bars: 3 } };
  assert.deepEqual(run([lte, { signal: null }, { signal: null }, lte]).flat(), []);
});
test('no SMS rule', () => {
  assert.deepEqual(run([{}, { sms: { messages: [{ address: '+1', date: '2', body: 'x' }] } }]).flat(), []);
});
