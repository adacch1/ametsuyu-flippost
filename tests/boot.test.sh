#!/bin/bash
# Tests for the module boot steps. Offline: fake iptables record their calls.
# Run: bash tests/boot.test.sh
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir "$TMP/bin"
for t in iptables ip6tables; do
  cat > "$TMP/bin/$t" <<FAKE
#!/bin/sh
echo "\$(basename \$0) \$*" >> "$TMP/calls"
case "\$*" in *" -C "*) [ -e "$TMP/has_jump" ] || exit 1 ;; esac
exit 0
FAKE
  chmod +x "$TMP/bin/$t"
done

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s\n' "$1"; fails=$((fails + 1)); }
has() { grep -qxF -- "$1" "$TMP/calls" && pass "$1" || fail "missing: $1"; }

PATH="$TMP/bin:$PATH" sh "$ROOT/magisk/firewall.sh" once
for t in iptables ip6tables; do
  has "$t -w -F ZF5_FW"
  has "$t -w -A ZF5_FW -i lo -j RETURN"
  has "$t -w -A ZF5_FW -i tailscale0 -j RETURN"
  has "$t -w -A ZF5_FW -p tcp --dport 5555 -j DROP"
  has "$t -w -A ZF5_FW -p tcp --dport 8000:8088 -j DROP"
  has "$t -w -A ZF5_FW -p tcp --dport 8886 -j DROP"
  has "$t -w -I INPUT 1 -j ZF5_FW"
done

: > "$TMP/calls"; touch "$TMP/has_jump"
PATH="$TMP/bin:$PATH" sh "$ROOT/magisk/firewall.sh" once
grep -q -- '-I INPUT' "$TMP/calls" && fail "rerun inserted a second jump" || pass "rerun skips the INPUT jump"

grep -q firewall.sh "$ROOT/magisk/service.sh" && pass "service.sh runs firewall.sh" || fail "service.sh lacks firewall.sh"
grep -q scrcpy-server.jar "$ROOT/tools/dex.sh" && fail "dex.sh still restores scrcpy-server.jar" || pass "dex.sh restore removed"

[ "$fails" -eq 0 ] && echo "all boot tests passed" || { echo "$fails failed"; exit 1; }
