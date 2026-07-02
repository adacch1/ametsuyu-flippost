# How to hook the module with a Discord bot

There are two ways to drive the phone daemon from Discord. Pick by where you can
run code and whether you want a public HTTPS endpoint.

| | **A. Interactions webhook** (`relay/relay.js`) | **B. Self-hosted Gateway bot** (`selfhost/bot`) |
|---|---|---|
| Transport | Discord → **your public HTTPS URL** | Bot → **outbound** WebSocket to Discord |
| Needs public inbound? | Yes (Cloudflare Worker / tunnel) | **No** — works behind CGNAT/NAT |
| Reaches phone via | Tailscale | Tailscale |
| Best for | serverless, no always-on host | a box you already self-host (matches ntfy) |

Both share the **same safety model**: only `read-status` and `radio-control`
scopes, **never `sms`** over Discord, owner allowlist (default-deny), and the
phone daemon stays loopback-bound — the only bridge is Tailscale.

## The bridge: expose the loopback daemon on your tailnet (once)

The daemon binds `127.0.0.1:18080` on purpose. To let a bot (on the same tailnet)
reach it, publish it with `tailscale serve` on the phone — no public port:

```sh
# on the phone (root). magisk-tailscaled is already logged in.
su -c 'tailscale serve --bg http://127.0.0.1:18080'
su -c 'tailscale serve status'     # shows https://<node>.<tailnet>.ts.net
```

Result: `https://samsung-sm-f731b.tail951eed.ts.net` → the daemon, reachable only
by devices in your tailnet (ACL-governed). That URL + a scoped bearer token is
what the bot uses as `DAEMON_BASE_URL`.

> Prefer this over binding the daemon to the tailnet IP: `serve` keeps the bind
> loopback-only and lets the tailnet ACL, not an open port, decide who connects.

## Option A — Interactions webhook relay (already in the repo)

`relay/relay.js` verifies Ed25519 + a ≤5s replay window, answers PING, enforces
the owner allowlist, and routes `/modem status|thermal|battery|network|tether|
prefer5g|cooldown` to the daemon. Deploy it as a Cloudflare Worker or a small
tailnet VPS, then set the **Interactions Endpoint URL** in the Discord Developer
Portal. Full steps in [`discord.md`](discord.md). Use this if you don't want an
always-on host.

## Option B — Self-hosted Gateway bot (recommended for self-hosting)

A long-lived bot process logs into the Discord **Gateway** over an outbound
WebSocket, so it needs **no public inbound** — ideal on a home server behind
CGNAT, the same box that runs your self-hosted ntfy. It registers the `/modem`
slash commands, calls the daemon over Tailscale, and (optionally) forwards
self-hosted **ntfy** alerts into a Discord channel.

Everything you need is in **[`selfhost/`](../selfhost/)**:

```
selfhost/
  docker-compose.yml      # ntfy + the bot, one `docker compose up -d`
  bot/index.js            # Gateway bot: slash commands -> daemon, ntfy -> Discord
  bot/register.js         # one-time slash-command registration
  bot/.env.example        # bot token, daemon URL + tokens, ntfy topic
  ntfy/server.yml         # self-hosted ntfy config
```

### 5-minute setup

1. **Create the app + bot** at <https://discord.com/developers/applications> →
   *Bot* → copy the **token** (this is the Gateway credential; keep it secret).
   Under *Installation* / *OAuth2*, invite it to your server with the
   `applications.commands` + `bot` scopes.
2. **Fill secrets:** copy `selfhost/bot/.env.example` → `.env`; set
   `DISCORD_TOKEN`, `DISCORD_APP_ID`, `DISCORD_GUILD_ID`, `OWNER_USER_IDS`,
   `DAEMON_BASE_URL` (the `tailscale serve` URL above), and the daemon
   `READ_STATUS` / `RADIO_CONTROL` tokens from the phone's
   `/data/adb/zflip5-modem/config.json`. **Do not set an SMS token.**
3. **Join the server box to the tailnet** (`tailscale up`) so it can reach the
   phone URL.
4. **Register commands + run:**
   ```sh
   cd selfhost && docker compose run --rm bot node register.js   # once
   docker compose up -d                                          # bot + ntfy
   ```
5. In Discord: `/modem status`, `/modem signal`, `/modem clients`, `/modem
   prefer5g` … Sensitive replies (network/writes) are ephemeral. `/modem sms` is
   refused by design.

### What the bot enforces (same as the relay)

- **Owner allowlist** — `OWNER_USER_IDS`, default-deny.
- **Scope split** — reads use `read-status`; `tether`/`prefer5g`/`cooldown` use
  `radio-control`; both are thermally gated **on the daemon**, not the bot.
- **No SMS** — the bot holds no `sms` token and refuses `/modem sms`.
- **Private path only** — all daemon calls go to the tailnet URL; if Tailscale is
  down the bot replies "daemon unreachable", never falls back to a public route.

See [`selfhost/README.md`](../selfhost/README.md) for the ntfy half (alerts) and
how the phone publishes thermal/data notifications into it.
