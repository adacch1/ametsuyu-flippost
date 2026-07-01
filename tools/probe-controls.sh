#!/bin/bash
# probe-controls.sh — GUARDED, consent-gated, auto-reverting control probe (D5).
#
# Purpose: empirically answer "does writing preferred_network_mode<sub> actually
# move the modem on this device?" before Todo 11 invests in a 5G state machine.
# This is the ONLY discovery tool that writes. It:
#   - refuses unless the operator passes --i-understand-this-writes (no adb calls
#     are made without consent),
#   - reads and remembers the original value,
#   - writes a single test value, samples the data network type, then ALWAYS
#     restores the original (via an EXIT trap, so it reverts even on interrupt).
#
# It NEVER touches thermal config, vendor files, IMEI/baseband, or SMS. It does
# not toggle tethering. Run only with the device owner present.
#
# Usage: probe-controls.sh --i-understand-this-writes --out FILE
#        [--adb ADB_CMD] [--sub SUBID] [--target-mode MODE]   (honors ADB_SERIAL)
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/lib/redact.sh
. "$HERE/lib/redact.sh"
# shellcheck source=tools/lib/adb-common.sh
. "$HERE/lib/adb-common.sh"

ADB="adb"
OUT=""
SUB="1"
TARGET_MODE="11"   # 11 = LTE-only (NETWORK_MODE_LTE_ONLY): a safe, reversible probe value
CONSENT=0
while [ $# -gt 0 ]; do
  case "$1" in
    --i-understand-this-writes) CONSENT=1; shift ;;
    --adb) ADB="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    --sub) SUB="$2"; shift 2 ;;
    --target-mode) TARGET_MODE="$2"; shift 2 ;;
    -h|--help) echo "usage: probe-controls.sh --i-understand-this-writes --out FILE [--adb ADB_CMD] [--sub SUBID] [--target-mode MODE]"; exit 0 ;;
    *) echo "error: unknown argument '$1'" >&2; exit 64 ;;
  esac
done

# Consent gate FIRST — make no adb call (no read, and certainly no write) until
# the operator has explicitly consented to a radio write.
if [ "$CONSENT" -ne 1 ]; then
  echo "REFUSED: probe-controls WRITES radio settings (preferred_network_mode${SUB})." >&2
  echo "        It auto-reverts, but it is the only discovery tool that writes." >&2
  echo "        Re-run with --i-understand-this-writes to consent." >&2
  exit 2
fi

require_device
[ -n "$OUT" ] || { echo "error: --out FILE is required" >&2; exit 64; }

KEY="preferred_network_mode${SUB}"
ORIG="$(adb_run shell settings get global "$KEY" 2>/dev/null | tr -d '\r')"

# Always restore the original value, even on interrupt or error.
restore() {
  if [ "$ORIG" = "null" ] || [ -z "$ORIG" ]; then
    adb_run shell settings delete global "$KEY" >/dev/null 2>&1 || true
  else
    adb_run shell settings put global "$KEY" "$ORIG" >/dev/null 2>&1 || true
  fi
}
trap restore EXIT INT TERM

net_type() {
  adb_run shell dumpsys telephony.registry 2>/dev/null \
    | grep -iE 'mDataNetworkType|getDataNetworkType' | head -n 1
}

tmp="$(mktemp "${TMPDIR:-/tmp}/probe.XXXXXX")" || { echo "error: mktemp failed" >&2; exit 1; }

{
  echo "PROBE_VERSION=1"
  echo "sub=${SUB} key=${KEY} target_mode=${TARGET_MODE}"
  echo "original_mode=${ORIG:-unset}"
  echo "--- before ---"
  net_type
  adb_run shell settings put global "$KEY" "$TARGET_MODE" >/dev/null 2>&1 || true
  sleep 5
  echo "--- after (target applied) ---"
  net_type
  restore
  trap - EXIT INT TERM
  sleep 2
  echo "--- after restore ---"
  net_type
  echo "PROBE_OK=1"
} | redact > "$tmp"

mv "$tmp" "$OUT"
echo "control probe written to $OUT (original mode restored)" >&2
