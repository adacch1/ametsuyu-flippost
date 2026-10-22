# Tailscale

## How the module uses Tailscale

The phone sits behind carrier CGNAT, so it has no public inbound address. The module reaches your tailnet through a userspace `tailscaled` and never opens a port to the internet.

The daemon binds loopback (`127.0.0.1`) only. `tailscale serve` is the sole bridge between your tailnet and the daemon, and your tailnet ACL decides who can reach it.

The daemon talks to `tailscaled` over its LocalAPI socket at `/data/adb/tailscale/tailscaled.sock`. The module needs no auth key, bundles no Tailscale binaries, and has no ingress setting.

## Check status

Send `GET /v1/tailscale` to read the state, IP address, DNS name, and serve status. The response never contains node keys or login URLs.

The `state` field takes one of these values: `running`, `needs_login`, `needs_machine_auth`, `stopped`, `starting`, `not_running`, `in_use_other_user`, or `error`.

## Sign in and sign out from the app

Only the Android app can sign in or out. The login and logout endpoints are owner-only, and any other caller gets 403 with the reason `app_only`. A browser on your tailnet never sees a login URL.

To sign in:

1. Open the app and go to the Tailscale card.
2. Tap **Sign in**. The daemon asks `tailscaled` for a login URL and returns it to the app.
3. Finish the sign-in in the browser that the app opens.

After sign-in completes, the daemon re-applies `tailscale serve` so that `https://<name>.<tailnet>.ts.net` proxies to `http://127.0.0.1:18080`. The daemon keeps any other serve entries you configured.

When you sign out, the daemon saves your hostname and routing prefs to `/data/adb/zflip5-modem/tailscale-prefs.json` with mode `0600`. The file never contains the node key. The daemon restores these prefs at the next sign-in.

A sign-in request fails with a reason code in these cases:

| Reason | Meaning |
|---|---|
| `busy` | Another sign-in is in progress. |
| `not_running` | `tailscaled` is not running. |
| `needs_machine_auth` | An admin must approve the device. |
| `no_login_url` | `tailscaled` returned no login URL within 20 seconds. |
| `localapi` | `tailscaled` rejected a request. |

## Reach the web UI

After sign-in, open `https://<name>.<tailnet>.ts.net` from any device on your tailnet. The iPhone Shortcuts in `docs/shortcuts.md` and the Discord relay in `docs/discord.md` use the same address.

Because `tailscale serve` forwards requests from loopback, the daemon treats tailnet callers as remote and keeps them read-only for sign-in and sign-out.

## Troubleshoot

- **`needs_machine_auth`**: Approve the device in the Tailscale admin console, then check the status again.
- **`not_running`**: Confirm that `tailscaled` is running and that the socket exists at `/data/adb/tailscale/tailscaled.sock`.
- **`serve_ok` is false**: Sign in again, or run `tailscale serve` against `http://127.0.0.1:18080`.
- **Verify no public exposure**: Run `ss -ltn` as root on the device. The daemon must listen on `127.0.0.1` only. `tools/security-check.sh --device` checks this too.
- **Ephemeral state**: The login URL is ephemeral. The daemon never stores it, so request a new one if it expires.
