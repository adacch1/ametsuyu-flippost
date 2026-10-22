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
# --- magisk/app-install.sh, with a stubbed pm ---
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/mod/app" "$TMP/t"
cat > "$TMP/bin/pm" <<'STUB'
#!/bin/sh
case "$1" in
  list) printf '%s\n' "$PM_LIST" ;;
  install) echo "$*" >> "$PM_LOG"; exit "${PM_RC:-0}" ;;
esac
STUB
chmod +x "$TMP/bin/pm"
export PM_LOG="$TMP/pm.log"
echo apk > "$TMP/mod/app/zflip5-kiosk.apk"; echo 2 > "$TMP/mod/app/versionCode"
run() { : > "$PM_LOG"; PATH="$TMP/bin:$PATH" ZF5_TMP="$TMP/t" sh "$ROOT/magisk/app-install.sh" "$TMP/mod"; }
check() { # name, expected substring, output
  case "$3" in *"$2"*) pass "$1" ;; *) fail "$1 (got: $3)" ;; esac
}
PM=com.zflip5.modem

out=$(PM_LIST="package:$PM versionCode:1" run)
check "installs when older" "APP_INSTALL=ok have=1 want=2" "$out"
grep -qx "install -r $TMP/t/zf5-app.apk" "$PM_LOG" && [ ! -e "$TMP/t/zf5-app.apk" ] \
  && pass "install logged, copy removed" || fail "install logged, copy removed"
out=$(PM_LIST="" run); check "installs when absent" "APP_INSTALL=ok have=0 want=2" "$out"
out=$(PM_LIST="package:$PM versionCode:2" run); check "skips when equal" "APP_INSTALL=skip have=2 want=2" "$out"
[ ! -s "$PM_LOG" ] && pass "equal: no install" || fail "equal: no install"
out=$(PM_LIST="package:$PM versionCode:3" run); check "skips when newer installed" "APP_INSTALL=skip have=3 want=2" "$out"
out=$(PM_LIST="package:$PM.debug versionCode:9" run); check "ignores lookalike package" "APP_INSTALL=ok have=0 want=2" "$out"
out=$(PM_RC=1 PM_LIST="package:$PM versionCode:1" run); rc=$?
check "reports failure" "APP_INSTALL=fail have=1 want=2 rc=1" "$out"
[ "$rc" -eq 0 ] && pass "failure exits 0" || fail "failure exits 0"
rm "$TMP/mod/app/zflip5-kiosk.apk"
out=$(PM_LIST="" run); check "no bundle" "APP_INSTALL=skip reason=no_bundle" "$out"
grep -q app-install.sh "$ROOT/magisk/service.sh" && pass "service.sh calls it" || fail "service.sh calls it"
{ ! grep -q coverwatch "$ROOT/magisk/service.sh" && [ ! -e "$ROOT/magisk/coverwatch.sh" ]; } \
  && pass "module never launches the app via coverwatch" || fail "module never launches the app via coverwatch"
exit "$fails"
