#!/usr/bin/env bash
# Asserts the removed bots, relay, and dead tooling stay removed.
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1
fail=0
chk() { echo "FAIL $1"; fail=1; }

for p in selfhost/telegram-bot selfhost/bot relay fixtures/discord \
  tools/test-discord-relay.sh tools/replay-shortcuts.sh tools/probe-controls.sh tools/collect-metrics.sh \
  tests/shortcuts.test.sh tests/probe-controls.test.sh tests/collect-metrics.test.sh; do
  [ -e "$p" ] && chk "$p still exists"
done

grep -qE 'telegram|bot:' selfhost/docker-compose.yml && chk "compose still names telegram or bot"
grep -q 'ntfy:' selfhost/docker-compose.yml || chk "compose lost the ntfy service"
grep -qE 'DISCORD|collect-metrics|probe-controls' tools/run-tests.sh && chk "run-tests.sh still names removed tests"
grep -q telegram-bot .gitignore && chk ".gitignore still names telegram-bot"

[ "$fail" -eq 0 ] && echo "all removed tests passed"
exit "$fail"
