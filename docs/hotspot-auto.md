# SSID-whitelist hotspot auto-toggle

The auto-toggle turns the data-sharing hotspot off when a whitelisted Wi-Fi
network (for example, home) is in range, and back on when no whitelisted
network has appeared for two consecutive scans.

## How it works

The daemon runs a one-shot Wi-Fi scan through the root helper
(`com.zflip5.tether.WifiScan`, shipped in `tether.jar`). The scan cadence
adapts to hotspot state: every 60 seconds while the hotspot is off (no
clients to disrupt, so the loop can catch leaving a whitelisted network on
the next scan), and every 3 minutes while it's on (a scan is off-channel
work, so the loop backs off to limit the throughput dip for connected
clients). The helper calls the internal `WifiScanner` API via `app_process`:
Samsung's `SemWifiManagerProxy` blocks the public `WifiManager.startScan`
path while the SoftAP is up, but not the internal one. Scan results never
leave the daemon; `GET /v1/hotspot` reports only which *whitelisted* names
matched.

Decision policy (`decideHotspot`, unit-tested):

- Any whitelisted SSID visible: stop the hotspot immediately.
- No whitelisted SSID for two consecutive scans: start it. This hysteresis
  keeps one flaky scan from bouncing the radio.
- Starting the hotspot is never gated on app thermal state — the hotspot is
  the modem's primary function, and Samsung's own thermal mitigation is the
  backstop at genuinely dangerous temperatures. Stopping it is always
  allowed.
- An empty whitelist turns the feature off; the loop idles.

## When scanning pauses

Two conditions can stop a scan from running. Rather than guess, the daemon
pauses visibly and retries on the next tick — no manual recovery needed.

- **Location is off.** Android only brings up the scan-only interface with
  location services on. `GET /v1/hotspot` reports `"paused": "location_off"`,
  and the dashboard's Hotspot card shows a banner asking you to turn Location
  back on. The daemon never touches this setting itself; turning Location on
  or off stays your call.
- **The scan itself fails.** `GET /v1/hotspot` reports `"paused":
  "scan_failed"` plus a `"paused_detail"` field with the helper's own reason
  (for example, `-1 not available`), and the dashboard banner shows it. On a
  failed scan, the daemon also checks `wifi_scan_always_enabled` — the one
  scan precondition it owns — and turns it back on if something (a Location
  sub-toggle, a power-saving mode) turned it off. This check never touches
  `wifi_on` or Location: bringing up a Wi-Fi client would force the SoftAP
  off its 802.11ax 80 MHz band onto 2.4 GHz.

Each pause and recovery logs one line (`hotspot auto: scanning paused: …` /
`hotspot auto: scanning resumed`) — a steadily paused loop adds no repeated
log lines.

The auto-toggle owns hotspot state while armed: a manual stop is undone on
the next hotspot-on tick (within 3 minutes) if no whitelisted SSID is around.
Clear the whitelist, or start a force-on override, to take manual control
back.

## Force on

To keep the hotspot on regardless of whitelist matches — for example, to
share internet from home for a few hours despite the home network being in
range — start a timed override:

```sh
curl -X POST -H "Authorization: Bearer $RADIO_CONTROL" \
  -H "Content-Type: application/json" \
  -d '{"hours":4}' \
  http://127.0.0.1:18080/v1/hotspot/override
```

`hours` accepts 0 through 24. The dashboard's Hotspot card offers the same
control as 2h/4h/8h/12h/24h buttons, plus a live countdown and a **Cancel**
button.

While an override is active:

- The daemon starts the hotspot immediately (it doesn't wait for the next
  tick), and the override suppresses the whitelist stop decision until the
  deadline.
- Scanning keeps running, so the nearby-networks list and preset
  auto-switching still work, and a pause (location off, scan failed) still
  shows on the dashboard — the override forces the AP, not the scan.
- `GET /v1/hotspot` reports `override_until` (RFC 3339) and
  `override_left_s` (seconds remaining).
- The deadline persists to `/data/adb/zflip5-modem/hotspot_override.json`
  and survives a daemon restart or a device reboot. A wall-clock deadline
  means a clock jump (for example, from NTP sync right after boot) shifts
  the expiry by the same amount.

To cancel early, send `{"hours":0}`. Normal whitelist logic resumes on the
next tick — within one scan interval (60 seconds off, 3 minutes on), never
longer.

An override never lasts more than 24 hours: a forgotten override can't pin
the hotspot up indefinitely.

## Configure the whitelist

Dashboard → Settings → **Hotspot auto-toggle**: one SSID per line, saved with
the radio-control token. Or use the API:

```sh
curl -X POST -H "Authorization: Bearer $RADIO_CONTROL" \
  -H "Content-Type: application/json" \
  -d '{"ssids":["HomeWifi","OfficeWifi"]}' \
  http://127.0.0.1:18080/v1/hotspot/whitelist
```

The whitelist persists to `hotspot.ssid_whitelist` in `config.json` (other
keys stay untouched).

Status: `GET /v1/hotspot` (read-status scope) returns `{active, auto,
paused?, paused_detail?, whitelist, matched, ap_count, last_scan,
last_action, override_until?, override_left_s?}`.

Limits: 16 SSIDs maximum, each 1 to 32 bytes, no tabs or newlines (scan
output is tab-delimited; the helper also strips tabs and newlines from AP
names so a hostile beacon can't forge output lines).
