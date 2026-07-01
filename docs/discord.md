# Discord relay — setup & deploy (stubbed)

A tiny public relay verifies Discord interactions and forwards approved commands
to the phone daemon over the **private tailnet only**. It holds **no `sms`
token** and refuses the SMS subcommand — SMS never traverses Discord.

## What the relay does

`relay/relay.js` (runtime-agnostic handler): Ed25519 verify + ≤5 s timestamp
window → PING/PONG → owner user-ID allowlist (default-deny) → route `/modem
status|thermal|battery|network|tether|prefer5g|cooldown` to the daemon; sensitive
replies (network/writes) are ephemeral. `/modem sms` returns an ephemeral refusal.

## Register the slash command (once)

Create a Discord application, add a `modem` command with a subcommand for each
route above. Set the Interactions Endpoint URL to your deployed relay; Discord
sends a signed PING which the relay answers with PONG.

## Deploy (fill in YOUR secrets — not committed)

1. Copy `relay/config.example.json` → `relay/config.json` and set:
   - `discord_public_key` — the app's Ed25519 public key (Developer Portal).
   - `owner_user_ids` — your Discord user ID(s).
   - `daemon_base_url` — the daemon reachable over the tailnet (e.g. the relay
     host joins the tailnet; never a public phone port).
   - `daemon_tokens.read-status` / `.radio-control` — from the phone's
     `/data/adb/zflip5-modem/config.json`. **Do not add an `sms` token.**
2. Host options: Cloudflare Worker (port the handler to `fetch` export) or a
   small always-on VPS on the tailnet. Either way the relay's only outbound
   reach to the phone is through Tailscale.
3. Set the Interactions Endpoint URL in the Developer Portal; confirm PONG.

## Verify locally

```
bash tools/test-discord-relay.sh --out .omo/evidence/task-9-zflip5-modem-module.txt
```

Proves PING→PONG, good command routing, bad-signature 401 (no daemon call),
stale-timestamp 401, unauthorized-user denial, and SMS-over-Discord refusal.
