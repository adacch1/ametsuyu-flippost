# Architecture

Ametsuyu Flippost turns a rooted Samsung Galaxy Z Flip 5 (SM-F731B) into a
controllable cellular modem and Wi-Fi 6 hotspot. You operate it from a native
app, a home-screen widget, and a web UI over Tailscale.

## Components

| Path | What | Runtime |
| --- | --- | --- |
| `daemon/` | Root Go daemon: loopback HTTP API, thermal gate, CPU policy, data meter, hotspot control, and the web UI | Static Go, arm64 |
| `helper/` | The native app (`com.zflip5.modem`) with its widget, and the `tether.jar` root helpers | Kotlin and Gradle |
| `magisk/` | `service.sh`, `firewall.sh`, `cleanup.sh`, `app-install.sh`, the ntfy runner, and the packaged daemon and APK | Magisk module |
| `schemas/` | `api.openapi.json` and `config.schema.json` | None |
| `tools/` | Build, packaging, lint, test, and `dex.sh` | Host |

## Front ends

Three front ends share one API:

- **Native app and widget.** Kotlin and Compose with the Flippost look. The
  app talks to the daemon on loopback and reads `/v1/summary` for the home
  screen. It is the only front end that can sign Tailscale in or out and
  change the open reads and open control switches.
- **Web UI.** A single page the daemon serves at `/`. Tailscale reaches it
  through `tailscale serve`. See [Web UI](dashboard.md).
- **ntfy runner.** A device-local Node process that reads `/v1/summary` and
  publishes alerts. See [ntfy alerts](ntfy.md).

## Trust boundaries

- **The loopback bind is not a trust boundary.** Any installed app shares
  `127.0.0.1`. The boundaries are the scoped bearer tokens, the app-UID check,
  and the module firewall.
- **App-UID authentication.** The daemon reads the caller's UID from the
  socket. Only the app's UID passes the `app_only` guard. UID 0 is never
  trusted, because `tailscale serve` traffic arrives as UID 0. A caller over
  `adb forward` arrives as UID 2000 and also fails the guard.
- **Loopback only.** The daemon refuses any other bind address. Remote access
  is a private Tailscale tunnel, never a port forward.
- **Module firewall.** `firewall.sh` drops port 5555 and the remote-screen
  ports on every interface except `lo` and `tailscale0`, for IPv4 and IPv6.
  See the [Threat model](threat-model.md).

## Endpoints

`schemas/api.openapi.json` lists every route, and a test pins the spec to the
router. Read routes need the `read-status` scope. Write routes need
`radio-control`. The Tailscale login and logout routes need the app UID.

`/v1/summary` returns the fields the app, widget, web UI, and ntfy runner
need in one call: signal, battery, data usage, hotspot, temperature, and
`slow_age_s`. Clients treat `slow_age_s` over 30 as stale. The daemon smooths
`temp_max_c` to a 25-second median for display, while the thermal gate stays
instantaneous. See [Temperature history](dashboard.md#temperature-history).

## Safety semantics

- **The thermal gate fails closed.** It reads battery temperature and
  `/sys/class/thermal`. The warning threshold is 44 °C, and writes are
  refused over `gate_c`. Bench thermal mode is the documented exception: it
  suspends the zones and replaces them with a 70 °C trip. See the README's
  [Safety](../README.md#safety) section.
- **No modem identity changes.** Nothing writes to the IMEI, baseband, SIM,
  or eSIM.

## Config

`config.schema.json` is authoritative. It enforces a loopback default bind,
scoped hex tokens of at least 256 bits, and `thermal.fail_closed` set to
true. The daemon still loads older configs that carry removed keys.
