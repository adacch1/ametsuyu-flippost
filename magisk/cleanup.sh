#!/system/bin/sh
# cleanup.sh — Runs on every boot from service.sh. Removes the firewall copy
# that older installs left in service.d, then restores the stock settings that
# earlier module versions changed. The stock-restored marker makes the restore
# run once. The module no longer forces the wallpaper, and the stock wallpaper
# cannot return because the old setter never saved it.
# Test overrides: DATADIR, SERVICED, DEVTMP.
DATADIR=${DATADIR:-/data/adb/zflip5-modem}
SERVICED=${SERVICED:-/data/adb/service.d}
DEVTMP=${DEVTMP:-/data/local/tmp}

rm -f "$SERVICED/50-zf5-firewall.sh"
[ -e "$DATADIR/stock-restored" ] && exit 0

for k in window_animation_scale transition_animation_scale animator_duration_scale; do
  settings put global "$k" 1
done
pm enable com.android.systemui/.subscreen.SubHomeActivity
pm enable com.sec.android.app.launcher/com.honeyspace.dexservice.SecondaryLauncher

# Mode first, then level, so the level is not rewritten by an auto mode.
for pair in cover-bright-mode.prev:sub_screen_brightness_mode cover-bright.prev:sub_screen_brightness; do
  f="$DATADIR/${pair%%:*}"
  v=$(cat "$f" 2>/dev/null)
  case "$v" in ''|*[!0-9]*) : ;; *) settings put system "${pair##*:}" "$v" ;; esac
  rm -f "$f"
done

rm -f "$DATADIR/cover-home-off" "$DATADIR/temps.json" "$SERVICED/50-zflip-remote.sh" \
  "$DEVTMP/scrcpy-server.jar" "$DEVTMP/zf5-tether.jar"
: > "$DATADIR/stock-restored"
