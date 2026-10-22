#!/system/bin/sh
# app-install.sh MODDIR — install or update the bundled Flippost APK when its
# versionCode is higher than the installed one. Prints one APP_INSTALL line and
# always exits 0, so a failed install never blocks boot.
MODDIR=${1:-${0%/*}}
TMPDIR_=${ZF5_TMP:-/data/local/tmp}
APK="$MODDIR/app/zflip5-kiosk.apk"
PKG=com.zflip5.modem

WANT=$(cat "$MODDIR/app/versionCode" 2>/dev/null)
case "$WANT" in ''|*[!0-9]*) WANT="" ;; esac
if [ ! -f "$APK" ] || [ -z "$WANT" ]; then
  echo "APP_INSTALL=skip reason=no_bundle"
  exit 0
fi

HAVE=$(pm list packages --show-versioncode "$PKG" 2>/dev/null | sed -n 's/^package:com\.zflip5\.modem versionCode:\([0-9]*\).*/\1/p' | head -n1)
HAVE=${HAVE:-0}
if [ "$HAVE" -ge "$WANT" ]; then
  echo "APP_INSTALL=skip have=$HAVE want=$WANT"
  exit 0
fi

# system_server may be denied reads from /data/adb, so install from a public copy.
COPY="$TMPDIR_/zf5-app.apk"
mkdir -p "$TMPDIR_"
cp "$APK" "$COPY" && chmod 0644 "$COPY"
pm install -r "$COPY" >/dev/null 2>&1
RC=$?
rm -f "$COPY"
if [ "$RC" -eq 0 ]; then
  echo "APP_INSTALL=ok have=$HAVE want=$WANT"
else
  echo "APP_INSTALL=fail have=$HAVE want=$WANT rc=$RC"
fi
exit 0
