# Admin dashboard

The daemon serves two self-contained pages:

- `/` — the **cover screen**: clock and date, mobile data used this month
  against the cap, battery, WAN IP, and three actions (hotspot toggle, IP
  rotate, **Refresh status**). Sized for the Z Flip 5 Flex Window (352×308 px),
  which is what the kiosk WebView loads. Polls every 10 s.
- `/dashboard` — the **control panel**: seven tabs covering battery,
  temperature, CPU load, memory, **mobile data usage (today / week / month)**,
  thermal-policy state, network type, signal detail, clients, presets, the
  Inbox, and ingress mode. See [Refresh cadence](#refresh-cadence) for how it
  stays current.

The cover screen is an always-on display. The kiosk holds the panel awake while
it is in front, and `service.sh` stops the phone sleeping on a charger
(`stay_on_while_plugged_in`), which `uninstall.sh` puts back. Because a lit OLED
showing a fixed layout burns in, the panel runs dim while the kiosk owns it and
the deck shifts a few pixels every minute. To tune the brightness, set
`cover.dim` in `config.json` to 1-255, or to 0 to keep your own level.

The cover screen runs on a true-black ground with dimmed surfaces, because the
Flex Window stays lit for as long as the phone is closed and an OLED panel spends
no power on black pixels. To recolor its buttons, open **Settings > Cover screen
buttons** on the control panel and pick one of the seven accents; the kiosk
picks up the change within ten seconds. The choice persists in `cover.accent`.

Both pages are dark and compact. To pull live usage, battery, and IP without
waiting for the next poll, press **Refresh status** — the floating button at
the bottom right of the control panel, or the button in the cover screen's
action row.

To reach the control panel from the cover screen, tap the grid icon in the
header.

## Refresh cadence

The control panel refreshes each field on a schedule matched to how often its
value actually changes, instead of re-fetching every field on one fixed
interval. Three tiers cover it: the header status and signal, mobile data
usage, hotspot state, and the client list refresh every 5 seconds; the CPU
governor and the Settings tab's usage summary refresh every 15 seconds; band
lock, USB tethering, and the temperature-history chart refresh every 60
seconds, because those three reads cost the most on the phone and rarely
change between polls.

A hotspot "forced on" countdown updates every second from a value the browser
computes locally, so it counts down between polls without an extra request.
The panel stops polling while its browser tab stays hidden, and it refreshes
immediately once the tab becomes visible again. **Refresh status** still
forces every visible field to update right away, regardless of its schedule.

## Open it

The page needs a `read-status` token once; it stores it in `localStorage` and
strips it from the URL:

```
# local (USB)
adb forward tcp:18080 tcp:18080
open "http://127.0.0.1:18080/?token=$READ_STATUS"           # cover screen
open "http://127.0.0.1:18080/dashboard?token=$READ_STATUS"  # control panel
```

Over Tailscale (see [Tailscale ingress](tailscale.md)), `tailscale serve` exposes
the same pages to your other devices:

```
https://zflip5.<tailnet>.ts.net/dashboard?token=$READ_STATUS
```

The **Add a device** QR in Settings encodes the control-panel URL, so a scanned
device lands on the panel rather than the cover screen.

The HTML itself carries no data — every number is fetched from the token-gated
`/v1/*` API, so an unauthenticated viewer sees an empty shell.

## Inbox (messages + notifications)

The **Inbox** tab reads the phone's SMS inbox (`/v1/sms/recent`, `content query`
on `content://sms/inbox`) and its active notification shade
(`/v1/notifications/recent`, root `dumpsys notification --noredact`). Both are
pull-only: nothing can be sent, replied to, dismissed or forwarded from here.

Bodies and titles are served **verbatim** — nothing masked, nothing truncated
— because this is a donor phone the owner reads directly (`redactBodies = false`
in `daemon/sms.go`; flip that const to restore the masked 120-char previews).
Rows show two lines and expand on tap. Only the *active* shade is parsed; the
dismissed `History Notification List:` block is skipped.

Both endpoints use the `sms` scope, which on this donor phone also follows the
**Open reads** toggle: with it on, the tab loads with no token at all (the
daemon embeds the `sms` token in the page and serves the two endpoints
tokenless). With it off, paste the `sms` token once into the field at the bottom
of the tab. `sms.enabled: false` in `config.json` turns the whole tab off.

Reads are rate-limited (`rate_limits.sms_per_min`, 3/min by default), so the tab
refreshes at most once every 20 s on open; "Refresh" forces one.

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
exact byte counts. Their `*_human` counterparts, the dashboard, and the cover
screen format them in SI decimal units — 1 GB = 10⁹ bytes — matching carrier
data meters and Android's own data usage screen, not the 1024-based GiB a
filesystem tool would use.

`today_bytes`, `week_bytes`, and `month_bytes` are calendar-aligned in the
device's local time zone (`persist.sys.timezone`), not rolling windows: today
is the local calendar date, week is the ISO week to date (Monday through
now), and month is the calendar month to date (the 1st through now). The
dashboard, the cover screen, and both bots already treat `month_bytes` as
month-to-date against a monthly cap, so a rolling 30-day total read high
against that cap. The data cap described in the following section is a
separate meter from these three fields.

### Data cap and reset schedule

The daemon tracks the data cap as a separate meter, `period_bytes`: bytes
used since the last reset, exact from the reset instant. Configure the limit
and schedule from the dashboard's **Data limit** settings card, or with
`POST /v1/usage/quota` (`radio-control` scope). The body takes `limit_bytes`
(bytes) and `period` (`daily`, `weekly`, `monthly`, or `manual`). It also
takes `reset_time` (device-local `HH:MM`) and `reset_day` (1–28, monthly
only, capped so every month has that day). A `limit_bytes` of `0` means the
owner hasn't set a cap yet, and the dashboard prompts for one on the home
screen and in Settings.

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
day temperature buckets for the dashboard's **System** tab. The daemon
samples the same 14-second display median that `/v1/status` reports as
`temp_max_c`, once a minute, and stores the buckets in
`/data/adb/zflip5-modem/temps.json` (mode 0600). The thermal gate never
reads this history: `safe` and the 48 °C ceiling stay computed from the
raw, instantaneous sensor reading.

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

The System tab's **Temperature · history** card fetches
`/v1/thermal/history` on the 60-second slow tier described in
[Refresh cadence](#refresh-cadence), and only while that tab stays open, to
bound mobile data and rate-limit use for a remote Tailscale viewer. Its four
range buttons — **1h**, **24h**, **7d**, and **40d** — switch between the
minute, hour, and day series already in memory, with no new fetch.

## Always-on the cover screen (Z Flip 5)

Samsung does not let arbitrary apps run on the cover screen out of the box.
Recommended path:

1. Install **Good Lock** → **MultiStar** → enable "I ♥ Galaxy Foldable" /
   "Cover screen apps" (lets you whitelist any app on the cover display).
2. Whitelist a lightweight browser (or a WebView kiosk app) on the cover screen.
3. Point it at the dashboard URL above (local loopback or the Tailscale URL) and
   pin it. Keep the screen-timeout long or use an always-on setting.

The WebView kiosk APK now ships: `helper/build-apk.sh` builds
`dist/zflip5-kiosk.apk` (`CoverKioskActivity`, fullscreen, shows-when-locked),
and the Magisk **Action** button launches it on the cover screen via
`magisk/action.sh`. Good Lock + a browser remains a no-code fallback. The token
is seeded once via the `?token=` URL and kept in the WebView's `localStorage`,
so shared views never expose it.
