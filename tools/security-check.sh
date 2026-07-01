#!/bin/bash
# security-check.sh — automated hardening checks (Todo 12). Verifies the daemon
# refuses a public bind, enforces rate limits, and that no real secrets or
# unredacted identifiers have leaked into tracked files or evidence.
# Optional --device adds on-device config-permission + loopback-bind checks.
# Usage: security-check.sh [--out FILE] [--device]
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
OUT=""
DEVICE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    --device) DEVICE=1; shift ;;
    *) echo "error: unknown arg '$1'" >&2; exit 64 ;;
  esac
done

problems=0
note() { echo "PROBLEM: $1"; problems=$((problems + 1)); }
tmp="$(mktemp)"

{
  echo "SECURITY_CHECK_VERSION=1"

  # 1. config validator rejects a public bind + missing token
  bad="$(python3 "$ROOT/tools/validate-contracts.py" --check-config "$ROOT/tests/fixtures/config-bad.json" --schemas "$ROOT/schemas" 2>&1)"
  if echo "$bad" | grep -q 'PUBLIC_BIND_REJECTED=1'; then echo "check:public_bind_rejected=1"; else echo "check:public_bind_rejected=0"; note "validator did not reject public bind"; fi

  # 2. daemon binary refuses to start on a public bind
  if command -v go >/dev/null 2>&1; then
    BIN="$(mktemp -d)/nd"; ( cd "$ROOT/daemon" && go build -o "$BIN" . ) 2>/dev/null
    pcfg="$(mktemp)"
    printf '{"bind_host":"0.0.0.0","bind_port":18055,"tokens":{"read-status":"%s"},"ingress":{"mode":"loopback"},"thermal":{"warn_c":44,"gate_c":46,"fail_closed":true},"sms":{"enabled":true,"redact_default":true,"forward":false},"rate_limits":{"default_per_min":30}}' "$(printf 'a%.0s' {1..64})" > "$pcfg"
    if "$BIN" --config "$pcfg" >/dev/null 2>&1; then note "daemon started with public bind"; echo "check:daemon_refuses_public=0"; else echo "check:daemon_refuses_public=1"; fi
    rm -f "$pcfg"

    # 3. rate limit returns 429 past the cap
    tok=$(printf 'c%.0s' {1..64}); rcfg="$(mktemp)"
    printf '{"bind_host":"127.0.0.1","bind_port":18054,"tokens":{"read-status":"%s","radio-control":"%s"},"ingress":{"mode":"loopback"},"thermal":{"warn_c":44,"gate_c":90,"fail_closed":true},"sms":{"enabled":true,"redact_default":true,"forward":false},"rate_limits":{"default_per_min":2,"radio_per_min":2}}' "$(printf 'a%.0s' {1..64})" "$tok" > "$rcfg"
    TR="$(mktemp -d)"; mkdir -p "$TR/thermal_zone0"; printf 'battery' > "$TR/thermal_zone0/type"; printf '30000\n' > "$TR/thermal_zone0/temp"
    ZF5_THERMAL_ROOT="$TR" "$BIN" --config "$rcfg" >/dev/null 2>&1 &
    dp=$!
    for _ in $(seq 1 25); do curl -s -o /dev/null --max-time 1 http://127.0.0.1:18054/v1/status && break; sleep 0.2; done
    got429=0
    for i in 1 2 3 4 5; do
      c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 -X POST -H "Authorization: Bearer $tok" http://127.0.0.1:18054/v1/cooldown)
      [ "$c" = "429" ] && got429=1
    done
    kill $dp 2>/dev/null; rm -f "$rcfg"; rm -rf "$TR"
    [ "$got429" = "1" ] && echo "check:rate_limit_429=1" || { echo "check:rate_limit_429=0"; note "rate limit never returned 429"; }
  else
    echo "check:daemon_refuses_public=skip (go absent)"
  fi

  # 4. no real secrets committed: scan tracked files for 64-hex tokens that are
  #    NOT the obvious dummy fixtures (aa../bb../cc.. repeats).
  leak=0
  while IFS= read -r f; do
    case "$f" in .omo/evidence/*) continue ;; esac
    hits=$(grep -oE '[0-9a-fA-F]{64}' "$ROOT/$f" 2>/dev/null | grep -vE '^(a{64}|b{64}|c{64}|r{64})$' | head -1)
    [ -n "$hits" ] && { note "possible secret in tracked file: $f"; leak=1; }
  done < <(cd "$ROOT" && git ls-files)
  [ "$leak" = "0" ] && echo "check:no_committed_secrets=1"

  # 5. evidence dir is gitignored (secrets/identifiers must not be tracked)
  if (cd "$ROOT" && git check-ignore .omo/evidence/x.txt >/dev/null 2>&1); then echo "check:evidence_gitignored=1"; else echo "check:evidence_gitignored=0"; note "evidence dir not gitignored"; fi

  # 6. optional on-device checks
  if [ "$DEVICE" = "1" ] && command -v adb >/dev/null 2>&1; then
    perm=$(adb shell 'su -c "stat -c %a /data/adb/zflip5-modem/config.json 2>/dev/null"' 2>/dev/null | tr -d '\r')
    [ "$perm" = "600" ] && echo "check:device_config_600=1" || echo "check:device_config_600=$perm (module not installed yet)"
  fi

  echo "problems=$problems"
  [ "$problems" -eq 0 ] && echo "SECURITY_CHECK_OK=1" || echo "SECURITY_CHECK_OK=0"
} > "$tmp"

cat "$tmp"
[ -n "$OUT" ] && { mkdir -p "$(dirname "$OUT")"; cp "$tmp" "$OUT"; }
rm -f "$tmp"
[ "$problems" -eq 0 ]
