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

A minimal WebView kiosk APK that hard-loads `http://127.0.0.1:18080/` can be
bundled later (helper APK, Todo 6); until then Good Lock + a browser is the
no-code route. Keep the token in the browser's `localStorage` (open the
`?token=` URL once) so shared views never expose it.
