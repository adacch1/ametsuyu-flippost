#!/bin/bash
# collect-metrics.sh — read-only device health snapshot (CPU / RAM / temp) for
# the bot's status command. Reads only /proc and /sys/class/thermal; never
# writes settings, toggles radios, grants permissions, or reads SMS. Emits
# parseable key=value lines so the daemon/bot can surface load, memory and
# temperature. This is the reference collector; the on-device Go daemon reads
# the same /proc + thermal paths natively.
#
# Usage: collect-metrics.sh --out FILE [--adb ADB_CMD]   (honors ADB_SERIAL)
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/lib/redact.sh
. "$HERE/lib/redact.sh"
# shellcheck source=tools/lib/adb-common.sh
. "$HERE/lib/adb-common.sh"

ADB="adb"
OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --adb) ADB="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    -h|--help) echo "usage: collect-metrics.sh --out FILE [--adb ADB_CMD]"; exit 0 ;;
    *) echo "error: unknown argument '$1'" >&2; exit 64 ;;
  esac
done
[ -n "$OUT" ] || { echo "error: --out FILE is required" >&2; exit 64; }

require_device

# Read the raw sources once (all read-only).
LOADAVG="$(adb_run shell cat /proc/loadavg 2>/dev/null | tr -d '\r')"
CPUINFO="$(adb_run shell cat /proc/cpuinfo 2>/dev/null | tr -d '\r')"
MEMINFO="$(adb_run shell cat /proc/meminfo 2>/dev/null | tr -d '\r')"
THERMAL="$(adb_run shell 'for z in /sys/class/thermal/thermal_zone*/; do echo "$(cat "$z"type 2>/dev/null)=$(cat "$z"temp 2>/dev/null)"; done' 2>/dev/null | tr -d '\r')"

tmp="$(mktemp "${TMPDIR:-/tmp}/metrics.XXXXXX")" || { echo "error: mktemp failed" >&2; exit 1; }

{
  echo "METRICS_VERSION=1"

  echo "=== CPU ==="
  echo "$LOADAVG" | awk '{print "cpu_load1="$1"\ncpu_load5="$2"\ncpu_load15="$3}'
  echo "$CPUINFO" | awk '/^processor/{n++} END{print "cpu_cores="n+0}'

  echo "=== MEM ==="
  echo "$MEMINFO" | awk '
    /^MemTotal:/     {t=$2}
    /^MemAvailable:/ {a=$2}
    END{
      printf "mem_total_kb=%d\n", t
      printf "mem_available_kb=%d\n", a
      if (t>0) printf "mem_used_pct=%d\n", int((t-a)/t*100)
    }'

  echo "=== TEMP ==="
  # Thermal zones report milli-degC. Convert to degC (1 dp); report battery and
  # the hottest zone. Ignore non-numeric / non-positive readings.
  echo "$THERMAL" | awk -F= '
    $2 ~ /^-?[0-9]+$/ {
      c = $2 / 1000
      if ($1 == "battery") printf "temp_battery_c=%.1f\n", c
      if ($2+0 > max+0) { max=$2; zone=$1 }
    }
    END{
      if (zone != "") { printf "temp_max_c=%.1f\n", max/1000; printf "temp_max_zone=%s\n", zone }
    }'

  echo "METRICS_OK=1"
} | redact > "$tmp"

mv "$tmp" "$OUT"
echo "metrics written to $OUT" >&2
