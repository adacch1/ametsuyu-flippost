#!/bin/bash
# Tests for tools/probe-controls.sh — the consent-gated, auto-reverting write
# probe (D5). The actual radio write/revert is device-only QA; here we verify
# the safety gate offline: no consent => refuse, and ZERO adb calls.
# Run: bash tests/probe-controls.test.sh
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
SCRIPT="$ROOT/tools/probe-controls.sh"
REC="$HERE/fixtures/recording-adb.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s :: %s\n' "$1" "$2"; fails=$((fails + 1)); }

# --- no consent => refuse, no out file, and ZERO adb calls ---
log="$TMP/calls.log"; : > "$log"
out="$TMP/p.txt"
PROBE_ADB_LOG="$log" bash "$SCRIPT" --adb "$REC" --out "$out" >/dev/null 2>"$TMP/refuse.err"
rc=$?
[ "$rc" -ne 0 ] && pass "no consent: exits nonzero" || fail "no consent: exits nonzero" "rc=$rc"
[ ! -e "$out" ] && pass "no consent: no out file" || fail "no consent: no out file" "exists"
calls=$(wc -l < "$log" | tr -d ' ')
[ "$calls" = "0" ] && pass "no consent: ZERO adb calls" || fail "no consent: ZERO adb calls" "$calls calls: $(cat "$log")"
if grep -qiE 'consent|refus|writes' "$TMP/refuse.err"; then
  pass "no consent: warning explains it writes"
else
  fail "no consent: warning explains it writes" "stderr=$(cat "$TMP/refuse.err")"
fi

# --- with consent, still requires a reachable device (real adb, bogus serial) ---
out2="$TMP/p2.txt"
ADB_SERIAL=missing bash "$SCRIPT" --i-understand-this-writes --out "$out2" >/dev/null 2>"$TMP/dev.err"
rc=$?
[ "$rc" -ne 0 ] && pass "consent+no device: exits nonzero" || fail "consent+no device: exits nonzero" "rc=$rc"
[ ! -e "$out2" ] && pass "consent+no device: no out file" || fail "consent+no device: no out file" "exists"
grep -qiE 'unavailable|no device|not found' "$TMP/dev.err" && pass "consent+no device: device error" || fail "consent+no device: device error" "$(cat "$TMP/dev.err")"

# --- safety structure: the write path must read original + restore it ---
grep -qF -- '--i-understand-this-writes' "$SCRIPT" && pass "has explicit consent flag" || fail "has explicit consent flag" "absent"
# original value must be READ before the first WRITE (auto-revert needs the original)
get_ln=$(grep -n 'settings get global' "$SCRIPT" | head -n1 | cut -d: -f1)
put_ln=$(grep -n 'settings put global' "$SCRIPT" | head -n1 | cut -d: -f1)
if [ -n "$get_ln" ] && [ -n "$put_ln" ] && [ "$get_ln" -lt "$put_ln" ]; then
  pass "reads original mode before first write (get@$get_ln < put@$put_ln)"
else
  fail "reads original mode before first write" "get=$get_ln put=$put_ln"
fi
grep -qi 'trap' "$SCRIPT" && pass "restore via trap (revert on exit)" || fail "restore via trap" "absent"
grep -qi 'restore' "$SCRIPT" && pass "has restore logic" || fail "has restore logic" "absent"

if [ "$fails" -ne 0 ]; then printf '\n%d test(s) failed\n' "$fails"; exit 1; fi
printf '\nall probe-controls tests passed\n'
