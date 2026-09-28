// Edge-triggered alert detection. Pure: detect(prev, snap, cfg) returns the
// messages to broadcast and the state to pass back next poll. No I/O here, so
// node --test covers every rule without a daemon (see alerts.test.js).
//
// snap fields are the raw daemon JSON; any of them may be undefined when that
// endpoint failed this poll, and each rule then keeps its previous state.
//   status  = /v1/status        usage = /v1/usage     signal = /v1/signal
//   hotspot = /v1/hotspot       sms   = /v1/sms/recent (only with SMS_READ)

export const esc = (s) => String(s ?? '').replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));

// Telegram HTML -> plain text for ntfy, which shows markup literally.
export const plain = (html) => html.replace(/<[^>]+>/g, '')
  .replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');

export const DEFAULTS = {
  batteryLow: 20,      // alert at or below this %
  batteryRearm: 5,     // clear only once back above low + rearm (no flapping at 19/20/21)
  signalSettle: 2,     // polls a new signal state must hold before it is announced
  capBytes: 0,         // DATA_CAP_GB fallback when the daemon has no limit_bytes
};

// First poll only seeds state: nothing is announced for conditions that were
// already true when the bot started (no "battery low" spam on every restart).
export function detect(prev, snap, cfg = DEFAULTS) {
  const out = [];
  const seeding = prev == null;
  const p = prev ?? {};
  const s = { ...p };
  const say = (m) => { if (!seeding) out.push(m); };

  // --- temperature (daemon's own gate + hysteresis decides HOT/COOLDOWN) ---
  const st = snap.status;
  if (st) {
    const safe = st.thermal?.safe !== false && st.policy_state !== 'HOT' && st.policy_state !== 'COOLDOWN';
    if (p.safe !== undefined && safe !== p.safe) {
      say(safe ? '✅ Modem cooled.' : `🔥 Modem hot: policy ${esc(st.policy_state)}, ${esc(st.thermal?.temp_max_c)}°C`);
    }
    s.safe = safe;

    // --- battery ---
    const b = st.battery;
    if (b?.available) {
      // Only matters off mains; plugging in clears it.
      const limit = p.battLow ? cfg.batteryLow + cfg.batteryRearm : cfg.batteryLow;
      const low = b.plugged === 'unplugged' && b.level <= limit;
      if (low && !p.battLow) say(`🪫 Battery low: ${b.level}%, not charging.`);
      s.battLow = low;
      if (p.plugged !== undefined && b.plugged !== p.plugged) {
        say(b.plugged === 'unplugged'
          ? `🔌 Power lost: on battery at ${b.level}%.`
          : `🔌 Power restored (${esc(b.plugged)}), ${b.level}%.`);
      }
      s.plugged = b.plugged;
    }
  }

  // --- data cap ---
  const us = snap.usage;
  if (us) {
    const cap = us.limit_bytes > 0 ? us.limit_bytes : cfg.capBytes;
    const used = us.limit_bytes > 0 ? us.period_bytes : us.month_bytes;
    const usedHuman = us.limit_bytes > 0 ? us.period_human : us.month_human;
    const near = cap > 0 && used > 0.9 * cap;
    if (near && !p.capNear) say(`📊 Data cap near: ${esc(usedHuman)} of ${Math.round(cap / 1e9)} GB used.`);
    s.capNear = near;
  }

  // --- signal: announce tech changes and loss/regain, only once settled ---
  const sg = snap.signal;
  if (sg && sg.available !== false) {
    const key = sg.level > 0 ? (sg.display || sg.tech || '?') : 'none';
    s.sigCandN = key === p.sigCand ? p.sigCandN + 1 : 1;
    s.sigCand = key;
    if (s.sigCandN >= cfg.signalSettle || p.sig === undefined) {
      if (p.sig !== undefined && key !== p.sig) {
        if (key === 'none') say('📵 Signal lost.');
        else if (p.sig === 'none') say(`📶 Signal back: ${esc(key)}, ${sg.level}/4 bars, ${esc(sg.operator)}.`);
        else say(`📶 Network ${esc(p.sig)} → ${esc(key)} (${sg.level}/4 bars).`);
      }
      s.sig = key;
    }
  }

  // --- whitelist hotspot: the controller already debounces (2 scans), so
  // every new last_action is a real start/stop ---
  const hs = snap.hotspot;
  if (hs) {
    if (hs.last_action && p.hsAction !== undefined && hs.last_action !== p.hsAction) {
      say(`${hs.active ? '📡' : '💤'} Hotspot ${esc(hs.last_action)}.`);
    }
    s.hsAction = hs.last_action ?? '';
    const paused = hs.paused || '';
    if (p.hsPaused !== undefined && paused !== p.hsPaused) {
      say(paused
        ? `⏸️ Hotspot auto-toggle paused: ${esc(paused)}${hs.paused_detail ? ` (${esc(hs.paused_detail)})` : ''}.`
        : '▶️ Hotspot auto-toggle resumed.');
    }
    s.hsPaused = paused;
  }

  // --- SMS: sender + body, by owner's choice (opt-in via SMS_READ) ---
  const sms = snap.sms;
  if (sms?.available !== false && Array.isArray(sms?.messages)) {
    const ids = sms.messages.map((m) => `${m.date}|${m.address}`);
    if (p.smsSeen) {
      // messages arrive newest-first; announce oldest-first
      sms.messages.filter((m, i) => !p.smsSeen.includes(ids[i])).reverse()
        .forEach((m) => say(`💬 SMS from <b>${esc(m.address)}</b>\n${esc(m.body)}`));
    }
    s.smsSeen = ids;
  }

  return { state: s, messages: out };
}
