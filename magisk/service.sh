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
    RC=$(cat /dev/urandom | head -c 32 | od -An -tx1 | tr -d ' \n')
    cat > "$CONFIG" <<EOF
{
  "bind_host": "127.0.0.1",
  "bind_port": 18080,
  "tokens": { "read-status": "$RS", "radio-control": "$RC" },
  "hotspot": { "enable_on_boot": true, "ssid_whitelist": [], "dual_band": false },
  "cpu": { "mode": "auto" },
  "thermal": { "warn_c": 44, "gate_c": 46, "fail_closed": true, "bench": false },
  "dashboard": { "open_reads": true, "open_control": true },
  "rate_limits": { "default_per_min": 600, "radio_per_min": 60 }
}
EOF
    chmod 600 "$CONFIG"
  fi

  # The SSID auto-toggle scans for whitelisted networks, and the scan-only
  # interface only exists when this flag is on. Keep it separate from wifi_on,
  # which stays 0 deliberately: a STA associated on 5GHz forces the SoftAP down
  # to 2.4GHz (DBS) and costs the hotspot its 802.11ax/80MHz link. Scanning
  # ALSO needs location services on, but that is the owner's switch to throw —
  # the daemon reports paused:location_off rather than turning it on for them.
  settings put global wifi_scan_always_enabled 1

  # Qualcomm's Wi-Fi diagnostic logger polls the firmware nonstop (~5% CPU)
  # for logs nobody collects on this phone. Wi-Fi and the hotspot don't need it.
  setprop ctl.stop vendor.cnss_diag

  # Cooler idle: stop charging at Samsung's protect-battery threshold (80%)
  # instead of holding 100% on USB, and skip window/transition animations.
  settings put global protect_battery 1
  settings put global window_animation_scale 0
  settings put global transition_animation_scale 0
  settings put global animator_duration_scale 0

  # Static wallpaper (kiosk slate) on the main and cover screens: Samsung's
  # live wallpaper decodes video under the kiosk nonstop. The helper must run
  # as uid 1000, which can't read /data/adb, so it runs from a public copy.
  if [ -f "$MODDIR/tether/tether.jar" ]; then
    cp "$MODDIR/tether/tether.jar" /data/local/tmp/zf5-tether.jar && chmod 0644 /data/local/tmp/zf5-tether.jar
    for WHICH in 7 19; do
      su 1000 -c "CLASSPATH=/data/local/tmp/zf5-tether.jar app_process /system/bin com.zflip5.tether.SetWallpaper set ff1b2436 $WHICH" >/dev/null 2>&1
    done
  fi

  # Wi-Fi 6 gate (One UI 7): without it the Sem start runs 11axmode=0 (ac only).
  # A wipe resets it, so set it on every boot, before the first start.
  settings put secure wifi_ap_11ax_mode_checked 1
  settings put global wifi_ap_11ax_mode_checked 1

  # Hotspot on boot from the saved SoftAP config. Sem first (the only 802.11ax
  # path), TetheringManager once as a last resort. hotspot-via tells the daemon
  # which path won, so the app can show "Wi-Fi 6 unavailable".
  HS=$(grep -o '"enable_on_boot"[^,}]*' "$CONFIG" | grep -oE 'true|false' | head -n1)
  if [ "$HS" != "false" ] && [ -f "$MODDIR/tether/tether.jar" ]; then
    n=0; VIA=""
    while [ "$n" -lt 6 ]; do
      OUT=$(CLASSPATH="$MODDIR/tether/tether.jar" app_process /system/bin com.zflip5.tether.TetherStart sem 2>&1)
      echo "$(date): hotspot sem try $n: $(printf '%s' "$OUT" | tr '\n' ' ')" >> "$LOG"
      case "$OUT" in *via=SemWifiManager*) VIA=sem; break ;; esac
      n=$((n + 1)); sleep 10
    done
    if [ -z "$VIA" ]; then
      OUT=$(CLASSPATH="$MODDIR/tether/tether.jar" app_process /system/bin com.zflip5.tether.TetherStart tm 2>&1)
      echo "$(date): hotspot tm fallback: $(printf '%s' "$OUT" | tr '\n' ' ')" >> "$LOG"
      case "$OUT" in *RESULT=STARTED*) VIA=tm ;; esac
    fi
    [ -n "$VIA" ] && echo "$VIA" > "$DATADIR/hotspot-via"
  fi

  # The zip does not preserve the exec bit; ensure the binary is runnable.
  [ -f "$BIN" ] && chmod 0755 "$BIN"

  # Always-on cover display, half of it: never sleep while on a charger (7 =
  # AC|USB|wireless), so the kiosk stays readable on a phone that lives on a
  # cable. The other half is FLAG_KEEP_SCREEN_ON in the kiosk activity, which
  # covers the unplugged case. Recorded first so uninstall.sh can put the
  # owner's value back rather than guessing a default.
  if [ ! -f "$DATADIR/stay-on.prev" ]; then
    settings get global stay_on_while_plugged_in > "$DATADIR/stay-on.prev" 2>/dev/null
  fi
  settings put global stay_on_while_plugged_in 7

  # Cover-screen home: keep the kiosk on the Flex Window. See coverwatch.sh for
  # why this is a watcher rather than a home-activity declaration.
  sh "$MODDIR/coverwatch.sh" &

  # Watchdog: restart the daemon if it dies. Backoff avoids a tight crash loop.
  # ZF5_MODDIR lets the daemon find the helper jar (tether/wifi-scan).
  # Honors Magisk's disable/remove flags: if the owner disables or removes the
  # module, stop respawning (Magisk only skips service.sh at the NEXT boot, so
  # without this the daemon would keep running until reboot). Also caps the log
  # so a crash loop can't fill /data.
  while true; do
    if [ -f "$MODDIR/disable" ] || [ -f "$MODDIR/remove" ]; then
      echo "$(date): module disabled/removed; watchdog exiting" >> "$LOG"
      break
    fi
    # Trim the log if it grows past ~1MB (keep the last ~500 lines).
    if [ -f "$LOG" ] && [ "$(wc -c < "$LOG")" -gt 1048576 ]; then
      tail -n 500 "$LOG" > "$LOG.trim" && mv "$LOG.trim" "$LOG"
    fi
    if [ -x "$BIN" ]; then
      ZF5_MODDIR="$MODDIR" /system/bin/sh -c "$BIN --config $CONFIG >> $LOG 2>&1"
    else
      echo "$(date): daemon binary missing at $BIN" >> "$LOG"
    fi
    sleep 10
  done
} &
