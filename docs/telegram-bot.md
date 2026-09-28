# Telegram bot

Self-hosted Telegram bot that controls the Z Flip 5 modem daemon. Replaces the
Discord bot with the same command set plus airplane/IP-rotate, whole-device
reboot, and scheduled auto-reboot. Lives in `selfhost/telegram-bot/`.

## Why Telegram
- **Long-polling (`getUpdates`)** — outbound only, no public inbound, no webhook
  to expose. Same "phone stays private" posture as the Discord Gateway bot.
- One-to-one with the owner; alerts land directly in your chat (no server/guild).
- First-class iOS + Android apps for both control and push.

## What it does
Reads (`READ_STATUS` token): `/status /ip /signal /battery /thermal /clients
/usage /network /cpu /bands /hotspot`. `/raw <cmd>` returns the full JSON of any
read.

Writes (`RADIO_CONTROL` token; thermal-gating enforced by the daemon):
- `/airplane on|off|cycle` — quick radio toggle; **cycle** rotates the carrier IP
  (airplane off→on→off, waits for data, restarts the hotspot, reports old→new IP).
- `/tether start|stop` — data hotspot.
- `/cooldown` — force the CPU into eco now.
- `/prefer5g` — report the current allowed network types.
- `/reboot` — reboot the whole phone (module restores the daemon + hotspot on boot).

The bot refuses the `/sms` command, same as the Discord bot.

## Alerts

The bot polls the daemon every 60 seconds and pushes each change once to the
owner chat, and to ntfy when `NTFY_URL` is set:

| Alert | Fires when | Anti-flap |
|---|---|---|
| Temperature | The daemon's policy enters or leaves `HOT`/`COOLDOWN`. | The daemon's own gate hysteresis. |
| Battery low | The level drops to `BATTERY_LOW_PCT` (default 20) while unplugged. | Rearms only once the level passes the limit plus 5 %. |
| Power | The charger state changes, for example `ac` to `unplugged`. | None; each change is real. |
| Signal | The network type changes (5G to 4G), or bars reach or leave 0. | The new state must hold for 2 polls. |
| Hotspot | The SSID-allowlist auto-toggle starts or stops the hotspot, or pauses. | The controller's 2-scan debounce. |
| Data cap | Usage passes 90 % of the cap. | Fires once per crossing. |
| SMS | A new message arrives, with `SMS_READ` set. | The bot announces each message once. |
| Unreachable | The daemon fails for about 3 minutes. | Fires once, then once on recovery. |

The bot announces nothing on its first poll, so a restart doesn't repeat alerts
for conditions that are already true. The rules live in
`selfhost/telegram-bot/alerts.js`. To test them, run
`node --test` in `selfhost/telegram-bot`.

Set `AUTOREBOOT_HHMM=04:30` for a daily auto-reboot.

### SMS alerts

SMS alerts are off by default. To turn them on, put the daemon's `sms` token in
`SMS_READ`. The bot then posts each new message's sender and full body.

**Warning:** SMS alerts copy message text, one-time codes included, to
Telegram's servers and to ntfy. Leave `SMS_READ` blank if that's not acceptable.
The daemon stays pull-only (`sms.forward` stays `false`); the bot pulls.

## Setup
1. **Create the bot:** message [@BotFather](https://t.me/BotFather) → `/newbot`,
   copy the token.
2. **Find your chat id:** start the bot (below), send it any message; it replies
   with your chat id. Put it in `OWNER_CHAT_IDS` (comma-separated for multiple).
   Anyone not listed is refused.
3. **Config:** `cp selfhost/telegram-bot/.env.example selfhost/telegram-bot/.env`
   and fill `TELEGRAM_TOKEN`, `OWNER_CHAT_IDS`, `DAEMON_BASE_URL` (the phone's
   `tailscale serve` URL), `READ_STATUS`, `RADIO_CONTROL`. Add `SMS_READ` only
   if you want SMS alerts; see [SMS alerts](#sms-alerts).
4. **Run:** `cd selfhost && docker compose up -d telegram ntfy`
   (the legacy Discord bot stays off unless you `--profile discord up`).

The host must be on the same tailnet as the phone (`tailscale up`) so it can
reach the daemon's `tailscale serve` URL. The daemon itself stays loopback-only.

## Security
- Owner-gated by Telegram chat id; leave `OWNER_CHAT_IDS` set (blank = anyone who
  finds the bot can control it — testing only).
- The bot holds `read-status` and `radio-control`, plus `sms` only when you
  opt into [SMS alerts](#sms-alerts). It never serves SMS as a chat command. Thermal safety and
  the 48 °C gate ceiling are enforced by the daemon regardless of what the bot asks.
- `/reboot` and scheduled auto-reboot use `radio-control`; the reboot survives via
  the module's late-start service (verified).
