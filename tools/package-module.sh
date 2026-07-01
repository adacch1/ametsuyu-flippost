#!/bin/bash
# package-module.sh — build the installable Magisk module zip from a source dir.
# Zip layout is module-root at the archive root (Magisk requirement).
# Usage: package-module.sh --src DIR --out FILE.zip
set -u

SRC=""
OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --src) SRC="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    -h|--help) echo "usage: package-module.sh --src DIR --out FILE.zip"; exit 0 ;;
    *) echo "error: unknown argument '$1'" >&2; exit 64 ;;
  esac
done
[ -n "$SRC" ] && [ -d "$SRC" ] || { echo "error: --src DIR required (got '$SRC')" >&2; exit 64; }
[ -n "$OUT" ] || { echo "error: --out FILE.zip required" >&2; exit 64; }
[ -f "$SRC/module.prop" ] || { echo "error: $SRC has no module.prop" >&2; exit 65; }

mkdir -p "$(dirname "$OUT")"
OUT_ABS="$(cd "$(dirname "$OUT")" && pwd)/$(basename "$OUT")"
rm -f "$OUT_ABS"

# zip from inside SRC so module.prop is at the archive root.
( cd "$SRC" && zip -qr -X "$OUT_ABS" . -x '.*' ) || { echo "error: zip failed" >&2; exit 1; }
echo "module packaged: $OUT ($(unzip -l "$OUT_ABS" 2>/dev/null | tail -n1 | awk '{print $2}') files)" >&2
