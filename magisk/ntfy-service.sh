#!/system/bin/sh
# Installed as /data/adb/service.d/60-zf5-ntfy.sh; independent of radio/thermal services.
umask 077
DATA=/data/adb/zflip5-modem
RUNNER=/data/adb/modules/zflip5_modem/ntfy/launch.cjs
NODE=/data/data/com.termux/files/usr/bin/node
PIDFILE="$DATA/ntfy-watchdog.pid"
if [ -f "$PIDFILE" ]; then
  oldpid=$(cat "$PIDFILE")
  if kill -0 "$oldpid" 2>/dev/null && tr '\000' ' ' < "/proc/$oldpid/cmdline" | grep -q '60-zf5-ntfy.sh'; then exit 0; fi
fi
# Re-exec in a detached shell so the boot/ADB caller can return immediately.
if [ "$1" != supervise ]; then
  nohup /system/bin/sh "$0" supervise >> "$DATA/ntfy.log" 2>&1 &
  exit 0
fi
echo $$ > "$PIDFILE"
trap 'rm -f "$PIDFILE"' EXIT
while [ "$(getprop sys.boot_completed)" != 1 ]; do sleep 5; done
export NODE_EXTRA_CA_CERTS=/data/data/com.termux/files/usr/etc/tls/cert.pem
while [ -f "$DATA/ntfy.json" ] && [ -f "$RUNNER" ]; do
  # Bound the diagnostic log; ntfy retains notification history server-side.
  if [ "$(wc -c < "$DATA/ntfy.log")" -gt 262144 ]; then
    tail -c 65536 "$DATA/ntfy.log" > "$DATA/ntfy.log.old"
    : > "$DATA/ntfy.log"
  fi
  "$NODE" "$RUNNER"
  sleep 10
done
