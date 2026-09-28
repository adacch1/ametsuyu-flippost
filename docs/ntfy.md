# ntfy — the self-hosted push-notification server

## What it is
[ntfy](https://ntfy.sh) is a tiny pub/sub notification server. You **publish** a
message to a *topic* with a plain HTTP POST, and any device **subscribed** to that
topic gets a push notification. No accounts, no Google/Apple push dependency for
self-hosted use — the ntfy app holds an outbound connection to your server.

In this project ntfy is the "something happened" channel for the modem:
device-hot, device-cooled, data-cap-near, daemon-unreachable. The Telegram bot
publishes these (it also messages your Telegram chat directly); ntfy is the
extra fan-out to phones/watches/desktops that have the ntfy app.

## How it's set up here (`selfhost/`)
- **Container:** `binwiederhier/ntfy` via `docker-compose.yml` (service `ntfy`).
- **Private by default:** `ntfy/server.yml` sets `auth-default-access: deny-all`
  — nobody can publish or subscribe without a token grant. Not an open relay.
- **Not exposed publicly:** the port is bound to `127.0.0.1:8080` on the host;
  reach it over the tailnet with `tailscale serve`, never on a public/all-interfaces bind.
- **Retention:** `cache-duration: 168h` (7 days of message history in the topic).

## First-run: create a token
```sh
cd selfhost && docker compose up -d ntfy
docker exec -it zf5-ntfy ntfy user add --role=admin zf5      # set a password
docker exec -it zf5-ntfy ntfy access zf5 "zf5-modem" rw      # grant rw on the topic
docker exec -it zf5-ntfy ntfy token add zf5                  # prints a token
```
Put that token in `selfhost/telegram-bot/.env` as `NTFY_TOKEN`, and in the ntfy
app on your phones so they can subscribe to the private topic.

## Publish to more than one server
The Telegram bot sends every alert to each server in `NTFY_URL`, a
comma-separated list. This install uses two servers:

| Server | Host | Reachability |
|---|---|---|
| VPS | `https://ntfy.ametsuyu.net` | Public (US VPS); anonymous access returns `403`. |
| Homelab | `https://ntfy-home.ametsuyu.net` | Private (`10.73.20.209`); the bot host needs a route to it. |

```sh
NTFY_URL=https://ntfy.ametsuyu.net,https://ntfy-home.ametsuyu.net
NTFY_TOPIC=zf5-modem
NTFY_TOKEN=tk_vps,tk_home   # or a single token that both servers accept
```

The bot strips the Telegram formatting, so ntfy shows plain text. A failed
server logs `ntfy <url>: ...` and doesn't block the other server or Telegram.

**Warning:** With [SMS alerts](telegram-bot.md#sms-alerts) on, message bodies
reach both servers. Keep `auth-default-access: deny-all` on each one.

## Subscribe from a phone (iOS / Android)
1. Install the **ntfy** app (App Store / Play Store / F-Droid).
2. Settings → Default server → your tailnet URL (e.g.
   `https://<host>.tailXXXX.ts.net`), and add the access token.
3. Subscribe to topic **`zf5-modem`** (matches `NTFY_TOPIC`).
   Alerts now push to that device. Web subscribe also works at
   `https://<host>.ts.net/zf5-modem` in any browser.

## Publish manually (test / your own alerts)
```sh
curl -H "Authorization: Bearer $NTFY_TOKEN" \
     -H "Title: test" -H "Priority: high" -H "Tags: fire" \
     -d "hello from the modem" \
     https://<host>.ts.net/zf5-modem
```
Headers `Title`, `Priority` (min/low/default/high/urgent), `Tags` (emoji
shortcodes), `Click` (deep link), `Actions` (buttons) shape the notification.

## ntfy vs the Telegram bot
They overlap on alerts by design:
- **Telegram** — two-way: you *send commands* and *get replies + alerts* in one
  chat. Primary control surface.
- **ntfy** — one-way push only, but reaches devices where you don't want a
  Telegram login (a spare phone, a watch, a wall tablet, a browser tab), and
  supports priority/tags/action-buttons natively.

Run both, or drop ntfy: the Telegram bot alerts your chat directly even with
`NTFY_URL` unset. The Discord bot's old ntfy→Discord bridge is gone with the
switch to Telegram.

## Device-local ntfy-only deployment (2026-09-24)

The connected SM-F731B runs the existing alert rules directly through its installed
Termux Node runtime. `NTFY_ONLY=true` starts polling without Telegram credentials.
SMS fetching, Telegram, radio-control commands, and automatic reboot are disabled
in this mode. The runner reads only the daemon's `read-status` token at startup.

- Runner: `/data/adb/modules/zflip5_modem/ntfy/launch.cjs`.
- Boot/watchdog: `/data/adb/service.d/60-zf5-ntfy.sh` (source `magisk/ntfy-service.sh`).
- Private configuration: `/data/adb/zflip5-modem/ntfy.json`, mode0600, containing
  `servers: [{url, token}, ...]`. Do not put publishing tokens in Git.
- Log: `/data/adb/zflip5-modem/ntfy.log` (bounded).
- Both existing servers receive topic `zf5-modem`, with a separate write-only
  publisher account/token on each. Existing phone logins can read this topic.

Subscribe to **zf5-modem** on both existing server accounts in your ntfy apps.
The first poll seeds state; subsequent changes generate alerts. Polls never
run concurrently. Each server publishes independently with a15-second timeout.
Failed publishes are logged, not durably queued by this runner.

A device-local runner can report a stalled modem daemon while Android/networking
still work. It cannot report its own complete power or network loss; that needs
an external check. Existing homelab/backup monitoring remains separate.

To update this integration without rebooting or replacing the modem daemon, push
`selfhost/telegram-bot/{index.js,alerts.js,package.json}` and
`magisk/ntfy/launch.cjs` to the runner directory using USB ADB and root; preserve
the private config. The boot script requires the existing Termux installation.
Run tests with `node --test selfhost/telegram-bot/*.test.js`.

Deployment verified via USB ADB serial R5CW80J9SVF: live daemon HTTP200;
on-device test publication accepted by both servers; ntfy-only runner and
Magisk boot watchdog running. Eleven Node tests pass, including independent
multi-server delivery when one endpoint fails. No device reboot was performed.

Power and low-battery checks now use a separate three-second loop, so slow
status/signal requests cannot delay them. Other alerts retain the one-minute
cadence. Power baselines and transitions are timestamped in ntfy.log. A new
integration test verifies power-loss delivery with the main status endpoint
stalled. Physical unplug/replug confirmation is pending.
