#!/bin/bash
# build-tether.sh — compile the root tether helper (helper/tether/TetherStart.java)
# to a dex jar shipped in the Magisk module at magisk/tether/tether.jar.
#
# The helper is pure reflection (no android.jar), so it compiles against a plain
# JDK; d8 turns the class into a dex the on-device app_process can load. Mirrors
# the reproducible, Gradle-free approach in helper/build-apk.sh.
#
# Usage: tools/build-tether.sh
set -euo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"
SRC=("$HERE/helper/tether/TetherStart.java" "$HERE/helper/tether/WifiScan.java" "$HERE/helper/tether/SetSoftApConfig.java")
OUT="$HERE/magisk/tether/tether.jar"

: "${ANDROID_HOME:=/opt/homebrew/share/android-commandlinetools}"
: "${JAVA_HOME:=$(/usr/libexec/java_home 2>/dev/null || echo /usr)}"
BT="$(ls -d "$ANDROID_HOME"/build-tools/*/ 2>/dev/null | sort -V | tail -1)"
[ -n "$BT" ] || { echo "error: no Android build-tools under $ANDROID_HOME" >&2; exit 1; }
for f in "${SRC[@]}"; do [ -f "$f" ] || { echo "error: source not found: $f" >&2; exit 1; }; done

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
mkdir -p "$W/classes" "$(dirname "$OUT")"

"$JAVA_HOME/bin/javac" --release 11 -d "$W/classes" "${SRC[@]}"
# shellcheck disable=SC2046  # intentional word-split: d8 takes each .class as an arg
"$BT/d8" --min-api 33 --output "$W" $(find "$W/classes" -name '*.class')
( cd "$W" && zip -qj "$OUT" classes.dex )

echo "tether helper built: ${OUT#"$HERE"/} ($(unzip -l "$OUT" | awk '/classes.dex/{print $1" bytes dex"}'))" >&2
