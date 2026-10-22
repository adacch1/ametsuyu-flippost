# Web UI

The daemon serves one self-contained web page at `/` on the tailnet. The page
shows the same data as the native app, and it installs as a progressive web
app (PWA). The page carries no data itself: it fetches everything from the
`/v1/*` API.

## Screens

The main view shows the connection chip, signal, mobile data against the cap,
battery, temperature, per-device speed, and the hotspot switch. Tap
**Details** to open the other screens:

| Screen | Contents |
|---|---|
| **Devices** | Connected clients and their speeds. |
| **Hotspot** | Band, presets, and the home-network SSID list. |
| **Data** | Usage today, this week, and this month, plus the data cap. |
| **System** | CPU, memory, thermal state, and temperature history. |
| **Tailscale** | Read-only state, DNS name, and serve status. |
| **Alerts** | A reminder that you set alerts on the phone. |
| **Advanced** | Read-only switch state and daemon details. |

The page treats the data as stale when the collector stops updating
(`slow_age_s` over 30 seconds), and it disables writes until data is fresh
again.

## Tailscale is read-only on the web

The web UI shows the Tailscale state but never a login URL. Tailscale login
and logout are owner-only: the daemon allows them only from the app's UID
(`app_only`). A tailnet browser gets 403 for both calls. See
[Tailscale](tailscale.md).

## Open the web UI

Over your tailnet, open `https://<name>.<tailnet>.ts.net/`. Over USB, forward
the port first:

```sh
adb forward tcp:18080 tcp:18080
open "http://127.0.0.1:18080/"
```

If **Open reads** is off, add `?token=<read-status token>` once. The page
reads the token from the URL and keeps it in the browser.

## Install as a PWA

Open the web UI in a browser on your phone or computer, then use the browser's
install option (**Add to Home Screen** on iOS and Android, **Install** on
desktop Chrome). The installed app opens the same page without browser chrome.

## Pair another device with the QR code

The app shows a pairing QR code on its **Advanced** screen. The QR code
encodes the web UI URL (`/`). It carries a `?token=` parameter only when
**Open reads** is off. When it is on, the URL carries no token.

## Open reads and open control

Two switches control tokenless access over the tailnet:

- **Open reads** serves read-only calls without a token.
- **Open control** also allows radio-control writes without a token.

Both are off by default. You change them only in the app's **Advanced** screen.
The web UI shows their state but cannot change them.

## Data usage source

`/v1/usage` samples cumulative mobile bytes from `/proc/net/dev`
(`rmnet_data*`) every 15 seconds and accumulates them into per-day buckets in
`/data/adb/zflip5-modem/usage.json` (mode 0600). Because the counters are
cumulative, every byte lands in some sample, so totals stay exact no matter
how far apart samples are. The 15-second interval only bounds two things: how
precisely a byte attributes to the correct side of a boundary, and how much
traffic an unclean shutdown can lose.

The sampler shortens its own wait so a sample lands 200 milliseconds before
and 200 milliseconds after local midnight, and 200 milliseconds before and
after the next scheduled data-cap reset. The first sample closes the old
bucket; the next one opens the new bucket. A counter reset (reboot) is
reset-safe: the daemon treats the new value as a fresh delta, never a
negative one.

The daemon persists the file at most once a minute, plus immediately after a
counter reset, a day boundary, or a data-cap reset; an unchanged counter
causes no write. Each write fsyncs the temporary file and its directory
before the rename completes, so the persisted state survives a hard reboot or
power loss, not only a clean shutdown. Loss bounds follow from this: a
daemon-only crash loses nothing, because the hardware counters keep counting
and the next sample re-derives the delta from the last saved total; an
API-triggered reboot flushes first, so at most the roughly 2 seconds between
the flush and the reboot itself is at risk; a hard reboot or power loss loses
at most the 60-second persist interval of unsaved traffic.

`today_bytes`, `week_bytes`, `month_bytes`, and `period_bytes` are always
exact byte counts. Their `*_human` counterparts, the app, and the web UI
format them in SI decimal units — 1 GB = 10⁹ bytes — matching carrier
data meters and Android's own data usage screen, not the 1024-based GiB a
filesystem tool would use.

`today_bytes`, `week_bytes`, and `month_bytes` are calendar-aligned in the
device's local time zone (`persist.sys.timezone`), not rolling windows: today
is the local calendar date, week is the ISO week to date (Monday through
now), and month is the calendar month to date (the 1st through now). The
app and the web UI treat `month_bytes` as
month-to-date against a monthly cap, so a rolling 30-day total read high
against that cap. The data cap described in the following section is a
separate meter from these three fields.

### Data cap and reset schedule

The daemon tracks the data cap as a separate meter, `period_bytes`: bytes
used since the last reset, exact from the reset instant. Configure the limit
and schedule from the **Data** screen, or with
`POST /v1/usage/quota` (`radio-control` scope). The body takes `limit_bytes`
(bytes) and `period` (`daily`, `weekly`, `monthly`, or `manual`). It also
takes `reset_time` (device-local `HH:MM`) and `reset_day` (1–28, monthly
only, capped so every month has that day). A `limit_bytes` of `0` means the
owner hasn't set a cap yet, and the app prompts you to set one.

Every period except `manual` resets automatically at the configured time;
`manual` resets only when triggered. `GET /v1/usage` reports the schedule
alongside the meter: `last_reset` and `next_reset` (RFC 3339, device-local),
plus the configured `limit_bytes`, `period`, `reset_time`, and `reset_day`.
The weekly period always resets on Monday, matching `week_bytes`.

Trigger a reset immediately from the Admin card's **Reset data usage**
button (two-tap confirm), or with `POST /v1/usage/reset`. Either way,
`today_bytes`, `week_bytes`, and `month_bytes` stay untouched.

The reset survives a daemon restart: `last_reset` and `period_bytes` persist
to `usage.json` immediately on every reset, never held only in memory. If
the daemon is down when a scheduled boundary passes, the next sample zeroes
the period before adding its delta. Bytes used during the downtime count
toward the new period — the conservative choice for a cap.

Changing the schedule, or upgrading from a build with no cap configured,
re-seeds `period_bytes` from the day buckets. It sums every day on or after
the new anchor date. This re-seed is exact for a midnight reset time. It
slightly overcounts the anchor day for any other time, since day buckets
carry no sub-day detail. Changing `limit_bytes` alone never touches
`last_reset` or `period_bytes`, so an earlier manual reset survives a limit
edit.

The data cap is a meter, not a gate. Reaching `limit_bytes` never stops,
throttles, or otherwise changes the hotspot, radio, or CPU behavior — the
owner decides what to do when a plan runs low.

The daemon retains 40 days of buckets and prunes older ones. Buckets from
before this change keep their original UTC-date keys instead of local-date
keys; the daemon doesn't migrate them, so they age out of the 40-day window
on their own.

## Temperature history

`GET /v1/thermal/history` (`read-status` scope) reports minute, hour, and
day temperature buckets for the **System** screen. The daemon
samples the same 25-second display median that `/v1/status` reports as
`temp_max_c`, once a minute, and stores the buckets in
`/data/adb/zflip5-modem/temps.json` (mode 0600). The thermal gate never
reads this history: `safe` and the ceiling (the configured gate, lifted to
the 70 °C bench trip while bench mode is up) stay computed from the raw,
instantaneous sensor reading.

Every hour and day bucket is the sample-weighted mean of the minute samples
that landed inside it — `sum(temp) / count`, in device-local time. A day
bucket is never the mean of its hour buckets, so a partial hour after a
restart doesn't get a full hour's weight. Each bucket's `n` field reports
its sample count, so a thin bucket after a restart or an outage is visible
instead of silently equal-weighted.

The daemon keeps 24 hours of minute samples, 7 days of hour buckets, and 40
days of day buckets. This is the same 40-day horizon as the data-usage
history, and the daemon prunes older buckets on every write. At full
retention, `temps.json` holds around 26 KB. The daemon writes the file
every 5 minutes, and immediately before an API-triggered device reboot, so
a crash between writes loses at most 5 minutes of samples.

The **System** screen's **Temperature · history** card fetches
`/v1/thermal/history` once a minute, and only while that screen stays open, to
bound mobile data and rate-limit use for a remote Tailscale viewer. Its four
range buttons — **1h**, **24h**, **7d**, and **40d** — switch between the
minute, hour, and day series already in memory, with no new fetch.

