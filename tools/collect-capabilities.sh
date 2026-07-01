#!/bin/bash
# collect-capabilities.sh — read-only inventory of the on-device command surface
# and security posture, to inform which control paths Todo 11 can safely use.
#
# Strictly read-only: it invokes only help/usage/list/get/cat. It NEVER changes
# settings, toggles radios/tethering, grants permissions, installs, or writes
# system files. The guarded write probe lives in probe-controls.sh (consent-gated).
#
# Usage: collect-capabilities.sh --out FILE [--adb ADB_CMD]   (honors ADB_SERIAL)
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
    -h|--help) echo "usage: collect-capabilities.sh --out FILE [--adb ADB_CMD]"; exit 0 ;;
    *) echo "error: unknown argument '$1'" >&2; exit 64 ;;
  esac
done
[ -n "$OUT" ] || { echo "error: --out FILE is required" >&2; exit 64; }

require_device

tmp="$(mktemp "${TMPDIR:-/tmp}/capabilities.XXXXXX")" || { echo "error: mktemp failed" >&2; exit 1; }

{
  echo "CAPABILITIES_VERSION=1"

  echo "=== CMD_PHONE ==="
  adb_run shell cmd phone 2>/dev/null | head -n 60 || true

  echo "=== CMD_WIFI ==="
  adb_run shell cmd wifi 2>/dev/null | head -n 60 || true

  echo "=== CMD_CONNECTIVITY ==="
  adb_run shell cmd connectivity 2>/dev/null | head -n 60 || true

  echo "=== SVC ==="
  adb_run shell svc 2>/dev/null | head -n 40 || true

  echo "=== SETTINGS_MOBILE ==="
  adb_run shell settings list global 2>/dev/null \
    | grep -Ei 'mobile|tether|preferred_network|nr_|5g|data_roaming' || true

  echo "=== DEFEX_KNOX ==="
  # Read-only posture probe: Knox warranty bit, bootloader lock, Knox version.
  adb_run shell getprop 2>/dev/null \
    | grep -Ei 'knox|warranty|defex|flash\.locked|verifiedboot|secure' || true

  echo "=== SMS_GRANT_SURFACE ==="
  # Read-only: does the platform expose READ_SMS + appops? The actual grant
  # feasibility test (a write) is performed by probe-controls.sh under consent.
  adb_run shell pm list permissions -g -d 2>/dev/null | grep -i sms || true
  adb_run shell cmd appops 2>/dev/null | head -n 3 || true

  echo "CAPABILITIES_OK=1"
} | redact > "$tmp"

mv "$tmp" "$OUT"
echo "capabilities written to $OUT" >&2
