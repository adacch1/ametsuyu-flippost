# zflip5-modem-module

A rooted Samsung Galaxy Z Flip 5 (SM-F731) modem control module: a small, local-only root service plus an Android helper app, controllable from an iPhone (Apple Shortcuts) and Discord (slash commands).

It reports network, tethering, thermal, battery, and recent SMS state, and can safely prefer/recover 5G — **without** ever bypassing thermal protection.

## Status

Project bootstrap. Plan written; implementation not started.

The authoritative work plan lives at [`.omo/plans/zflip5-modem-module.md`](.omo/plans/zflip5-modem-module.md) (14 todos across 4 waves + a final audit). The original draft is kept at `dwkpnx6.md`.

## Safety guarantees (non-negotiable)

- No disabling or bypassing Samsung/Android thermal mitigation. Android thermal status is a hard safety gate; 44 °C is a warning threshold.
- No public/unauthenticated API. The daemon binds `127.0.0.1` by default; remote access only through an explicit private tunnel.
- SMS is pull-only, redacted by default, owner-only, and rate-limited. Never auto-forwarded.
- No IMEI / baseband / SIM / eSIM modification and no carrier-provisioning bypass.

## Layout

| Path | Purpose |
| --- | --- |
| `.omo/plans/` | Canonical work plan |
| `.omo/drafts/` | Design drafts and interview notes |
| `.omo/evidence/` | Agent-executed QA evidence (per-todo) |
| `tools/` | Discovery, packaging, test, and verification scripts |
| `docs/` | Architecture, threat model, install, safety, troubleshooting |
| `schemas/` | API (OpenAPI) and config JSON schemas |
| `fixtures/` | Test fixtures (e.g. signed Discord interaction payloads) |
| `tests/` | Offline unit/policy tests |

Component source directories (Magisk module, daemon, Android helper, Discord relay) are intentionally **not** created yet — their structure is decided by the architecture contract (Todo 2) and the runtime decision still open.
