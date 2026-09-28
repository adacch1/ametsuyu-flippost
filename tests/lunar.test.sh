#!/bin/bash
# Tests the Vietnamese lunar calendar shipped in the dashboard page.
# The algorithm is astronomy, not arithmetic you can eyeball, so it is checked
# against dates whose lunar value is a matter of public record.
# Run: bash tests/lunar.test.sh   (needs node; skips without it)
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

command -v node >/dev/null 2>&1 || { echo "SKIP - node not installed"; exit 0; }

# Pull the calendar functions straight out of the served page, so the test
# exercises what ships rather than a copy that can drift.
SRC="$(mktemp -t lunar).js"
python3 - "$ROOT/daemon/dashboard.go" > "$SRC" <<'PY'
import pathlib, sys
s = pathlib.Path(sys.argv[1]).read_text()
print(s[s.index('  var LUNAR_TZ=7;'):s.index('  var clkTime=document.getElementById')])
print("module.exports={solarToLunar:solarToLunar};")
PY

node -e '
const {solarToLunar} = require(process.argv[1]);
// [solar d,m,y] -> expected "day/month", why it proves something
const cases = [
  [[17,2,2026], "1/1", "Tet Binh Ngo falls on 17 Feb 2026"],
  [[29,1,2025], "1/1", "Tet At Ty falls on 29 Jan 2025"],
  [[21,1,1985], "1/1", "Tet 1985 is 21 Jan in Vietnam"],
  [[20,2,1985], "1/2", "20 Feb 1985 is the CHINESE new year, not the Vietnamese one"],
];
let fails = 0;
for (const [d, want, why] of cases) {
  const r = solarToLunar(d[0], d[1], d[2], 7);
  const got = r[0] + "/" + r[1];
  if (got === want) console.log("ok   - " + why);
  else { console.log("FAIL - " + why + "\n     expected: " + want + "\n     got:      " + got); fails++; }
}
process.exit(fails ? 1 : 0);
' "$SRC"
rc=$?
rm -f "$SRC"
[ "$rc" -eq 0 ] && echo "all lunar tests passed"
exit "$rc"
