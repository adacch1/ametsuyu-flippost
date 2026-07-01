#!/bin/bash
# Tests for tools/discover-device.sh.
# Offline-only seams: disconnect handling (real adb, bogus serial) and the
# happy path driven by a fake adb stub. The real device run is QA on hardware.
# Run: bash tests/discover-device.test.sh
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
SCRIPT="$ROOT/tools/discover-device.sh"
FAKE="$HERE/fixtures/fake-adb.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s :: %s\n' "$1" "$2"; fails=$((fails + 1)); }

# --- disconnect: nonzero exit, no partial out file, clear error ---
out="$TMP/disconnected.txt"
err="$TMP/disc.err"
ADB_SERIAL=missing bash "$SCRIPT" --out "$out" >/dev/null 2>"$err"
rc=$?
[ "$rc" -ne 0 ] && pass "disconnect exits nonzero" || fail "disconnect exits nonzero" "rc=$rc"
[ ! -e "$out" ] && pass "disconnect leaves no partial out file" || fail "disconnect leaves no partial out file" "$out exists"
if grep -qiE 'unavailable|not (found|available)|no device' "$err"; then
  pass "disconnect error names device unavailable"
else
  fail "disconnect error names device unavailable" "stderr=$(cat "$err")"
fi

# --- happy path via fake adb ---
ok="$TMP/ok.txt"
bash "$SCRIPT" --adb "$FAKE" --out "$ok" >/dev/null 2>"$TMP/ok.err"
rc=$?
[ "$rc" -eq 0 ] && pass "happy path exits 0" || fail "happy path exits 0" "rc=$rc err=$(cat "$TMP/ok.err")"
[ -f "$ok" ] && pass "happy path writes out file" || fail "happy path writes out file" "missing $ok"

if [ -f "$ok" ]; then
  grep -q 'DISCOVERY_OK=1' "$ok" && pass "emits DISCOVERY_OK=1" || fail "emits DISCOVERY_OK=1" "absent"
  for sec in BUILD MAGISK TELEPHONY THERMAL BATTERY SUBSCRIPTIONS PORTS; do
    grep -q "=== $sec ===" "$ok" && pass "section $sec present" || fail "section $sec present" "absent"
  done
  # redaction applied to evidence: raw IMEI/serial must not survive
  grep -q '351756051523999' "$ok" && fail "IMEI redacted in evidence" "raw IMEI present" || pass "IMEI redacted in evidence"
  grep -q 'R5CWA1B2C3D' "$ok" && fail "serial redacted in evidence" "raw serial present" || pass "serial redacted in evidence"
  grep -q 'DISCOVERY_OK=1' "$ok" && grep -q '127.0.0.1:18080' "$ok" && pass "ports captured" || fail "ports captured" "absent"
fi

# --- script must never read SMS bodies (Todo 1 guardrail) ---
grep -q 'content://sms' "$SCRIPT" && fail "no SMS provider access in discovery" "found content://sms" || pass "no SMS provider access in discovery"

if [ "$fails" -ne 0 ]; then
  printf '\n%d test(s) failed\n' "$fails"
  exit 1
fi
printf '\nall discover-device tests passed\n'
