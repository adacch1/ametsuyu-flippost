# Admin dashboard

The daemon serves a self-contained dashboard at `/` (and `/dashboard`) showing
battery, temperature, CPU load, memory, **mobile data usage (today / week /
month)**, thermal-policy state, network type, and ingress mode. It auto-refreshes
every 5 s and is styled for the Z Flip 5 cover screen (compact, dark).

## Open it

The page needs a `read-status` token once; it stores it in `localStorage` and
strips it from the URL:

```
# local (USB)
adb forward tcp:18080 tcp:18080
open "http://127.0.0.1:18080/?token=$READ_STATUS"
```

Over Tailscale (see docs/tailscale.md), `tailscale serve` exposes the same page
to your other devices:

```
https://zflip5.<tailnet>.ts.net/?token=$READ_STATUS
```

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

`/v1/usage` samples cumulative mobile bytes from `/proc/net/dev` (`rmnet_data*`),
accumulates per-day buckets in `/data/adb/zflip5-modem/usage.json` (0600), and is
reboot-safe (a counter reset is treated as a fresh delta, never negative). A
background sampler runs every 60 s so day/week/month accrue even when nobody is
watching. Because buckets accrue from install time, week/month fill in over the
first days of use.

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
