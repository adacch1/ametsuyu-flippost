#!/bin/bash
# run-tests.sh — offline test + evidence harness (Todo 3).
#
#   bash tools/run-tests.sh --offline --out FILE
#
# Runs every host-side (offline) suite under tests/ plus JSON validation of all
# schemas and JSON fixtures. On-device acceptance runs (real adb hardware) are
# listed as SKIPPED, never counted as passed. Emits OFFLINE_TESTS_OK=1 and exits
# 0 only when every offline check passes; exits nonzero and names the offender
# otherwise (e.g. a corrupt JSON fixture).
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

OFFLINE=0
OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --offline) OFFLINE=1; shift ;;
    --out) OUT="$2"; shift 2 ;;
    -h|--help) echo "usage: run-tests.sh --offline --out FILE"; exit 0 ;;
    *) echo "error: unknown argument '$1'" >&2; exit 64 ;;
  esac
done

# On-device acceptance runs — real hardware only, reported as skipped offline.
DEVICE_TESTS="
discover-device.sh (real adb): DISCOVERY_OK=1 from hardware
collect-capabilities.sh (real adb): CAPABILITIES_OK=1 from hardware
collect-metrics.sh (real adb): METRICS_OK=1 from hardware
probe-controls.sh (real adb, consented): PROBE_OK=1 write+revert
"

tmp="$(mktemp "${TMPDIR:-/tmp}/runtests.XXXXXX")" || { echo "error: mktemp failed" >&2; exit 1; }
passes=0
failures=0

{
  echo "RUN_TESTS_VERSION=1"
  echo "mode=offline"

  echo "=== SHELL TEST SUITES ==="
  for t in "$ROOT"/tests/*.test.sh; do
    [ -e "$t" ] || continue
    name="$(basename "$t")"
    if bash "$t" >/dev/null 2>&1; then
      echo "PASS $name"; passes=$((passes + 1))
    else
      echo "FAIL $name"; failures=$((failures + 1))
    fi
  done

  echo "=== JSON VALIDATION (schemas + fixtures) ==="
  for j in "$ROOT"/schemas/*.json "$ROOT"/tests/fixtures/*.json; do
    [ -e "$j" ] || continue
    rel="${j#"$ROOT"/}"
    if python3 -m json.tool "$j" >/dev/null 2>&1; then
      echo "PASS json $rel"; passes=$((passes + 1))
    else
      echo "FAIL json $rel (invalid JSON)"; failures=$((failures + 1))
    fi
  done

  echo "=== GO DAEMON UNIT TESTS ==="
  if command -v go >/dev/null 2>&1 && [ -f "$ROOT/daemon/go.mod" ]; then
    if ( cd "$ROOT/daemon" && go test ./... ) >/dev/null 2>&1; then
      echo "PASS go test daemon"; passes=$((passes + 1))
    else
      echo "FAIL go test daemon"; failures=$((failures + 1))
    fi
  else
    echo "SKIP go test daemon (go toolchain absent)"
  fi

  echo "=== CONTRACTS ==="
  if python3 "$ROOT/tools/validate-contracts.py" --schemas "$ROOT/schemas" --docs "$ROOT/docs" >/dev/null 2>&1; then
    echo "PASS validate-contracts"; passes=$((passes + 1))
  else
    echo "FAIL validate-contracts"; failures=$((failures + 1))
  fi

  echo "=== SKIPPED (device acceptance — real hardware) ==="
  echo "$DEVICE_TESTS" | sed '/^$/d;s/^/SKIP /'

  echo "passes=${passes}"
  echo "failures=${failures}"
  if [ "$failures" -eq 0 ]; then
    echo "OFFLINE_TESTS_OK=1"
  else
    echo "OFFLINE_TESTS_OK=0"
  fi
} > "$tmp"

cat "$tmp"
if [ -n "$OUT" ]; then mv "$tmp" "$OUT"; else rm -f "$tmp"; fi

# exit nonzero on any offline failure
[ "$failures" -eq 0 ]
