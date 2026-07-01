#!/bin/bash
# test-5g-policy.sh — exercise the thermal-aware 5G policy against a running
# daemon using a synthesized thermal environment. Honors:
#   THERMAL_STATUS=severe|none   BATTERY_TEMP_C=<n>
# A severe status or a battery temp at/above the gate makes the daemon refuse
# prefer5g (409, "thermal safety gate blocked"); otherwise it is applied (200).
# Usage: test-5g-policy.sh [--out FILE]
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
OUT=""
[ "${1:-}" = "--out" ] && OUT="$2"

if ! command -v go >/dev/null 2>&1; then echo "SKIP test-5g-policy (go absent)"; exit 0; fi

STATUS="${THERMAL_STATUS:-none}"
BTEMP="${BATTERY_TEMP_C:-30}"
GATE=46
# hot if severe status OR battery temp at/above gate
milli=$(( BTEMP * 1000 ))
maxmilli=41000
if [ "$STATUS" = "severe" ] || [ "$BTEMP" -ge "$GATE" ]; then maxmilli=55000; fi

BIN="$(mktemp -d)/nd"; ( cd "$ROOT/daemon" && go build -o "$BIN" . ) || { echo "build failed"; exit 1; }
TROOT="$(mktemp -d)"
mkdir -p "$TROOT/thermal_zone0" "$TROOT/thermal_zone1"
printf 'battery' > "$TROOT/thermal_zone0/type"; printf '%s\n' "$milli" > "$TROOT/thermal_zone0/temp"
printf 'cpu-1-0' > "$TROOT/thermal_zone1/type"; printf '%s\n' "$maxmilli" > "$TROOT/thermal_zone1/temp"

PORT=18066
RC=$(printf 'c%.0s' {1..64})
cfg="$(mktemp)"
cat > "$cfg" <<EOF
{"bind_host":"127.0.0.1","bind_port":$PORT,"tokens":{"read-status":"$(printf 'a%.0s' {1..64})","radio-control":"$RC"},"ingress":{"mode":"loopback"},"thermal":{"warn_c":44,"gate_c":$GATE,"fail_closed":true},"sms":{"enabled":true,"redact_default":true,"forward":false},"rate_limits":{"default_per_min":300,"radio_per_min":300}}
EOF
ZF5_THERMAL_ROOT="$TROOT" "$BIN" --config "$cfg" >/dev/null 2>&1 &
DPID=$!
trap 'kill $DPID 2>/dev/null; rm -f "$cfg"; rm -rf "$TROOT"' EXIT
for _ in $(seq 1 25); do curl -s -o /dev/null --max-time 1 "http://127.0.0.1:$PORT/v1/status" && break; sleep 0.2; done

tmp="$(mktemp)"
{
  echo "FIVEG_POLICY_VERSION=1"
  echo "input: THERMAL_STATUS=$STATUS BATTERY_TEMP_C=$BTEMP gate_c=$GATE"
  resp="$(curl -s --max-time 5 -X POST -H "Authorization: Bearer $RC" -H 'Content-Type: application/json' -d '{"enable":true}' "http://127.0.0.1:$PORT/v1/prefer5g")"
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST -H "Authorization: Bearer $RC" -H 'Content-Type: application/json' -d '{"enable":true}' "http://127.0.0.1:$PORT/v1/prefer5g")"
  echo "http=$code"
  echo "resp=$resp"
  ok=0
  if [ "$STATUS" = "severe" ] || [ "$BTEMP" -ge "$GATE" ]; then
    if [ "$code" = "409" ] && echo "$resp" | grep -qi 'thermal safety gate'; then echo "expected: DENIED by thermal gate"; ok=1; fi
  else
    if [ "$code" = "200" ] && echo "$resp" | grep -qi '"applied":true'; then echo "expected: APPLIED (safe)"; ok=1; fi
  fi
  [ "$ok" = "1" ] && echo "FIVEG_POLICY_OK=1" || echo "FIVEG_POLICY_OK=0"
} > "$tmp"
cat "$tmp"
[ -n "$OUT" ] && { mkdir -p "$(dirname "$OUT")"; cp "$tmp" "$OUT"; }
grep -q 'FIVEG_POLICY_OK=1' "$tmp"; rc=$?
rm -f "$tmp"
exit $rc
