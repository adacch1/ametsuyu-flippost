#!/system/bin/sh
# uninstall.sh — Magisk runs this when the module is removed. Clean up the
# runtime data dir (config + tokens + logs) so no secrets linger. Does NOT touch
# radio settings; the daemon restores any probe changes at runtime.
# Put the device back to sleeping normally: service.sh set
# stay_on_while_plugged_in=7 for the always-on cover screen, and leaving a phone
# that never sleeps behind is a surprise the owner did not ask for. The kiosk's
# own FLAG_KEEP_SCREEN_ON and the dimmed cover brightness go with the APK and
# the daemon, so neither needs undoing here.
PREV=$(cat /data/adb/zflip5-modem/stay-on.prev 2>/dev/null)
case "$PREV" in ''|*[!0-9]*) PREV=0 ;; esac
settings put global stay_on_while_plugged_in "$PREV"
BRI=$(cat /data/adb/zflip5-modem/cover-bright.prev 2>/dev/null)
case "$BRI" in ''|*[!0-9]*) : ;; *) settings put system sub_screen_brightness "$BRI" ;; esac
MODE=$(cat /data/adb/zflip5-modem/cover-bright-mode.prev 2>/dev/null)
case "$MODE" in ''|*[!0-9]*) : ;; *) settings put system sub_screen_brightness_mode "$MODE" ;; esac

rm -rf /data/adb/zflip5-modem
