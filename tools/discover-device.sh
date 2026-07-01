#!/bin/bash
# discover-device.sh — read-only Z Flip 5 capability inventory over adb.
#
# Collects redacted device facts (build, Magisk, telephony, thermal, battery,
# subscriptions, listening ports) into an evidence file. Strictly read-only:
# it never changes settings, toggles radios/tethering, writes system/vendor
# files, or reads SMS bodies. Identifiers are stripped by redact() before write.
#
# Usage: discover-device.sh --out FILE [--adb ADB_CMD]
#        ADB_SERIAL=<serial>  selects a specific device.
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
    -h|--help) echo "usage: discover-device.sh --out FILE [--adb ADB_CMD]"; exit 0 ;;
    *) echo "error: unknown argument '$1'" >&2; exit 64 ;;
  esac
done

if [ -z "$OUT" ]; then
  echo "error: --out FILE is required" >&2
  exit 64
fi

# Reachability gate — bail before any write so a disconnected run leaves no
# partial evidence file.
require_device

tmp="$(mktemp "${TMPDIR:-/tmp}/discover.XXXXXX")" || { echo "error: mktemp failed" >&2; exit 1; }

{
  echo "DISCOVERY_VERSION=1"

  echo "=== BUILD ==="
  adb_run shell getprop 2>/dev/null \
    | grep -E '^\[ro\.(product|build|boot|serialno|hardware)' || true

  echo "=== MAGISK ==="
  printf 'magisk_version='
  adb_run shell su -c 'magisk -V' 2>/dev/null || echo "unavailable"
  adb_run shell su -c 'ls -d /data/adb/modules 2>/dev/null' 2>/dev/null || true

  echo "=== TELEPHONY ==="
  adb_run shell dumpsys telephony.registry 2>/dev/null | head -n 80 || true

  echo "=== CONNECTIVITY ==="
  adb_run shell dumpsys connectivity 2>/dev/null | head -n 40 || true

  echo "=== BATTERY ==="
  adb_run shell dumpsys battery 2>/dev/null || true

  echo "=== THERMAL ==="
  adb_run shell su -c 'cat /sys/class/thermal/thermal_zone*/type' 2>/dev/null || true

  echo "=== SUBSCRIPTIONS ==="
  adb_run shell dumpsys isub 2>/dev/null | head -n 40 || true

  echo "=== PORTS ==="
  adb_run shell su -c 'cat /proc/net/tcp /proc/net/tcp6' 2>/dev/null || true

  echo "DISCOVERY_OK=1"
} | redact > "$tmp"

mv "$tmp" "$OUT"
echo "discovery written to $OUT" >&2
