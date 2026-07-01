#!/bin/bash
# lint-magisk-module.sh — static safety lint for the Magisk module zip.
# Confirms required files exist, module.prop is well-formed, there is no unsafe
# setprop in post-fs-data, no broad-permissive SELinux, and uninstall cleans up.
# Usage: lint-magisk-module.sh MODULE.zip [--out FILE]
set -u

ZIP=""
OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    -h|--help) echo "usage: lint-magisk-module.sh MODULE.zip [--out FILE]"; exit 0 ;;
    *) ZIP="$1"; shift ;;
  esac
done
[ -n "$ZIP" ] && [ -f "$ZIP" ] || { echo "error: MODULE.zip required (got '$ZIP')" >&2; exit 64; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
unzip -qo "$ZIP" -d "$WORK" || { echo "error: cannot unzip $ZIP" >&2; exit 1; }

problems=0
note() { echo "PROBLEM: $1"; problems=$((problems + 1)); }

tmp="$(mktemp "${TMPDIR:-/tmp}/magisklint.XXXXXX")"
{
  echo "MAGISK_LINT_VERSION=1"

  # 1. required files
  for f in module.prop service.sh uninstall.sh; do
    if [ -f "$WORK/$f" ]; then echo "present:$f=1"; else echo "present:$f=0"; note "missing required file: $f"; fi
  done

  # 2. module.prop well-formed
  if [ -f "$WORK/module.prop" ]; then
    for k in id version versionCode; do
      grep -q "^$k=" "$WORK/module.prop" && echo "prop:$k=1" || { echo "prop:$k=0"; note "module.prop missing $k"; }
    done
  fi

  # 3. no unsafe setprop in post-fs-data (deadlock/early-boot hazard)
  if [ -f "$WORK/post-fs-data.sh" ]; then
    if grep -qE '(^|[^a-zA-Z])setprop' "$WORK/post-fs-data.sh"; then
      note "unsafe setprop in post-fs-data.sh"
    else
      echo "post-fs-data:setprop=none"
    fi
  else
    echo "post-fs-data:absent=1"
  fi

  # 4. no broad-permissive SELinux (ignore comment lines)
  if [ -f "$WORK/sepolicy.rule" ]; then
    if grep -vE '^[[:space:]]*#' "$WORK/sepolicy.rule" | grep -qiE 'permissive|setenforce[[:space:]]+0'; then
      note "broad-permissive SELinux in sepolicy.rule"
    else
      echo "sepolicy:permissive=none"
    fi
  fi
  # also scan all scripts for setenforce 0 / magiskpolicy permissive (non-comment)
  if grep -rIhiE 'setenforce[[:space:]]+0|magiskpolicy.*permissive' "$WORK" 2>/dev/null | grep -qvE '^[[:space:]]*#'; then
    note "setenforce 0 / magiskpolicy permissive found in scripts"
  fi

  # 5. uninstall cleanup present
  if [ -f "$WORK/uninstall.sh" ] && grep -qE 'rm .*zflip5-modem' "$WORK/uninstall.sh"; then
    echo "uninstall:cleanup=1"
  else
    echo "uninstall:cleanup=0"; note "uninstall.sh does not clean /data/adb/zflip5-modem"
  fi

  # 6. service.sh must not block boot (must background its work)
  if [ -f "$WORK/service.sh" ]; then
    grep -qE '&[[:space:]]*$' "$WORK/service.sh" && echo "service:backgrounded=1" || { echo "service:backgrounded=0"; note "service.sh may block boot (no backgrounded block)"; }
  fi

  echo "problems=${problems}"
  [ "$problems" -eq 0 ] && echo "MAGISK_LINT_OK=1" || echo "MAGISK_LINT_OK=0"
} > "$tmp"

cat "$tmp"
[ -n "$OUT" ] && { mkdir -p "$(dirname "$OUT")"; cp "$tmp" "$OUT"; }
rm -f "$tmp"
[ "$problems" -eq 0 ]
