#!/bin/bash
# Offline checks for the Gradle-built app install path.
# Run: bash tests/app-install.test.sh
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s\n' "$1"; fails=$((fails + 1)); }

grep -q 'am start --user 0 -n com.zflip5.modem/.CoverKioskActivity' "$ROOT/magisk/action.sh" \
  && pass "action.sh opens the app" || fail "action.sh opens the app"
! grep -qiE 'token|read-status|radio-control' "$ROOT/magisk/action.sh" \
  && pass "action.sh holds no tokens" || fail "action.sh holds no tokens"
{ grep -q gradlew "$ROOT/helper/build-apk.sh" && ! grep -q 'aapt2" link' "$ROOT/helper/build-apk.sh"; } \
  && pass "build-apk.sh uses gradle" || fail "build-apk.sh uses gradle"
{ [ ! -e "$ROOT/helper/src" ] && [ ! -e "$ROOT/helper/AndroidManifest.xml" ] && [ ! -e "$ROOT/helper/res/values/strings.xml" ]; } \
  && pass "old webview sources gone" || fail "old webview sources gone"
! grep -rq WebView "$ROOT/helper/cover-app/app/src/main" \
  && pass "app has no WebView" || fail "app has no WebView"
exit "$fails"
