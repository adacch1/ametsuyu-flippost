# Architecture — zflip5-modem-module

Rooted Samsung Galaxy Z Flip 5 (SM-F731B, OneUI 6.1 / Android 14, Magisk) used as
a controllable cellular modem/hotspot, queried and safely controlled from the
owner's iPhone (Apple Shortcuts) and Discord.

Confirmed on-device by Todo 1: SELinux + dm-verity enforcing, Knox tripped
(`warranty_bit=1`), bootloader unlocked, arm64-v8a only, carrier VINAPHONE on LTE.

## Components

| Dir | What | Runtime |
| --- | --- | --- |
| `daemon/` | `api-front` (unprivileged, loopback TCP, bearer auth) **+** `root-broker` (Unix socket `0600`, `SO_PEERCRED`, named-verb allowlist) | static Go, arm64-v8a (`CGO_ENABLED=0 GOOS=linux GOARCH=arm64`) |
| `helper/` | Android helper APK, foreground service, `READ_SMS` only, daemon-supervised | Kotlin/Gradle |
| `relay/` | public Discord relay: Ed25519 verify + deferred-ephemeral responses | Cloudflare Worker / small VPS |
| `magisk/` | `module.prop`, `service.sh` (late_start fork/watchdog), `uninstall.sh`, capped `sepolicy.rule` | Magisk module |
| `schemas/` | `api.openapi.json`, `config.schema.json` | — |
| `tools/` | discovery, packaging, lint, test, metrics collectors (bash) | host + device |

## Trust boundaries

- **The network bind is NOT a trust boundary.** Any installed app shares
  `127.0.0.1`, so a loopback listener alone protects nothing. The real
  boundaries are: (1) scoped bearer tokens, (2) the root-broker Unix socket with
  `SO_PEERCRED` + a named-verb allowlist (no free-form shell strings), and (3)
  the Tailscale ACL for remote reach.
- **Default bind is loopback only.** A public / all-interfaces bind is rejected
  by `config.schema.json` (`bind_host` pattern). Remote access is a private
  Tailscale tunnel, never a port-forward (the cellular link is behind CGNAT).
- **api-front never holds root.** State-changing and SMS verbs cross the broker
  socket as allowlisted named verbs with typed args.

## Endpoints

See `schemas/api.openapi.json`. Read endpoints require the `read-status` scope;
`/v1/sms/recent` and `/v1/notifications/recent` require `sms`; writes
(`/v1/tether`, `/v1/prefer5g`, `/v1/cooldown`, `/v1/service/restart`) require
`radio-control`.

Both `sms`-scope reads are pull-only and neither is ever reachable from the
Discord relay. Body redaction is compiled off on this donor phone
(`redactBodies` in `daemon/sms.go`), so responses carry full text; the masking
path is intact behind that const. Both also ride the `dashboard.open_reads`
switch: with open reads on they serve without a token, like every other read;
turning it off closes them again.

`/v1/health` and the device-host block of `/v1/status` report CPU load (1/5/15m),
core count, RAM used %, battery temp, and the hottest thermal zone — sourced from
`/proc/loadavg`, `/proc/meminfo`, and `/sys/class/thermal` (reference collector:
`tools/collect-metrics.sh`).

## Safety semantics

- **"Prefer 5G" == safe prefer/recover NR**, applied via
  `cmd phone set-allowed-network-types-for-users` (Todo 1 proved the per-sub
  `preferred_network_mode<sub>` setting is a no-op on this device). It is a
  preference, never a force, and is refused in an unsafe thermal state.
- **Thermal gate is PRIMARY on battery temp + `/sys/class/thermal` and fails
  closed** (Android `getCurrentThermalStatus()` is advisory — often `NONE` on
  Samsung). 44 °C is the warning threshold; writes are refused above `gate_c`.
  The module never disables, patches, or works around thermal mitigation, and
  never edits vendor thermal files or thermal daemons.
- **SMS is owner-only, pull-only, redacted by default**, reachable only over the
  iPhone/Tailscale path, and is never routed through the Discord relay. There is
  no default forwarding of messages to any channel.
- Sensitive Discord replies are **ephemeral**; the owner allowlist is
  default-deny.

## Config

`config.schema.json` is authoritative. Key invariants enforced by schema:
loopback-only default bind, scoped hex tokens (≥256-bit), `thermal.fail_closed`
true, `sms.forward` false, `sms.redact_default` true, and a Discord relay that
structurally cannot carry the `sms` scope.
