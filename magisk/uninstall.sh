#!/system/bin/sh
# uninstall.sh — Magisk runs this when the module is removed. Clean up the
# runtime data dir (config + tokens + logs) so no secrets linger. Does NOT touch
# radio settings; the daemon restores any probe changes at runtime.
rm -rf /data/adb/zflip5-modem
