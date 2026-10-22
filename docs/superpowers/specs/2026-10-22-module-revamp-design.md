# Module revamp: native app, widget, and slimmer web UI

Date: 2026-10-22. Owner: the module's single user (trungtin). Status: design for approval.

## Intent

The Galaxy Z Flip5 has a broken inner screen. Until now the module repurposed the phone as a hotspot appliance and took over the small cover screen with a WebView kiosk.

The phone now runs OnigiriDeX, which turns the cover into a full phone screen. The module no longer needs to own the cover. It becomes a background hotspot and modem service with three front ends:

- A native app, opened occasionally on the phone.
- A home-screen widget for quick glances and one-tap actions.
- A web UI used from other devices over Tailscale.

The owner wants the app to feel native and responsive, and to keep the existing Flippost anime look.

## Goals

- Replace the WebView kiosk with a native Kotlin app built with Jetpack Compose and Material 3. The app contains no WebView, except the captive-portal screen in phase 4.
- Add a home-screen widget with hotspot toggle, rotate IP, current IP, temperature, and data usage.
- Redesign the web UI around a minimal main view, with everything else in detail screens.
- Make dual band an opt-in setting instead of a button.
- Add Tailscale status and sign-in to the app.
- Remove the features the owner dropped (see [Feature decisions](#feature-decisions)).

## Non-goals

- Taking over the cover screen. The app opens on demand and never relaunches itself.
- Remote screen access from the phone. scrcpy runs only from the Mac over USB through `tools/dex.sh`, which already exists.
- Raising DeX past 60 fps.
- Changing the Go daemon's language or replacing it. It stays the single backend for all three front ends.

## Anti-goals

The revamp fails, even if every screen works, if it does any of the following:

- Breaks the hotspot's 802.11ax (Wi-Fi 6) mode. Every hotspot start tries `SemWifiManager.setWifiApEnabled` first, with retries. The existing `TetheringManager` fallback stays only as a last resort, and the app and web UI then show "Wi-Fi 6 unavailable" (my call: a slower hotspot beats none).
- Weakens the thermal protection or the 48 °C clamp outside bench mode.
- Exposes the daemon beyond loopback, or opens a new network listener on the phone.
- Lets a remote browser log Tailscale out or obtain a Tailscale login URL.
- Kills or restarts system processes by accident. See [Root command rule](#constraints).

## Constraints

**Device and ROM** (verified 2026-10-22):

- SM-F731B on OnigiriDeX build `64e2bae` (One UI 7, Android 15), firmware base `F731BXXS5EYI1`.
- Root is KernelSU 3.3.1. Root shells run as `u:r:ksu:s0`.
- No KernelSU metamodule is installed, so a module's `system/` folder is not mounted. `service.sh`, `system.prop`, `sepolicy.rule` and `/data/adb/service.d/*.sh` all work.
- The cover is display 0 (748×720 px, about 352×340 dp, 60 Hz). The dead inner screen is display 1. A camera cutout covers the bottom-right 174×31 dp of the cover.
- Adb-Root-Enabler sets `ro.adb.secure=0` and `ro.debuggable=1`. A boot script firewalls ports 5555, 8000-8088 and 8886 to loopback and Tailscale on IPv4 and IPv6.
- Wi-Fi 6 on the hotspot needs `settings put secure wifi_ap_11ax_mode_checked 1` (and the same in `global`), plus a hotspot start through `SemWifiManager.setWifiApEnabled`. A wipe resets the setting.

**Root command rule:** pipe root commands into `adb shell su` (or `su`) on stdin. Never pass a quoted multi-word argument through `su -c`. KernelSU re-joins `su -c` arguments and drops inner quotes. A `pkill -f "app_process /system/bin DexInput"` sent that way matched 58 system daemons, killed `vold`, and rebooted the phone three times.

**Safety invariants** (from project memory):

- The daemon binds `127.0.0.1` only.
- The 48 °C clamp stays, except in bench mode, which trips at 70 °C.
- No baseband writes.
- Never remove Adb-Root-Enabler.
- Agents never reboot the phone without the owner's approval.

**Build tooling on the Mac:**

- Gradle 9.8.0, Kotlin 2.4.10, Temurin JDK 21, and the Android SDK at `/opt/homebrew/share/android-commandlinetools`.
- A probe project built on 2026-10-20 with AGP 9.4.1, Compose BOM 2026.09.00, `compileSdk` 37, `minSdk` 33 and `targetSdk` 34. The release APK with R8 was 1.65 MB.

## Feature decisions

The owner chose these from an 86-item inventory. Item numbers match that inventory, which is in the Mac scratchpad file `zf5-feature-inventory.md`.

**Keep:**

| Area | Items |
|---|---|
| App and cover | 2 app (now native), 3 Action button, 7 cover page content (now the app home), 11 stay awake while plugged in |
| Hotspot | 13 boot start, 14 Wi-Fi 6 gate (the module must now set it), 15 allowlist auto on/off (`whitelist` in code), 16 timed force-on, 17 manual scan, 18 toggle, 19 dual band (as opt-in), 20 status, 21 presets, 22 preset auto-switch, 23 USB tethering, 24 clients list |
| Network | 25 scan-always on with Wi-Fi client off, 26 stop `cnss_diag`, 27 airplane, 28 rotate IP, 29 WAN IP, 35 bench network tuning, 36 `fq_codel` |
| Thermal and power | 37 battery cap, 38 thermal sweep, 39 thermal gate, 40 48 °C clamp, 41 smoothed temperature, 45 5G thermal state machine, 46 thermal limits, 47 bench mode, 48 CPU modes, 49 CPU auto, 50 cooldown |
| Data | 51 meter, 52 quota and period, 53 reset |
| Remote | 54 daemon API, 55 web UI, 56 PWA, 57 QR pairing, 58 Tailscale, 60 root ADB, 61 firewall |
| Alerts | 65 ntfy, 66 alert rules, 70 self-hosted ntfy config |
| Core | 72 first-boot config, 73 watchdog, 74 restart daemon, 75 reboot phone, 76 boot scripts, 77 token scopes, 78 open reads and control |
| Rate limits (79) | Changed, not removed: no limit for the phone's own app, loose limits for remote callers |
| New | 81 native app, 82 Tailscale panel, 83 minimal web main view, 84 dual-band opt-in, 85 shop Wi-Fi (phase 4) |

**Remove** (delete the code, docs and config keys):

1 cover wake watcher (`magisk/coverwatch.sh`), 4 kiosk system-app overlay (`magisk/system/app/`), 5 cover-home switch (`daemon/coverhome.go` home logic and `/v1/cover/home`), 6 cover dim, 8 accent colours (`/v1/cover/accent`), 9 boot wallpaper (`helper/tether/SetWallpaper.java`), 10 animations off, 12 accidental-touch setting, 30 signal detail endpoint, 31 network summary, 32 bands report, 33 prefer-5G stub, 34 speedtest, 42 temperature history, 43 battery facts endpoint, 44 health endpoint, 59 on-phone Termux remote-screen stack, 62 SMS reader, 63 notification reader, 64 inbox audit log, 67 Telegram bot, 68 Discord bot, 69 Discord relay, 71 iPhone Shortcuts docs, 80 Mac tooling scripts that only served removed features.

**Cleanup restores stock settings** (owner's choice):
- Set `window_animation_scale`, `transition_animation_scale` and `animator_duration_scale` back to `1`, and stop forcing the solid wallpaper.
- Re-enable `com.android.systemui/.subscreen.SubHomeActivity` and `com.sec.android.app.launcher/com.honeyspace.dexservice.SecondaryLauncher` if the old cover-home switch left them disabled.
- Restore `sub_screen_brightness` and its mode from `cover-bright*.prev` if those files exist, then delete them.
- Remove `/data/adb/zflip5-modem/cover-home-off`.

Removing an endpoint must not remove data that a kept screen shows. For example, the app still shows signal bars, battery level and temperature through `/v1/summary`.

Removing item 59 means stopping and deleting the Termux services, removing `/data/adb/service.d/50-zflip-remote.sh` except its firewall rules, and deleting `/data/local/tmp/scrcpy-server.jar`. The firewall rules must survive in a module-owned boot step. Port 5555 stays firewalled because Adb-Root-Enabler stays.

## Design

### Native app

- **Package and signing:** keep package `com.zflip5.modem` and activity `com.zflip5.modem.CoverKioskActivity`, signed with `dist/debug.keystore` (alias `zf5`, cert SHA-256 `47965ec3…a75a`). It then installs over the existing kiosk. Use versionCode 2 or higher.
- **Source:** a Gradle project in `helper/cover-app/`, built by `helper/build-apk.sh` into `dist/zflip5-kiosk.apk`.
- **Installation (my call):** the module installs the APK as a normal user app, because `system/` does not mount. `service.sh` runs `pm install` when the installed versionCode is lower than the bundled one.
- **Launch:** from the launcher icon, the widget, or the module Action button. Never automatically.
- **Look:** the Flippost style from `DESIGN.md` and `docs/assets/cover-kiosk.png`:
  - the rain-girl avatar (`docs/assets/flippost-logo.png`)
  - a drizzle-sky gradient background
  - frosted-glass cards, gradient pill buttons, and a striped data bar
  - the Nunito font, bundled
  - Map the colours to an M3 dark colour scheme.
  - The signal indicator is a four-bar icon with unreached bars dimmed. Do not draw it with `▂▄▆` characters.
- **Approved home layout ("option C"):**
  1. A header row with the avatar and a hotspot card: SSID, "Wi-Fi 6 · 5 GHz · N devices", and an M3 `Switch`.
  2. A chip row: signal bars plus network type, temperature with a status dot, WAN IP, and Tailscale state.
  3. A glass card with live total speed (down and up), data used in the cycle against the cap, a striped bar, "Today X GB", and "Y GB left".
  4. **Rotate IP** and **More** pills, kept inside the left 176 dp so they clear the camera cutout.

  The home screen never scrolls. The approved mockup is `.superpowers/brainstorm/4315-1792640803/content/app-home-c-flippost-v2.html`.
- **More screens:**

  | Screen | Contents |
  |---|---|
  | Devices | Each client with live down and up speed. The phone itself shows as an approximate row. |
  | Hotspot | Presets and auto-switch, allowlist, timed force-on, manual scan, USB tethering, and **Dual band** as an opt-in switch. |
  | Data | Quota, period, reset time, and a reset button. |
  | System | Bench mode, thermal limits, CPU mode, cooldown, battery cap, airplane, rotate IP. |
  | Tailscale | Status, IP, MagicDNS name, key expiry; **Log in** shows a full-screen QR code and polls until running; **Log out** has a destructive confirm. |
  | Alerts | ntfy on/off and alert rules. |
  | Advanced | Restart daemon, reboot phone (with confirm), browser pairing QR, open reads and open control switches. |

- **Feel:**
  - Optimistic switches with haptic feedback.
  - A failed write rolls back and shows the daemon's error in a Snackbar.
  - If no update succeeds for 5 s, values grey out with "updated N s ago" and writes are disabled.
  - Polls `/v1/summary` once a second only while the app is visible.
- **Dual-band confirm:** a dialog offering "Apply now (clients drop about 10 s)", "At next start", or Cancel.

### Widget

- **Contents:** a hotspot switch with device count, current WAN IP, temperature with a status colour, data used against the cap with a bar, and a **Rotate IP** button.
- **Look:** the same Flippost style. The approved mockup sits next to the app home in the same file.
- **Rotate IP:** confirms before running, because it drops connectivity for about 10 to 30 seconds. It shows progress, then the new IP.
- **Refresh (my call):** every 15 minutes, and immediately after any tap.
- **Daemon down:** shows "service not running" instead of stale values.
- Built with Jetpack Glance or RemoteViews; the builder decides.

### Web UI

- **Served by:** the daemon, for remote browsers over Tailscale (`tailscale serve`). Same Flippost look as the app.
- **Main view:**
  - signal
  - data used
  - live speed for each client, including the phone
  - hotspot on/off
- **Details menu:** the same screens as the app's More, except Tailscale is read-only. The web UI never offers Tailscale login or logout.
- **Routing:** `/` serves the new web app to every caller. The old WebView cover page and `/dashboard` go away once the native app ships.
- **PWA and QR pairing:** stay.

### Daemon API changes

**New `GET /v1/summary`** (read-status scope):
- Returns everything the app home, the widget and the web main view need, in one call, from memory.
- Does no subprocess work on the request path. A collector refreshes slow values every 5 to 10 s.
- Fields: signal (network type, bars), usage (cycle bytes, cap, today), speed (WAN total, phone approximate, per-client list or an "unavailable" marker), hotspot (active, SSID, band, clients, dual-band setting), WAN IP, temperature and state, battery, Tailscale brief, airplane.

**Per-client speed** sources, in order:
1. nl80211 station byte counters.
2. A read of Samsung's `semSmartHS` BPF maps.
3. Totals only, with `"per_client":"unavailable"`.

Never attach `tc` filters, and never turn off tethering offload.

**Dual band:**
- A persisted `hotspot.dual_band` setting, default off.
- The owner chose that auto features win. Dual band stops Wi-Fi scanning, which the allowlist auto on/off (15) and preset auto-switch (22) need. So the setting can only be turned on while the allowlist is empty and auto-switch is off. Otherwise the switch is disabled with a short explanation, and the API returns `409`.
- Existing presets with `band: "dual"` migrate to `"5"`, with the setting left off.
- `POST /v1/hotspot/dual {"enabled", "apply_now"}`.
- Remove `"dual"` from preset bands and migrate existing presets.
- Keep `/v1/hotspot/band` one release as a deprecated alias.

**Tailscale:**
- The running Tailscale is the separate `magisk-tailscaled` module (kernel `tailscale0` interface, socket `/data/adb/tailscale/tailscaled.sock`, state in `/data/adb/tailscale/`). The app and daemon manage that instance (my call).
- Delete this module's own unused Tailscale path: the block in `magisk/service.sh` gated by `ingress.mode`, the `ingress` config keys, and `docs/tailscale.md` (rewrite it for `magisk-tailscaled`).
- After a successful login, the daemon re-applies the `tailscale serve` config if it is missing, so the web UI stays reachable at its tailnet URL (my call).
- `GET /v1/tailscale` (read-status): status from tailscaled's LocalAPI. Never returns keys or the login URL.
- `POST /v1/tailscale/login` and `POST /v1/tailscale/logout`: app only. See **App authentication** in this section.

**Wi-Fi 6 gate:** the boot path sets `wifi_ap_11ax_mode_checked=1` in `secure` and `global` before starting the hotspot.

**App authentication (my call):**
- The daemon authorizes a loopback request with full scopes when the connecting socket belongs to the app's UID. It looks that up in `/proc/net/tcp` and `/proc/net/tcp6` by the client port.
- The app and widget then hold no tokens.
- Tailscale login and logout, and the "no rate limit" rule, apply only to that UID.
- Remote browsers keep using tokens or the open-reads and open-control modes.

**Open reads and open control** (kept, item 78): the defaults stay on, so any device on the private tailnet can view and control without a token (owner's choice). The two switches move to the app's Advanced screen, app only. The pairing QR encodes the web UI's tailnet URL `/`, and adds the read-status token only when open reads is off.

**Rate limits:** none for the app's UID. Remote callers keep per-scope limits for radio actions, loosened so normal tapping never returns 429.

### Phases

1. **Daemon foundation:**
   - `/v1/summary` and per-client speed.
   - App-UID authentication and the rate-limit change.
   - The dual-band setting.
   - The Wi-Fi 6 gate at boot.
   - Removals that touch only the daemon.
2. **Native app and widget,** replacing the WebView kiosk, plus module install logic.
3. **Web UI redesign,** then removal of the old cover page, `/dashboard`, item 59, and the remaining dropped features.
4. **Shop Wi-Fi (item 85).** The owner left this for last, and it needs its own design conversation first. Known facts:
   - Android tethers through a single upstream network.
   - The kernel lacks multipath routing.
   - A 5 GHz Wi-Fi client forces the hotspot to 2.4 GHz.
   - Joining any network exposes the phone to that LAN, so the IPv4 firewall must cover `wlan0` before joining.

## Verification

- `cd daemon && go test -race ./...` and `tools/run-tests.sh` pass.
- `cd helper/cover-app && ./gradlew test assembleRelease` passes. The APK is under 3 MB and carries the existing signing cert.
- Compose UI tests at 352×340 dp:
  - the home never scrolls;
  - no tap target sits in the bottom-right 174×31 dp;
  - every target is at least 48 dp.
- On the device:
  - `/v1/summary` answers in under 10 ms.
  - The app cold-starts in under 700 ms.
  - The widget toggles the hotspot.
  - Rotate IP changes the WAN IP.
  - After any hotspot change, a connected client still reports 802.11ax, 80 MHz, about 1200 Mbps. A client is the only valid check; the phone's own report is unreliable.
- A tailnet browser (with open reads and open control on):
  - lands on the main view;
  - cannot reach Tailscale login or logout (403);
  - every kept control is reachable through Details.

## Rollback

- **Daemon:** keep the previous binary as `modemd.prev` on the device.
- **App:** the WebView kiosk can be rebuilt from git history with a higher versionCode and installed over the native app.
- **Config:** the new config keys are additive.

## Left to the builder

- Module and file layout inside `helper/cover-app/`, and the widget technology.
- Exact JSON field names, as long as they carry the data listed here.
- Library versions, within the probe's working set.
- How the collector schedules its refreshes.
- Test structure.
