#!/bin/bash
# Tests for tools/collect-capabilities.sh — read-only command-surface inventory.
# Run: bash tests/collect-capabilities.test.sh
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
SCRIPT="$ROOT/tools/collect-capabilities.sh"
FAKE="$HERE/fixtures/fake-adb.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s :: %s\n' "$1" "$2"; fails=$((fails + 1)); }

# disconnect: nonzero, no partial out
out="$TMP/disc.txt"
ADB_SERIAL=missing bash "$SCRIPT" --out "$out" >/dev/null 2>"$TMP/disc.err"
rc=$?
[ "$rc" -ne 0 ] && pass "disconnect exits nonzero" || fail "disconnect exits nonzero" "rc=$rc"
[ ! -e "$out" ] && pass "disconnect no partial out" || fail "disconnect no partial out" "exists"

# happy path
ok="$TMP/ok.txt"
bash "$SCRIPT" --adb "$FAKE" --out "$ok" >/dev/null 2>"$TMP/ok.err"
rc=$?
[ "$rc" -eq 0 ] && pass "happy exits 0" || fail "happy exits 0" "rc=$rc err=$(cat "$TMP/ok.err")"
if [ -f "$ok" ]; then
  grep -q 'CAPABILITIES_OK=1' "$ok" && pass "emits CAPABILITIES_OK=1" || fail "emits CAPABILITIES_OK=1" "absent"
  for sec in CMD_PHONE CMD_WIFI SVC SETTINGS_MOBILE DEFEX_KNOX SMS_GRANT_SURFACE; do
    grep -q "=== $sec ===" "$ok" && pass "section $sec present" || fail "section $sec present" "absent"
  done
  grep -qi 'set-allowed-network-types' "$ok" && pass "captures network-type cmd surface" || fail "captures network-type cmd surface" "absent"
  grep -qi 'warranty_bit\|knox' "$ok" && pass "captures Knox/DEFEX surface" || fail "captures Knox/DEFEX surface" "absent"
else
  fail "happy writes out file" "missing"
fi

# read-only guarantee: the script must contain no state-changing verbs
for verb in 'settings put' 'svc data enable' 'svc data disable' 'set-allowed-network-types' 'start-softap' 'pm grant' 'pm install' 'content insert' 'content delete'; do
  if grep -qF "$verb" "$SCRIPT"; then
    fail "read-only: no '$verb'" "found in script"
  else
    pass "read-only: no '$verb'"
  fi
done

if [ "$fails" -ne 0 ]; then printf '\n%d test(s) failed\n' "$fails"; exit 1; fi
printf '\nall collect-capabilities tests passed\n'
