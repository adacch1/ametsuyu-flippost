# zflip5-modem-module

A rooted Samsung Galaxy Z Flip 5 (SM-F731) modem control module: a small, local-only root service plus an Android helper app, controllable from an iPhone (Apple Shortcuts) and Discord (slash commands).

It reports network, tethering, thermal, battery, and recent SMS state, and can safely prefer/recover 5G — **without** ever bypassing thermal protection.

## Status

Implemented and running on-device. The Go daemon (`daemon/`), Magisk module (`magisk/`), WebView cover-screen kiosk (`helper/`), and self-hosted ntfy + Discord bot (`selfhost/`) are all built and deployed.

The authoritative work plan lives at [`.omo/plans/zflip5-modem-module.md`](.omo/plans/zflip5-modem-module.md). The original draft is kept at `dwkpnx6.md`.

## Safety guarantees (non-negotiable)

- No disabling or bypassing Samsung/Android thermal mitigation. Android thermal status is a hard safety gate; 44 °C is a warning threshold.
- No public/unauthenticated API. The daemon binds `127.0.0.1` by default; remote access only through an explicit private tunnel.
- SMS is pull-only, redacted by default, owner-only, and rate-limited. Never auto-forwarded.
- No IMEI / baseband / SIM / eSIM modification and no carrier-provisioning bypass.

## Layout

| Path | Purpose |
| --- | --- |
| `daemon/` | Go root daemon: loopback API, thermal/CPU policy, served dashboard |
| `magisk/` | Magisk module: `service.sh`, `action.sh`, packaged daemon + `tether.jar` |
| `helper/` | WebView cover-screen kiosk APK + root tether/wifi-scan helpers |
| `selfhost/` | Self-hosted ntfy + Discord Gateway bot (docker-compose) |
| `tools/` | Build, packaging, and verification scripts |
| `docs/` | Architecture, threat model, install, safety, troubleshooting |
| `schemas/` | API (OpenAPI) and config JSON schemas |
