#!/bin/bash
# Tests for tools/collect-metrics.sh — read-only device health (CPU/RAM/temp)
# collector that feeds the bot's status command. Verifies: read-only (no write
# verbs), fails closed on no device, emits parseable key=value metrics.
# Run: bash tests/collect-metrics.test.sh
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
SCRIPT="$ROOT/tools/collect-metrics.sh"
FAKE="$HERE/fixtures/fake-adb.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s :: %s\n' "$1" "$2"; fails=$((fails + 1)); }

# --- missing --out => usage error ---
bash "$SCRIPT" --adb "$FAKE" >/dev/null 2>"$TMP/noout.err"; rc=$?
[ "$rc" -eq 64 ] && pass "missing --out: exit 64" || fail "missing --out: exit 64" "rc=$rc"

# --- no device => fail closed, no out file ---
out="$TMP/m.txt"
ADB_SERIAL=missing bash "$SCRIPT" --out "$out" >/dev/null 2>"$TMP/dev.err"; rc=$?
[ "$rc" -ne 0 ] && pass "no device: exits nonzero" || fail "no device: exits nonzero" "rc=$rc"
[ ! -e "$out" ] && pass "no device: no out file" || fail "no device: no out file" "exists"
grep -qiE 'unavailable|no device|not found' "$TMP/dev.err" && pass "no device: device error" || fail "no device: device error" "$(cat "$TMP/dev.err")"

# --- happy path against fake adb ---
out2="$TMP/m2.txt"
bash "$SCRIPT" --adb "$FAKE" --out "$out2" >/dev/null 2>"$TMP/ok.err"; rc=$?
[ "$rc" -eq 0 ] && pass "fake adb: exit 0" || fail "fake adb: exit 0" "rc=$rc :: $(cat "$TMP/ok.err")"
[ -e "$out2" ] && pass "fake adb: out file written" || fail "fake adb: out file written" "absent"

grep -q 'METRICS_VERSION=1' "$out2" && pass "has version banner" || fail "has version banner" "absent"
grep -q 'METRICS_OK=1' "$out2" && pass "has OK sentinel" || fail "has OK sentinel" "absent"

# CPU
grep -q 'cpu_load1=0.90' "$out2" && pass "cpu_load1 parsed" || fail "cpu_load1 parsed" "$(grep cpu_load "$out2")"
grep -q 'cpu_load5=0.84' "$out2" && pass "cpu_load5 parsed" || fail "cpu_load5 parsed" "absent"
grep -q 'cpu_load15=0.68' "$out2" && pass "cpu_load15 parsed" || fail "cpu_load15 parsed" "absent"
grep -q 'cpu_cores=8' "$out2" && pass "cpu_cores counted" || fail "cpu_cores counted" "$(grep cpu_cores "$out2")"

# RAM
grep -q 'mem_total_kb=7329468' "$out2" && pass "mem_total_kb parsed" || fail "mem_total_kb parsed" "$(grep mem_total "$out2")"
grep -q 'mem_available_kb=4058836' "$out2" && pass "mem_available_kb parsed" || fail "mem_available_kb parsed" "absent"
# used% = (7329468-4058836)/7329468*100 = 44
grep -q 'mem_used_pct=44' "$out2" && pass "mem_used_pct computed" || fail "mem_used_pct computed" "$(grep mem_used "$out2")"

# Temp (thermal zone milli-C -> C; hottest was cpu-1-0=43600 -> 43.6)
grep -q 'temp_battery_c=33.1' "$out2" && pass "temp_battery_c computed" || fail "temp_battery_c computed" "$(grep temp_battery "$out2")"
grep -q 'temp_max_c=43.6' "$out2" && pass "temp_max_c computed" || fail "temp_max_c computed" "$(grep temp_max "$out2")"
grep -q 'temp_max_zone=cpu-1-0' "$out2" && pass "temp_max_zone named" || fail "temp_max_zone named" "$(grep temp_max_zone "$out2")"

# --- read-only guarantee: NO write verbs anywhere in the script ---
if grep -nE 'settings put|settings delete|svc [a-z]+ (enable|disable)|pm grant|pm install|start-softap|set-allowed-network|airplane-mode (enable|disable)|set-wifi-enabled' "$SCRIPT" >/dev/null; then
  fail "read-only: no write verbs" "$(grep -nE 'settings put|settings delete|pm grant|start-softap|set-allowed-network' "$SCRIPT")"
else
  pass "read-only: no write verbs"
fi

if [ "$fails" -ne 0 ]; then printf '\n%d test(s) failed\n' "$fails"; exit 1; fi
printf '\nall collect-metrics tests passed\n'
