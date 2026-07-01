#!/bin/bash
# Tests for tools/replay-shortcuts.sh + docs/shortcuts.md (Todo 8). Builds a
# native daemon, starts it on a loopback port, waits until it is ready, then
# replays every documented Shortcut request (full-token: all pass; read-only
# token: sensitive endpoints denied). Skips cleanly if go is absent.
# Run: bash tests/shortcuts.test.sh
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s :: %s\n' "$1" "$2"; fails=$((fails + 1)); }

if ! command -v go >/dev/null 2>&1; then
  echo "SKIP shortcuts.test.sh (go toolchain absent)"; exit 0
fi

BIN="$(mktemp -d)/nd"
( cd "$ROOT/daemon" && go build -o "$BIN" . ) || { echo "FAIL - build native daemon"; exit 1; }

PORT=18077
TOK=$(printf 'a%.0s' {1..64}); SMS=$(printf 'b%.0s' {1..64}); RC=$(printf 'c%.0s' {1..64})
cfg="$(mktemp)"
cat > "$cfg" <<EOF
{"bind_host":"127.0.0.1","bind_port":$PORT,"tokens":{"read-status":"$TOK","sms":"$SMS","radio-control":"$RC"},"ingress":{"mode":"loopback"},"thermal":{"warn_c":44,"gate_c":90,"fail_closed":true},"sms":{"enabled":true,"redact_default":true,"forward":false},"rate_limits":{"default_per_min":300,"sms_per_min":50,"radio_per_min":50}}
EOF

# Fake a safe thermal sysfs so writes aren't fail-closed on a host without
# /sys/class/thermal (the daemon's fail-closed behavior is covered by go tests).
TROOT="$(mktemp -d)"
mkdir -p "$TROOT/thermal_zone0" "$TROOT/thermal_zone1"
printf 'battery' > "$TROOT/thermal_zone0/type";  printf '30000\n' > "$TROOT/thermal_zone0/temp"
printf 'cpu-1-0' > "$TROOT/thermal_zone1/type";  printf '41000\n' > "$TROOT/thermal_zone1/temp"

ZF5_THERMAL_ROOT="$TROOT" "$BIN" --config "$cfg" >/dev/null 2>&1 &
DPID=$!
trap 'kill $DPID 2>/dev/null; rm -f "$cfg"; rm -rf "$TROOT"' EXIT

# wait up to 5s for readiness
ready=0
for _ in $(seq 1 25); do
  if curl -s -o /dev/null --max-time 1 "http://127.0.0.1:$PORT/v1/status"; then ready=1; break; fi
  sleep 0.2
done
[ "$ready" = "1" ] && pass "daemon reachable" || fail "daemon reachable" "never bound"

# full-token replay -> all OK
if bash "$ROOT/tools/replay-shortcuts.sh" --base-url "http://127.0.0.1:$PORT" --token "$TOK" --sms-token "$SMS" --radio-token "$RC" --out "$HERE/../.omo/evidence/task-8-zflip5-modem-module.txt" | grep -q 'SHORTCUTS_REPLAY_OK=1'; then
  pass "full-token replay: SHORTCUTS_REPLAY_OK=1"
else
  fail "full-token replay" "not OK"
fi

# read-only token -> sensitive endpoints denied (403), still OK overall
if bash "$ROOT/tools/replay-shortcuts.sh" --base-url "http://127.0.0.1:$PORT" --token "$TOK" | grep -q 'SHORTCUTS_REPLAY_OK=1'; then
  pass "read-only token: sensitive endpoints denied"
else
  fail "read-only token denial" "unexpected"
fi

if [ "$fails" -ne 0 ]; then printf '\n%d test(s) failed\n' "$fails"; exit 1; fi
printf '\nall shortcuts tests passed\n'
