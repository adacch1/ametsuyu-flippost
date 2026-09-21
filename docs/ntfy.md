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
