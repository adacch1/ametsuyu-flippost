#!/bin/bash
# replay-shortcuts.sh — replay every documented iPhone Shortcut request against a
# running daemon and validate status codes. Doubles as the Todo 8 evidence and as
# a contract check that docs/shortcuts.md matches the real API.
#
# Usage: replay-shortcuts.sh --base-url URL --token READ_STATUS_TOKEN
#          [--sms-token TOK] [--radio-token TOK] [--out FILE]
#
# With only --token, sensitive (sms/radio) endpoints are expected to fail auth,
# which is itself a checked outcome (no token in shared Shortcut text).
set -u

BASE=""
TOKEN=""
SMS_TOKEN=""
RADIO_TOKEN=""
OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --base-url) BASE="$2"; shift 2 ;;
    --token) TOKEN="$2"; shift 2 ;;
    --sms-token) SMS_TOKEN="$2"; shift 2 ;;
    --radio-token) RADIO_TOKEN="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    -h|--help) echo "usage: replay-shortcuts.sh --base-url URL --token TOK [--sms-token T] [--radio-token T] [--out FILE]"; exit 0 ;;
    *) echo "error: unknown argument '$1'" >&2; exit 64 ;;
  esac
done
[ -n "$BASE" ] && [ -n "$TOKEN" ] || { echo "error: --base-url and --token required" >&2; exit 64; }

code() { # method path token expected
  local m="$1" p="$2" t="$3"
  local args=(-s -o /dev/null -w '%{http_code}' --max-time 6 -X "$m")
  [ -n "$t" ] && args+=(-H "Authorization: Bearer $t")
  [ "$m" = "POST" ] && args+=(-H 'Content-Type: application/json' -d '{}')
  curl "${args[@]}" "$BASE$p"
}

fails=0
tmp="$(mktemp "${TMPDIR:-/tmp}/replay.XXXXXX")"
{
  echo "SHORTCUTS_REPLAY_VERSION=1"
  echo "base=$BASE"
  check() { # label method path token expected
    local got; got="$(code "$2" "$3" "$4")"
    if [ "$got" = "$5" ]; then echo "OK   $1 ($2 $3) -> $got"; else echo "FAIL $1 ($2 $3) -> $got want $5"; fails=$((fails + 1)); fi
  }

  # read endpoints (read-status token)
  check status   GET  /v1/status  "$TOKEN" 200
  check health   GET  /v1/health  "$TOKEN" 200
  check thermal  GET  /v1/thermal "$TOKEN" 200
  check network  GET  /v1/network "$TOKEN" 200
  check battery  GET  /v1/battery "$TOKEN" 200

  # sms (needs sms scope; with only read-status it is 403, unauth is 401)
  if [ -n "$SMS_TOKEN" ]; then check sms_recent GET /v1/sms/recent "$SMS_TOKEN" 200
  else check sms_recent_denied GET /v1/sms/recent "$TOKEN" 403; fi

  # writes (radio-control scope, thermal-gated -> 200 when cool)
  if [ -n "$RADIO_TOKEN" ]; then
    check tether    POST /v1/tether          "$RADIO_TOKEN" 200
    check prefer5g  POST /v1/prefer5g         "$RADIO_TOKEN" 200
    check cooldown  POST /v1/cooldown         "$RADIO_TOKEN" 200
    check restart   POST /v1/service/restart  "$RADIO_TOKEN" 200
  else
    check tether_denied   POST /v1/tether  "$TOKEN" 403
    check prefer5g_denied POST /v1/prefer5g "$TOKEN" 403
  fi

  echo "fails=$fails"
  [ "$fails" -eq 0 ] && echo "SHORTCUTS_REPLAY_OK=1" || echo "SHORTCUTS_REPLAY_OK=0"
} > "$tmp"

cat "$tmp"
[ -n "$OUT" ] && { mkdir -p "$(dirname "$OUT")"; cp "$tmp" "$OUT"; }
rm -f "$tmp"
[ "$fails" -eq 0 ]
