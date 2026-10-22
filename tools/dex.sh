#!/bin/bash
# dex.sh — open the phone's Samsung DeX desktop in scrcpy over USB.
#
# Starts DeX on its own 1920x1080 virtual display (no monitor needed), mirrors
# that display, and turns DeX off again when the scrcpy window closes, so the
# phone can deep-sleep. `cmd desktopmode` is a debug-only command: it works
# because Adb-Root-Enabler sets ro.debuggable=1.
#
# Clicks need two things. Samsung creates that display without touch support,
# so every DeX window is NOT_TOUCHABLE until the module's DexInput helper adds
# an input-capable display on the same layer stack; the script runs it for the
# session. And the mouse and keyboard are real HID devices (uhid), since the
# synthetic events scrcpy injects by default do not drive DeX.
#
# Needs the zflip5_modem module's tether.jar with DexInput (tools/build-tether.sh).
#
# Usage: dex.sh [extra scrcpy options]
#        ANDROID_SERIAL=<serial>  selects a specific device (adb and scrcpy).
set -u

adb get-state >/dev/null 2>&1 || { echo "dex.sh: no device over adb" >&2; exit 1; }

# The DeX display is the virtual one Samsung names "Desktop". Its id is not
# fixed (it was 2 in testing), so look it up after starting DeX. Only accept
# it while it is ON: after `cmd desktopmode off` the display lingers in state
# OFF, and mirroring that gives a black window.
dex_display() {
  adb shell dumpsys display | grep 'mBaseDisplayInfo=' \
    | grep 'virtual:android,1000,Desktop' | grep 'state ON' \
    | grep -oE 'displayId [0-9]+' | head -1 | cut -d' ' -f2
}

JAR=/data/adb/modules/zflip5_modem/tether/tether.jar

# Root commands go to su on stdin. `adb shell su -c '...'` re-joins its
# arguments and drops inner quotes, so a quoted multi-word pattern splits into
# several: pkill -f "app_process /system/bin DexInput" matched every
# /system/bin daemon, killed vold, and rebooted the phone.
root() { printf '%s\n' "$@" | adb shell su >/dev/null 2>&1; }

stop_dex() {
  [ -n "${INPUT_PID:-}" ] && kill "$INPUT_PID" 2>/dev/null
  # One space-free word that only the helper's command line carries.
  root 'pkill -f "[D]exInput"'
  adb shell cmd desktopmode off >/dev/null 2>&1
}

ID=$(dex_display)
if [ -z "$ID" ]; then
  adb shell cmd desktopmode on >/dev/null
  for _ in $(seq 1 20); do
    ID=$(dex_display)
    [ -n "$ID" ] && break
    sleep 0.5
  done
fi
if [ -z "$ID" ]; then
  echo "dex.sh: DeX display did not appear (is ro.debuggable=1?)" >&2
  stop_dex
  exit 1
fi
trap stop_dex EXIT
trap 'exit 130' INT HUP TERM   # run the EXIT cleanup on Ctrl-C, hangup or kill too

# Keep the helper's adb shell attached for the session; its display lives only
# as long as the process does.
LOG=$(mktemp)
printf '%s\n' "CLASSPATH=$JAR app_process /system/bin DexInput $ID" | adb shell su >"$LOG" 2>&1 &
INPUT_PID=$!
for _ in $(seq 1 20); do
  grep -q RESULT "$LOG" && break
  sleep 0.5
done
if ! grep -q RESULT=OK "$LOG"; then
  echo "dex.sh: DexInput failed, clicks will not work:" >&2
  cat "$LOG" >&2
fi
rm -f "$LOG"

echo "DeX on display $ID"
scrcpy --display-id="$ID" --mouse=uhid --keyboard=uhid --no-audio \
  --window-title="DeX" "$@"
