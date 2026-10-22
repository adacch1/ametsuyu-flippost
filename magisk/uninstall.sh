#!/system/bin/sh
# uninstall.sh — Magisk runs this when the module is removed. It restores the
# stay-on setting that service.sh changed, keeps the firewall active by copying
# it to service.d, and deletes the runtime data dir (config, tokens, logs) so no
# secrets linger. It does not touch radio settings.
MODDIR=${0%/*}
PREV=$(cat /data/adb/zflip5-modem/stay-on.prev 2>/dev/null)
case "$PREV" in ''|*[!0-9]*) PREV=0 ;; esac
settings put global stay_on_while_plugged_in "$PREV"
mkdir -p /data/adb/service.d && cp "$MODDIR/firewall.sh" /data/adb/service.d/50-zf5-firewall.sh && chmod 0755 /data/adb/service.d/50-zf5-firewall.sh

rm -rf /data/adb/zflip5-modem
