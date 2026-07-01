#!/bin/bash
# Tests for tools/lib/redact.sh — the discovery redaction filter.
# Run: bash tests/redact.test.sh
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
# shellcheck source=/dev/null
. "$ROOT/tools/lib/redact.sh"

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s\n     expected: %s\n     got:      %s\n' "$1" "$2" "$3"; fails=$((fails + 1)); }

# assert_redacts NAME INPUT MUST_NOT_CONTAIN CONTEXT_KEPT
# CONTEXT_KEPT must survive — guards against a broken/empty redact passing vacuously.
assert_redacts() {
  local name="$1" input="$2" secret="$3" keep="$4" out
  out="$(printf '%s' "$input" | redact)"
  case "$out" in
    *"$secret"*) fail "$name" "no '$secret'" "$out"; return ;;
  esac
  case "$out" in
    *"$keep"*) pass "$name" ;;
    *) fail "$name" "context '$keep' kept" "$out" ;;
  esac
}

# assert_keeps NAME INPUT MUST_CONTAIN
assert_keeps() {
  local name="$1" input="$2" keep="$3" out
  out="$(printf '%s' "$input" | redact)"
  case "$out" in
    *"$keep"*) pass "$name" ;;
    *) fail "$name" "contains '$keep'" "$out" ;;
  esac
}

# --- required strips: IMEI / IMSI / ICCID / MSISDN / serial ---
assert_redacts "IMEI 15-digit masked"      "Device IMEI: 351756051523999"             "351756051523999"   "IMEI:"
assert_redacts "IMSI 15-digit masked"      "mImsi=310170123456789"                    "310170123456789"   "mImsi="
assert_redacts "ICCID 19-digit masked"     "iccid=8901260123456789012"                "8901260123456789012" "iccid="
assert_redacts "MSISDN phone masked"       "line1Number=+15551234567"                 "15551234567"       "line1Number="
assert_redacts "serial getprop masked"     "[ro.serialno]: [R5CWA1B2C3D]"             "R5CWA1B2C3D"       "ro.serialno"
assert_redacts "boot serial masked"        "[ro.boot.serialno]: [R5CWA1B2C3D]"        "R5CWA1B2C3D"       "ro.boot.serialno"
assert_redacts "bearer token masked"       "Authorization: Bearer abc123XYZ_tok.999"  "abc123XYZ_tok.999" "Bearer"

# --- must preserve: small ints needed in evidence ---
assert_keeps "subId preserved"             "mSubId=1 active"                          "mSubId=1"
assert_keeps "battery level preserved"     "level: 87"                                "level: 87"
assert_keeps "port preserved"              "LISTEN 127.0.0.1:18080"                   "18080"
assert_keeps "thermal temp preserved"      "temp=41200 zone=battery"                  "41200"
assert_keeps "redaction marker emitted"    "IMEI: 351756051523999"                    "[REDACTED-NUM]"

if [ "$fails" -ne 0 ]; then
  printf '\n%d test(s) failed\n' "$fails"
  exit 1
fi
printf '\nall redact tests passed\n'
