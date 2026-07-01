# zflip5-modem-module — design context (executor brief)

This is the "interview context" the todos in `.omo/plans/zflip5-modem-module.md` cite. Executors have no other conversation history — everything load-bearing is captured here and in the plan's **Post-critique refinements** section.

## Goal in one line

A rooted Samsung Galaxy Z Flip 5 used as a controllable cellular modem/hotspot, queried and (safely) controlled from the owner's iPhone (Apple Shortcuts) and Discord, with thermal/SMS/carrier safety as hard constraints.

## Target device (assumed until Todo 1 confirms)

- Samsung Galaxy Z Flip 5, model **SM-F731** (US/intl variants vary; Todo 1 records the exact build).
- SoC: Qualcomm **Snapdragon 8 Gen 2** → **arm64-v8a only** (64-bit userspace on OneUI A13+).
- OneUI on Android 13/14/15, **SELinux enforcing**, Samsung **Knox** (root trips the Knox warranty fuse; Samsung Pay / Secure Folder / some Knox features stop working — accepted), and Samsung **DEFEX** (can kill root processes that exec binaries from non-system paths).
- **Magisk** root. The module ships as a Magisk module.
- Cellular link is behind carrier **CGNAT** — the phone has no stable public inbound IP. This is why ingress is a mesh VPN + relay, not a port-forward.

## Locked decisions (authoritative — also in the plan)

| Area | Decision |
| --- | --- |
| v1 scope | **Full**: telemetry + SMS reader + tether/safe-5G writes + iPhone + Discord. Risky writes are discovery-gated, never block release. |
| Daemon runtime | **Static Go, arm64-v8a** (`CGO_ENABLED=0 GOOS=linux GOARCH=arm64`). Pinned toolchain, reproducible build + checksum. |
| Ingress | **Tailscale** (root `tailscaled`, userspace-net, from the Magisk module) + `tailscale serve` for the iPhone path; **tiny public relay** (Cloudflare Worker / $5 VPS) on the same tailnet for Discord. One outbound connection. |
| SMS | **iPhone/Tailscale only, never Discord.** Redaction fails closed. Full body is a separate off-by-default endpoint with its own credential + per-request confirm + out-of-band read alert. |
| Local transport | api-front (unprivileged, loopback TCP, bearer auth) **+** root-broker (Unix socket `0600`, `SO_PEERCRED`, named-verb allowlist). Loopback is NOT a trust boundary. |
| Tokens | Scoped + independently revocable (`read-status` / `sms` / `radio-control`), ≥256-bit, generated at first boot, `0600` under `/data/adb`, rotate/revoke kill-switch. |
| Thermal gate | Battery temp + `/sys/class/thermal` (root) are PRIMARY and **fail closed**; Android `getCurrentThermalStatus()` is advisory (often stays `NONE` on Samsung). 44 °C = warning. |

## Threat model summary (full version → `docs/threat-model.md`, Todo 2)

- **Asset crown jewel:** SMS (carries 2FA/OTP → account takeover) and root radio control.
- **Primary attacker on-device:** any other installed app — it shares `127.0.0.1`, so the network bind is not a boundary; the tailnet ACL + scoped bearer tokens + the broker's named-verb allowlist are. SMS/radio paths must be unreachable via free-form strings (no shell injection) and unreadable without the correct scoped token + `SO_PEERCRED`.
- **Primary attacker off-device:** anyone who reaches the public Discord relay or steals a token. Mitigations: relay holds no `sms` token; Ed25519 verify + ≤5 s timestamp window + nonce dedup; owner user-ID allowlist (default-deny); one-command revoke/rotate; out-of-band alert on SMS reads and state-changing commands.
- **Forbidden (scope guardrails, unchanged):** no thermal-mitigation bypass, no public/0.0.0.0 bind, no SMS auto-forward, no IMEI/baseband/SIM/eSIM/carrier-provisioning changes, no broad SELinux permissive, no disabling DEFEX/Knox.

## What Todo 1 discovery MUST settle (de-risks everything downstream)

1. Exact build/firmware, Android version, Magisk version, SELinux mode, DEFEX/Knox state.
2. Whether NR/5G display type (NSA/SA) + signal + service state are already in `dumpsys telephony.registry` (root) — may shrink the helper APK.
3. Whether SMS is readable via root `content query --uri content://sms/inbox` — and whether root can grant the hard-restricted `READ_SMS` to a sideloaded package on this build (`pm grant` + `appops`/`pm set-permission-flags`), capturing the exact sequence.
4. Whether writing `preferred_network_mode<subId>` actually moves `dumpsys telephony.registry` data network type (guarded, owner-consented, auto-reverting probe) — and whether any tether toggle works (`cmd wifi`, `service call connectivity`, `svc usb`) incl. DUN/entitlement state.
5. Whether Android thermal status ever leaves `NONE` under a controlled CPU/GPU+charging load (fixtures can't prove this).
6. The daemon's actual runtime SELinux context from a process forked inside `service.sh`, whether it can bind loopback while enforcing, and any `avc: denied` for each candidate operation (so `sepolicy.rule` stays minimal + daemon-scoped, never untrusted_app-scoped).
7. Full `cmd phone` / `cmd wifi` / `cmd connectivity` / `svc` help output and current listening ports.

All discovery is read-only except the single guarded, auto-reverting control-probe in (4), which requires explicit owner consent and immediate restore.

## Component map

- `tools/` — discovery, packaging, lint, test, replay, security-check, docs-check scripts (bash; shellcheck-clean).
- `daemon/` (to be created) — Go api-front + root-broker.
- `helper/` (to be created) — Android helper APK (Gradle/Kotlin), foreground service, READ_SMS only, daemon-supervised.
- `relay/` (to be created) — public Discord relay (Cloudflare Worker or VPS), Ed25519 + deferred-ephemeral + followup.
- `magisk/` (to be created) — `module.prop`, `service.sh` (fork/detach + watchdog), `uninstall.sh`, default config, sepolicy.rule (capped).
- `schemas/` — `api.openapi.json`, `config.schema.json`.
- `docs/` — architecture, threat-model, install, shortcuts, discord, safety, troubleshooting.
- `shortcuts/` — importable iPhone Shortcut recipes + replay fixtures.
- `fixtures/discord/` — signed/tampered interaction fixtures.

Component source dirs are created by their owning todo, not up front — their exact shape follows the Todo 2 architecture contract.

## Reference links (consolidated from the todos)

- Magisk modules: https://topjohnwu.github.io/Magisk/guides.html
- TelephonyManager / TelephonyDisplayInfo / SubscriptionManager: https://developer.android.com/reference/android/telephony/TelephonyManager
- PowerManager (thermal): https://developer.android.com/reference/android/os/PowerManager
- BatteryManager: https://developer.android.com/reference/android/os/BatteryManager
- SMS provider: https://developer.android.com/reference/android/provider/Telephony.Sms
- Runtime permissions: https://developer.android.com/training/permissions/requesting
- AOSP thermal mitigation: https://source.android.com/docs/core/power/thermal-mitigation
- Discord interactions + Ed25519: Context7 `/discord/discord-api-docs`
- Apple Shortcuts API / URL: https://support.apple.com/guide/shortcuts/request-your-first-api-apd58d46713f/ios , https://support.apple.com/guide/shortcuts/run-a-shortcut-from-a-url-apd624386f42/ios
- Tailscale serve / userspace networking: https://tailscale.com/kb/1242/tailscale-serve , https://tailscale.com/kb/1112/userspace-networking
