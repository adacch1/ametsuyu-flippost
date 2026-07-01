# iPhone Shortcuts — modem control recipes

These recipes drive the modem daemon over the **private Tailscale path**
(`tailscale serve` exposes the loopback daemon to your tailnet only — no public
phone port, no port-forward). The base URL is your phone's MagicDNS name, e.g.
`https://zflip5.tailnet-name.ts.net`.

## Token handling (do NOT hard-code)

Store each scoped token in a Shortcut **Text** action saved to an **iCloud
Keychain / imported variable**, never inline in shared shortcut text or a
screenshot. Recommended: one shortcut "Modem: Set Tokens" writes the tokens to a
dictionary in a **Data Jar** / **Keychain** entry; every command shortcut reads
from there. Scopes:

| Token | Reaches |
| --- | --- |
| `read-status` | status, health, thermal, network, battery |
| `sms` | sms recent (owner-only, redacted) |
| `radio-control` | tether, prefer5g, cooldown, restart |

## Request pattern (Get Contents of URL)

Every command is the same shape:

- **URL**: `{{BASE}}{{path}}`
- **Method**: `GET` (reads) or `POST` (writes)
- **Headers**: `Authorization: Bearer {{token}}`  (+ `Content-Type: application/json` for POST)
- **Request Body** (POST): JSON (see per-command below)

## Commands

| Shortcut | Method | Path | Token | Body | Expect |
| --- | --- | --- | --- | --- | --- |
| Status | GET | `/v1/status` | read-status | — | 200 JSON summary |
| Health (CPU/RAM/temp) | GET | `/v1/health` | read-status | — | 200 JSON metrics |
| Thermal | GET | `/v1/thermal` | read-status | — | 200 JSON gate state |
| Network | GET | `/v1/network` | read-status | — | 200 JSON |
| Battery | GET | `/v1/battery` | read-status | — | 200 JSON |
| SMS recent | GET | `/v1/sms/recent` | sms | — | 200 redacted list |
| Tether | POST | `/v1/tether` | radio-control | `{"state":"on"}` | 200 / 409 if unsafe |
| Prefer 5G | POST | `/v1/prefer5g` | radio-control | `{"enable":true}` | 200 / 409 if unsafe |
| Cooldown | POST | `/v1/cooldown` | radio-control | `{}` | 200 |
| Restart service | POST | `/v1/service/restart` | radio-control | `{}` | 200 |

Writes are **thermal-gated**: when the device is in an unsafe thermal state the
daemon replies `409` with `{"error":"refused: unsafe thermal state"}` — the
Shortcut should surface that message, not retry.

## Validate the recipes

`tools/replay-shortcuts.sh` replays every request above against a running daemon
and checks the status codes, so the docs can't drift from the API:

```
bash tools/replay-shortcuts.sh --base-url http://127.0.0.1:18080 \
  --token "$READ_STATUS" --sms-token "$SMS" --radio-token "$RADIO" \
  --out .omo/evidence/task-8-zflip5-modem-module.txt
```

Omitting `--sms-token`/`--radio-token` asserts the sensitive endpoints are
denied (403) — proving the read-status token in a shared shortcut can't reach
SMS or radio control.
