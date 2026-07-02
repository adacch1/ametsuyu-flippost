#!/system/bin/sh
# action.sh — Magisk Manager "Action" button. Runs on demand as root when the
# owner taps Action on this module. Opens the admin dashboard by launching the
# WebView kiosk activity (helper APK) seeded with the read-status token and,
# for the owner's own kiosk, the radio-control token so writes (thermal gate,
# hotspot whitelist) work without pasting it every time.
#
# Scope: read-status + radio-control (NOT sms). Both tokens are read from the
# root-only config and handed to the kiosk via intent extras; the activity
# stashes them in its sandboxed SharedPreferences and strips them from the URL.
# radio-control is only seeded THIS way (the owner's Action launch) — the plain
# dashboard served to a browser over Tailscale never receives it, so a
# read-only remote view stays read-only. Tradeoff: like read-status, the tokens
# are transiently visible in the am argv (ps/logcat) to the local shell uid —
# the documented, owner-only, on-device cost of one-tap launch.
MODDIR=${0%/*}
DATADIR=/data/adb/zflip5-modem
CONFIG="$DATADIR/config.json"
PKG=com.zflip5.modem
ACT=.CoverKioskActivity

[ -f "$CONFIG" ] || { echo "config not found at $CONFIG — is the daemon installed and booted once?"; exit 1; }

# read-status token: "tokens": { "read-status": "<hex>", ... }
RS=$(grep -o '"read-status"[^,}]*' "$CONFIG" | head -n1 | sed 's/.*"read-status"[^"]*"\([^"]*\)".*/\1/')
[ -n "$RS" ] || { echo "read-status token missing from config"; exit 1; }

# radio-control token (optional): seeded into the kiosk so owner writes work
# without pasting. Missing token just means the kiosk asks for it as before.
RC=$(grep -o '"radio-control"[^,}]*' "$CONFIG" | head -n1 | sed 's/.*"radio-control"[^"]*"\([^"]*\)".*/\1/')

# bind_port (default 18080), for the listen check.
PORT=$(grep -o '"bind_port"[^,}]*' "$CONFIG" | grep -oE '[0-9]+' | head -n1)
[ -n "$PORT" ] || PORT=18080

# Guard: the daemon must actually be listening before we launch a WebView at it,
# else the kiosk shows a connection error. /proc/net/tcp{,6} lists LISTEN (state
# 0A) sockets with the local port in uppercase hex. Retry briefly in case Action
# is tapped right after boot while the watchdog is still starting the daemon.
HEXPORT=$(printf '%04X' "$PORT")
i=0
while [ "$i" -lt 5 ]; do
  if grep -qiE "^[[:space:]]*[0-9]+:[[:space:]]+[0-9A-F]*:${HEXPORT}[[:space:]]+[0-9A-F:]+[[:space:]]+0A" /proc/net/tcp /proc/net/tcp6 2>/dev/null; then
    break
  fi
  i=$((i + 1)); sleep 1
done
if [ "$i" -ge 5 ]; then
  echo "daemon not listening on 127.0.0.1:${PORT} yet — try again in a moment"
  exit 1
fi

# Launch the kiosk on the COVER display (id 1). Pinning the display matters once
# the inner panel is gone/dead: without it the activity can land on display 0
# (inner) instead of the cover. exported=true, so a root am can start it.
# Pass radio-control only when present (extra omitted otherwise).
if [ -n "$RC" ]; then
  am start --user 0 --display 1 -n "$PKG/$ACT" -e token "$RS" -e rtoken "$RC" >/dev/null 2>&1 \
    && echo "dashboard opened (kiosk, read-status + radio-control, cover, 127.0.0.1:${PORT})" \
    || echo "am start failed — is the kiosk APK (dist/zflip5-kiosk.apk) installed?"
else
  am start --user 0 --display 1 -n "$PKG/$ACT" -e token "$RS" >/dev/null 2>&1 \
    && echo "dashboard opened (kiosk, read-status scope, cover, 127.0.0.1:${PORT})" \
    || echo "am start failed — is the kiosk APK (dist/zflip5-kiosk.apk) installed?"
fi
