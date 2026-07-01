#!/bin/bash
# Tests for tools/validate-contracts.py (Todo 2). Verifies the API/config schemas
# parse, the safety-policy tokens are present in docs (and unsafe recommendations
# are absent), and the config validator rejects a public bind + missing token.
# Run: bash tests/validate-contracts.test.sh
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
V="$ROOT/tools/validate-contracts.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s :: %s\n' "$1" "$2"; fails=$((fails + 1)); }

# --- schemas are valid JSON (plan acceptance) ---
python3 -m json.tool "$ROOT/schemas/api.openapi.json" >/dev/null 2>&1 && pass "api.openapi.json parses" || fail "api.openapi.json parses" "invalid JSON"
python3 -m json.tool "$ROOT/schemas/config.schema.json" >/dev/null 2>&1 && pass "config.schema.json parses" || fail "config.schema.json parses" "invalid JSON"

# --- required safety-policy tokens present in docs/schemas (plan acceptance grep) ---
for tok in 'prefer/recover NR' 'unsafe thermal' 'owner-only' 'ephemeral' 'loopback'; do
  if grep -rqF -- "$tok" "$ROOT/docs" "$ROOT/schemas"; then pass "policy token present: $tok"; else fail "policy token present: $tok" "absent"; fi
done

# --- unsafe recommendations absent from docs/schemas (plan acceptance grep) ---
if grep -rnE 'thermal bypass|kill thermal|0\.0\.0\.0|SMS forwarding by default' "$ROOT/docs" "$ROOT/schemas" >/dev/null 2>&1; then
  fail "no unsafe recommendation in docs/schemas" "$(grep -rnE 'thermal bypass|kill thermal|0\.0\.0\.0|SMS forwarding by default' "$ROOT/docs" "$ROOT/schemas")"
else
  pass "no unsafe recommendation in docs/schemas"
fi

# --- happy path: full contract validation ---
evid="$TMP/task2.txt"
python3 "$V" --schemas "$ROOT/schemas" --docs "$ROOT/docs" --out "$evid" >/dev/null 2>"$TMP/h.err"; rc=$?
[ "$rc" -eq 0 ] && pass "validate: exit 0" || fail "validate: exit 0" "rc=$rc :: $(cat "$TMP/h.err")"
[ -e "$evid" ] && grep -q 'CONTRACTS_OK=1' "$evid" && pass "validate: CONTRACTS_OK=1 in evidence" || fail "validate: CONTRACTS_OK=1 in evidence" "$( [ -e "$evid" ] && cat "$evid" || echo no-file)"

# --- good config accepted ---
python3 "$V" --check-config "$HERE/fixtures/config-good.json" --schemas "$ROOT/schemas" >/dev/null 2>"$TMP/g.err"; rc=$?
[ "$rc" -eq 0 ] && pass "good config: exit 0" || fail "good config: exit 0" "rc=$rc :: $(cat "$TMP/g.err")"

# --- bad config rejected: public bind + missing token ---
out="$(python3 "$V" --check-config "$HERE/fixtures/config-bad.json" --schemas "$ROOT/schemas" 2>&1)"; rc=$?
[ "$rc" -ne 0 ] && pass "bad config: exits nonzero" || fail "bad config: exits nonzero" "rc=$rc"
echo "$out" | grep -q 'PUBLIC_BIND_REJECTED=1' && pass "bad config: PUBLIC_BIND_REJECTED=1" || fail "bad config: PUBLIC_BIND_REJECTED=1" "$out"
echo "$out" | grep -qiE 'token' && pass "bad config: names missing token" || fail "bad config: names missing token" "$out"

if [ "$fails" -ne 0 ]; then printf '\n%d test(s) failed\n' "$fails"; exit 1; fi
printf '\nall validate-contracts tests passed\n'
