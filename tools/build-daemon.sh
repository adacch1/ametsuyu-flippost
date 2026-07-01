#!/bin/bash
# build-daemon.sh — reproducible static arm64 build of the modem daemon.
# ABI: Android arm64-v8a (Snapdragon 8 Gen 2). Static, no CGO -> no libc dep, so
# it runs under the Magisk root context without DEFEX exec issues from libc.
# Usage: build-daemon.sh [--out FILE]   (default dist/zflip5-modemd)
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
OUT="$ROOT/dist/zflip5-modemd"
[ "${1:-}" = "--out" ] && OUT="$2"

cd "$ROOT/daemon"
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "$OUT" .
echo "built $OUT"
file "$OUT" 2>/dev/null || true
