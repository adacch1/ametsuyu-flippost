#!/bin/bash
# test-discord-relay.sh — run the Discord relay unit tests (Ed25519 verify, PING,
# allowlist, ephemeral, SMS-refused-over-Discord) and write evidence.
# Usage: test-discord-relay.sh [--signed-fixtures DIR] [--out FILE]
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
FIXDIR="$ROOT/fixtures/discord"
OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --signed-fixtures) FIXDIR="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    -h|--help) echo "usage: test-discord-relay.sh [--signed-fixtures DIR] [--out FILE]"; exit 0 ;;
    *) echo "error: unknown argument '$1'" >&2; exit 64 ;;
  esac
done

if ! command -v node >/dev/null 2>&1; then
  echo "SKIP test-discord-relay (node absent)"; exit 0
fi

tmp="$(mktemp "${TMPDIR:-/tmp}/discord.XXXXXX")"
FIXDIR="$FIXDIR" node "$ROOT/relay/relay.test.js" > "$tmp" 2>&1
rc=$?
cat "$tmp"
[ -n "$OUT" ] && { mkdir -p "$(dirname "$OUT")"; cp "$tmp" "$OUT"; }
rm -f "$tmp"
exit $rc
