#!/usr/bin/env bash
# Checks the pure JavaScript in daemon/web/index.html with node.
set -euo pipefail
cd "$(dirname "$0")/.."
command -v node >/dev/null || { echo "SKIP - node not installed"; exit 0; }
html=daemon/web/index.html
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
awk '/<script>/{f=1;next} /<\/script>/{f=0} f' "$html" > "$tmp/all.js"
[ -s "$tmp/all.js" ] || { echo "FAIL - no inline script"; exit 1; }
node --check "$tmp/all.js"
awk '/\/\*PURE-START\*\//{f=1} f{print} /\/\*PURE-END\*\//{f=0}' "$html" > "$tmp/pure.js"
[ -s "$tmp/pure.js" ] || { echo "FAIL - no PURE block"; exit 1; }
echo 'module.exports={fmtBytes,fmtRate,hotspotLine,deviceRows,isStale,dualBlockText};' >> "$tmp/pure.js"
cp "$tmp/pure.js" "$tmp/pure.cjs"
cat > "$tmp/check.cjs" <<'JS'
const assert = require("node:assert/strict");
const { fmtBytes, fmtRate, hotspotLine, deviceRows, isStale, dualBlockText } = require("./pure.cjs");
const eq = assert.equal, deq = assert.deepEqual;
eq(fmtBytes(999), "999 B"); eq(fmtBytes(1500), "1.50 KB"); eq(fmtBytes(1234567890), "1.23 GB"); eq(fmtBytes(87654321099), "87.65 GB");
eq(fmtRate(0), "0 kbps"); eq(fmtRate(12000), "96 kbps"); eq(fmtRate(2500000), "20.0 Mbps"); eq(fmtRate(25000000), "200 Mbps");
eq(hotspotLine({wifi6:true, band:"5", clients:1}), "Wi-Fi 6 · 5 GHz · 1 device");
eq(hotspotLine({wifi6:false, band:"dual", clients:3}), "Wi-Fi 6 unavailable · 2.4 + 5 GHz · 3 devices");
eq(hotspotLine({wifi6:null, band:null, clients:0}), "0 devices");
deq(deviceRows({per_client:"unavailable", phone_down_bytes_s:null, phone_up_bytes_s:null}), []);
deq(deviceRows({per_client:[{mac:"aa:bb:cc:dd:ee:ff", ipv4:null, down_bytes_s:5, up_bytes_s:6, signal_dbm:null}], phone_down_bytes_s:1, phone_up_bytes_s:2}),
    [{label:"aa:bb:cc:dd:ee:ff", down:5, up:6}, {label:"This phone (approximate)", down:1, up:2}]);
eq(isStale(0, 5000), false); eq(isStale(0, 5001), true); eq(isStale(0, 1000, 31), true); eq(isStale(0, 1000, 30), false);
eq(dualBlockText("whitelist"), "Clear the allowlist first: dual band stops Wi-Fi scanning.");
eq(dualBlockText("auto_switch"), "Turn off preset auto-switch first: dual band stops Wi-Fi scanning.");
eq(dualBlockText(null), "");
console.log("all web tests passed");
JS
node "$tmp/check.cjs"
