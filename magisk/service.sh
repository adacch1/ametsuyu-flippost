#!/system/bin/sh
# service.sh — Magisk late_start service. Starts the modem daemon after boot
# without ever blocking boot, generates scoped tokens on first run, and keeps a
# lightweight watchdog. Runs as root in the magisk context.
#
# DEFEX note: Samsung DEFEX can kill root processes that exec binaries from
# non-system paths. We exec the daemon through /system/bin/sh and keep the
# binary under the module dir; if a future on-device denial proves DEFEX blocks
# it, the mitigation is documented in docs/ (copy to an allowed path), NOT a
# broad permissive policy.
MODDIR=${0%/*}
DATADIR=/data/adb/zflip5-modem
CONFIG="$DATADIR/config.json"
LOG="$DATADIR/service.log"
BIN="$MODDIR/daemon/zflip5-modemd"

# Never block boot: wait for boot_completed in the background, then supervise.
{
  # wait (bounded) for the framework to finish booting
  i=0
  while [ "$(getprop sys.boot_completed)" != "1" ] && [ "$i" -lt 120 ]; do
    sleep 2; i=$((i + 1))
  done

  mkdir -p "$DATADIR"
  chmod 700 "$DATADIR"

  # First-boot: generate scoped >=256-bit hex tokens and a loopback config.
  if [ ! -f "$CONFIG" ]; then
    RS=$(cat /dev/urandom | head -c 32 | od -An -tx1 | tr -d ' \n')
    SM=$(cat /dev/urandom | head -c 32 | od -An -tx1 | tr -d ' \n')
    RC=$(cat /dev/urandom | head -c 32 | od -An -tx1 | tr -d ' \n')
    cat > "$CONFIG" <<EOF
{
  "bind_host": "127.0.0.1",
  "bind_port": 18080,
  "tokens": { "read-status": "$RS", "sms": "$SM", "radio-control": "$RC" },
  "ingress": { "mode": "loopback" },
  "hotspot": { "enable_on_boot": true },
  "thermal": { "warn_c": 44, "gate_c": 46, "fail_closed": true },
  "sms": { "enabled": true, "redact_default": true, "forward": false, "path": "iphone-tailscale" },
  "rate_limits": { "default_per_min": 30, "sms_per_min": 3, "radio_per_min": 6 }
}
EOF
    chmod 600 "$CONFIG"
  fi

  # Private ingress: when ingress.mode == "tailscale", bring up userspace
  # tailscaled (no TUN needed) and expose ONLY the loopback daemon port over the
  # tailnet via `tailscale serve`. Requires the arm64 tailscaled/tailscale
  # binaries under $MODDIR/tailscale and a one-time auth key (see docs/tailscale.md).
  # Loopback mode (default) skips this entirely — no public exposure either way.
  MODE=$(grep -o '"mode"[^,}]*' "$CONFIG" | head -n1 | sed 's/.*"mode"[^"]*"\([^"]*\)".*/\1/')
  if [ "$MODE" = "tailscale" ] && [ -x "$MODDIR/tailscale/tailscaled" ]; then
    TS="$MODDIR/tailscale"
    STATE="$DATADIR/tailscaled.state"
    "$TS/tailscaled" --tun=userspace-networking --state="$STATE" \
      --socket="$DATADIR/tailscaled.sock" >> "$LOG" 2>&1 &
    sleep 3
    # authkey file is 0600 and consumed once; never logged.
    if [ -f "$DATADIR/tailscale.authkey" ]; then
      "$TS/tailscale" --socket="$DATADIR/tailscaled.sock" up \
        --authkey="$(cat "$DATADIR/tailscale.authkey")" --hostname=zflip5 >> "$LOG" 2>&1
      rm -f "$DATADIR/tailscale.authkey"
    fi
    PORT=$(grep -o '"bind_port"[^,}]*' "$CONFIG" | grep -o '[0-9]\+')
    "$TS/tailscale" --socket="$DATADIR/tailscaled.sock" serve --bg \
      "http://127.0.0.1:${PORT:-18080}" >> "$LOG" 2>&1
  fi

  # Hotspot on boot (owner request): enable the data-sharing Wi-Fi hotspot using
  # the phone's SAVED SoftAP config (SSID/passphrase already set in Settings).
  # The root tether helper runs through app_process and calls the framework
  # TetheringManager; TETHERING_WIFI with no SoftApConfiguration reuses the
  # stored config. Enabled unless config sets hotspot.enable_on_boot=false.
  # This does NOT disable thermal mitigation — the phone still throttles when
  # hot; per owner request it only skips our own extra gate. Non-fatal: failure
  # is logged and boot continues. Retries while the Wi-Fi stack finishes booting.
  HS=$(grep -o '"enable_on_boot"[^,}]*' "$CONFIG" | grep -o 'true\|false' | head -n1)
  if [ "$HS" != "false" ] && [ -f "$MODDIR/tether/tether.jar" ]; then
    n=0
    while [ "$n" -lt 6 ]; do
      OUT=$(CLASSPATH="$MODDIR/tether/tether.jar" app_process /system/bin com.zflip5.tether.TetherStart 2>&1)
      echo "$(date): hotspot try $n: $(printf '%s' "$OUT" | tr '\n' ' ')" >> "$LOG"
      case "$OUT" in
        *RESULT=STARTED*|*code=5*) break ;;  # started, or already active
      esac
      n=$((n + 1)); sleep 10
    done
  fi

  # The zip does not preserve the exec bit; ensure the binary is runnable.
  [ -f "$BIN" ] && chmod 0755 "$BIN"

  # Watchdog: restart the daemon if it dies. Backoff avoids a tight crash loop.
  while true; do
    if [ -x "$BIN" ]; then
      /system/bin/sh -c "$BIN --config $CONFIG >> $LOG 2>&1"
    else
      echo "$(date): daemon binary missing at $BIN" >> "$LOG"
    fi
    sleep 10
  done
} &
