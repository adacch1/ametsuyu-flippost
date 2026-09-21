# Self-hosted control + notifications (ntfy + Telegram bot)

A fully self-hosted plane for the Z Flip 5 modem: a private **ntfy** server for
push notifications and a self-hosted **Telegram bot** that drives the daemon and
sends alerts. Nothing depends on a third-party cloud, and the phone daemon stays
loopback-bound — the only path in is your tailnet.

> **Default bot is now Telegram** — see [docs/telegram-bot.md](../docs/telegram-bot.md).
> The old Discord bot still ships under a compose profile (`--profile discord`);
> the diagram below shows the Discord topology but Telegram is identical (swap
> "Discord ⇄ Gateway" for "Telegram ⇄ long-poll", alerts also go to your chat).
> Related: [docs/ntfy.md](../docs/ntfy.md) · [docs/scrcpy-remote.md](../docs/scrcpy-remote.md).

```
                         your server (on the tailnet)
                     ┌──────────────────────────────────┐
  Discord  ⇄ Gateway │  bot  ──/modem──▶ daemon (phone)  │   phone (Z Flip 5)
 (outbound WS,       │   │   ◀──alerts── poll /v1/status  │  ┌─────────────────┐
  no inbound)        │   │              /v1/usage         │  │ daemon 127.0.0.1 │
                     │   └──publish──▶ ntfy ──subscribe──▶│  │ :18080 (loopback)│
                     │                  ▲                 │  │ tailscale serve  │
                     └──────────────────┼─────────────────┘  └────────┬────────┘
                              iPhone / desktop ntfy app                │
                                        └───────────── tailnet ────────┘
```

## Prerequisites

- A Linux host (home server, NUC, VPS) with Docker + Docker Compose.
- That host **joined to the same tailnet** as the phone: `tailscale up`.
- The phone running `magisk-tailscaled` (already logged in) — see
  [`../docs/tailscale.md`](../docs/tailscale.md).

## 1. Expose the phone daemon on the tailnet (once, on the phone)

The daemon binds loopback only. Publish it to the tailnet with `tailscale serve`
— no public port, ACL-governed:

```sh
su -c 'tailscale serve --bg http://127.0.0.1:18080'
su -c 'tailscale serve status'    # note the https://<node>.<tailnet>.ts.net URL
```

Use that URL as `DAEMON_BASE_URL` in `bot/.env`.

## 2. Configure the bot

```sh
cp bot/.env.example bot/.env
# edit bot/.env: Discord token/app/guild/owner IDs, DAEMON_BASE_URL,
# READ_STATUS + RADIO_CONTROL tokens (from the phone's config.json). NO sms token.
```

Create the Discord app + bot at <https://discord.com/developers/applications>
(*Bot* → Reset Token → copy). Invite it with scopes `bot` + `applications.commands`.

## 3. Bring up ntfy and lock it down

```sh
# set base-url in ntfy/server.yml to THIS host's tailnet IP first
docker compose up -d ntfy

# create an admin user + access token (server is deny-all by default)
docker exec -it zf5-ntfy ntfy user add --role=admin zf5           # set a password
docker exec -it zf5-ntfy ntfy token add zf5                       # prints tk_...
```

Put the `tk_...` token in `bot/.env` as `NTFY_TOKEN`. Grant the topic if you use
a non-admin user: `docker exec -it zf5-ntfy ntfy access zf5 zf5-modem rw`.

## 4. Register commands + start everything

```sh
docker compose run --rm bot node register.js    # one-time /modem registration
docker compose up -d                            # bot + ntfy
docker compose logs -f bot                       # expect "bot ready as ..."
```

In Discord: `/modem status`, `/modem signal`, `/modem clients`, `/modem bands`,
`/modem prefer5g`, `/modem cooldown`. Sensitive replies (network + writes) are
ephemeral. `/modem sms` is refused by design.

## 5. Subscribe to alerts

- **Phone / desktop:** install the ntfy app → add server
  `http://<host-tailnet-ip>:8080`, topic `zf5-modem`, and paste the token
  (Settings → this server → access token).
- **Discord:** set `ALERT_CHANNEL_ID` in `.env`; the bot mirrors every ntfy
  message into that channel.

## What triggers an alert

The bot polls the daemon every 60s and publishes to ntfy **only on state change**
(edge-triggered — no spam):

| Event | ntfy title | Priority |
|---|---|---|
| Thermal leaves SAFE (policy HOT/COOLDOWN, or `thermal.safe=false`) | 🔥 Modem thermal | high |
| Thermal returns to safe | ✅ Modem cooled | default |
| Usage > 90% of the configured cap | 📊 Data cap near | high |

The bots prefer the daemon's own `limit_bytes`/`period_bytes` (the same cap
and meter the dashboard's ring shows). `DATA_CAP_GB` in `.env` (decimal GB)
is the fallback, checked against `month_bytes`, for when no limit is set yet.

## Security notes

- **No public inbound.** The Gateway bot dials out; ntfy is tailnet-only; the
  daemon is loopback + `tailscale serve`. Nothing is exposed to the internet.
- **Scope split, no SMS.** Reads use `read-status`; `tether/prefer5g/cooldown`
  use `radio-control`; both are thermally gated on the daemon. The bot holds no
  `sms` token and refuses `/modem sms` — SMS never traverses Discord.
- **Owner allowlist**, default-deny, on `OWNER_USER_IDS`.
- **Secrets stay local.** `bot/.env` is git-ignored. Rotate a leaked daemon token
  by editing the phone's `config.json` and restarting the daemon; rotate the bot
  token in the Developer Portal.

## Relation to `relay/`

`relay/relay.js` is the **webhook** alternative (Discord → your public HTTPS URL,
good for serverless/Cloudflare Workers). This `selfhost/` bot is the **Gateway**
alternative (no public inbound), which fits a box you already self-host. Pick one;
they hit the same daemon with the same safety rules. See
[`../docs/discord-bot.md`](../docs/discord-bot.md).
