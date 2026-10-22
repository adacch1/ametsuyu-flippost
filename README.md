# Ametsuyu Flippost

<p align="center">
  <img src="docs/assets/social-card.png" alt="Ametsuyu Flippost card: the Flippost logo beside the title and the tagline A rooted Z Flip 5 as a 5G modem and Wi-Fi 6 hotspot, over a rainy slate sky" width="100%">
</p>

## Best with OnigiriDeX

Ametsuyu Flippost works best on
[OnigiriDeX](https://github.com/Kinue72/OnigiriDeX), a third-party firmware for
the Galaxy Z Flip5 (SM-F731B). This module is designed around it, and root
comes from KernelSU. If you own a Z Flip5, consider installing it.

What you gain:

- **The whole phone runs on the cover screen.** OnigiriDeX turns the Flip5
  cover into a full phone screen, so a broken inner screen stops mattering.
- **Samsung DeX on a monitor.** Use the phone as a desktop.
- **Built-in KernelSU root.** The module needs no separate rooting step.
- **A module built around it.** The app, widget, and hotspot controls assume
  the cover screen is your main display.

To get it, follow the build and flashing instructions in the
[OnigiriDeX repository](https://github.com/Kinue72/OnigiriDeX).

Warning: Flashing wipes the phone, so back up your data first. OnigiriDeX is
third-party firmware for the SM-F731B only.

## Overview

Ametsuyu Flippost turns a rooted Samsung Galaxy Z Flip 5 (SM-F731B) into a
dedicated 5G/LTE modem and Wi-Fi 6 hotspot. It ships as a KernelSU module with
a local-only Go daemon, a native app with a home-screen widget, and a slim web
UI over Tailscale. Nothing listens on a public address.

## Features

- **Native app.** Kotlin and Compose with the Flippost look: signal, data
  against your cap, battery, temperature, per-device speed, and one-tap
  hotspot, dual-band, and IP-rotation controls.
- **Home-screen widget.** Toggle the hotspot, and see the IP, temperature,
  and data at a glance. Rotate IP asks you to confirm first.
- **Web UI over Tailscale.** A main view plus a **Details** list, installable
  as a PWA. See [Web UI](docs/dashboard.md).
- **Wi-Fi 6 hotspot.** The daemon starts the SoftAP the way Samsung Settings
  does, so clients get 802.11ax at 80 MHz, up to 1200 Mbps.
- **Dual band, opt in.** One SSID on 2.4 GHz and 5 GHz at once. Dual band
  stops home-network scanning, so you turn it on deliberately.
- **Home-network auto-toggle.** The hotspot turns off when a network from
  `ssid_whitelist` is in range and back on when you leave. See
  [Hotspot auto-toggle](docs/hotspot-auto.md).
- **Hotspot presets.** Save named SSID, passphrase, security, and band sets.
- **Bench thermal mode.** The daemon lifts Android and Samsung thermal
  throttling and enforces its own 70 °C trip. It warns at 65 °C.
- **CPU policy.** Auto, performance, balanced, eco, and off modes.
- **Alerts.** A device-local ntfy runner reports temperature, battery, power,
  data cap, signal, and hotspot changes. See [ntfy alerts](docs/ntfy.md).
- **DeX over USB.** `tools/dex.sh` opens the phone's Samsung DeX desktop in
  scrcpy on your computer.

## Design

The app and web UI share one token set drawn from the logo: a rain-sky slate
ground, icy blue glass cards, cream text, and a blush pink accent.
[`DESIGN.md`](DESIGN.md) lists every token.

## Safety

- **Bench thermal mode is on.** With `thermal.bench=true`, the daemon disables
  the thermal zones, zeroes the cooling devices, and lifts Samsung's kernel
  CPU frequency cap. A watchdog re-applies this every 5 seconds.
- **The 70 °C trip is the last line of defense.** When the hottest sensor
  reaches 70 °C, the daemon restores all stock thermal mitigation. It lifts
  the limits again once the phone cools to 55 °C or lower.
- **Stock mode is one setting away.** Set `thermal.bench=false` to restore
  Samsung's mitigation, a 44 °C warning, and a hard 48 °C cap.

Warning: Bench mode suits battery-less donor hardware on a bench supply. With
a battery installed, sustained heat near 70 °C can swell or damage the cell.

- **Loopback only.** The daemon binds `127.0.0.1` and refuses any other bind
  address. Remote access goes through Tailscale, never a port forward.
- **Tailscale sign-in is owner-only.** Only the app can sign Tailscale in or
  out. A browser on your tailnet cannot.
- **No modem identity changes.** Nothing writes to the IMEI, baseband, SIM, or
  eSIM, and nothing bypasses carrier provisioning.

Warning: Your carrier can route a public IPv6 prefix to the phone. The module
firewall keeps adb (port 5555) reachable only on `lo` and `tailscale0`. Add
every new port to it. For details, see [Threat model](docs/threat-model.md).

## Repository layout

| Path | Contents |
| --- | --- |
| `daemon/` | Go root daemon: loopback API, thermal and CPU policy, web UI |
| `magisk/` | Module: `service.sh`, `firewall.sh`, ntfy runner, packaged daemon, APK, and `tether.jar` |
| `helper/` | Native app and widget, and root helpers for tethering and Wi-Fi scans |
| `selfhost/` | Optional self-hosted ntfy server (Docker Compose) |
| `tools/` | Build, packaging, verification, and `dex.sh` |
| `docs/` | Install, web UI, alerts, Tailscale, and threat-model guides |
| `schemas/` | OpenAPI and config JSON schemas |

## Get started

You need a rooted Z Flip 5 (SM-F731B), ideally on OnigiriDeX, and a computer
with `adb`, Go, and a JDK with the Android build tools.

1. Clone the repository:

   ```sh
   git clone https://github.com/adacch1/ametsuyu-flippost.git
   cd ametsuyu-flippost
   ```

1. Build and install the module by following [Install](docs/install.md).
1. Set up remote access by following [Tailscale](docs/tailscale.md).
1. Open the web UI as described in [Web UI](docs/dashboard.md).

### Update the daemon

The module runs a watchdog that restarts the daemon, so a daemon update needs
no reboot. The kernel refuses to overwrite a running binary, so rename it
first:

```sh
bash tools/build-daemon.sh
adb push dist/zflip5-modemd /data/local/tmp/modemd.new
printf '%s\n' \
  'BIN=/data/adb/modules/zflip5_modem/daemon/zflip5-modemd' \
  'cp "$BIN" /data/adb/zflip5-modem/modemd.prev' \
  'mv "$BIN" "$BIN.old"; cp /data/local/tmp/modemd.new "$BIN"' \
  'chmod 0755 "$BIN"; rm "$BIN.old"; pkill -x zflip5-modemd' \
  | adb shell su
```

The watchdog starts the updated daemon within about 10 seconds. To undo an
update, see [Rollback](docs/rollback.md).

## Disclaimer

Ametsuyu Flippost targets one device and one owner's workflow, and it's
published for reference. Rooting trips Knox, which disables Samsung Pay and
Secure Folder, and voids the warranty. Use it at your own risk.

## License

MIT. See [LICENSE](LICENSE).
