#!/bin/bash
# Tests for tools/package-module.sh + tools/lint-magisk-module.sh (Todo 4).
# Offline: builds the module zip, lints it (expect MAGISK_LINT_OK=1), then builds
# a deliberately-unsafe module and asserts the linter rejects it and names the
# problem (unsafe setprop in post-fs-data / permissive SELinux).
# Run: bash tests/magisk.test.sh
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
PKG="$ROOT/tools/package-module.sh"
LINT="$ROOT/tools/lint-magisk-module.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s :: %s\n' "$1" "$2"; fails=$((fails + 1)); }

# --- package the real module ---
zip="$TMP/mod.zip"
bash "$PKG" --src "$ROOT/magisk" --out "$zip" >/dev/null 2>"$TMP/pkg.err"; rc=$?
[ "$rc" -eq 0 ] && pass "package: exit 0" || fail "package: exit 0" "rc=$rc :: $(cat "$TMP/pkg.err")"
[ -s "$zip" ] && pass "package: zip created" || fail "package: zip created" "absent/empty"
for f in module.prop service.sh uninstall.sh; do
  unzip -l "$zip" 2>/dev/null | grep -qE "[[:space:]]$f\$" && pass "zip contains $f" || fail "zip contains $f" "missing"
done

# --- lint the good zip ---
evid="$TMP/task4.txt"
bash "$LINT" "$zip" --out "$evid" >/dev/null 2>"$TMP/lint.err"; rc=$?
[ "$rc" -eq 0 ] && pass "lint good: exit 0" || fail "lint good: exit 0" "rc=$rc :: $(cat "$TMP/lint.err")"
[ -e "$evid" ] && grep -q 'MAGISK_LINT_OK=1' "$evid" && pass "lint good: MAGISK_LINT_OK=1" || fail "lint good: MAGISK_LINT_OK=1" "$( [ -e "$evid" ] && cat "$evid" || echo no-file)"

# --- build an unsafe module and expect rejection ---
bad="$TMP/bad"; cp -R "$ROOT/magisk" "$bad"
printf '#!/system/bin/sh\nsetprop ro.unsafe 1\n' > "$bad/post-fs-data.sh"
printf 'permissive daemon\n' >> "$bad/sepolicy.rule"
badzip="$TMP/bad.zip"
bash "$PKG" --src "$bad" --out "$badzip" >/dev/null 2>&1
out="$(bash "$LINT" "$badzip" 2>&1)"; rc=$?
[ "$rc" -ne 0 ] && pass "lint bad: exits nonzero" || fail "lint bad: exits nonzero" "rc=$rc"
echo "$out" | grep -qi 'setprop' && pass "lint bad: flags setprop in post-fs-data" || fail "lint bad: flags setprop" "$out"
echo "$out" | grep -qi 'permissive' && pass "lint bad: flags permissive SELinux" || fail "lint bad: flags permissive" "$out"

# --- missing-file rejection ---
bare="$TMP/bare"; mkdir -p "$bare"; printf 'id=x\nversion=v1\nversionCode=1\n' > "$bare/module.prop"
barezip="$TMP/bare.zip"; bash "$PKG" --src "$bare" --out "$barezip" >/dev/null 2>&1
bout="$(bash "$LINT" "$barezip" 2>&1)"; brc=$?
[ "$brc" -ne 0 ] && pass "lint bare: nonzero (missing service.sh/uninstall.sh)" || fail "lint bare: nonzero" "rc=$brc"

if [ "$fails" -ne 0 ]; then printf '\n%d test(s) failed\n' "$fails"; exit 1; fi
printf '\nall magisk tests passed\n'
