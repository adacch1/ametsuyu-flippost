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

SMS is refused over Telegram (no `sms` token given to the bot), same as Discord.

Alerts (edge-triggered, no spam) go straight to the owner chat **and** optionally
to ntfy: device hot / cooled, monthly data-cap near, and daemon-unreachable
(~3 min → likely wedged). Set `AUTOREBOOT_HHMM=04:30` for a daily auto-reboot.

## Setup
1. **Create the bot:** message [@BotFather](https://t.me/BotFather) → `/newbot`,
   copy the token.
2. **Find your chat id:** start the bot (below), send it any message; it replies
   with your chat id. Put it in `OWNER_CHAT_IDS` (comma-separated for multiple).
   Anyone not listed is refused.
3. **Config:** `cp selfhost/telegram-bot/.env.example selfhost/telegram-bot/.env`
   and fill `TELEGRAM_TOKEN`, `OWNER_CHAT_IDS`, `DAEMON_BASE_URL` (the phone's
   `tailscale serve` URL), `READ_STATUS`, `RADIO_CONTROL`. Never the `sms` token.
4. **Run:** `cd selfhost && docker compose up -d telegram ntfy`
   (the legacy Discord bot stays off unless you `--profile discord up`).

The host must be on the same tailnet as the phone (`tailscale up`) so it can
reach the daemon's `tailscale serve` URL. The daemon itself stays loopback-only.

## Security
- Owner-gated by Telegram chat id; leave `OWNER_CHAT_IDS` set (blank = anyone who
  finds the bot can control it — testing only).
- The bot holds `read-status` + `radio-control`, never `sms`. Thermal safety and
  the 48 °C gate ceiling are enforced by the daemon regardless of what the bot asks.
- `/reboot` and scheduled auto-reboot use `radio-control`; the reboot survives via
  the module's late-start service (verified).
