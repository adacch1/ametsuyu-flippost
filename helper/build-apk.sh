#!/bin/bash
# build-apk.sh — build the WebView cover-screen kiosk APK WITHOUT Gradle, using
# the SDK build-tools directly (aapt2 link -> javac -> d8 -> zipalign ->
# apksigner). Produces dist/zflip5-kiosk.apk signed with a local debug key.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
: "${ANDROID_HOME:=/opt/homebrew/share/android-commandlinetools}"
: "${JAVA_HOME:=/Library/Java/JavaVirtualMachines/temurin-21.jdk/Contents/Home}"
BT="$(ls -d "$ANDROID_HOME"/build-tools/*/ | sort -V | tail -1)"
PLAT="$ANDROID_HOME/platforms/android-34/android.jar"
JC="$JAVA_HOME/bin/javac"
OUT="$ROOT/dist/zflip5-kiosk.apk"
W="$(mktemp -d)"

# 1. compile resources (network security config) then link + manifest -> base APK
"$BT/aapt2" compile --dir "$HERE/res" -o "$W/res.zip"
"$BT/aapt2" link -I "$PLAT" --manifest "$HERE/AndroidManifest.xml" \
  --min-sdk-version 33 --target-sdk-version 34 -o "$W/base.apk" "$W/res.zip"

# 2. compile Java -> 3. dex
mkdir -p "$W/classes"
"$JC" --release 17 -classpath "$PLAT" \
  -d "$W/classes" $(find "$HERE/src" -name '*.java')
"$BT/d8" --min-api 33 --output "$W" $(find "$W/classes" -name '*.class')

# 4. add classes.dex into the apk
( cd "$W" && "$BT/aapt2" version >/dev/null; zip -qj base.apk classes.dex )

# 5. align + 6. sign with a local debug keystore (generated once)
KS="$ROOT/dist/debug.keystore"
if [ ! -f "$KS" ]; then
  "$JAVA_HOME/bin/keytool" -genkeypair -keystore "$KS" -storepass android \
    -keypass android -alias zf5 -keyalg RSA -keysize 2048 -validity 10000 \
    -dname "CN=zflip5" >/dev/null 2>&1
fi
mkdir -p "$ROOT/dist"
"$BT/zipalign" -f 4 "$W/base.apk" "$W/aligned.apk"
"$BT/apksigner" sign --ks "$KS" --ks-pass pass:android --key-pass pass:android \
  --out "$OUT" "$W/aligned.apk"
rm -rf "$W"
echo "built $OUT"
"$BT/apksigner" verify "$OUT" && echo "APK_SIGNED_OK"
