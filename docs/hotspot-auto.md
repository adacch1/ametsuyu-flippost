# SSID-whitelist hotspot auto-toggle

Turns the data-sharing hotspot OFF when a whitelisted Wi-Fi network (e.g. home)
is in range, and back ON when none has been seen for two consecutive scans.

## How it works

Every 2 minutes the daemon runs a one-shot Wi-Fi scan through the root helper
(`com.zflip5.tether.WifiScan`, shipped in `tether.jar`). The helper calls the
internal `WifiScanner` API via `app_process` — the public
`WifiManager.startScan` path is blocked by Samsung (`SemWifiManagerProxy`)
while the SoftAP is up, the internal one is not. Scan results never leave the
daemon; `/v1/hotspot` only reports which *whitelisted* names matched.

Decision policy (`decideHotspot`, unit-tested):

- any whitelisted SSID visible → stop the hotspot immediately
- no whitelisted SSID for **2 consecutive scans** → start it (hysteresis, so a
  single flaky scan can't bounce the radio)
- auto-START defers to the thermal gate (never brings the radio up while hot);
  stopping is always allowed
- empty whitelist → feature off, loop idles

## Requirements & caveats

- **Location services must be ON.** Android only brings up the scan-only
  interface then; with location off the loop pauses and `/v1/hotspot` reports
  `"paused": "location_off"` instead of guessing. (Verified on-device: same
  scan succeeds with location on, fails with `-1 not available` when off.)
- Wi-Fi (STA) stays off; scanning works alongside the active SoftAP.
- A scan is off-channel work for the Wi-Fi radio: expect a sub-second
  throughput dip on the hotspot every 2 minutes while the feature is armed.
- SSID matching is exact and case-sensitive.
- The auto-toggle owns hotspot state while armed: a manual stop will be undone
  ~4 minutes later if no whitelisted SSID is around. Clear the whitelist to
  take manual control back.

## Configure

Dashboard → Settings → "Hotspot auto-toggle": one SSID per line, saved with the
radio-control token. Or over the API:

```sh
curl -X POST -H "Authorization: Bearer $RADIO_CONTROL" \
  -H "Content-Type: application/json" \
  -d '{"ssids":["HomeWifi","OfficeWifi"]}' \
  http://127.0.0.1:18080/v1/hotspot/whitelist
```

Persisted to `hotspot.ssid_whitelist` in `config.json` (other keys preserved).
Status: `GET /v1/hotspot` (read-status scope) →
`{active, auto, paused?, whitelist, matched, ap_count, last_scan, last_action}`.

Limits: ≤16 SSIDs, each 1–32 bytes, no tabs/newlines (scan output is
tab-delimited; the helper also strips tabs/newlines from AP names so a hostile
beacon can't forge output lines).
