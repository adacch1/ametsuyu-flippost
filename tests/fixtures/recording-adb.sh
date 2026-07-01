#!/bin/bash
# Recording adb stub: logs every invocation to $PROBE_ADB_LOG so a test can
# assert that probe-controls.sh makes ZERO adb calls when consent is absent.
set -u
: "${PROBE_ADB_LOG:?PROBE_ADB_LOG must be set}"
echo "$*" >> "$PROBE_ADB_LOG"
case "$*" in
  *get-state*) echo "device" ;;
  *) : ;;
esac
