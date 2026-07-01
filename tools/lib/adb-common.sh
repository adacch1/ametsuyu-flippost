# adb-common.sh — sourced helpers shared by the device tools.
# Expects the caller to have set $ADB (the adb command). Honors $ADB_SERIAL.

# adb_run ARGS... — run adb against the selected device.
adb_run() {
  if [ -n "${ADB_SERIAL:-}" ]; then
    "$ADB" -s "$ADB_SERIAL" "$@"
  else
    "$ADB" "$@"
  fi
}

# require_device — exit nonzero with a clear message if no device is reachable.
# Callers must invoke this BEFORE creating any output, so a disconnected run
# leaves no partial artifacts.
require_device() {
  local state
  state="$(adb_run get-state 2>/dev/null || true)"
  if [ "$state" != "device" ]; then
    echo "error: device unavailable (adb get-state returned '${state:-none}')" >&2
    exit 3
  fi
}
