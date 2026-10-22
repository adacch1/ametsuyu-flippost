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

# cleanup.sh: restore stock settings once, always drop the firewall copy.
for t in settings pm; do
  cat > "$TMP/bin/$t" <<FAKE
#!/bin/sh
echo "\$(basename \$0) \$*" >> "$TMP/ccalls"
FAKE
  chmod +x "$TMP/bin/$t"
done
D="$TMP/data"; S="$TMP/svcd"; V="$TMP/devtmp"
mkdir -p "$D" "$S" "$V"
echo 120 > "$D/cover-bright.prev"; echo junk > "$D/cover-bright-mode.prev"
: > "$D/cover-home-off"; : > "$D/temps.json"
: > "$S/50-zflip-remote.sh"; : > "$S/50-zf5-firewall.sh"
: > "$V/scrcpy-server.jar"; : > "$V/zf5-tether.jar"
runclean() { PATH="$TMP/bin:$PATH" DATADIR="$D" SERVICED="$S" DEVTMP="$V" sh "$ROOT/magisk/cleanup.sh"; }
: > "$TMP/ccalls"; runclean
cat > "$TMP/want" <<WANT
settings put global window_animation_scale 1
settings put global transition_animation_scale 1
settings put global animator_duration_scale 1
pm enable com.android.systemui/.subscreen.SubHomeActivity
pm enable com.sec.android.app.launcher/com.honeyspace.dexservice.SecondaryLauncher
settings put system sub_screen_brightness 120
WANT
cmp -s "$TMP/want" "$TMP/ccalls" && pass "cleanup calls" || { fail "cleanup calls"; cat "$TMP/ccalls"; }
left=0
for f in "$D/cover-bright.prev" "$D/cover-bright-mode.prev" "$D/cover-home-off" "$D/temps.json" "$S/50-zflip-remote.sh" "$S/50-zf5-firewall.sh" "$V/scrcpy-server.jar" "$V/zf5-tether.jar"; do [ -e "$f" ] && left=1; done
[ "$left" -eq 0 ] && [ -e "$D/stock-restored" ] && pass "cleanup removes files and marks restored" || fail "cleanup files"
: > "$S/50-zf5-firewall.sh"; : > "$TMP/ccalls"; runclean
[ ! -s "$TMP/ccalls" ] && [ ! -e "$S/50-zf5-firewall.sh" ] && pass "rerun only drops the firewall copy" || fail "rerun"

chk() { if eval "$2"; then pass "$1"; else fail "$1"; fi; }
chk "service.sh drops cover and wallpaper stacks" "! grep -qE 'animation_scale 0|SetWallpaper|coverwatch|su 1000' '$ROOT/magisk/service.sh'"
chk "service.sh runs cleanup.sh" "grep -q cleanup.sh '$ROOT/magisk/service.sh'"
chk "build-tether drops SetWallpaper" "! grep -q SetWallpaper '$ROOT/tools/build-tether.sh'"
chk "removed paths are gone" "[ ! -e '$ROOT/magisk/coverwatch.sh' ] && [ ! -e '$ROOT/magisk/system' ] && [ ! -e '$ROOT/helper/tether/SetWallpaper.java' ]"
chk "uninstall.sh persists the firewall" "grep -q 50-zf5-firewall.sh '$ROOT/magisk/uninstall.sh'"
chk "module.prop versionCode" "grep -q '^versionCode=2\$' '$ROOT/magisk/module.prop'"
chk "module.prop has no sms" "! grep -qi sms '$ROOT/magisk/module.prop'"

[ "$fails" -eq 0 ] && echo "all boot tests passed" || { echo "$fails failed"; exit 1; }
