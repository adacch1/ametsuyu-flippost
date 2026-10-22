#!/bin/bash
# Builds the native cover-screen app with Gradle, verifies its signature, and
# bundles it into the module. Output: dist/zflip5-kiosk.apk, magisk/app/.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
: "${ANDROID_HOME:=/opt/homebrew/share/android-commandlinetools}"
: "${JAVA_HOME:=/Library/Java/JavaVirtualMachines/temurin-21.jdk/Contents/Home}"
export ANDROID_HOME JAVA_HOME
CERT=47965ec3ab41b576b2cf0ab7f414c3127f0e95dbd58ecd57d21651ecdacca75a
BT="$(ls -d "$ANDROID_HOME"/build-tools/*/ | sort -V | tail -1)"
OUT="$ROOT/dist/zflip5-kiosk.apk"

if [ ! -f "$ROOT/dist/debug.keystore" ]; then
  echo "error: dist/debug.keystore missing; restore it from backup (a new key cannot update the installed app)" >&2
  exit 1
fi

(cd "$HERE/cover-app" && ./gradlew --no-daemon -q assembleRelease)
mkdir -p "$ROOT/dist" "$ROOT/magisk/app"
cp "$HERE/cover-app/app/build/outputs/apk/release/app-release.apk" "$OUT"

"$BT/apksigner" verify --print-certs "$OUT" | grep -q "$CERT" \
  || { echo "error: APK is not signed with the expected certificate" >&2; exit 1; }
SIZE=$(wc -c < "$OUT")
[ "$SIZE" -lt 3000000 ] || { echo "error: APK is $SIZE bytes; the limit is 3000000" >&2; exit 1; }
VC=$("$BT/aapt2" dump badging "$OUT" | sed -n "s/.*versionCode='\([0-9]*\)'.*/\1/p" | head -n1)
[ "${VC:-0}" -ge 2 ] || { echo "error: versionCode ${VC:-unknown} is below 2" >&2; exit 1; }

cp "$OUT" "$ROOT/magisk/app/zflip5-kiosk.apk"
echo "$VC" > "$ROOT/magisk/app/versionCode"
echo "built $OUT versionCode=$VC"
echo "APK_SIGNED_OK"
