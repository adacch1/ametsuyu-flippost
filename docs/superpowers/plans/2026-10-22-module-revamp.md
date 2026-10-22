# Module revamp (phases 1-3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the Z Flip 5 module into a background hotspot and modem service with three front ends: a native Compose app, a home-screen widget, and a slim web UI over Tailscale. The work also removes the dropped features.

**Architecture:** The Go daemon (`daemon/`, package `main`) stays the only backend. It gains an in-memory `/v1/summary` collector, per-client speed from nl80211, app trust by socket UID, an opt-in dual-band setting, and Tailscale LocalAPI management. A native Kotlin app in `helper/cover-app/` replaces the WebView kiosk. The app talks to `127.0.0.1:18080` with no tokens, and the module installs it as a user app. A single embedded page at `/` replaces `/dashboard` and the cover page.

**Tech Stack:**
- Daemon: Go stdlib with `syscall` netlink and `github.com/skip2/go-qrcode`.
- Boot: POSIX `sh` boot scripts, running under KernelSU.
- Helper: Java `app_process` helper (`tether.jar`).
- App: Kotlin 2.4.10, AGP 9.4.1, Compose BOM 2026.09.00, Material 3, zxing-core 3.5.3, and Robolectric 4.14.1 (test only).
- Web UI: vanilla HTML and JS. Its tests run in Node.

**Spec:** /Users/adacchi/module-zf5/docs/superpowers/specs/2026-10-22-module-revamp-design.md

**Out of scope:** Phase 4 (shop Wi-Fi, item 85) needs its own design conversation and is not in this plan.

## Global Constraints

- **Device:** SM-F731B on OnigiriDeX build `64e2bae` (One UI 7, Android 15), firmware base `F731BXXS5EYI1`. Root is KernelSU 3.3.1, and root shells run as `u:r:ksu:s0`. A module's `system/` folder is not mounted. `service.sh`, `system.prop`, `sepolicy.rule` and `/data/adb/service.d/*.sh` all work.
- **Cover display:** display 0, 748×720 px, about 352×340 dp at 60 Hz. A camera cutout covers the bottom-right 174×31 dp.
- **Root command rule:** pipe root commands into `adb shell su` (or `su`) on stdin. Never pass a quoted multi-word argument through `su -c`. KernelSU re-joins `su -c` arguments and drops inner quotes. A `pkill -f "app_process /system/bin DexInput"` sent that way matched 58 system daemons, killed `vold`, and rebooted the phone three times.
- **Safety invariants:**
  - The daemon binds `127.0.0.1` only.
  - The 48 °C clamp stays, except in bench mode, which trips at 70 °C.
  - No baseband writes.
  - Never remove Adb-Root-Enabler.
  - Agents never reboot the phone without the owner's approval.
- **Anti-goal, Wi-Fi 6:** every hotspot start tries `SemWifiManager.setWifiApEnabled` first, with retries. `TetheringManager` stays only as a last resort, and the UI then shows "Wi-Fi 6 unavailable".
- **Wi-Fi 6 gate:** `settings put secure wifi_ap_11ax_mode_checked 1`, and the same in `global`, set before every hotspot start.
- **Other anti-goals:**
  - Never weaken the thermal gate.
  - Open no new network listener.
  - A remote browser never gets a Tailscale login URL and can never log Tailscale out.
- **Per-client speed:** never attach `tc` filters, and never turn off tethering offload.
- **App:**
  - Package `com.zflip5.modem`, activity `com.zflip5.modem.CoverKioskActivity`, versionCode 2 or higher.
  - Signed with `dist/debug.keystore` (alias `zf5`, cert SHA-256 `47965ec3ab41b576b2cf0ab7f414c3127f0e95dbd58ecd57d21651ecdacca75a`).
  - APK under 3 MB.
  - No WebView.
  - Never launches itself.
- **Build:** Gradle 9.8.0, Kotlin 2.4.10, Temurin JDK 21 (`JAVA_HOME=/Library/Java/JavaVirtualMachines/temurin-21.jdk/Contents/Home`), and the Android SDK at `/opt/homebrew/share/android-commandlinetools`.
- **Verification:**
  - Offline: `cd daemon && go test -race ./...`, `tools/run-tests.sh` and `cd helper/cover-app && ./gradlew test assembleRelease` all pass.
  - Device: a connected client reports 802.11ax, 80 MHz and about 1200 Mbps after any hotspot change. The client is the only valid check.
- **Copy:** invoke the `google-writing` skill before writing any doc, UI string, error or log message.
- **Commits:** use `git commit -F -` with a heredoc. End every message with:
  ```
  Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_016CxXBEUwnS21RZmwBGTAMW
  ```
- **Paths:** relative to `/Users/adacchi/module-zf5`. Line numbers refer to `main` at `0452833`, so anchor on text once earlier tasks shift lines.

## Review Focus

1. **Upgrade with an existing on-device `config.json`.** The live config still carries `tokens.sms`, `ingress`, `sms`, `rate_limits.sms_per_min`, `cover.dim` and the legacy limits 120 and 6. It must load and validate, and normalize to 600 and 60. Pinned by `TestLoadConfigLegacyKeys` (Task 1.2).
2. **Collector stuck while HTTP still answers.** `slow_age_s` grows past 30, but the app keeps showing "Live" with frozen values and keeps writes enabled. The app and web must treat `slow_age_s > 30` as stale. Pinned by `staleWhenCollectorStuck` (Task 2.3) and the `isStale(…, slowAgeS)` assertions (Task 3.2).
3. **Concurrent whitelist and dual-band POSTs leave both on.** The checks and writes share `s.cfgMu`. Pinned by `TestDualWhitelistRace` under `-race` (Task 1.15).
4. **Counter resets produce huge rates.** An airplane cycle recreates `rmnet_data*`, and a client reconnect resets station counters. The rate must read 0, never about 1.8e19 B/s. Pinned by `TestSpeedCounterResetIsZero` (Task 1.7). The sibling case is phone-only traffic with idle clients, which flips `per_client` to `"unavailable"` and must recover. Pinned by `TestSpeedUnchangedCountersMarkUnavailable` and `TestSpeedRecoversAfterUnavailable` (Task 1.7).
5. **A tailnet browser or adb caller treated as the app.** These arrive as UID 0 through `tailscale serve`, or as UID 2000 through adb forward. Pinned by `TestIsAppNeverTrustsUID0` (Task 1.5) and `TestGuardAppRejectsNonApp` (Task 1.15).

Device-only risk, with no offline test: after an airplane cycle, `restartHotspotRetry` (`airplane.go:77`) can land on TM. Task 1.16 Step 9 measures this.

---

## File Structure

| Area | Files | Responsibility |
|---|---|---|
| Daemon, new | `daemon/apierr.go` | `writeErrReason` |
| | `daemon/peeruid.go` | app UID from `packages.list` and `/proc/net/tcp{,6}` |
| | `daemon/nl80211.go`, `nl80211_linux.go`, `nl80211_other.go` | station counters |
| | `daemon/speed.go` | WAN and per-client rate sampler |
| | `daemon/dualband.go` | dual-band setting and owner rule |
| | `daemon/tailscale.go` | LocalAPI client and handlers |
| | `daemon/summary.go` | collector and `/v1/summary` |
| | `daemon/coverrestore.go` | 1a; deleted in 3a |
| | `daemon/web.go`, `daemon/web/index.html` | embedded web app |
| | `daemon/testdata/station_dump.bin` | nl80211 fixture |
| Daemon, modified | `server.go`, `config.go`, `main.go`, `hotspot.go`, `presets.go`, `usage.go`, `policy.go`, `dashboard.go` | |
| Daemon, deleted | `sms*.go`, `notifications*.go`, `speedtest.go`, `temps*.go`, `bands.go`, `coverhome.go`, `cover.go`, `cover_test.go`, `dashboard_test.go` | |
| Helper | `helper/tether/TetherStart.java` | modes `sem` and `tm` |
| | `helper/tether/SetWallpaper.java` | deleted |
| App | `helper/cover-app/**` | Gradle project; layout in contract §3.1 |
| | `helper/build-apk.sh` | Gradle build |
| | `helper/src/`, `helper/AndroidManifest.xml`, `helper/res/` | moved or deleted |
| Module | `magisk/service.sh` | boot |
| | `magisk/app-install.sh` | APK install by versionCode |
| | `magisk/firewall.sh` | port lockdown |
| | `magisk/cleanup.sh` | one-time stock restore |
| | `magisk/uninstall.sh`, `magisk/action.sh`, `magisk/module.prop` | |
| | `magisk/ntfy/{launch.cjs,runner.mjs,alerts.mjs}` | ntfy runner |
| | `magisk/coverwatch.sh`, `magisk/system/` | deleted |
| Contracts and tests | `schemas/config.schema.json`, `schemas/api.openapi.json`, `tools/validate-contracts.py` | |
| | `tests/{validate-contracts,app-install,boot,web,removed}.test.sh`, `tests/ntfy/*.mjs`, `tools/run-tests.sh`, `tools/security-check.sh` | |
| | `tests/shortcuts.test.sh`, `tools/replay-shortcuts.sh`, `tools/test-5g-policy.sh`, `tests/lunar.test.sh` | deleted |
| Removed stacks | `selfhost/telegram-bot/`, `selfhost/bot/`, `relay/`, `fixtures/discord/`, `tools/{test-discord-relay,probe-controls,collect-metrics}.sh` and their tests | |
| Docs | `docs/**`, `README.md`, `PRODUCT.md`, `DESIGN.md` | |
| | `dwkpnx6.md` | deleted |

---

## Shared Contract

Source: the spec. This section has the Phase 1 and 2 corrections applied, and it overrides the drafts wherever they disagree.

### 0. Conventions

- **Error body:** `writeErr` (`daemon/server.go:365`) returns `{"error":string,"code":int}`. `writeErrReason` adds `"reason":string`, a machine-readable snake or kebab token. Every 409 and every app-only 403 carries a `reason`.
- **Units and formats:**
  - Byte counts are JSON numbers: `uint64` in Go, `Long` in Kotlin.
  - Rates are bytes per second, with the suffix `_bytes_s`.
  - Times are RFC3339 strings or `null`.
  - Temperatures are °C as `float64`, rounded to 1 decimal.
- **Nullability:** `T|null` means the key is always present and its value may be `null`.
- **Bands:** `"2"|"5"|"6"|"dual"`. Presets do not accept `"dual"`.

### 1. HTTP API

#### 1.1 `GET /v1/summary`

New in 1c. `guard("read-status")`. The handler reads memory only, with no subprocess and no file I/O.

```json
{
  "v": 1, "updated_at": "2026-10-22T10:00:00+07:00", "slow_age_s": 3,
  "signal": {"display": "5G", "tech": "NR", "operator": "Viettel", "bars": 3},
  "usage": {"period_bytes": 12345678901, "limit_bytes": 100000000000, "today_bytes": 1234567890,
            "left_bytes": 87654321099, "period": "monthly", "next_reset": "2026-11-01T00:00:00+07:00"},
  "speed": {"interval_ms": 1000, "wan_down_bytes_s": 2500000, "wan_up_bytes_s": 300000,
            "phone_down_bytes_s": 12000, "phone_up_bytes_s": 4000,
            "per_client": [{"mac": "aa:bb:cc:dd:ee:ff", "ipv4": "192.168.1.23", "down_bytes_s": 2488000, "up_bytes_s": 296000, "signal_dbm": -48}]},
  "hotspot": {"active": true, "ssid": "Flippost", "band": "5", "wifi6": true, "clients": 1,
              "dual_band": false, "dual_active": false, "dual_band_block": null},
  "wan_ip": "100.64.12.34", "wan_iface": "rmnet_data2",
  "temperature": {"c": 41.2, "raw_c": 41.8, "state": "SAFE", "safe": true, "warn_c": 44, "gate_c": 46, "bench": false},
  "battery": {"level": 80, "plugged": "ac", "temp_c": 33.1},
  "tailscale": {"state": "running", "ip": "100.110.36.27"},
  "airplane": false
}
```

| Field | Type | Source and rule |
|---|---|---|
| `v` | int | Always `1`. |
| `updated_at` | RFC3339 | Time of the last slow tick. |
| `slow_age_s` | int | Seconds since the last slow tick. A value above 30 means the collector is stuck, and clients treat it as stale. |
| `signal` | obj\|null | `deviceSignal()` (`signal.go:142`) once per slow tick. `null` when `!Available`. |
| `signal.display` / `tech` / `operator` | string | `Signal.Display` / `Tech` / `Operator`. |
| `signal.bars` | int 0..4\|null | `Signal.Level`. `null` when `Tech=="NR" && Level==0`. |
| `usage.*` | | `s.usage.Snapshot()`. Maps to `PeriodBytes`, `LimitBytes` (0 = no cap), `TodayBytes` and `Period`. `NextReset` of `""` becomes `null`. |
| `usage.left_bytes` | int\|null | `max(0, limit-period)`. `null` when `limit_bytes==0`. |
| `speed.wan_*` | uint | Δ of the summed `rmnet_data*` rx (down) and tx (up) from `/proc/net/dev`, every 1 s. |
| `speed.per_client` | array \| `"unavailable"` | nl80211 station counters: AP TX = client down, AP RX = client up. Otherwise `"unavailable"`. A semSmartHS BPF read is not in phases 1-3. |
| `per_client[].ipv4` | string\|null | Joined by MAC with `parseClients` (`clients.go:50`). |
| `per_client[].signal_dbm` | int\|null | `STA_INFO_SIGNAL`. |
| `speed.phone_*` | uint\|null | `max(0, wan − Σclients)`. `null` when `per_client=="unavailable"`. |
| `hotspot.active` | bool | `s.hs.Status().Active`. |
| `hotspot.ssid` / `band` | string\|null | Cached `APConfig`. `band` is `"dual"` when `dual_active`. |
| `hotspot.wifi6` | bool\|null | From `readHotspotVia()`: `"sem"` gives `true`, `"tm"` gives `false`, unknown gives `null`. |
| `hotspot.clients` | int | `len(per_client)` when that is an array. Otherwise the cached `deviceClients().Count`. |
| `hotspot.dual_band` | bool | The persisted `hotspot.dual_band`. |
| `hotspot.dual_active` | bool | `HotspotStatus.Dual`. |
| `hotspot.dual_band_block` | `"whitelist"`\|`"auto_switch"`\|null | `s.dualBandBlock()`. |
| `wan_ip` / `wan_iface` | string\|null | `deviceWanIP()` (`airplane.go:28`). `null` when `!Available`. |
| `temperature.c` | float\|null | `s.smooth.median()`. Falls back to `raw_c` when `!ok`. |
| `temperature.raw_c` | float | `max(th.BatteryC, th.MaxC)`, the `handleStatus` pattern at `server.go:496-500`. |
| `temperature.state` | `SAFE\|WARM\|HOT\|COOLDOWN\|RECOVERY` | `s.pol.Evaluate(raw_c)`. When `th.Source=="degraded"` it evaluates `effectiveGateC()`, which fails closed. |
| `temperature.safe` / `warn_c` / `gate_c` / `bench` | | `th.Safe`, `s.effectiveWarnC()`, `s.effectiveGateC()` and `s.bench.Active()`. |
| `battery` | obj\|null | `s.col.Battery()`. `null` when `!Available`. |
| `tailscale` | obj | The cached `Brief()`. |
| `airplane` | bool\|null | `airplaneOn()` (`airplane.go:59`). |

**Cadence:**
- The speed tick runs every 1 s.
- The slow tick runs every 5 s, on its own loop.
- `RefreshAPConfig` runs every 60 s, and synchronously after a dual-band, preset or band change.
- `ListenAndServe` runs `collectSlow()` once before it listens.

#### 1.2 `GET /v1/tailscale`

`guard("read-status")`. It makes a live LocalAPI call with a 3 s timeout and always returns 200.

```json
{"state":"running","backend_state":"Running","ip":"100.110.36.27","ips":["100.110.36.27","fd7a:115c:a1e0::1"],
 "dns_name":"zflip5.tailXXXX.ts.net","hostname":"zflip5","online":true,"key_expiry":"2027-04-01T00:00:00Z","expired":false,
 "tailnet":"user@example.com","serve_ok":true,"web_url":"https://zflip5.tailXXXX.ts.net/","version":"1.80.0","health":[],"error":null}
```

- **`state` enum:**

  | `state` | Source |
  |---|---|
  | `running` | `Running` |
  | `needs_login` | `NeedsLogin`, `NoState` |
  | `needs_machine_auth` | `NeedsMachineAuth` |
  | `stopped` | `Stopped` |
  | `starting` | `Starting` |
  | `in_use_other_user` | `InUseOtherUser` |
  | `not_running` | socket `ENOENT` or `ECONNREFUSED` |
  | `error` | any other failure; `error` holds the message |

- **Null fields:** every field except `state`, `serve_ok`, `online`, `expired`, `ips` and `health` is `null` unless the state is `running`. `ips` and `health` are never `null`. `dns_name` has its trailing dot trimmed.
- **`serve_ok`:** `Web["<fqdn>:443"].Handlers["/"].Proxy == "http://127.0.0.1:<cfg.BindPort>"`.
- **Never emitted:** `AuthURL`, `BrowseToURL`, keys, `Prefs`, `Persist`, `Peer`, `User`.

#### 1.3 `POST /v1/tailscale/login`

`guardApp(POST)`, no body.

- **200:** `{"state":"needs_login","login_url":"https://login.tailscale.com/a/…"}`, or `{"state":"running","login_url":null}` when already logged in.

Flow:
1. Open the watch: `GET /localapi/v0/watch-ipn-bus?mask=2`.
2. If the state is `NoState`, send `POST /start {}`.
3. Send `PATCH /prefs` with `WantRunning`, `Hostname:"zflip5"` and the saved prefs, each with its `*Set` flag.
4. Send `POST /login-interactive`.
5. Return the first `BrowseToURL` within 20 s. As a fallback, poll `/status` `.AuthURL`.

A background watcher runs for up to 3 min. On `State==6` it calls `ensureServe` once.

| Error | Body |
|---|---|
| 403 | `app_only` |
| 409 | `needs_machine_auth` |
| 409 | `busy` |
| 503 | `not_running` |
| 504 | `no_login_url` |
| 502 | `localapi` |

#### 1.4 `POST /v1/tailscale/logout`

`guardApp(POST)`.

- **200:** `{"state":"needs_login"}`.
- **Before logout:** save `{Hostname, NetfilterMode, CorpDNS, RouteAll, ShieldsUp, RunSSH, AdvertiseRoutes}` to `/data/adb/zflip5-modem/tailscale-prefs.json` (0600). Then send `POST /logout`, where 204 or 200 counts as OK.
- **Errors:** 403 `app_only`, 503 `not_running`, 502 `localapi`.

#### 1.5 `POST /v1/hotspot/dual`

`guardAuth("radio-control", POST)`. Body capped at 256 B.

- **Request:** `{"enabled": bool, "apply_now": bool}`. `apply_now` defaults to `false`.
- **200:** `{"dual_band": bool, "applied": "now"|"next_start", "hotspot": <HotspotStatus>}`.

Order of checks:

| # | Condition | Result |
|---|---|---|
| 1 | Bad body | 400 `"bad body"` |
| 2 | `airplaneBusy`, or `presets.ApplyBusy()` | 409 `busy` |
| 3 | Under `s.cfgMu`: `enabled` and the whitelist is non-empty | 409 `whitelist`, `"clear the allowlist first: dual band stops Wi-Fi scanning"` |
| 4 | Under `s.cfgMu`: `enabled` and `AutoOn()` | 409 `auto_switch`, `"turn off preset auto-switch first: dual band stops Wi-Fi scanning"` |
| 5 | Pass | `persistHotspotDualBand`, `cfg.Hotspot.DualBand`, `hs.SetDualBand`, then `hs.SetBand("dual"\|"5")` |
| 6 | Helper failure | Revert all three, then 502 with reason `helper` |
| 7 | Unlock | `RefreshAPConfig()` |
| 8 | `apply_now && apUp` | `bounceHotspot()`, which is `stopHotspot`, 1.5 s, then `restartHotspotRetry`; `applied="now"` |

Disabling is never blocked by checks 3 and 4.

#### 1.6 Changed responses

| Endpoint | Change | Phase |
|---|---|---|
| `POST /v1/hotspot/band` | Deprecated alias over `applyDualBand(…, true)`. Sends `Deprecation: true`. 409s carry their `reason`. The 200 body stays `HotspotStatus`. | 1c |
| `GET /v1/hotspot` | `HotspotStatus` gains `dual_band` and `wifi6` (bool or null). | 1b |
| `POST /v1/hotspot/whitelist` | A non-empty list while dual band is on → 409 `dual_band`, `"turn off dual band first: it stops Wi-Fi scanning"`. | 1c |
| `POST /v1/presets/auto` | `{"on":true}` while dual band is on → the same 409. | 1c |
| `POST /v1/presets` | `band:"dual"` → 400 `"band must be 2, 5, or 6"`. Stored `"dual"` migrates to `"5"` once at startup. | 1b/1c |
| `POST /v1/presets/apply` | With dual band on, the helper gets `dual`. Band `"5"` appends channel `149`. Calls `RefreshAPConfig` after success. | 1b/1c |
| `POST /v1/tether?action=start` | Uses the new `startHotspot`. The body is unchanged. | 1b |
| `POST /v1/dashboard/open`, `/control` | `guardApp(POST)`. | 1c |
| `GET /v1/status` | Drops `cover_home` (1a) and `cover_accent` (3a). | 1a/3a |
| `GET /v1/qr` | Encodes `pairTarget(r)`: the tailnet `web_url` for a loopback host, else `scheme://host/`, plus `?token=` only when `!openReads`. | 3a |
| Any guarded route | An app-UID caller skips token, scope and rate-limit checks. | 1c |
| Removed → 404 | `/v1/health`, `/v1/network`, `/v1/thermal/history`, `/v1/bands`, `/v1/prefer5g`, `/v1/speedtest`, `/v1/sms/recent`, `/v1/notifications/recent`, `/v1/cover/home` | 1a |
| Removed → 404 | `/v1/signal`, `/v1/battery`, `/v1/cover/accent`, `/dashboard` | 3a |

### 2. Go (package `main`, `daemon/`)

#### 2.1 `apierr.go`

```go
func writeErrReason(w http.ResponseWriter, code int, msg, reason string) // {"code","error","reason"}
```

#### 2.2 `peeruid.go`

```go
const appPackage = "com.zflip5.modem"
var packagesListPath = "/data/system/packages.list"
var procNetTCPPaths = []string{"/proc/net/tcp", "/proc/net/tcp6"}
type ctxKey int
const ctxPeerUID ctxKey = 0
type appAuth struct { mu sync.Mutex; uid int; ok bool; at time.Time; now func() time.Time; read func(string) ([]byte, error) }
func newAppAuth() *appAuth
func (a *appAuth) UID() (int, bool)            // cache 30 s; re-read when stale or not found
func (a *appAuth) ConnContext(ctx context.Context, c net.Conn) context.Context
func (a *appAuth) IsApp(r *http.Request) bool  // false if either UID unknown or peer UID == 0
func withPeerUID(ctx context.Context, uid int) context.Context
func peerUIDFrom(ctx context.Context) (int, bool)
func parsePackagesList(b []byte, pkg string) (int, bool)
func parseProcNetTCP(b []byte, clientPort, serverPort int) (int, bool)
func socketUID(read func(string) ([]byte, error), clientPort, serverPort int) (int, bool)
```

`parseProcNetTCP` matches the client-side row only:
- `local_address` is `0100007F:%04X(clientPort)` or `0000000000000000FFFF00000100007F:%04X`.
- `rem_address` has port `%04X(serverPort)`.
- The UID is field 7.

#### 2.3 Speed and nl80211

```go
// speed.go
var procNetDevPath = "/proc/net/dev"
var bridgeBrifDir = "/sys/class/net/" + bridgeIface + "/brif"
type ifaceBytes struct{ RX, TX uint64 }
func parseNetDev(raw string) map[string]ifaceBytes   // parts[1]=rx, parts[9]=tx, as readMobileBytes (usage.go:577)
type ClientSpeed struct { MAC string `json:"mac"`; IPv4 *string `json:"ipv4"`; DownBytesS uint64 `json:"down_bytes_s"`; UpBytesS uint64 `json:"up_bytes_s"`; SignalDBm *int `json:"signal_dbm"` }
type SpeedSnap struct { IntervalMs int `json:"interval_ms"`; WanDownBytesS, WanUpBytesS uint64 /* wan_down_bytes_s, wan_up_bytes_s */; PhoneDownBytesS, PhoneUpBytesS *uint64; PerClient any `json:"per_client"` }
type speedSampler struct { mu sync.Mutex; prevWAN ifaceBytes; prevSta map[string]ifaceBytes; prevAt time.Time; now func() time.Time; readDev func() (string, error); dump func(string) ([]Station, error); snap SpeedSnap; still int; warned bool }
func newSpeedSampler() *speedSampler
func (sp *speedSampler) tick(apIfaces []string, clients []Client)
func (sp *speedSampler) Snapshot() SpeedSnap
func apIfaces() []string
// nl80211.go (portable)
type Station struct { MAC string; RXBytes, TXBytes uint64; SignalDBm int; InactiveMs uint32 }
func parseStations(b []byte) ([]Station, error)          // raw netlink bytes; correction: not []syscall.NetlinkMessage (linux-only type)
// nl80211_linux.go
func dumpStations(iface string) ([]Station, error)
func genlFamilyID(fd int, name string) (uint16, error)
// nl80211_other.go (//go:build !linux): dumpStations returns errors.New("nl80211: linux only")
// usage.go
func (u *UsageTracker) Snapshot() UsageReport            // Report() without sampleLocked()
```

`PerClient` becomes `"unavailable"` in any of these cases. Each cause is logged once, and the rule is not sticky.
- `dumpStations` errors.
- It returns 0 entries while a client is REACHABLE.
- Every counter is unchanged for 3 ticks while WAN Δ > 0.

#### 2.4 Hotspot, presets and dual band

```go
// hotspot.go
var helperRun = runHelper; var apProbe = hotspotActive; var semRetryGap = 2 * time.Second; var settingsCmd = runCmd
var hotspotViaPath = "/data/adb/zflip5-modem/hotspot-via"
func ensureWifi6Gate()
func startHotspot() bool
func readHotspotVia() string        // "sem"|"tm"|""
func wifi6FromVia(via string) *bool
type APConfig struct{ SSID, Band, Security string }
func parseAPConfig(out string) (APConfig, bool)
func (h *HotspotController) SetDualBand(on bool)
func (h *HotspotController) DualBand() bool
func (h *HotspotController) APConfig() (APConfig, bool)
func (h *HotspotController) RefreshAPConfig()
func (h *HotspotController) SetBand(band string) (HotspotStatus, error) // config write only, no bounce
// HotspotStatus: DualBand bool `json:"dual_band"`; Wifi6 *bool `json:"wifi6"`
// presets.go
func (pm *PresetManager) MigrateDualBand() bool
func (pm *PresetManager) ApplyBusy() bool
func applyPresetLive(p Preset, dual bool) error
// PresetManager.dual func() bool
// dualband.go
var bounceHotspot = func() { stopHotspot(); time.Sleep(1500 * time.Millisecond); restartHotspotRetry() }
func (s *Server) dualBandBlock() string
func (s *Server) applyDualBand(on, applyNow bool) (st HotspotStatus, applied string, code int, reason string, err error)
func (s *Server) handleHotspotDual(w http.ResponseWriter, r *http.Request)
```

`TetherStart.java` modes:
- `sem`: prints `RESULT=STARTED via=SemWifiManager.setWifiApEnabled`, or `RESULT=FAILED reason=sem_rejected`, or `RESULT=FAILED reason=sem_unavailable:<msg>`. It never falls through to TM.
- `tm`: prints `RESULT=STARTED via=TetheringManager` or `RESULT=FAILED code=N`.
- No argument, `stop` and `hotspot`: unchanged.

#### 2.5 `tailscale.go`

```go
var tailscaleSock = "/data/adb/tailscale/tailscaled.sock"
var tailscalePrefsPath = "/data/adb/zflip5-modem/tailscale-prefs.json"
var loginURLWait = 20 * time.Second; var loginWatchFor = 3 * time.Minute
var errTSNotRunning = errors.New("tailscaled not running")
const localAPIHost = "local-tailscaled.sock"
type TailscaleBrief struct { State string `json:"state"`; IP *string `json:"ip"` }
type TailscaleStatus struct { State string; BackendState, IP *string; IPs []string; DNSName, Hostname *string; Online bool; KeyExpiry *string; Expired bool; Tailnet *string; ServeOK bool; WebURL, Version *string; Health []string; Error *string } // json tags = §1.2 keys
type tailscaleClient struct { hc *http.Client; port int; loginBusy atomic.Bool; mu sync.Mutex; brief TailscaleBrief }
func newTailscaleClient(sock string, servePort int) *tailscaleClient
func mapBackendState(s string) string
func (t *tailscaleClient) Status(ctx context.Context) TailscaleStatus
func (t *tailscaleClient) RefreshBrief(ctx context.Context)
func (t *tailscaleClient) Brief() TailscaleBrief   // default {State:"not_running"}
func (t *tailscaleClient) Login(ctx context.Context) (state, loginURL string, code int, reason string, err error)
func (t *tailscaleClient) Logout(ctx context.Context) error
func (t *tailscaleClient) ensureServe(ctx context.Context) error
func (t *tailscaleClient) handleStatus(w http.ResponseWriter, r *http.Request)
func (t *tailscaleClient) handleLogin(w http.ResponseWriter, r *http.Request)
func (t *tailscaleClient) handleLogout(w http.ResponseWriter, r *http.Request)
```

#### 2.6 `summary.go`

```go
type SummarySignal  struct { Display, Tech, Operator string; Bars *int }                                   // json: display, tech, operator, bars
type SummaryUsage   struct { PeriodBytes uint64; LimitBytes int64; TodayBytes uint64; LeftBytes *int64; Period string; NextReset *string }
type SummaryHotspot struct { Active bool; SSID, Band *string; Wifi6 *bool; Clients int; DualBand, DualActive bool; DualBandBlock *string }
type SummaryTemp    struct { C *float64; RawC float64; State string; Safe bool; WarnC, GateC float64; Bench bool }
type SummaryBattery struct { Level int; Plugged string; TempC float64 }
type Summary struct { V int; UpdatedAt string; SlowAgeS int; Signal *SummarySignal; Usage SummaryUsage; Speed SpeedSnap; Hotspot SummaryHotspot; WanIP, WanIface *string; Temperature SummaryTemp; Battery *SummaryBattery; Tailscale TailscaleBrief; Airplane *bool }
type summaryState struct { mu sync.RWMutex; slow Summary; at time.Time; clients ClientsReport }
func summarySignal(sig Signal) *SummarySignal
func summaryUsage(r UsageReport) SummaryUsage
func (s *Server) summaryTemp(th Thermal) SummaryTemp
func (s *Server) runSummary()
func (s *Server) collectSlow()
func (s *Server) summary() Summary
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request)
```

All JSON tags are the snake-case keys from §1.1.

#### 2.7 Server wiring and config

- **`Server` fields:** `app *appAuth`, `speed *speedSampler`, `ts *tailscaleClient`, `sum summaryState`.
- **`NewServer`:**
  - `cfg.RateLimits = normalizeRateLimits(cfg.RateLimits)`
  - `s.hs.SetDualBand(cfg.Hotspot.DualBand)`
  - `s.presets.dual = s.hs.DualBand`
  - `s.app = newAppAuth()`
  - `s.speed = newSpeedSampler()`
  - `s.ts = newTailscaleClient(tailscaleSock, cfg.BindPort)`
- **`ListenAndServe`:** `s.presets.MigrateDualBand()`, then `s.collectSlow()`, then `go s.runSummary()`, then `http.Server{…, ConnContext: s.app.ConnContext}`.
- **`guard` and `guardAuth`:** after the method check, `if s.app.IsApp(r) { h(w, r); return }`.
- **New guard:** `func (s *Server) guardApp(method string, h http.HandlerFunc) http.HandlerFunc`. It returns 405, then 403 `writeErrReason(403,"app only","app_only")`.
- **Routes:**

  | Path | Guard |
  |---|---|
  | `/v1/summary` | `guard("read-status")` |
  | `/v1/tailscale` | `guard("read-status")` |
  | `/v1/tailscale/login`, `/logout` | `guardApp(POST)` |
  | `/v1/hotspot/dual` | `guardAuth("radio-control", POST)` |
  | `/v1/dashboard/open`, `/control` | `guardApp(POST)` |

- **`config.go`:**
  - `type RateLimits struct { DefaultPerMin int \`json:"default_per_min"\`; RadioPerMin int \`json:"radio_per_min"\` }`. This is a named type now; it used to be an anonymous struct.
  - `Hotspot.DualBand bool \`json:"dual_band"\``
  - `func persistHotspotDualBand(path string, on bool) error`
  - `func normalizeRateLimits(rl RateLimits) RateLimits`: 0 or 120 → 600, 0 or 6 → 60, in memory only.
  - Remove in 1a: `Ingress`, `SMS`, `SMSPerMin`, `Cover.Dim` and `CoverDim()`.
  - Remove in 3a: `Cover` and `persistCoverAccent`.
  - Decoding ignores old keys.
- **Config keys:**

  | Key | Change |
  |---|---|
  | `hotspot.dual_band` | Added, default `false` |
  | `rate_limits.default_per_min` | Default 600 |
  | `rate_limits.radio_per_min` | Default 60 |
  | `ingress`, `sms`, `rate_limits.sms_per_min`, `tokens.sms`, `cover.dim` | Removed in 1a |
  | `cover` | Removed in 3a |

### 3. Kotlin (`helper/cover-app/`, package `com.zflip5.modem`)

#### 3.1 Layout

- **Build:**
  - `compileSdk 37`, `minSdk 33`, `targetSdk 34`, `versionCode 2`, `versionName "0.2.0"`.
  - Signing config `zf5` uses `../../dist/debug.keystore` with alias `zf5` and passwords `android`.
  - The release build runs R8 with shrink resources.
- **Dependencies:**
  - Runtime: compose-bom, material3, ui, activity-compose, navigation-compose and zxing-core. No serialization, Glance or WorkManager. JSON goes through `org.json`.
  - Test: ui-test-junit4, junit, robolectric and coroutines-test. ui-test-manifest is a debug dependency.
- **`NavHost` routes:** `"home"`, `"more"`, `"devices"`, `"hotspot"`, `"data"`, `"system"`, `"tailscale"`, `"alerts"`, `"advanced"`.
- **Manifest:**
  - `INTERNET`, `allowBackup=false`, and a cleartext config for `127.0.0.1` and `localhost` only.
  - `.CoverKioskActivity`: `exported`, `singleTask`, `LAUNCHER` only.
  - Widget entries: added by P2-C in Task 2.13.
- **Sources:**
  - `data/{Models,SummaryParser,DaemonClient,SummaryRepository,Format}.kt`
  - `ui/theme/*`, `ui/components/*`, `ui/home/HomeScreen.kt`
  - `ui/more/*`
  - `widget/{FlippostWidget,WidgetRenderer,WidgetRefresh,RotateIpActivity}.kt`

#### 3.2 Models

```kotlin
data class Summary(val v: Int, val updatedAt: String, val slowAgeS: Int, val signal: SignalInfo?, val usage: UsageInfo, val speed: SpeedInfo,
  val hotspot: HotspotInfo, val wanIp: String?, val wanIface: String?, val temperature: TempInfo, val battery: BatteryInfo?, val tailscale: TailscaleBrief, val airplane: Boolean?)
data class SignalInfo(val display: String, val tech: String, val operator: String, val bars: Int?)
data class UsageInfo(val periodBytes: Long, val limitBytes: Long, val todayBytes: Long, val leftBytes: Long?, val period: String, val nextReset: String?)
data class SpeedInfo(val intervalMs: Int, val wanDownBytesS: Long, val wanUpBytesS: Long, val phoneDownBytesS: Long?, val phoneUpBytesS: Long?, val perClient: List<ClientSpeed>?) // null ⇔ "unavailable"
data class ClientSpeed(val mac: String, val ipv4: String?, val downBytesS: Long, val upBytesS: Long, val signalDbm: Int?)
data class HotspotInfo(val active: Boolean, val ssid: String?, val band: String?, val wifi6: Boolean?, val clients: Int, val dualBand: Boolean, val dualActive: Boolean, val dualBandBlock: String?)
data class TempInfo(val c: Double?, val rawC: Double, val state: String, val safe: Boolean, val warnC: Double, val gateC: Double, val bench: Boolean)
data class BatteryInfo(val level: Int, val plugged: String, val tempC: Double)
data class TailscaleBrief(val state: String, val ip: String?)
data class TailscaleStatus(val state: String, val backendState: String?, val ip: String?, val ips: List<String>, val dnsName: String?, val hostname: String?, val online: Boolean,
  val keyExpiry: String?, val expired: Boolean, val tailnet: String?, val serveOk: Boolean, val webUrl: String?, val version: String?, val health: List<String>, val error: String?)
data class LoginResult(val state: String, val loginUrl: String?)
data class DualResult(val dualBand: Boolean, val applied: String)
```

#### 3.3 Client and repository

```kotlin
class DaemonException(val code: Int, override val message: String, val reason: String?) : Exception(message)
class DaemonDownException(cause: Throwable) : java.io.IOException("service not running", cause)
open class DaemonClient(private val base: String = "http://127.0.0.1:18080") {
  open suspend fun get(path: String, timeoutMs: Int = 3_000): JSONObject
  open suspend fun post(path: String, body: JSONObject? = null, timeoutMs: Int = 10_000): JSONObject
  suspend fun summary(): Summary; suspend fun tailscale(): TailscaleStatus
  suspend fun tailscaleLogin(): LoginResult; suspend fun tailscaleLogout()
  suspend fun setDualBand(enabled: Boolean, applyNow: Boolean): DualResult
  suspend fun tether(start: Boolean): JSONObject; suspend fun rotateIp(): JSONObject
}
data class SummaryState(val summary: Summary?, val lastOkAtMs: Long, val error: String?, val down: Boolean) {
  fun stale(nowMs: Long) = nowMs - lastOkAtMs > 5_000 || (summary?.slowAgeS ?: 0) > 30
}
fun SummaryState.writable(nowMs: Long): Boolean = summary != null && !down && !stale(nowMs)
class SummaryRepository(private val client: DaemonClient, private val scope: CoroutineScope, private val now: () -> Long = System::currentTimeMillis) {
  val state: StateFlow<SummaryState>; fun start(); fun stop(); fun refreshNow()
}
```

The app sends no tokens and no `Authorization` header. The widget runs in the same UID.

#### 3.4 Widget

The widget uses RemoteViews and needs no new dependency.
- Actions: `com.zflip5.modem.widget.TOGGLE` and `com.zflip5.modem.widget.REFRESH`.
- `WidgetRefresh.schedule` sets an inexact 15-minute alarm. `refreshAll` sends REFRESH right away.
- `RotateIpActivity`: not exported, excluded from recents.

### 4. Phases and ownership

Sub-phases run in order. Tasks inside one sub-phase run in parallel unless the task says otherwise. No file has two owners within a sub-phase.

| Sub | Owner | Tasks | Owns |
|---|---|---|---|
| 1a | P1-A | 1.1–1.4 (sequential) | `daemon/{server,config,main,auth,apierr,coverhome,coverrestore,bands,speedtest,temps*,sms*,notifications*,policy,policy_test,server_test,cover_test}.go`, `daemon/go.{mod,sum}`, `schemas/config.schema.json`, `tests/fixtures/*`, `tools/validate-contracts.py`, `tests/validate-contracts.test.sh`, `tools/security-check.sh`, `tools/test-5g-policy.sh`, `tools/run-tests.sh`, `tests/shortcuts.test.sh`, `tools/replay-shortcuts.sh`, `magisk/service.sh` |
| 1b | P1-B | 1.5 | `daemon/peeruid{,_test}.go` |
| 1b | P1-C | 1.6–1.7 | `daemon/nl80211{,_linux,_other,_test}.go`, `daemon/testdata/station_dump.bin`, `daemon/speed{,_test}.go`, `daemon/usage{,_test}.go` |
| 1b | P1-D | 1.8–1.11 | `daemon/hotspot{,_test}.go`, `presets{,_test}.go`, `dualband{,_test}.go`, `helper/tether/TetherStart.java` |
| 1b | P1-E | 1.12–1.13 | `daemon/tailscale{,_test}.go`, `docs/tailscale.md` |
| 1c | P1-F | 1.14–1.16 | `daemon/summary{,_test}.go`, `daemon/server{,_test}.go`, `schemas/api.openapi.json` |
| 2a | P2-A | 2.1–2.5 | `helper/cover-app/**` except `ui/more/**` bodies, `widget/**`, `res/layout/widget_*`, `res/xml/flippost_widget_info.xml` and `res/drawable/widget_*`; plus the `git mv` of `helper/res/*` |
| 2a | P2-D | 2.6–2.7, start after 2.1 commits | `helper/build-apk.sh`, `magisk/{service,action,app-install}.sh`, `tools/run-tests.sh`, `.gitignore`, `tests/app-install.test.sh`, and the deletion of `helper/src/`, `helper/AndroidManifest.xml` and `helper/res/values/strings.xml` |
| 2b | P2-B | 2.8–2.12 | `…/ui/more/**` |
| 2b | P2-C | 2.13–2.14 | `…/widget/**`, `res/layout/widget_flippost.xml`, `res/xml/flippost_widget_info.xml`, `res/drawable/widget_*`, `app/src/main/AndroidManifest.xml` |
| 2c | orchestrator | 2.15 | none |
| 3a | P3-A | 3.1–3.3 | `daemon/**`, `schemas/{api.openapi,config.schema}.json`, `tests/{lunar,web}.test.sh` |
| 3a | P3-B | 3.4–3.5 | `magisk/**` except `magisk/ntfy/**`, `magisk/firewall.sh`, `magisk/cleanup.sh`, `helper/tether/SetWallpaper.java`, `tools/build-tether.sh`, `tools/dex.sh`, `tests/boot.test.sh` |
| 3a | P3-C | 3.6–3.7 | `magisk/ntfy/**`, `selfhost/**`, `relay/`, `fixtures/discord/`, `tools/{test-discord-relay,probe-controls,collect-metrics}.sh`, `tests/{probe-controls,collect-metrics,removed}.test.sh`, `tests/ntfy/**`, `tools/run-tests.sh`, `.gitignore` |
| 3a | P3-D | 3.8 | `docs/**` except `tailscale.md`, `README.md`, `PRODUCT.md`, `DESIGN.md`, `dwkpnx6.md` |
| 3b | P3-E | 3.9 | `tools/validate-contracts.py`, `tests/validate-contracts.test.sh` |
| 3c | orchestrator | 3.10 | none |

In 1b, `airplane.go`, `clients.go`, `signal.go` and `collect.go` are read-only.

Known gap: the on-phone ntfy SMS, signal and battery rules get 404s from 1a and 3a until the 3.6 runner is installed.

### 5. Decisions awaiting the owner's confirmation

1. The Alerts settings (ntfy on/off, rules) and the battery cap stay read-only in the app and web. No daemon API exists for them, so making them editable needs `/v1/alerts` and `/v1/battery/cap`.
2. Legacy rate limits are raised in memory only. The values on disk are never rewritten.
3. On 5G SA, `bars` is `null`.
4. nl80211 counters are unverified on qcacld. `"unavailable"` is an accepted result.
5. The Wi-Fi 6 gate follows `zf5-onigiri-rom.md:17`, which says it is required. This contradicts `zf5-softap-wifi6.md`, which calls it a dead end. The client PHY check (1.16 Step 8) settles it.
6. The ntfy SMS rule stays broken from 1a until 3.6.
7. New libraries: zxing-core at runtime, which draws the QR codes, and Robolectric for tests only.
8. The app hides the system bars, so the home gets the full 352×340 dp.
9. The `pairTarget` QR for the app encodes the tailnet `web_url`.
10. `uninstall.sh` leaves `50-zf5-firewall.sh` in `service.d`.
11. The stock-settings cleanup runs once, gated by a `stock-restored` marker.
12. The web UI uses Nunito with a system fallback and downloads no font.
13. Item 12 (the accidental-touch setting): nothing in the repo sets it, so there is nothing to remove or restore.

---

## Phase 1: daemon foundation

### Task 1.1: Delete the dropped daemon features (P1-A, 1a)

**Files:**
- Delete:
  - `daemon/sms.go`, `daemon/sms_test.go`
  - `daemon/notifications.go`, `daemon/notifications_test.go`
  - `daemon/speedtest.go`
  - `daemon/temps.go`, `daemon/temps_test.go`
  - `daemon/bands.go`
  - `daemon/coverhome.go`
- Create: `daemon/coverrestore.go`. It holds `coverBrightPrev`, `coverBrightModePrev` and `restoreCoverBrightness()`, moved verbatim from `coverhome.go:36-38,61-75`.
- Modify `daemon/server.go`:
  - fields `temps` (:39-43) and `speedtestBusy` (:58-59), plus the `openReads` comment (:51-53, which drops the SMS wording)
  - `NewServer` :113
  - routes :252, :254-257, :267, :294-296, :329-333, :341-343, :349-350. Keep `/v1/battery` and `/v1/signal`; they go in 3a.
  - `guard` SMS clause :378-383, which becomes `need == "read-status"`
  - delete `guardWrite` :406-436
  - `guardAuth` comment :446-448
  - `limitFor` `case "sms"` :474-475
  - handlers `handleHealth` :483, `handleThermalHistory` :491, `handleNetwork` :528, `handleSMSRecent` :536-564, `handleNotificationsRecent` :566-596, `handleBands` :651, `handleSpeedtest` :750-759, `handlePrefer5G` :1005-1031 and `handleCoverHome` :1059-1080
  - the `"cover_home"` key :518
  - `s.temps.Flush()` :1119 and `go s.temps.run()` :1179-1181
- Modify `daemon/config.go`:
  - delete `Ingress` (:17-19), `SMS` (:26-31), `RateLimits.SMSPerMin` (:34), `Cover.Dim` (:49-53) and `CoverDim()` (:70-84)
  - in `Validate`, delete :328-333 and :348-350, and keep the `loopbackRe` check (:336-338)
- Modify `daemon/main.go:45-52`: the block becomes a single unconditional `restoreCoverBrightness()`.
- Modify `daemon/policy.go`: delete `Prefer5GAllowed` (:76-85) and `readAllowedTypes` (:87-96).
- Modify `daemon/policy_test.go`: delete the tests at :23, :60 and :70. Keep `hotCollector`.
- Modify `daemon/cover_test.go`: delete `TestCoverDimDefaultsAndClamps` (:150-165). Move `postJSON` (:95-105) into `server_test.go`.
- Modify `daemon/server_test.go`:
  - `testCfg` (:28-39) loses the `sms` token, `SMSPerMin` and the `SMS.*` fields
  - re-point `TestWrongMethod405` and `TestWrongScope403`
  - delete `TestThermalHistoryEndpoint` (:267-311)
- Modify: `daemon/go.mod` and `daemon/go.sum`.

**Interfaces:**
- Produces:
  - `restoreCoverBrightness()`, unchanged
  - `testCfg()` with tokens `read-status` = `"a"`×64 and `radio-control` = `"c"`×64, and RateLimits 30/6
  - `postJSON(s *Server, path, token, body string) *httptest.ResponseRecorder`, now in `server_test.go`

- [ ] **Step 1: Write the failing tests in `daemon/server_test.go`**

```go
func TestRemovedRoutes404(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	for _, rt := range []struct{ method, path, tok string }{
		{"GET", "/v1/health", "a"}, {"GET", "/v1/network", "a"}, {"GET", "/v1/thermal/history", "a"},
		{"GET", "/v1/bands", "a"}, {"GET", "/v1/sms/recent", "a"}, {"GET", "/v1/notifications/recent", "a"},
		{"POST", "/v1/prefer5g", "c"}, {"POST", "/v1/speedtest", "c"}, {"POST", "/v1/cover/home", "c"},
	} {
		if w := do(s, rt.method, rt.path, strings.Repeat(rt.tok, 64)); w.Code != 404 {
			t.Errorf("%s %s: want 404, got %d", rt.method, rt.path, w.Code)
		}
	}
}

func TestStatusDropsCoverHome(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if b := do(s, "GET", "/v1/status", strings.Repeat("a", 64)).Body.String(); strings.Contains(b, "cover_home") {
		t.Fatalf("status still has cover_home: %s", b)
	}
}
```

Re-point the two existing tests:
- `TestWrongMethod405`: its second assertion becomes `do(s,"GET","/v1/cooldown","c"×64).Code == 405`.
- `TestWrongScope403`: it asserts `do(s,"POST","/v1/tether","a"×64).Code == 403` and `do(s,"POST","/v1/cooldown","a"×64).Code == 403`.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestRemovedRoutes404|TestStatusDropsCoverHome' .`

Expected: FAIL. The routes return 200, 403 or 405, and the status body contains `cover_home`.

- [ ] **Step 3: Delete the code listed under Files**

Keep these:
- the `Collector` methods `Health`, `Battery` and `Network`, because `handleStatus` uses them
- `signal.go` and `thermalSmoother`
- `cover.go` and `dashboard.go`, untouched

- [ ] **Step 4: Drop the speedtest dependency**

Run: `cd /Users/adacchi/module-zf5/daemon && go mod tidy && cat go.mod`

Expected: the only requirement is `github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e`.

- [ ] **Step 5: Run the whole package**

Run: `cd /Users/adacchi/module-zf5/daemon && go vet ./... && go test -race ./...`

Expected: `ok  	zflip5/modemd`.

- [ ] **Step 6: Commit**

`git add -A daemon`. Subject: `refactor(daemon): remove SMS, speedtest, temps, cover home and dropped endpoints`.

---

### Task 1.2: Add error reasons, rate-limit defaults and the dual-band key (P1-A, 1a)

**Files:**
- Create: `daemon/apierr.go`
- Modify `daemon/config.go`:
  - add the named `RateLimits` type
  - `Hotspot.DualBand`
  - `persistHotspotDualBand`, next to `persistHotspotWhitelist` (:230)
  - `normalizeRateLimits`
- Modify `daemon/server.go`: `NewServer` (:102) applies `normalizeRateLimits`.
- Test: `daemon/server_test.go`

**Interfaces:**
- Produces: `writeErrReason`, `RateLimits`, `persistHotspotDualBand` and `normalizeRateLimits`, as in contract §2.1 and §2.7.

- [ ] **Step 1: Write the failing tests**

```go
func TestWriteErrReason(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrReason(w, 409, "clear the allowlist first", "whitelist")
	if w.Code != 409 || w.Body.String() != `{"code":409,"error":"clear the allowlist first","reason":"whitelist"}`+"\n" {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestNormalizeRateLimits(t *testing.T) {
	for _, tc := range []struct{ in, want RateLimits }{
		{RateLimits{120, 6}, RateLimits{600, 60}}, {RateLimits{0, 0}, RateLimits{600, 60}},
		{RateLimits{30, 2}, RateLimits{30, 2}}, {RateLimits{300, 50}, RateLimits{300, 50}},
	} {
		if got := normalizeRateLimits(tc.in); got != tc.want {
			t.Errorf("%+v: want %+v, got %+v", tc.in, tc.want, got)
		}
	}
}

func TestPersistHotspotDualBandPreservesKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"hotspot":{"enable_on_boot":false,"ssid_whitelist":["x"]},"cpu":{"mode":"eco"}}`), 0o600)
	if err := persistHotspotDualBand(p, true); err != nil {
		t.Fatal(err)
	}
	var m map[string]map[string]any
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, &m)
	if m["hotspot"]["dual_band"] != true || m["hotspot"]["enable_on_boot"] != false || m["cpu"]["mode"] != "eco" {
		t.Fatalf("keys lost: %s", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

// Review Focus 1: the live on-device config still carries removed keys and legacy limits.
func TestLoadConfigLegacyKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"bind_host":"127.0.0.1","bind_port":18080,
	 "tokens":{"read-status":"`+strings.Repeat("a", 64)+`","radio-control":"`+strings.Repeat("c", 64)+`","sms":"`+strings.Repeat("d", 64)+`"},
	 "ingress":{"mode":"off"},"sms":{"enabled":false,"path":"/x"},"cover":{"dim":30,"accent":"teal"},
	 "thermal":{"warn_c":44,"gate_c":46,"fail_closed":true},"dashboard":{"open_reads":true,"open_control":true},
	 "rate_limits":{"default_per_min":120,"radio_per_min":6,"sms_per_min":6}}`), 0o600)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("legacy config rejected: %v", err)
	}
	if s := NewServer(cfg, fakeCollector{safe: true}); s.cfg.RateLimits != (RateLimits{600, 60}) || s.cfg.Hotspot.DualBand {
		t.Fatalf("%+v", s.cfg.RateLimits)
	}
}
```

`TestRateLimit429` (:122) stays unchanged and must still pass, because 2 is not a legacy value.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestWriteErrReason|TestNormalizeRateLimits|TestPersistHotspotDualBand|TestLoadConfigLegacyKeys' .`

Expected: build failure, `undefined: writeErrReason`.

- [ ] **Step 3: Implement**

- `persistHotspotDualBand` copies the raw-map pattern of `persistHotspotWhitelist`.
- `writeErrReason` calls `writeJSON` with a `map[string]any`.

- [ ] **Step 4: Run the package**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -race ./...`

Expected: `ok`.

- [ ] **Step 5: Commit**

`git add daemon/apierr.go daemon/config.go daemon/server.go daemon/server_test.go`. Subject: `feat(daemon): add error reasons, loosen rate limits, add hotspot.dual_band`.

---

### Task 1.3: Update the schemas, fixtures and test tooling (P1-A, 1a)

**Files:**
- Modify `schemas/config.schema.json`:
  - :7 `required` drops `"ingress"` and `"sms"`
  - :13 the description drops the `ingress.mode` wording
  - delete `tokens.sms` :24, `ingress` :28-50, `cover.dim` :76, `sms` :99-109 and `sms_per_min` :116
  - `hotspot.properties` gains `"dual_band": {"type":"boolean","default":false}`
  - the defaults become 600 and 60
- Modify `tests/fixtures/config-good.json` and `tests/fixtures/config-bad.json`: drop `tokens.sms`, `ingress`, `sms` and `sms_per_min`.
- Modify `tools/validate-contracts.py`:
  - :22 `REQUIRED_TOKENS` drops `"prefer/recover NR"`
  - delete the SMS checks at :65-70
  - the docstring drops "no-forward / redact-by-default"
- Modify `tests/validate-contracts.test.sh:23`: the token loop drops `'prefer/recover NR'`.
- Modify `tools/security-check.sh`:
  - :36 and :42 drop `"ingress"` and `"sms"`
  - :73 becomes `perm=$(echo 'stat -c %a /data/adb/zflip5-modem/config.json 2>/dev/null' | adb shell su 2>/dev/null | tr -d '\r')`
- Modify `tools/run-tests.sh`: delete the `=== 5G THERMAL POLICY ===` block at :76-85.
- Delete: `tools/test-5g-policy.sh`, `tests/shortcuts.test.sh` and `tools/replay-shortcuts.sh`. These call removed routes, which moves part of item 71 forward.

- [ ] **Step 1: Add the failing assertions to `tests/validate-contracts.test.sh` after the good-config check**

```bash
grep -q '"sms"\|"ingress"' "$HERE/fixtures/config-good.json" && fail "fixture has no removed keys" "sms/ingress present" || pass "fixture has no removed keys"
python3 -c 'import json,sys; s=json.load(open(sys.argv[1])); assert "ingress" not in s["required"] and "sms" not in s["required"]; assert s["properties"]["hotspot"]["properties"]["dual_band"]["default"] is False; assert s["properties"]["rate_limits"]["properties"]["radio_per_min"]["default"]==60' "$ROOT/schemas/config.schema.json" && pass "schema revamp keys" || fail "schema revamp keys" "schema unchanged"
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `bash /Users/adacchi/module-zf5/tests/validate-contracts.test.sh`

Expected: `FAIL - fixture has no removed keys` and `FAIL - schema revamp keys`.

- [ ] **Step 3: Make the edits listed under Files**

- [ ] **Step 4: Run the offline suite**

Run: `bash /Users/adacchi/module-zf5/tests/validate-contracts.test.sh && bash /Users/adacchi/module-zf5/tools/run-tests.sh | tail -3`

Expected: `all validate-contracts tests passed`, `failures=0`, `OFFLINE_TESTS_OK=1`, and the output still reports `check:rate_limit_429=1`.

- [ ] **Step 5: Commit**

`git add -A schemas/config.schema.json tests tools`. Subject: `chore: drop sms, ingress and prefer-5G from schemas, fixtures and tooling`.

---

### Task 1.4: Update the boot script: config template, Wi-Fi 6 gate and Sem-first start (P1-A, 1a)

**Files:**
- Modify `magisk/service.sh`:
  - :31 delete `SM`
  - :33-45 update the template
  - :50-82 delete the ingress block
  - :113-132 rewrite the hotspot start
- Test: `tests/magisk.test.sh`, unchanged

**Interfaces:**
- Produces: `$DATADIR/hotspot-via`, containing `sem` or `tm`. Task 1.8 reads it. The old jar treats both modes as its default path, so the order of deploys does not matter.

- [ ] **Step 1: Write the failing check**

Run: `cd /Users/adacchi/module-zf5 && grep -c 'wifi_ap_11ax_mode_checked 1' magisk/service.sh; grep -c '"ingress"\|"sms"\|sms_per_min' magisk/service.sh`

Expected now: `0` and `3`.

- [ ] **Step 2: Edit the template**

- `tokens` becomes `{ "read-status": "$RS", "radio-control": "$RC" }`.
- Delete the `ingress` and `sms` lines.
- The hotspot line becomes `"hotspot": { "enable_on_boot": true, "ssid_whitelist": [], "dual_band": false },`.
- `"rate_limits": { "default_per_min": 600, "radio_per_min": 60 }`.
- `dashboard` is unchanged.

- [ ] **Step 3: Replace :113-132 with this exact block**

```sh
  # Wi-Fi 6 gate (One UI 7): without it the Sem start runs 11axmode=0 (ac only).
  # A wipe resets it, so set it on every boot, before the first start.
  settings put secure wifi_ap_11ax_mode_checked 1
  settings put global wifi_ap_11ax_mode_checked 1

  # Hotspot on boot from the saved SoftAP config. Sem first (the only 802.11ax
  # path), TetheringManager once as a last resort. hotspot-via tells the daemon
  # which path won, so the app can show "Wi-Fi 6 unavailable".
  HS=$(grep -o '"enable_on_boot"[^,}]*' "$CONFIG" | grep -oE 'true|false' | head -n1)
  if [ "$HS" != "false" ] && [ -f "$MODDIR/tether/tether.jar" ]; then
    n=0; VIA=""
    while [ "$n" -lt 6 ]; do
      OUT=$(CLASSPATH="$MODDIR/tether/tether.jar" app_process /system/bin com.zflip5.tether.TetherStart sem 2>&1)
      echo "$(date): hotspot sem try $n: $(printf '%s' "$OUT" | tr '\n' ' ')" >> "$LOG"
      case "$OUT" in *via=SemWifiManager*) VIA=sem; break ;; esac
      n=$((n + 1)); sleep 10
    done
    if [ -z "$VIA" ]; then
      OUT=$(CLASSPATH="$MODDIR/tether/tether.jar" app_process /system/bin com.zflip5.tether.TetherStart tm 2>&1)
      echo "$(date): hotspot tm fallback: $(printf '%s' "$OUT" | tr '\n' ' ')" >> "$LOG"
      case "$OUT" in *RESULT=STARTED*) VIA=tm ;; esac
    fi
    [ -n "$VIA" ] && echo "$VIA" > "$DATADIR/hotspot-via"
  fi
```

- [ ] **Step 4: Run the checks**

Run: `cd /Users/adacchi/module-zf5 && sh -n magisk/service.sh && echo SYNTAX_OK; grep -c 'wifi_ap_11ax_mode_checked 1' magisk/service.sh; grep -c '"ingress"\|"sms"\|sms_per_min\|tailscaled' magisk/service.sh; bash tests/magisk.test.sh | tail -1`

Expected: `SYNTAX_OK`, `2`, `0`, `all magisk tests passed`.

- [ ] **Step 5: Commit**

`git add magisk/service.sh`. Subject: `feat(magisk): set the Wi-Fi 6 gate at boot and start the hotspot via Sem first`.

---

### Task 1.5: Identify the app by socket UID (P1-B, 1b)

**Files:**
- Create: `daemon/peeruid.go`, `daemon/peeruid_test.go`

**Interfaces:**
- Produces: exactly contract §2.2.
- Rules:
  - `IsApp` is false when the peer UID is unknown, when the app UID is unknown, or when the peer UID is 0.
  - `UID()` serves the cache while `ok && now()-at < 30s`.

- [ ] **Step 1: Write the failing tests**

```go
const pkgList = "com.termux 10325 0 /data/user/0/com.termux default 3003\n" +
	"com.zflip5.modem.debug 999 0 /x default none\n" +
	"com.zflip5.modem 10234 0 /data/user/0/com.zflip5.modem default:targetSdkVersion=34 3003\n"

const tcpHdr = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

func tcpRow(local, rem string, uid int) string {
	return fmt.Sprintf("   1: %s %s 01 00000000:00000000 00:00000000 00000000 %5d        0 1 1 0000000000000000 20 4 30 10 -1\n", local, rem, uid)
}

func TestParsePackagesList(t *testing.T) {
	if uid, ok := parsePackagesList([]byte(pkgList), appPackage); !ok || uid != 10234 {
		t.Fatalf("got %d %v", uid, ok)
	}
	if _, ok := parsePackagesList([]byte("com.termux 10325 0 /x\n"), appPackage); ok {
		t.Fatal("matched a missing package")
	}
}

func TestParseProcNetTCPClientRowOnly(t *testing.T) {
	b := tcpHdr + tcpRow("0100007F:46A0", "00000000:0000", 0) + tcpRow("0100007F:46A0", "0100007F:C350", 0) + tcpRow("0100007F:C350", "0100007F:46A0", 10234)
	if uid, ok := parseProcNetTCP([]byte(b), 0xC350, 18080); !ok || uid != 10234 {
		t.Fatalf("got %d %v", uid, ok)
	}
	if _, ok := parseProcNetTCP([]byte(tcpHdr+tcpRow("0100007F:46A0", "0100007F:C350", 0)), 0xC350, 18080); ok {
		t.Fatal("matched the server-side row")
	}
}

func TestSocketUIDFallsBackToTCP6(t *testing.T) {
	read := func(p string) ([]byte, error) {
		if p == "/proc/net/tcp6" {
			return []byte(tcpHdr + tcpRow("0000000000000000FFFF00000100007F:C350", "0000000000000000FFFF00000100007F:46A0", 10234)), nil
		}
		return []byte(tcpHdr), nil
	}
	if uid, ok := socketUID(read, 0xC350, 18080); !ok || uid != 10234 {
		t.Fatalf("got %d %v", uid, ok)
	}
}

func TestIsAppNeverTrustsUID0(t *testing.T) { // Review Focus 5
	a := newAppAuth()
	a.read = func(string) ([]byte, error) { return []byte("com.zflip5.modem 0 0 /x\n"), nil }
	r := httptest.NewRequest("GET", "/", nil)
	if a.IsApp(r.WithContext(withPeerUID(r.Context(), 0))) {
		t.Fatal("uid 0 trusted")
	}
}

func TestConnContextIsApp(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	cli, _ := net.Dial("tcp", ln.Addr().String())
	defer cli.Close()
	srv, _ := ln.Accept()
	defer srv.Close()
	cp, sp := cli.LocalAddr().(*net.TCPAddr).Port, ln.Addr().(*net.TCPAddr).Port
	a := newAppAuth()
	a.read = func(p string) ([]byte, error) {
		switch p {
		case packagesListPath:
			return []byte(pkgList), nil
		case "/proc/net/tcp":
			return []byte(tcpHdr + tcpRow(fmt.Sprintf("0100007F:%04X", sp), fmt.Sprintf("0100007F:%04X", cp), 0) +
				tcpRow(fmt.Sprintf("0100007F:%04X", cp), fmt.Sprintf("0100007F:%04X", sp), 10234)), nil
		}
		return []byte(tcpHdr), nil
	}
	if !a.IsApp(httptest.NewRequest("GET", "/", nil).WithContext(a.ConnContext(context.Background(), srv))) {
		t.Fatal("app connection not recognised")
	}
	if a.IsApp(httptest.NewRequest("GET", "/", nil)) {
		t.Fatal("unknown peer trusted")
	}
}

func TestUIDCacheRefreshes(t *testing.T) {
	now := time.Unix(1000, 0)
	list := pkgList
	a := newAppAuth()
	a.now = func() time.Time { return now }
	a.read = func(string) ([]byte, error) { return []byte(list), nil }
	if uid, _ := a.UID(); uid != 10234 {
		t.Fatal(uid)
	}
	list = "com.zflip5.modem 10400 0 /x\n"
	if uid, _ := a.UID(); uid != 10234 {
		t.Fatal("cache not used within 30 s")
	}
	now = now.Add(31 * time.Second)
	if uid, _ := a.UID(); uid != 10400 {
		t.Fatal("cache not refreshed after 30 s")
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestParsePackagesList|TestParseProcNetTCP|TestSocketUID|TestIsApp|TestConnContext|TestUIDCache' .`

Expected: build failure, `undefined: appAuth`.

- [ ] **Step 3: Implement contract §2.2**

- Split each row with `strings.Fields`.
- `f[1]` is local and `f[2]` is remote. Split each at the last `:` and parse the port with `strconv.ParseUint(…,16,16)`.
- `f[7]` is the UID.
- When the lookup fails, `ConnContext` returns `ctx` unchanged.

- [ ] **Step 4: Run the tests again**

Run the Step 2 command with `-race -v`.

Expected: 6 × `--- PASS`.

- [ ] **Step 5: Commit**

`git add daemon/peeruid.go daemon/peeruid_test.go`. Subject: `feat(daemon): identify the Flippost app by its socket UID`.

---

### Task 1.6: Read station counters over nl80211 (P1-C, 1b)

**Files:**
- Create:
  - `daemon/nl80211.go`: `Station`, `parseStations` and the constants
  - `daemon/nl80211_linux.go`: `dumpStations` and `genlFamilyID`
  - `daemon/nl80211_other.go`
  - `daemon/nl80211_test.go`
  - `daemon/testdata/station_dump.bin`

**Interfaces:**
- Produces: contract §2.3, nl80211 part.
- `parseStations` rules:
  - It stops at `NLMSG_DONE` (3).
  - `NLMSG_ERROR` (2) with a nonzero errno returns an error.
  - Truncated input returns an error.
- `dumpStations`:
  - Gets the ifindex with `net.InterfaceByName`.
  - Sends `NL80211_CMD_GET_STATION` (17) with `NLM_F_REQUEST|NLM_F_DUMP` and `ATTR_IFINDEX` (3).
  - Opens `syscall.Socket(AF_NETLINK, SOCK_RAW|SOCK_CLOEXEC, NETLINK_GENERIC)` with a 2 s `SO_RCVTIMEO` and a 64 KiB buffer.
  - Concatenates reads until `DONE`.
- `genlFamilyID`: sends `CTRL_CMD_GETFAMILY` (3) to 0x10 and reads attribute 2 → attribute 1.
- Attributes:
  - `ATTR_MAC` = 6, `STA_INFO` = 21
  - inside `STA_INFO`: `INACTIVE` = 1 (u32), `RX` = 2 (u32), `TX` = 3 (u32), `SIGNAL` = 7 (s8), `RX64` = 23 and `TX64` = 24
- Parsing rules:
  - Prefer the 64-bit counters.
  - Mask the attribute type with `0x3fff`.
  - Use 4-byte alignment and little-endian order.
  - Format the MAC as lowercase `aa:bb:…`.

- [ ] **Step 1: Generate the fixture**

```bash
cd /Users/adacchi/module-zf5/daemon && mkdir -p testdata && python3 -I - <<'EOF'
import struct
def nla(t, p):
    l = 4 + len(p); return struct.pack('<HH', l, t) + p + b'\0' * ((4 - l % 4) % 4)
def sta(mac, info):
    body = struct.pack('<BBH', 19, 1, 0) + nla(3, struct.pack('<I', 42)) + nla(6, bytes(mac)) + nla(21 | 0x8000, info)
    return struct.pack('<IHHII', 16 + len(body), 0x1c, 2, 1, 0) + body
s1 = sta([0xaa,0xbb,0xcc,0xdd,0xee,0xff], nla(1, struct.pack('<I',120)) + nla(2, struct.pack('<I',111)) + nla(3, struct.pack('<I',222))
        + nla(7, struct.pack('<b',-48)) + nla(23, struct.pack('<Q',5_000_000_000)) + nla(24, struct.pack('<Q',9_000_000_000)))
s2 = sta([0x11,0x22,0x33,0x44,0x55,0x66], nla(2, struct.pack('<I',1000)) + nla(3, struct.pack('<I',2000)) + nla(7, struct.pack('<b',-71)))
done = struct.pack('<IHHIIi', 20, 3, 2, 1, 0, 0)
open('testdata/station_dump.bin', 'wb').write(s1 + s2 + done)
EOF
ls -l testdata/station_dump.bin
```

Expected: about 180 bytes.

- [ ] **Step 2: Write the failing tests**

```go
func TestParseStationsFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/station_dump.bin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseStations(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []Station{
		{MAC: "aa:bb:cc:dd:ee:ff", RXBytes: 5_000_000_000, TXBytes: 9_000_000_000, SignalDBm: -48, InactiveMs: 120},
		{MAC: "11:22:33:44:55:66", RXBytes: 1000, TXBytes: 2000, SignalDBm: -71},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseStationsTruncated(t *testing.T) {
	b, _ := os.ReadFile("testdata/station_dump.bin")
	if _, err := parseStations(b[:30]); err == nil {
		t.Fatal("truncated dump parsed without error")
	}
}

func TestParseStationsNetlinkError(t *testing.T) {
	b := make([]byte, 36)
	binary.LittleEndian.PutUint32(b[0:], 36)
	binary.LittleEndian.PutUint16(b[4:], 2)
	binary.LittleEndian.PutUint32(b[16:], uint32(0xffffffed)) // -19 ENODEV
	if _, err := parseStations(b); err == nil {
		t.Fatal("NLMSG_ERROR ignored")
	}
}
```

- [ ] **Step 3: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run TestParseStations .`

Expected: `undefined: parseStations`.

- [ ] **Step 4: Implement the three files**

- [ ] **Step 5: Run the tests and cross-compile**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -race -run TestParseStations -v . && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go vet ./...`

Expected: 3 × `--- PASS` and no vet output.

- [ ] **Step 6: Commit**

`git add daemon/nl80211*.go daemon/testdata/station_dump.bin`. Subject: `feat(daemon): read SoftAP station byte counters over nl80211`.

---

### Task 1.7: Sample WAN and per-client speed, and add the usage snapshot (P1-C, 1b)

**Files:**
- Create: `daemon/speed.go`, `daemon/speed_test.go`
- Modify: `daemon/usage.go`. Add `Snapshot()` after `Report()` (:472), and factor the shared body into `reportLocked()`.
- Test: `daemon/usage_test.go`

**Interfaces:**
- Consumes: `Station` and `dumpStations` (1.6); `Client` and `ClientsReport` (`clients.go:12-25`); `softApIface` and `bridgeIface` (`clients.go:112-115`).
- Produces: contract §2.3, speed and usage parts.

**`tick` rules:**

| Case | Behavior |
|---|---|
| First call | Records the baselines only. |
| Before two samples | `PerClient` is `"unavailable"`. |
| Rates | `Δ*1000/elapsed_ms`, with `IntervalMs = elapsed_ms`. |
| Negative Δ | Re-baseline and report 0. |
| No AP interfaces | `PerClient = []ClientSpeed{}`, and phone = WAN. |
| Unavailable | Any of the three contract rules. |
| Recovery | The next good tick returns an array. |

`apIfaces()` behaves as follows:
1. If `bridgeBrifDir` exists, it returns that directory's entries.
2. Otherwise, if `net.InterfaceByName(softApIface)` succeeds, it returns `[]string{softApIface}`.
3. Otherwise it returns `nil`.

- [ ] **Step 1: Write the failing tests**

```go
func dev(rx0, tx0 uint64) string {
	return fmt.Sprintf("Inter-|   Receive\n face |bytes\n  rmnet_data0: %d 1 0 0 0 0 0 0 %d 1 0 0 0 0 0 0\n    swlan0: 999 1 0 0 0 0 0 0 999 1 0 0 0 0 0 0\n", rx0, tx0)
}

func newTestSampler(devs []string, dumps [][]Station) (*speedSampler, *time.Time) {
	now := time.Unix(1000, 0)
	i := 0
	sp := newSpeedSampler()
	sp.now = func() time.Time { return now }
	sp.readDev = func() (string, error) { return devs[min(i, len(devs)-1)], nil }
	sp.dump = func(string) ([]Station, error) { d := dumps[min(i, len(dumps)-1)]; i++; return d, nil }
	return sp, &now
}

var reach = []Client{{MAC: "aa:bb:cc:dd:ee:ff", IPv4: "192.168.1.23", State: "REACHABLE"}}

func TestSpeedPerClientAndPhone(t *testing.T) {
	sp, now := newTestSampler([]string{dev(1_000_000, 100_000), dev(3_500_000, 400_000)},
		[][]Station{{{MAC: "aa:bb:cc:dd:ee:ff", SignalDBm: -48}}, {{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 2_488_000, RXBytes: 296_000, SignalDBm: -48}}})
	sp.tick([]string{"swlan0"}, reach)
	*now = now.Add(time.Second)
	sp.tick([]string{"swlan0"}, reach)
	s := sp.Snapshot()
	pc, ok := s.PerClient.([]ClientSpeed)
	if !ok || len(pc) != 1 || pc[0].DownBytesS != 2_488_000 || pc[0].UpBytesS != 296_000 || *pc[0].IPv4 != "192.168.1.23" || *pc[0].SignalDBm != -48 {
		t.Fatalf("per_client %+v", s.PerClient)
	}
	if s.WanDownBytesS != 2_500_000 || s.WanUpBytesS != 300_000 || *s.PhoneDownBytesS != 12_000 || *s.PhoneUpBytesS != 4_000 || s.IntervalMs != 1000 {
		t.Fatalf("snap %+v", s)
	}
}

func TestSpeedDumpErrorUnavailable(t *testing.T) {
	sp, now := newTestSampler([]string{dev(0, 0), dev(1000, 1000)}, nil)
	sp.dump = func(string) ([]Station, error) { return nil, errors.New("ENODEV") }
	sp.tick([]string{"swlan0"}, reach)
	*now = now.Add(time.Second)
	sp.tick([]string{"swlan0"}, reach)
	if s := sp.Snapshot(); s.PerClient != "unavailable" || s.PhoneDownBytesS != nil {
		t.Fatalf("%+v", s)
	}
}

func TestSpeedZeroStationsWithReachableClient(t *testing.T) {
	sp, now := newTestSampler([]string{dev(0, 0), dev(1000, 1000)}, [][]Station{{}, {}})
	sp.tick([]string{"swlan0"}, reach)
	*now = now.Add(time.Second)
	sp.tick([]string{"swlan0"}, reach)
	if sp.Snapshot().PerClient != "unavailable" {
		t.Fatal("0 stations with a REACHABLE neighbour must be unavailable")
	}
}

func TestSpeedUnchangedCountersMarkUnavailable(t *testing.T) { // Review Focus 4 (sibling)
	st := []Station{{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 5, RXBytes: 5}}
	sp, now := newTestSampler([]string{dev(0, 0), dev(1e6, 1e5), dev(2e6, 2e5), dev(3e6, 3e5)}, [][]Station{st})
	for i := 0; i < 4; i++ {
		sp.tick([]string{"swlan0"}, reach)
		*now = now.Add(time.Second)
	}
	if sp.Snapshot().PerClient != "unavailable" {
		t.Fatal("3 unchanged ticks with WAN traffic must be unavailable")
	}
}

func TestSpeedRecoversAfterUnavailable(t *testing.T) {
	sp, now := newTestSampler([]string{dev(0, 0), dev(1000, 1000), dev(2000, 2000)},
		[][]Station{{{MAC: "aa:bb:cc:dd:ee:ff"}}, nil, {{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 500}}})
	errOnce := true
	inner := sp.dump
	sp.dump = func(i string) ([]Station, error) {
		if errOnce && sp.prevAt.Unix() == 1000 {
			errOnce = false
			return nil, errors.New("x")
		}
		return inner(i)
	}
	for i := 0; i < 3; i++ {
		sp.tick([]string{"swlan0"}, reach)
		*now = now.Add(time.Second)
	}
	if _, ok := sp.Snapshot().PerClient.([]ClientSpeed); !ok {
		t.Fatal("did not recover")
	}
}

func TestSpeedCounterResetIsZero(t *testing.T) { // Review Focus 4
	sp, now := newTestSampler([]string{dev(9_000_000, 9_000_000), dev(100, 100)},
		[][]Station{{{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 1e9}}, {{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 10}}})
	sp.tick([]string{"swlan0"}, reach)
	*now = now.Add(time.Second)
	sp.tick([]string{"swlan0"}, reach)
	s := sp.Snapshot()
	if s.WanDownBytesS != 0 || s.PerClient.([]ClientSpeed)[0].DownBytesS != 0 {
		t.Fatalf("reset produced a huge rate: %+v", s)
	}
}

func TestSpeedNoAPIfaces(t *testing.T) {
	sp, now := newTestSampler([]string{dev(0, 0), dev(1000, 0)}, nil)
	sp.tick(nil, nil)
	*now = now.Add(time.Second)
	sp.tick(nil, nil)
	s := sp.Snapshot()
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"per_client":[]`) || *s.PhoneDownBytesS != 1000 {
		t.Fatalf("%s", b)
	}
}
```

Add this to `usage_test.go`:

```go
func TestUsageSnapshotDoesNotSample(t *testing.T) {
	u := NewUsageTracker(filepath.Join(t.TempDir(), "u.json"))
	calls := 0
	u.readB = func() uint64 { calls++; return 1000 }
	u.Sample()
	before := calls
	r := u.Snapshot()
	if calls != before || r.PeriodBytes != u.Report().PeriodBytes {
		t.Fatal("Snapshot sampled or disagrees with Report")
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestSpeed|TestUsageSnapshot' .`

Expected: `undefined: newSpeedSampler`.

- [ ] **Step 3: Implement `speed.go` and `UsageTracker.Snapshot`**

- [ ] **Step 4: Run the tests again**

Run the Step 2 command with `-race -v`.

Expected: 8 × `--- PASS`.

- [ ] **Step 5: Commit**

`git add daemon/speed.go daemon/speed_test.go daemon/usage.go daemon/usage_test.go`. Subject: `feat(daemon): sample WAN and per-client speed; add an I/O-free usage snapshot`.

---

### Task 1.8: Start the hotspot Sem-first with a TM fallback and the Wi-Fi 6 gate (P1-D, 1b)

**Files:**
- Modify `helper/tether/TetherStart.java:47-69`: add the `sem` mode with no fallthrough and a new `tm` mode.
- Modify `daemon/hotspot.go`:
  - the seams
  - rewrite `startHotspot` (:498)
  - `ensureWifi6Gate`, `readHotspotVia` and `wifi6FromVia`
  - every `runHelper` call goes through `helperRun`
- Test: `daemon/hotspot_test.go`

**Interfaces:**
- Produces: contract §2.4, hotspot start part.
- `ensureWifi6Gate`: for `secure` and then `global`, run `get`, and when the value is not `"1"`, `put` 1 and log it.
- `startHotspot`:
  1. If `apProbe()` is true, return true with no calls.
  2. Run `ensureWifi6Gate`.
  3. Try `sem` up to 3 times, `semRetryGap` apart. Success is `"via=SemWifiManager"` in the output.
  4. Otherwise try `tm` once. Success is `"RESULT=STARTED"`.
  5. Write the via file (0600).
  6. Return `waitFor(true, apProbe, hotspotSettle, hotspotPollWait)`.

- [ ] **Step 1: Write the failing tests**

Test helper `stubStart(t, outs map[string][]string, up *bool) *[]string`:
- It saves and restores `helperRun`, `apProbe`, `semRetryGap`, `settingsCmd` and `hotspotViaPath` through `t.Cleanup`.
- It points `hotspotViaPath` into `t.TempDir()` and sets `semRetryGap = 0`.
- Its `helperRun` appends `args[0]` to the call list, then returns the next entry from `outs[mode]`. The last entry repeats. It sets `*up = true` when the output contains `STARTED`.
- `apProbe` returns `*up`.
- `settingsCmd` appends `strings.Join(args," ")` and returns `""`.

```go
func TestStartHotspotSemFirst(t *testing.T) {
	up := false
	calls := stubStart(t, map[string][]string{"sem": {"RESULT=FAILED reason=sem_rejected", "RESULT=STARTED via=SemWifiManager.setWifiApEnabled"}}, &up)
	if !startHotspot() || readHotspotVia() != "sem" {
		t.Fatal("sem start not recorded")
	}
	want := []string{"get secure wifi_ap_11ax_mode_checked", "put secure wifi_ap_11ax_mode_checked 1",
		"get global wifi_ap_11ax_mode_checked", "put global wifi_ap_11ax_mode_checked 1", "sem", "sem"}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls %v", *calls)
	}
}

func TestStartHotspotFallsBackToTM(t *testing.T) {
	up := false
	calls := stubStart(t, map[string][]string{"sem": {"RESULT=FAILED reason=sem_rejected"}, "tm": {"RESULT=STARTED via=TetheringManager"}}, &up)
	if !startHotspot() || readHotspotVia() != "tm" {
		t.Fatal("tm fallback not recorded")
	}
	n := 0
	for _, c := range *calls {
		if c == "sem" {
			n++
		}
	}
	if n != 3 || (*calls)[len(*calls)-1] != "tm" {
		t.Fatalf("calls %v", *calls)
	}
}

func TestStartHotspotAllFail(t *testing.T) {
	up := false
	stubStart(t, map[string][]string{"sem": {"RESULT=FAILED reason=sem_rejected"}, "tm": {"RESULT=FAILED code=3"}}, &up)
	if startHotspot() || readHotspotVia() != "" {
		t.Fatal("failure reported as success")
	}
}

func TestStartHotspotAlreadyUp(t *testing.T) {
	up := true
	calls := stubStart(t, map[string][]string{}, &up)
	if !startHotspot() || len(*calls) != 0 {
		t.Fatalf("calls %v", *calls)
	}
}

func TestEnsureWifi6GateSkipsWhenSet(t *testing.T) {
	up := false
	calls := stubStart(t, nil, &up)
	settingsCmd = func(name string, a ...string) string { *calls = append(*calls, strings.Join(a, " ")); return "1\n" }
	ensureWifi6Gate()
	for _, c := range *calls {
		if strings.HasPrefix(c, "put") {
			t.Fatalf("re-put a set gate: %v", *calls)
		}
	}
}

func TestWifi6FromVia(t *testing.T) {
	if *wifi6FromVia("sem") != true || *wifi6FromVia("tm") != false || wifi6FromVia("") != nil {
		t.Fatal("mapping")
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestStartHotspot|TestEnsureWifi6Gate|TestWifi6FromVia' .`

Expected: `undefined: helperRun`.

- [ ] **Step 3: Implement the Go and Java changes**

- [ ] **Step 4: Run the tests and build the jar**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -race ./... && cd .. && bash tools/build-tether.sh`

Expected: `ok`, then `tether helper built: magisk/tether/tether.jar (... bytes dex)`.

- [ ] **Step 5: Commit**

`git add helper/tether/TetherStart.java daemon/hotspot.go daemon/hotspot_test.go`. Subject: `feat(hotspot): start via Sem with retries, fall back to TM, record the path`.

---

### Task 1.9: Add the AP config cache, dual-band flag and status fields (P1-D, 1b)

**Files:**
- Modify `daemon/hotspot.go`:
  - `HotspotStatus` (:58-76)
  - `HotspotController` (:91-111) gains `dualBand atomic.Bool`, `ap APConfig`, `apOK bool` and `readAP func() (APConfig, bool)`
  - `NewHotspotController` wires `readAP`
  - `Status()` (:249)
  - `SetBand` (:200) becomes a config write only
- Test: `daemon/hotspot_test.go`

**Interfaces:**
- Produces: contract §2.4: `APConfig`, `parseAPConfig`, `SetDualBand`, `DualBand`, `APConfig()`, `RefreshAPConfig`, `SetBand` and the new status fields.
- `parseAPConfig`:
  - It reads the line `CFG ssid=<ssid> band=<n> sec=<n>`.
  - The SSID is the text between `ssid=` and the last ` band=`, with surrounding quotes stripped.
  - Band 1 → `"2"`, 2 → `"5"`, 4 → `"6"`. Any other value stays raw.
- The default `readAP` is `parseAPConfig(helperRun("com.zflip5.tether.SetSoftApConfig","get"))`.
- `SetBand` runs `helperRun(SetSoftApConfig,"band",band)`. `"RESULT=OK"` is success; anything else is an error.
- `Status().Wifi6` is `wifi6FromVia(readHotspotVia())`.

- [ ] **Step 1: Write the failing tests**

```go
func TestParseAPConfig(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want APConfig
		ok   bool
	}{
		{"CFG ssid=\"Flippost\" band=2 sec=1\n", APConfig{"Flippost", "5", "1"}, true},
		{"CFG ssid=My Net band=1 sec=0", APConfig{"My Net", "2", "0"}, true},
		{"CFG ssid=X band=4 sec=3", APConfig{"X", "6", "3"}, true},
		{"CFG ssid=X band=3 sec=1", APConfig{"X", "3", "1"}, true},
		{"RESULT=FAILED reason=no_wifi_manager", APConfig{}, false},
	} {
		if got, ok := parseAPConfig(tc.in); ok != tc.ok || got != tc.want {
			t.Errorf("%q: got %+v %v", tc.in, got, ok)
		}
	}
}

func TestStatusReportsDualBandAndWifi6(t *testing.T) {
	h, _ := newTestHC(t, nil)
	old := hotspotViaPath
	hotspotViaPath = filepath.Join(t.TempDir(), "via")
	t.Cleanup(func() { hotspotViaPath = old })
	if st := h.Status(); st.DualBand || st.Wifi6 != nil {
		t.Fatalf("%+v", st)
	}
	h.SetDualBand(true)
	os.WriteFile(hotspotViaPath, []byte("tm\n"), 0o600)
	if st := h.Status(); !st.DualBand || st.Wifi6 == nil || *st.Wifi6 {
		t.Fatalf("%+v", st)
	}
}

func TestRefreshAPConfigCaches(t *testing.T) {
	h, _ := newTestHC(t, nil)
	n := 0
	h.readAP = func() (APConfig, bool) { n++; return APConfig{"Flippost", "5", "1"}, true }
	h.RefreshAPConfig()
	if ap, ok := h.APConfig(); !ok || ap.SSID != "Flippost" || n != 1 {
		t.Fatal("not cached")
	}
	h.APConfig()
	if n != 1 {
		t.Fatal("APConfig re-read")
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestParseAPConfig|TestStatusReportsDualBand|TestRefreshAPConfig' .`

Expected: `undefined: parseAPConfig`.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the package**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -race ./...`

Expected: `ok`. `TestStepDualBandNoScan` still passes.

- [ ] **Step 5: Commit**

`git add daemon/hotspot.go daemon/hotspot_test.go`. Subject: `feat(hotspot): cache the SoftAP config and report dual band and Wi-Fi 6`.

---

### Task 1.10: Remove the dual band from presets (P1-D, 1b)

**Files:**
- Modify `daemon/presets.go`:
  - the `Preset.Band` comment (:42)
  - `validatePreset` (:71-75)
  - `PresetManager.dual`
  - `NewPresetManager` (:140)
  - `MigrateDualBand` and `ApplyBusy`
  - `applyPresetLive` (:367)
- Test: `daemon/presets_test.go`

**Interfaces:**
- Consumes: `helperRun` and `apProbe` (1.8).
- Produces:
  - `MigrateDualBand`: changes `"dual"` to `"5"`, and calls `persistLocked()` when anything changed.
  - `ApplyBusy`: `!applyMu.TryLock()`, unlocking right away on success.
  - `applyPresetLive(p, dual)`: the band is `"dual"` if `dual`. Otherwise it is `p.Band`, with `""` → `"5"`. Band `"5"` appends `"149"`. The bounce uses `apProbe()`.
  - `NewPresetManager` sets `apply = func(p Preset) error { return applyPresetLive(p, pm.dual != nil && pm.dual()) }`.

- [ ] **Step 1: Write the failing tests**

```go
func TestValidatePresetRejectsDual(t *testing.T) {
	p := Preset{Name: "Home", SSID: "Flippost", Security: "wpa2", Passphrase: "12345678", Band: "dual"}
	if err := validatePreset(p); err == nil || err.Error() != "band must be 2, 5, or 6" {
		t.Fatalf("got %v", err)
	}
}

func TestMigrateDualBand(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"hotspot":{"enable_on_boot":true}}`), 0o600)
	pm := NewPresetManager(HotspotPresets{Presets: []Preset{{ID: "a", Band: "dual"}, {ID: "b", Band: "2"}}}, func() string { return p })
	if !pm.MigrateDualBand() || pm.MigrateDualBand() {
		t.Fatal("want true then false")
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), `"dual"`) || !strings.Contains(string(b), `"enable_on_boot": true`) {
		t.Fatalf("%s", b)
	}
	if pm.statusRedacted()["presets"].([]map[string]any)[0]["band"] != "5" {
		t.Fatal("in-memory band not migrated")
	}
}

func TestApplyBusy(t *testing.T) {
	pm := NewPresetManager(HotspotPresets{}, nil)
	pm.applyMu.Lock()
	if !pm.ApplyBusy() {
		t.Fatal("held lock not reported")
	}
	pm.applyMu.Unlock()
	if pm.ApplyBusy() {
		t.Fatal("free lock reported busy")
	}
}

func TestApplyPresetLiveBandArgs(t *testing.T) {
	oh, oa := helperRun, apProbe
	t.Cleanup(func() { helperRun, apProbe = oh, oa })
	apProbe = func() bool { return false }
	var got []string
	helperRun = func(c string, a ...string) string { got = a; return "RESULT=OK" }
	p := Preset{SSID: "Flippost", Security: "wpa2", Passphrase: "12345678"}
	for _, tc := range []struct {
		band string
		dual bool
		tail []string
	}{{"5", false, []string{"5", "149"}}, {"5", true, []string{"dual"}}, {"2", false, []string{"2"}}, {"", false, []string{"5", "149"}}} {
		p.Band = tc.band
		if err := applyPresetLive(p, tc.dual); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got[4:], tc.tail) {
			t.Errorf("band %q dual %v: args %v", tc.band, tc.dual, got)
		}
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestValidatePresetRejectsDual|TestMigrateDualBand|TestApplyBusy|TestApplyPresetLiveBandArgs' .`

Expected: `undefined: (*PresetManager).MigrateDualBand`.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the package**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -race ./...`

Expected: `ok`.

- [ ] **Step 5: Commit**

`git add daemon/presets.go daemon/presets_test.go`. Subject: `feat(presets): drop the dual band, migrate it to 5 GHz, pin ch149`.

---

### Task 1.11: Add the dual-band setting with the owner rule (P1-D, 1b)

**Files:**
- Create: `daemon/dualband.go`, `daemon/dualband_test.go`

**Interfaces:**
- Consumes:
  - `persistHotspotDualBand`, `Config.Hotspot.DualBand` and `writeErrReason` (1.2)
  - `SetDualBand`, `DualBand`, `SetBand` and `RefreshAPConfig` (1.9)
  - `ApplyBusy` (1.10)
  - `s.airplaneBusy` and `s.cfgMu` (`server.go:50,71`)
- Produces: `bounceHotspot`, `dualBandBlock`, `applyDualBand` and `handleHotspotDual`. The check order is contract §1.5. `handleHotspotDual` uses `http.MaxBytesReader` with a 256-byte limit.

- [ ] **Step 1: Write the failing tests**

```go
func dualServer(t *testing.T, helperOut string) (*Server, *[]string, *int) {
	t.Helper()
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.cfgPath = filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(s.cfgPath, []byte(`{"hotspot":{"enable_on_boot":true}}`), 0o600)
	s.hs.apUp = func() bool { return false }
	s.hs.readAP = func() (APConfig, bool) { return APConfig{"Flippost", "5", "1"}, true }
	var args []string
	bounces := 0
	oh, ob := helperRun, bounceHotspot
	t.Cleanup(func() { helperRun, bounceHotspot = oh, ob })
	helperRun = func(c string, a ...string) string { args = a; return helperOut }
	bounceHotspot = func() { bounces++ }
	return s, &args, &bounces
}

func TestDualBandBlockedByWhitelist(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=OK")
	s.hs.SetWhitelist([]string{"Home"})
	if _, _, code, reason, _ := s.applyDualBand(true, false); code != 409 || reason != "whitelist" {
		t.Fatalf("%d %q", code, reason)
	}
	if _, _, code, _, _ := s.applyDualBand(false, false); code != 200 {
		t.Fatalf("disable blocked: %d", code)
	}
}

func TestDualBandBlockedByAutoSwitch(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=OK")
	s.presets.SetAuto(true)
	if _, _, code, reason, _ := s.applyDualBand(true, false); code != 409 || reason != "auto_switch" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestDualBandBusy(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=OK")
	s.airplaneBusy.Store(true)
	if _, _, code, reason, _ := s.applyDualBand(false, false); code != 409 || reason != "busy" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestDualBandPersistsAndCallsHelper(t *testing.T) {
	s, args, bounces := dualServer(t, "RESULT=OK")
	_, applied, code, _, err := s.applyDualBand(true, true)
	if err != nil || code != 200 || applied != "next_start" || *bounces != 0 {
		t.Fatalf("%v %d %q %d", err, code, applied, *bounces)
	}
	b, _ := os.ReadFile(s.cfgPath)
	if !reflect.DeepEqual(*args, []string{"band", "dual"}) || !s.hs.DualBand() || !s.cfg.Hotspot.DualBand ||
		!strings.Contains(string(b), `"dual_band": true`) || !strings.Contains(string(b), `"enable_on_boot": true`) {
		t.Fatalf("args %v cfg %s", *args, b)
	}
	s.applyDualBand(false, false)
	if !reflect.DeepEqual(*args, []string{"band", "5"}) {
		t.Fatalf("disable args %v", *args)
	}
}

func TestDualBandHelperFailureReverts(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=FAILED reason=x")
	if _, _, code, _, _ := s.applyDualBand(true, false); code != 502 {
		t.Fatalf("code %d", code)
	}
	b, _ := os.ReadFile(s.cfgPath)
	if s.hs.DualBand() || s.cfg.Hotspot.DualBand || strings.Contains(string(b), `"dual_band": true`) {
		t.Fatalf("not reverted: %s", b)
	}
}

func TestDualBandApplyNowBounces(t *testing.T) {
	s, _, bounces := dualServer(t, "RESULT=OK")
	s.hs.apUp = func() bool { return true }
	if _, applied, _, _, _ := s.applyDualBand(true, true); applied != "now" || *bounces != 1 {
		t.Fatalf("%q %d", applied, *bounces)
	}
}

func TestHandleHotspotDualBody(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=OK")
	for _, body := range []string{`{bad`, `{"enabled":true,"apply_now":false,"x":"` + strings.Repeat("a", 300) + `"}`} {
		w := httptest.NewRecorder()
		s.handleHotspotDual(w, httptest.NewRequest("POST", "/v1/hotspot/dual", strings.NewReader(body)))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "bad body") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	s.handleHotspotDual(w, httptest.NewRequest("POST", "/v1/hotspot/dual", strings.NewReader(`{"enabled":true}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"applied":"next_start"`) || !strings.Contains(w.Body.String(), `"dual_band":true`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestDualBand|TestHandleHotspotDual' .`

Expected: `undefined: (*Server).applyDualBand`.

- [ ] **Step 3: Implement `daemon/dualband.go`**

- [ ] **Step 4: Run the tests again**

Run the Step 2 command with `-race -v`.

Expected: 7 × `--- PASS`.

- [ ] **Step 5: Commit**

`git add daemon/dualband.go daemon/dualband_test.go`. Subject: `feat(hotspot): add the dual-band setting, blocked while auto features are on`.

---

### Task 1.12: Read Tailscale status over the LocalAPI (P1-E, 1b)

**Files:**
- Create: `daemon/tailscale.go`, `daemon/tailscale_test.go`

**Interfaces:**
- Produces: the status half of contract §2.5: `tailscaleSock`, `tailscalePrefsPath`, `localAPIHost`, `errTSNotRunning`, both structs, `newTailscaleClient`, `mapBackendState`, `Status`, `RefreshBrief`, `Brief` and `handleStatus`.

**Behavior:**
- Requests go to `http://local-tailscaled.sock/localapi/v0/…`.
- A dial error with `errors.Is(err, syscall.ENOENT)` or `syscall.ECONNREFUSED` maps to `not_running`.
- The status decode struct has no `AuthURL` field.
- When running:
  - `IP` is the first entry without a `:`.
  - `DNSName` is `Self.DNSName`, trimmed.
  - `Tailnet` is `CurrentTailnet.Name`.
  - `WebURL` is `"https://"+fqdn+"/"`.
  - `ServeOK` comes from `GET /serve-config`.
- `handleStatus` always returns 200 and uses a 3 s ctx.

- [ ] **Step 1: Write the failing tests**

```go
func fakeTS(t *testing.T, h http.HandlerFunc) *tailscaleClient {
	t.Helper()
	dir, _ := os.MkdirTemp("", "zts") // short path: unix socket names are capped at 104 bytes on macOS
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: h}}
	srv.Start()
	t.Cleanup(srv.Close)
	return newTailscaleClient(sock, 18080)
}

const tsRunning = `{"BackendState":"Running","AuthURL":"https://login.tailscale.com/a/SECRET","TailscaleIPs":["fd7a:115c:a1e0::1","100.110.36.27"],
"Self":{"DNSName":"zflip5.tail1234.ts.net.","HostName":"zflip5","Online":true,"KeyExpiry":"2027-04-01T00:00:00Z"},
"CurrentTailnet":{"Name":"user@example.com"},"Version":"1.80.0","Health":[]}`

func TestMapBackendState(t *testing.T) {
	for in, want := range map[string]string{"Running": "running", "NeedsLogin": "needs_login", "NoState": "needs_login",
		"NeedsMachineAuth": "needs_machine_auth", "Stopped": "stopped", "Starting": "starting", "InUseOtherUser": "in_use_other_user", "Weird": "error"} {
		if got := mapBackendState(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestTailscaleStatusRunning(t *testing.T) {
	ts := fakeTS(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != localAPIHost {
			http.Error(w, "bad host", 403)
			return
		}
		switch r.URL.Path {
		case "/localapi/v0/status":
			io.WriteString(w, tsRunning)
		case "/localapi/v0/serve-config":
			io.WriteString(w, `{"Web":{"zflip5.tail1234.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:18080"}}}}}`)
		}
	})
	st := ts.Status(context.Background())
	b, _ := json.Marshal(st)
	if st.State != "running" || *st.IP != "100.110.36.27" || *st.DNSName != "zflip5.tail1234.ts.net" || !st.ServeOK ||
		*st.WebURL != "https://zflip5.tail1234.ts.net/" || *st.Tailnet != "user@example.com" || !st.Online {
		t.Fatalf("%s", b)
	}
	if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "AuthURL") {
		t.Fatalf("leaked auth URL: %s", b)
	}
}

func TestTailscaleStatusNotRunning(t *testing.T) {
	st := newTailscaleClient(filepath.Join(t.TempDir(), "missing.sock"), 18080).Status(context.Background())
	b, _ := json.Marshal(st)
	if st.State != "not_running" || !strings.Contains(string(b), `"ips":[]`) || !strings.Contains(string(b), `"ip":null`) {
		t.Fatalf("%s", b)
	}
}

func TestTailscaleStatusNeedsLoginNullsFields(t *testing.T) {
	ts := fakeTS(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"BackendState":"NeedsLogin","AuthURL":"https://login.tailscale.com/a/SECRET","TailscaleIPs":["100.1.2.3"],"Version":"1.80.0"}`)
	})
	if st := ts.Status(context.Background()); st.State != "needs_login" || st.IP != nil || st.Version != nil || st.WebURL != nil || st.ServeOK {
		t.Fatalf("%+v", st)
	}
}

func TestTailscaleBriefRefresh(t *testing.T) {
	ts := fakeTS(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tsRunning) })
	if ts.Brief().State != "not_running" {
		t.Fatal("default brief")
	}
	ts.RefreshBrief(context.Background())
	if b := ts.Brief(); b.State != "running" || *b.IP != "100.110.36.27" {
		t.Fatalf("%+v", b)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestMapBackendState|TestTailscaleStatus|TestTailscaleBrief' .`

Expected: `undefined: newTailscaleClient`.

- [ ] **Step 3: Implement the status half**

- [ ] **Step 4: Run the tests again**

Run the Step 2 command with `-race -v`.

Expected: 5 × `--- PASS`.

- [ ] **Step 5: Commit**

`git add daemon/tailscale.go daemon/tailscale_test.go`. Subject: `feat(daemon): read Tailscale status from the LocalAPI without auth URLs`.

---

### Task 1.13: Add Tailscale login, logout, serve re-apply and docs (P1-E, 1b)

**Files:**
- Modify: `daemon/tailscale.go`, `daemon/tailscale_test.go`
- Rewrite: `docs/tailscale.md`

**Interfaces:**
- Consumes: `writeErrReason` (1.2) and `writeConfigAtomic` (`config.go:193`).
- Produces: `Login`, `Logout`, `ensureServe`, `handleLogin`, `handleLogout`, `loginURLWait` and `loginWatchFor`. `Login` reads both vars once at its top.

**`Login` flow:**
1. If `!loginBusy.CompareAndSwap(false,true)`, return 409 `busy`. Release it when `Login` returns.
2. `GET /status`:
   - a dial error returns 503 `not_running`
   - `Running` returns `("running","",200,"",nil)`
   - `NeedsMachineAuth` returns 409 `needs_machine_auth`
3. Open `watch-ipn-bus?mask=2` under `context.WithTimeout(Background, loginWatchFor)`. A goroutine decodes `{State *int; BrowseToURL *string}` lines.
4. If the state is `NoState`, `POST /start {}`.
5. `PATCH /prefs` with `WantRunning:true` and `WantRunningSet:true`. `Hostname` is the saved non-empty value, or `"zflip5"`, with `HostnameSet:true`. Each saved field goes with its `*Set:true`.
6. `POST /login-interactive`, which returns 204.
7. Return the first `BrowseToURL` within `loginURLWait`, polling `/status` `AuthURL` every 1 s meanwhile. If nothing arrives, return 504 `no_login_url`.
8. On `State==6`, the watcher calls `ensureServe` once and exits.

Any other LocalAPI non-2xx returns 502 `localapi`.

**`Logout`:**
1. `GET /prefs`.
2. Save the seven fields with `writeConfigAtomic` (0600).
3. `POST /logout`. Both 204 and 200 are OK.

A dial error returns `errTSNotRunning`.

**`ensureServe`:**
1. Read the FQDN from `/status`.
2. `GET /serve-config` and keep the `Etag`. Decode into `map[string]any`; `null` becomes an empty map.
3. Set `TCP["443"]={"HTTPS":true}`.
4. Set `Web[fqdn+":443"].Handlers["/"]={"Proxy":"http://127.0.0.1:<port>"}`, keeping every other key.
5. `POST` with `If-Match`. On 412, retry once.

**Handlers:**
- `handleLogin`: ctx of 25 s. It returns `{"state","login_url"}`, with `null` for an empty URL.
- `handleLogout`: returns `{"state":"needs_login"}`.

- [ ] **Step 1: Write the failing tests**

Test fake `tsFake{mu sync.Mutex; state string; calls, posts, ifm []string; notes chan string; code412 int}`. Its `handler` appends `r.Method+" "+r.URL.Path` to `calls` under `mu`, then answers:

| Request | Answer |
|---|---|
| `GET /localapi/v0/status` | `{"BackendState":<state>,"Self":{"DNSName":"zflip5.tail1234.ts.net."}}` |
| `GET /localapi/v0/watch-ipn-bus` | flush, then write each string from `notes` plus `\n`, flushing each |
| `POST start`, `login-interactive`, `logout` | 204 |
| `PATCH prefs` | append `"BODY "+body` to `calls`, then `{}` |
| `GET prefs` | `{"Hostname":"zflip5","NetfilterMode":2,"CorpDNS":true,"RouteAll":false,"ShieldsUp":false,"RunSSH":false,"AdvertiseRoutes":null,"Persist":{"PrivateNodeKey":"privkey:SECRET"}}` |
| `GET serve-config` | header `Etag: abc`, body `{"TCP":{"8443":{"HTTPS":true}},"Web":{"other.ts.net:8443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:9"}}}}}` |
| `POST serve-config` | record the body and `If-Match`; return 412 while `code412 > 0`, decrementing it |

Tests:

```go
func TestLoginReturnsBrowseURL(t *testing.T) {
	f := &tsFake{state: "NoState", notes: make(chan string, 4)}
	ts := fakeTS(t, f.handler)
	old := tailscalePrefsPath
	tailscalePrefsPath = filepath.Join(t.TempDir(), "p.json")
	t.Cleanup(func() { tailscalePrefsPath = old; close(f.notes) })
	f.notes <- `{"State":2}`
	f.notes <- `{"BrowseToURL":"https://login.tailscale.com/a/abc"}`
	state, url, code, _, err := ts.Login(context.Background())
	if err != nil || code != 200 || state != "needs_login" || url != "https://login.tailscale.com/a/abc" {
		t.Fatalf("%q %q %d %v", state, url, code, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	j := strings.Join(f.calls, "|")
	if !(strings.Index(j, "watch-ipn-bus") < strings.Index(j, "POST /localapi/v0/start") &&
		strings.Index(j, "POST /localapi/v0/start") < strings.Index(j, "PATCH") &&
		strings.Index(j, "PATCH") < strings.Index(j, "login-interactive")) ||
		!strings.Contains(j, `"WantRunningSet":true`) || !strings.Contains(j, `"Hostname":"zflip5"`) {
		t.Fatalf("order or body: %s", j)
	}
}

func TestLoginAlreadyRunning(t *testing.T) {
	f := &tsFake{state: "Running"}
	state, url, code, _, _ := fakeTS(t, f.handler).Login(context.Background())
	if state != "running" || url != "" || code != 200 || strings.Contains(strings.Join(f.calls, "|"), "login-interactive") {
		t.Fatalf("%q %q %d %v", state, url, code, f.calls)
	}
}

func TestLoginNeedsMachineAuth(t *testing.T) {
	if _, _, code, reason, _ := fakeTS(t, (&tsFake{state: "NeedsMachineAuth"}).handler).Login(context.Background()); code != 409 || reason != "needs_machine_auth" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestLoginBusy(t *testing.T) {
	ts := fakeTS(t, (&tsFake{state: "NeedsLogin"}).handler)
	ts.loginBusy.Store(true)
	if _, _, code, reason, _ := ts.Login(context.Background()); code != 409 || reason != "busy" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestLoginNotRunning(t *testing.T) {
	ts := newTailscaleClient(filepath.Join(t.TempDir(), "x.sock"), 18080)
	if _, _, code, reason, _ := ts.Login(context.Background()); code != 503 || reason != "not_running" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestLoginNoURLTimesOut(t *testing.T) {
	f := &tsFake{state: "NeedsLogin", notes: make(chan string)}
	ts := fakeTS(t, f.handler)
	old := loginURLWait
	loginURLWait = 1500 * time.Millisecond
	t.Cleanup(func() { loginURLWait = old; close(f.notes) })
	if _, _, code, reason, _ := ts.Login(context.Background()); code != 504 || reason != "no_login_url" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestLoginWatcherReappliesServe(t *testing.T) {
	f := &tsFake{state: "NeedsLogin", notes: make(chan string, 4)}
	ts := fakeTS(t, f.handler)
	t.Cleanup(func() { close(f.notes) })
	f.notes <- `{"BrowseToURL":"https://login.tailscale.com/a/abc"}`
	ts.Login(context.Background())
	f.mu.Lock()
	f.state = "Running"
	f.mu.Unlock()
	f.notes <- `{"State":6}`
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		f.mu.Lock()
		n := len(f.posts)
		f.mu.Unlock()
		if n > 0 {
			return
		}
	}
	t.Fatal("ensureServe not called after Running")
}

func TestLogoutSavesPrefs(t *testing.T) {
	f := &tsFake{state: "Running"}
	ts := fakeTS(t, f.handler)
	old := tailscalePrefsPath
	tailscalePrefsPath = filepath.Join(t.TempDir(), "p.json")
	t.Cleanup(func() { tailscalePrefsPath = old })
	if err := ts.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(tailscalePrefsPath)
	fi, _ := os.Stat(tailscalePrefsPath)
	if fi.Mode().Perm() != 0o600 || strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "Persist") ||
		!strings.Contains(string(b), `"NetfilterMode":2`) || !strings.Contains(strings.Join(f.calls, "|"), "POST /localapi/v0/logout") {
		t.Fatalf("%s %v", b, f.calls)
	}
}

func TestEnsureServeMergesWithIfMatch(t *testing.T) {
	f := &tsFake{state: "Running", code412: 1}
	if err := fakeTS(t, f.handler).ensureServe(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := f.posts[len(f.posts)-1]
	if len(f.posts) != 2 || f.ifm[1] != "abc" || !strings.Contains(last, `"8443"`) || !strings.Contains(last, `other.ts.net:8443`) ||
		!strings.Contains(last, `"zflip5.tail1234.ts.net:443"`) || !strings.Contains(last, `"http://127.0.0.1:18080"`) {
		t.Fatalf("posts %v ifm %v", f.posts, f.ifm)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestLogin|TestLogout|TestEnsureServe' .`

Expected: `undefined: (*tailscaleClient).Login`.

- [ ] **Step 3: Implement the login half**

- [ ] **Step 4: Run the tests again**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -race -run 'TestLogin|TestLogout|TestEnsureServe|TestTailscale' -v .`

Expected: all `--- PASS`, with no race report.

- [ ] **Step 5: Rewrite `docs/tailscale.md` (invoke `google-writing` first)**

Headings:
- "How the module uses Tailscale"
- "Check status"
- "Sign in and sign out from the app"
- "Reach the web UI"
- "Troubleshoot"

Facts it contains:
- the socket path `/data/adb/tailscale/tailscaled.sock`
- `GET /v1/tailscale` never returns keys or login URLs
- login and logout are app only, and return 403 `app_only` elsewhere
- after login the daemon re-applies `tailscale serve` to `http://127.0.0.1:18080`
- logout saves prefs to `/data/adb/zflip5-modem/tailscale-prefs.json` and restores them at the next login
- `needs_machine_auth` means approval in the admin console
- no authkey, no bundled binaries and no `ingress.mode`

Keep the words "loopback", "owner-only" and "ephemeral" anywhere in `docs/`, because `REQUIRED_TOKENS` still lists them until 3.9.

Run: `python3 /Users/adacchi/.claude/skills/google-writing/scripts/style_lint.py /Users/adacchi/module-zf5/docs/tailscale.md && python3 /Users/adacchi/module-zf5/tools/validate-contracts.py --schemas /Users/adacchi/module-zf5/schemas --docs /Users/adacchi/module-zf5/docs | tail -1`

Expected: no lint findings, then `CONTRACTS_OK=1`.

- [ ] **Step 6: Commit**

`git add daemon/tailscale.go daemon/tailscale_test.go docs/tailscale.md`. Subject: `feat(daemon): add Tailscale login, logout and serve re-apply over the LocalAPI`.

---

### Task 1.14: Build the summary collector (P1-F, 1c)

**Files:**
- Create: `daemon/summary.go`, `daemon/summary_test.go`
- Modify: `daemon/server.go`. Add the `Server` fields `speed`, `ts` and `sum`.

**Interfaces:**
- Consumes:
  - from 1.7: `speedSampler`, `apIfaces` and `UsageTracker.Snapshot`
  - from 1.9: `Status`, `APConfig`, `RefreshAPConfig` and `DualBand`
  - from 1.8: `readHotspotVia` and `wifi6FromVia`
  - from 1.11: `dualBandBlock`
  - from 1.12: `RefreshBrief` and `Brief`
  - existing: `deviceSignal`, `deviceClients`, `deviceWanIP` and `airplaneOn`
  - existing: `s.col.Thermal`, `s.col.Battery`, `s.smooth.median`, `s.pol.Evaluate`, `s.effectiveWarnC`, `effectiveGateC` and `s.bench.Active`
- Produces: contract §2.6.

**Rules:**
- `summary()`:
  - takes `RLock`
  - `SlowAgeS = int(time.Since(at).Seconds())`
  - `UpdatedAt = at.Format(time.RFC3339)`
  - merges `s.speed.Snapshot()` and `summaryUsage(s.usage.Snapshot())`
  - `Hotspot.Clients` is `len(per_client)` when that is an array, else `s.sum.clients.Count`
- `collectSlow()`, stored under `Lock`:
  - `band` is `"dual"` when `st.Dual`, else `APConfig().Band`
  - SSID and band are `nil` when `!apOK`
  - `dual_band_block` of `""` becomes `nil`
  - `wan_ip`, `wan_iface` and `battery` are `nil` when `!Available`
  - `RefreshBrief` runs with a 3 s ctx
- `runSummary()`:
  - the 5 s loop runs `collectSlow`, and `RefreshAPConfig` on every 12th pass
  - the 1 s loop runs `s.speed.tick(apIfaces(), cached s.sum.clients.Clients)`

- [ ] **Step 1: Write the failing tests**

Until Task 1.15 lands, set `s.speed = newSpeedSampler()` inside each test.

```go
func TestSummarySignalBars(t *testing.T) {
	if summarySignal(Signal{Available: false}) != nil {
		t.Fatal("unavailable must be nil")
	}
	if s := summarySignal(Signal{Available: true, Tech: "NR", Display: "5G", Level: 0}); s.Bars != nil {
		t.Fatal("NR level 0 must be null")
	}
	if s := summarySignal(Signal{Available: true, Tech: "LTE", Display: "4G+", Operator: "Viettel", Level: 3}); *s.Bars != 3 || s.Display != "4G+" || s.Operator != "Viettel" {
		t.Fatalf("%+v", s)
	}
}

func TestSummaryUsageLeft(t *testing.T) {
	u := summaryUsage(UsageReport{PeriodBytes: 12_345_678_901, LimitBytes: 100_000_000_000, TodayBytes: 5, Period: "monthly", NextReset: "2026-11-01T00:00:00+07:00"})
	if *u.LeftBytes != 87_654_321_099 || *u.NextReset != "2026-11-01T00:00:00+07:00" {
		t.Fatalf("%+v", u)
	}
	if u := summaryUsage(UsageReport{PeriodBytes: 5}); u.LeftBytes != nil || u.NextReset != nil {
		t.Fatalf("%+v", u)
	}
	if u := summaryUsage(UsageReport{PeriodBytes: 9, LimitBytes: 5}); *u.LeftBytes != 0 {
		t.Fatal("over cap must floor at 0")
	}
}

func TestSummaryTemp(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	tp := s.summaryTemp(Thermal{BatteryC: 30, MaxC: 40.04, Safe: true, Source: "sysfs+battery"})
	if *tp.C != 40 || tp.RawC != 40 || tp.State != "SAFE" || tp.WarnC != 44 || tp.GateC != 46 || tp.Bench {
		t.Fatalf("%+v", tp)
	}
	s.smooth.push(45)
	if tp := s.summaryTemp(Thermal{MaxC: 40}); *tp.C != 45 {
		t.Fatal("median not used")
	}
	if tp := s.summaryTemp(Thermal{MaxC: 55, BatteryC: 48}); tp.State != "HOT" {
		t.Fatalf("hot state %q", tp.State)
	}
	s2 := NewServer(testCfg(), fakeCollector{safe: true})
	if tp := s2.summaryTemp(Thermal{MaxC: 30, Source: "degraded"}); tp.State != "HOT" {
		t.Fatalf("degraded must fail closed, got %q", tp.State)
	}
}

func TestSummaryMergesNoIO(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.sum.mu.Lock()
	s.sum.at = time.Now().Add(-3 * time.Second)
	s.sum.slow = Summary{V: 1, Hotspot: SummaryHotspot{Active: true}, Tailscale: TailscaleBrief{State: "running"}}
	s.sum.clients = ClientsReport{Count: 2}
	s.sum.mu.Unlock()
	if got := s.summary(); got.SlowAgeS < 3 || got.SlowAgeS > 4 || got.Speed.PerClient != "unavailable" || got.Hotspot.Clients != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestSummaryEndpointKeysAlwaysPresent(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.sum.slow = Summary{V: 1, Tailscale: TailscaleBrief{State: "not_running"}}
	w := httptest.NewRecorder()
	s.handleSummary(w, httptest.NewRequest("GET", "/v1/summary", nil))
	var m map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &m)
	for _, k := range []string{"v", "updated_at", "slow_age_s", "signal", "usage", "speed", "hotspot", "wan_ip", "wan_iface", "temperature", "battery", "tailscale", "airplane"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q: %s", k, w.Body.String())
		}
	}
	var hs map[string]json.RawMessage
	json.Unmarshal(m["hotspot"], &hs)
	for _, k := range []string{"ssid", "band", "wifi6", "dual_band_block"} {
		if _, ok := hs[k]; !ok {
			t.Errorf("hotspot missing %q", k)
		}
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestSummary' .`

Expected: `undefined: summarySignal`.

- [ ] **Step 3: Implement `daemon/summary.go` and the `Server` fields**

- [ ] **Step 4: Run the tests again**

Run the Step 2 command with `-race -v`.

Expected: 5 × `--- PASS`.

- [ ] **Step 5: Commit**

`git add daemon/summary.go daemon/summary_test.go daemon/server.go`. Subject: `feat(daemon): add the in-memory summary collector`.

---

### Task 1.15: Wire up the server: app bypass, `guardApp`, routes, 409s and OpenAPI (P1-F, 1c)

**Files:**
- Modify `daemon/server.go`:
  - `NewServer` (:102) and `ListenAndServe` (:1160)
  - `guard` (:370) and `guardAuth` (:438)
  - new `guardApp`
  - `routes()` (:250)
  - `handleHotspotBand` (:791)
  - `handlePresetApply` (:864) and `handlePresetAuto` (:888)
  - `handleHotspotWhitelist` (:902)
- Modify `schemas/api.openapi.json`:
  - delete the paths `/v1/health` :406-432, `/v1/network` :433-452, `/v1/thermal/history` :480-509, `/v1/sms/recent` :530-556, `/v1/notifications/recent` :557-583, `/v1/prefer5g` :638-686 and `/v1/bands` :900-929
  - delete `ThermalHistory` and `ThermalHistoryPoint`
  - drop SMS and prefer-5G from `info.description`, keeping "owner-only" and "loopback"
  - drop `sms` from the bearer scopes
  - add the five new paths with the codes from contract §1.1-1.5
- Test: `daemon/server_test.go`

**Interfaces:**
- Consumes: Tasks 1.5-1.14.
- Produces: `guardApp`, plus the `NewServer` and `ListenAndServe` wiring in contract §2.7.

**Behavior:**
- `handleHotspotBand`: `"dual"` maps to `applyDualBand(true,true)` and `"5"` to `applyDualBand(false,true)`. It always sets `Deprecation: true`.
- `handleHotspotWhitelist`: under `cfgMu`, a non-empty list with `DualBand()` on returns 409 `dual_band` with `"turn off dual band first: it stops Wi-Fi scanning"`.
- `handlePresetAuto`: under `cfgMu`, `on && DualBand()` returns the same 409.
- `handlePresetApply`: `RefreshAPConfig()` after success.

- [ ] **Step 1: Write the failing tests**

```go
func appReq(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r.WithContext(withPeerUID(r.Context(), 10234)))
	return w
}

func appServer(t *testing.T, radioPerMin int) *Server {
	cfg := testCfg()
	cfg.RateLimits.RadioPerMin = radioPerMin
	s := NewServer(cfg, fakeCollector{safe: true})
	s.app.read = func(string) ([]byte, error) { return []byte("com.zflip5.modem 10234 0 /x\n"), nil }
	return s
}

func TestAppUIDSkipsTokenAndRateLimit(t *testing.T) {
	s := appServer(t, 2)
	for i := 0; i < 5; i++ {
		if w := appReq(s, "POST", "/v1/cooldown", ""); w.Code != 200 {
			t.Fatalf("call %d: %d", i, w.Code)
		}
	}
	if w := appReq(s, "GET", "/v1/status", ""); w.Code != 200 {
		t.Fatalf("app read: %d", w.Code)
	}
	if w := do(s, "POST", "/v1/cooldown", ""); w.Code != 401 {
		t.Fatalf("non-app tokenless: %d", w.Code)
	}
}

func TestGuardAppRejectsNonApp(t *testing.T) { // Review Focus 5
	s := appServer(t, 60)
	s.openControl.Store(true)
	for _, p := range []string{"/v1/tailscale/login", "/v1/tailscale/logout", "/v1/dashboard/open", "/v1/dashboard/control"} {
		if w := do(s, "POST", p, strings.Repeat("c", 64)); w.Code != 403 || !strings.Contains(w.Body.String(), `"reason":"app_only"`) {
			t.Errorf("%s token: %d %s", p, w.Code, w.Body.String())
		}
		if w := do(s, "GET", p, strings.Repeat("c", 64)); w.Code != 405 {
			t.Errorf("%s GET: %d", p, w.Code)
		}
		for _, uid := range []int{0, 2000} {
			r := httptest.NewRequest("POST", p, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r.WithContext(withPeerUID(r.Context(), uid)))
			if w.Code != 403 {
				t.Errorf("%s uid %d: %d", p, uid, w.Code)
			}
		}
	}
}

func TestSummaryAndTailscaleRoutes(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.ts = newTailscaleClient(filepath.Join(t.TempDir(), "x.sock"), 18080)
	if w := do(s, "GET", "/v1/summary", strings.Repeat("a", 64)); w.Code != 200 || !strings.Contains(w.Body.String(), `"v":1`) {
		t.Fatalf("summary %d %s", w.Code, w.Body.String())
	}
	if w := do(s, "GET", "/v1/summary", ""); w.Code != 401 {
		t.Fatalf("summary no token: %d", w.Code)
	}
	if w := do(s, "GET", "/v1/tailscale", strings.Repeat("a", 64)); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"not_running"`) {
		t.Fatalf("tailscale %d %s", w.Code, w.Body.String())
	}
}

func TestHotspotBandAliasDeprecated(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.hs.SetWhitelist([]string{"Home"})
	w := postJSON(s, "/v1/hotspot/band", strings.Repeat("c", 64), `{"band":"dual"}`)
	if w.Code != 409 || w.Header().Get("Deprecation") != "true" || !strings.Contains(w.Body.String(), `"reason":"whitelist"`) {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body.String())
	}
}

func TestWhitelist409WhenDualBand(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.hs.SetDualBand(true)
	if w := postJSON(s, "/v1/hotspot/whitelist", strings.Repeat("c", 64), `{"ssids":["Home"]}`); w.Code != 409 || !strings.Contains(w.Body.String(), `"reason":"dual_band"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := postJSON(s, "/v1/hotspot/whitelist", strings.Repeat("c", 64), `{"ssids":[]}`); w.Code != 200 {
		t.Fatalf("clearing blocked: %d", w.Code)
	}
}

// Review Focus 3: run with -race.
func TestDualWhitelistRace(t *testing.T) {
	s, _, _ := dualServer(t, "RESULT=OK")
	for i := 0; i < 50; i++ {
		s.applyDualBand(false, false)
		s.hs.SetWhitelist(nil)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); s.applyDualBand(true, false) }()
		go func() { defer wg.Done(); postJSON(s, "/v1/hotspot/whitelist", strings.Repeat("c", 64), `{"ssids":["Home"]}`) }()
		wg.Wait()
		if s.hs.DualBand() && len(s.hs.Whitelist()) > 0 {
			t.Fatalf("iteration %d: dual band and allowlist both on", i)
		}
	}
}

func TestPresetAuto409WhenDualBand(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.hs.SetDualBand(true)
	if w := postJSON(s, "/v1/presets/auto", strings.Repeat("c", 64), `{"on":true}`); w.Code != 409 || !strings.Contains(w.Body.String(), `"reason":"dual_band"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := postJSON(s, "/v1/presets/auto", strings.Repeat("c", 64), `{"on":false}`); w.Code != 200 {
		t.Fatalf("auto off blocked: %d", w.Code)
	}
}

func TestPresetsRejectDualBand(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	w := postJSON(s, "/v1/presets", strings.Repeat("c", 64), `{"name":"Home","ssid":"Flippost","security":"wpa2","passphrase":"12345678","band":"dual"}`)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "band must be 2, 5, or 6") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestDualRouteRadioControl(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if w := postJSON(s, "/v1/hotspot/dual", strings.Repeat("a", 64), `{"enabled":false}`); w.Code != 403 {
		t.Fatalf("read token: %d", w.Code)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestAppUID|TestGuardApp|TestSummaryAndTailscaleRoutes|TestHotspotBandAlias|TestWhitelist409|TestDualWhitelistRace|TestPresetAuto409|TestPresetsRejectDual|TestDualRoute' .`

Expected: FAIL. The new routes return 404 and `s.app` is nil.

- [ ] **Step 3: Implement the wiring and update `schemas/api.openapi.json`**

- [ ] **Step 4: Run every offline check**

Run: `cd /Users/adacchi/module-zf5/daemon && go vet ./... && go test -race ./... && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null . && cd .. && bash tools/run-tests.sh | tail -2`

Expected: `ok  	zflip5/modemd`, `failures=0` and `OFFLINE_TESTS_OK=1`.

- [ ] **Step 5: Commit**

`git add daemon/server.go daemon/server_test.go schemas/api.openapi.json`. Subject: `feat(daemon): wire summary, Tailscale, dual band and app-UID auth into the API`.

---

### Task 1.16: Verify on the device (owner present)

**Files:** none. Run every root command through stdin. Steps marked **OWNER APPROVAL** wait for the owner's go-ahead in chat. Never call `/v1/tailscale/logout` in this task.

- [ ] **Step 1: Build**

Run: `cd /Users/adacchi/module-zf5 && bash tools/build-daemon.sh && bash tools/build-tether.sh && ls -l dist/zflip5-modemd magisk/tether/tether.jar`

Expected: both files exist, and the `go test` inside the build prints `ok`.

- [ ] **Step 2: OWNER APPROVAL (installs). Deploy the daemon, the jar and `service.sh`, keeping `modemd.prev`**

```bash
adb push dist/zflip5-modemd /data/local/tmp/zf5-modemd.new
adb push magisk/tether/tether.jar /data/local/tmp/zf5-tether.new
adb push magisk/service.sh /data/local/tmp/zf5-service.new
printf '%s\n' \
  'M=/data/adb/modules/zflip5_modem' \
  'cp $M/daemon/zflip5-modemd $M/daemon/modemd.prev' \
  'mv $M/daemon/zflip5-modemd $M/daemon/zflip5-modemd.old' \
  'cp /data/local/tmp/zf5-modemd.new $M/daemon/zflip5-modemd && chmod 0755 $M/daemon/zflip5-modemd && rm $M/daemon/zflip5-modemd.old' \
  'cp /data/local/tmp/zf5-tether.new $M/tether/tether.jar' \
  'cp /data/local/tmp/zf5-service.new $M/service.sh && chmod 0755 $M/service.sh' \
  'rm /data/local/tmp/zf5-modemd.new /data/local/tmp/zf5-tether.new /data/local/tmp/zf5-service.new' \
  'pkill -x zflip5-modemd' | adb shell su
sleep 15
echo 'tail -n 5 /data/adb/zflip5-modem/service.log' | adb shell su
```

Expected: `zflip5-modemd listening on 127.0.0.1:18080 (loopback)`.

- [ ] **Step 3: Check the summary (read-only)**

```bash
adb forward tcp:18080 tcp:18080
curl -s http://127.0.0.1:18080/v1/summary | python3 -m json.tool
for i in 1 2 3 4 5; do curl -s -o /dev/null -w '%{time_total}\n' http://127.0.0.1:18080/v1/summary; done
```

Expected:
- every key from §1.1 is present, and `slow_age_s` ≤ 5
- `signal.display` matches the status bar
- `tailscale` is `running` with `100.110.36.27`
- each `time_total` is under 0.050 s. That figure includes adb forwarding; the spec target of under 10 ms is on the device.

- [ ] **Step 4: Check that removed and app-only routes refuse (read-only)**

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18080/v1/sms/recent
curl -s -X POST -w ' %{http_code}\n' http://127.0.0.1:18080/v1/tailscale/login
curl -s -X POST -w ' %{http_code}\n' https://zflip5.<tailnet>.ts.net/v1/tailscale/login
```

Expected: `404`, then `{"code":403,"error":"app only","reason":"app_only"} 403` twice.

- [ ] **Step 5: Check the Tailscale status contents (read-only)**

Run: `curl -s http://127.0.0.1:18080/v1/tailscale | python3 -m json.tool; curl -s http://127.0.0.1:18080/v1/tailscale | grep -c 'login.tailscale.com\|AuthURL\|privkey'`

Expected: `running`, `serve_ok: true` and `web_url` set, then a count of `0`.

- [ ] **Step 6: Check per-client speed (owner connects a client and starts a download)**

```bash
for i in 1 2 3 4 5; do curl -s http://127.0.0.1:18080/v1/summary | python3 -c 'import json,sys; s=json.load(sys.stdin)["speed"]; print(s["wan_down_bytes_s"], s["per_client"])'; sleep 1; done
echo 'grep -m5 "speed:" /data/adb/zflip5-modem/service.log' | adb shell su
```

Expected: a list, with `down_bytes_s` within about 20 % of the client's own rate. `"unavailable"` is acceptable (decision 4); record the logged cause.

- [ ] **Step 7: Check the dual-band rules (no bounce)**

```bash
curl -s -X POST -d '{"enabled":false,"apply_now":false}' http://127.0.0.1:18080/v1/hotspot/dual
curl -s http://127.0.0.1:18080/v1/hotspot | python3 -c 'import json,sys; h=json.load(sys.stdin); print(h["dual_band"], h["whitelist"], h["wifi6"])'
```

Expected: `"applied":"next_start"`, then `False [...] true|false|None`. If the allowlist is non-empty, also POST `{"enabled":true}`. It must return 409 `whitelist`.

- [ ] **Step 8: OWNER APPROVAL (bounces the hotspot). Bounce and confirm Wi-Fi 6**

```bash
curl -s -X POST 'http://127.0.0.1:18080/v1/tether?action=stop'; sleep 3
curl -s -X POST 'http://127.0.0.1:18080/v1/tether?action=start'
printf '%s\n' 'cat /data/adb/zflip5-modem/hotspot-via' 'settings get secure wifi_ap_11ax_mode_checked' 'settings get global wifi_ap_11ax_mode_checked' 'grep -E "^(ieee80211ac|11axmode)=" /data/vendor/wifi/hostapd/hostapd_swlan0.conf' | adb shell su
curl -s http://127.0.0.1:18080/v1/summary | python3 -c 'import json,sys; print(json.load(sys.stdin)["hotspot"])'
```

Expected:
- the start returns `{"applied":true,"action":"start","active":true}`
- the root output is `sem`, `1`, `1`, `ieee80211ac=1` and `11axmode=1`
- the summary shows `"wifi6": true`

On the client, `system_profiler SPAirPortDataType` must show `802.11ax`, `149 (5GHz, 80MHz)` and about `1200`.

- [ ] **Step 9: OWNER APPROVAL (drops WAN 10 to 30 s). Check Wi-Fi 6 after a Rotate IP cycle (device-only risk)**

Run: `curl -s -X POST -d '{"mode":"cycle"}' --max-time 90 http://127.0.0.1:18080/v1/airplane; echo 'cat /data/adb/zflip5-modem/hotspot-via' | adb shell su`

Expected: `changed:true`, `hotspot_active:true` and `sem`. If it reads `tm`, record it. The fix is a longer settle in `restartHotspotRetry` (`airplane.go`), which is outside phase 1 ownership.

- [ ] **Step 10: OWNER APPROVAL (reboot). Verify the boot path**

```bash
adb reboot; adb wait-for-device; sleep 90
printf '%s\n' 'grep "hotspot sem try\|tm fallback" /data/adb/zflip5-modem/service.log | tail -n 3' 'cat /data/adb/zflip5-modem/hotspot-via' 'settings get secure wifi_ap_11ax_mode_checked' | adb shell su
```

Expected: a `hotspot sem try N: ... via=SemWifiManager...` line, then `sem` and `1`. Repeat the client PHY check from Step 8.

- [ ] **Step 11: Roll back if any step fails (OWNER APPROVAL)**

Run: `printf '%s\n' 'M=/data/adb/modules/zflip5_modem/daemon' 'mv $M/zflip5-modemd $M/zflip5-modemd.bad' 'cp $M/modemd.prev $M/zflip5-modemd && chmod 0755 $M/zflip5-modemd' 'pkill -x zflip5-modemd' | adb shell su`

Expected: `/v1/status` answers within about 15 s.

---

## Phase 2: native app and widget

Every Gradle command in this phase runs from `/Users/adacchi/module-zf5/helper/cover-app`, with `JAVA_HOME` exported as in Global Constraints. Paths of the form `app/src/...` are relative to `helper/cover-app/`.

### Task 2.1: Gradle project, Flippost M3 theme, signed shell APK (P2-A, 2a)

**Files:**
- Copy verbatim from `/private/tmp/claude-503/-Users-adacchi-module-zf5/862b9fb9-6555-4058-8bcf-3ea201957315/scratchpad/compose-probe/`:
  - `settings.gradle.kts`, `build.gradle.kts`, `gradle.properties`
  - `gradlew`, `gradlew.bat`
  - `gradle/wrapper/gradle-wrapper.{jar,properties}` (gradle-9.8.0)
- Create:
  - `gradle/libs.versions.toml`, `app/build.gradle.kts`
  - `app/src/main/AndroidManifest.xml`
  - `app/src/test/resources/robolectric.properties` (`sdk=34`)
  - `NUNITO-OFL.txt`
  - `app/src/main/res/font/nunito_variable.ttf`
  - `app/src/main/res/drawable/flippost_logo.png`
  - `local.properties` (`sdk.dir=/opt/homebrew/share/android-commandlinetools`; already gitignored)
- Move with `git mv`: `helper/res/{mipmap-anydpi-v26,mipmap-xxxhdpi}/`, `helper/res/values/colors.xml` and `helper/res/xml/network_security_config.xml`, into `app/src/main/res/` under the same names.
- Create: `app/src/main/java/com/zflip5/modem/ui/theme/{Color,Type,Theme}.kt` and the shell `CoverKioskActivity.kt`.
- Test: `app/src/test/java/com/zflip5/modem/ui/theme/ThemeTest.kt`

**Interfaces:**
- Produces:
  - `val FlippostDarkScheme: ColorScheme`
  - `object FlippostColors { green, amber, red, track, line, glass, inset, skyTop, skyBottom }`
  - `val Nunito: FontFamily`, `val FlippostTypography: Typography`
  - `@Composable fun FlippostTheme(content: @Composable () -> Unit)`
  - `fun Modifier.drizzle(): Modifier`
  - `R.drawable.flippost_logo`, `R.font.nunito_variable`, `R.mipmap.ic_launcher`

- [ ] **Step 1: Copy the probe and write the build files**

`libs.versions.toml`:
- keeps the probe versions: `agp="9.4.1"`, `kotlin="2.4.10"`, `composeBom="2026.09.00"`, `activity="1.13.0"`, `navigation="2.10.2"`
- adds `junit="4.13.2"`, `robolectric="4.14.1"`, `coroutines="1.10.2"`, `zxing="3.5.3"`
- adds the libraries `ui-test-junit4`, `ui-test-manifest`, `junit`, `robolectric`, `coroutines-test` and `zxing-core`

`app/build.gradle.kts`:
- `namespace` and `applicationId` = `com.zflip5.modem`
- SDK and version values from contract §3.1
- signing config `zf5`
- release: `isMinifyEnabled = true`, `isShrinkResources = true` and `proguard-android-optimize.txt`
- `testOptions.unitTests.isIncludeAndroidResources = true`
- dependencies as in contract §3.1
- disable release host tests, because ui-test-manifest exists only in debug:

  ```kotlin
  androidComponents { beforeVariants(selector().withBuildType("release")) {
      it.hostTests[com.android.build.api.variant.HostTestBuilder.UNIT_TEST_TYPE]?.enable = false } }
  ```

  If AGP 9.4.1 rejects that line, use `(it as com.android.build.api.variant.HasUnitTestBuilder).enableUnitTest = false`.

- [ ] **Step 2: Add the assets**

```bash
curl -fL -o app/src/main/res/font/nunito_variable.ttf 'https://github.com/google/fonts/raw/main/ofl/nunito/Nunito%5Bwght%5D.ttf'
curl -fL -o NUNITO-OFL.txt 'https://github.com/google/fonts/raw/main/ofl/nunito/OFL.txt'
sips -Z 128 ../../docs/assets/flippost-logo.png --out app/src/main/res/drawable/flippost_logo.png
file app/src/main/res/font/nunito_variable.ttf
```

Expected: `TrueType Font data`, and a PNG under 60 KB.

- [ ] **Step 3: Write `AndroidManifest.xml`**

- `INTERNET`
- `<application android:label="Flippost" android:icon="@mipmap/ic_launcher" android:allowBackup="false" android:networkSecurityConfig="@xml/network_security_config" android:theme="@android:style/Theme.Material.NoActionBar">`
- `.CoverKioskActivity`: `exported="true"`, `launchMode="singleTask"`, `configChanges="orientation|screenSize|screenLayout|smallestScreenSize|keyboardHidden|density"`, with a `MAIN`+`LAUNCHER` filter only.
- No widget entries; Task 2.13 adds them.

- [ ] **Step 4: Write the failing test**

```kotlin
@RunWith(RobolectricTestRunner::class)
class ThemeTest {
  @Test fun darkSchemeUsesDesignTokens() {
    assertEquals(Color(0xFFA9CDFB), FlippostDarkScheme.primary)
    assertEquals(Color(0xFF1B2436), FlippostDarkScheme.onPrimary)
    assertEquals(Color(0xFF1B2436), FlippostDarkScheme.background)
    assertEquals(Color(0xFFFEF7EE), FlippostDarkScheme.onSurface)
    assertEquals(Color(0xFF9EB1CB), FlippostDarkScheme.onSurfaceVariant)
    assertEquals(Color(0xFFFF9AA6), FlippostDarkScheme.error)
    assertEquals(Color(0xFF9EE6C3), FlippostColors.green)
    assertEquals(Color(0xFFFFD49A), FlippostColors.amber)
    assertEquals(Color(0x12D5E7FD), FlippostColors.glass)
    assertEquals(Color(0x26D5E7FD), FlippostColors.line)
  }
}
```

- [ ] **Step 5: Run the test and confirm it fails**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.ui.theme.ThemeTest'`

Expected: `Unresolved reference 'FlippostDarkScheme'`.

- [ ] **Step 6: Implement the theme**

`FlippostColors`:

| Name | Value |
|---|---|
| `green` | `0xFF9EE6C3` |
| `amber` | `0xFFFFD49A` |
| `red` | `0xFFFF9AA6` |
| `track` | `0x1FD5E7FD` |
| `line` | `0x26D5E7FD` |
| `glass` | `0x12D5E7FD` |
| `inset` | `0x730E1421` |
| `skyTop` | `0xFF222D43` |
| `skyBottom` | `0xFF141B29` |

`darkColorScheme` roles:

| Role | Value |
|---|---|
| `primary` | `A9CDFB` |
| `onPrimary` | `1B2436` |
| `primaryContainer` | `6FA4F0` |
| `onPrimaryContainer` | `1B2436` |
| `secondary` | `9EE6C3` |
| `tertiary` | `C4B5FF` |
| `error` | `FF9AA6` |
| `background` | `1B2436` |
| `surface` | `222D43` |
| `onBackground`, `onSurface` | `FEF7EE` |
| `onSurfaceVariant` | `9EB1CB` |
| `surfaceVariant` | `0x12D5E7FD` |
| `outline` | `0x26D5E7FD` |

`Nunito` uses `Font(R.font.nunito_variable, w, variationSettings = FontVariation.Settings(FontVariation.weight(w.weight)))` for weights 500, 600, 700 and 800.

`FlippostTypography` (every style uses Nunito):

| Style | Size | Weight | Extra |
|---|---|---|---|
| `displaySmall` | 40sp | 700 | `"tnum"` |
| `headlineSmall` | 30sp | 700 | `"tnum"` |
| `titleMedium` | 14.5sp | 800 | |
| `titleSmall` | 17sp | 800 | `"tnum"` |
| `bodyMedium` | 13.5sp | 500 | |
| `bodySmall` | 11.5sp | 500 | |
| `labelSmall` | 10.5sp | 700 | letterSpacing 0.08.em |

`Modifier.drizzle()`:
- `Brush.verticalGradient(skyTop, skyBottom)`.
- Rain streaks tiled every 64dp: segments (13,4)→(10,13), (42,17)→(39,26), (27,38)→(24,47) and (56,47)→(53,56) dp.
- Colour `0x17D5E7FD`, stroke 1.3dp, `StrokeCap.Round`.

The shell `CoverKioskActivity` calls `setContent { FlippostTheme { Box(Modifier.fillMaxSize().drizzle()) } }`.

- [ ] **Step 7: Run the test and the build**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.ui.theme.ThemeTest' assembleRelease && /opt/homebrew/share/android-commandlinetools/build-tools/36.0.0/apksigner verify --print-certs app/build/outputs/apk/release/app-release.apk | grep SHA-256; ./gradlew tasks --all | grep -c testReleaseUnitTest`

Expected: `BUILD SUCCESSFUL`, `SHA-256 digest: 47965ec3ab41b576b2cf0ab7f414c3127f0e95dbd58ecd57d21651ecdacca75a`, then `0`.

- [ ] **Step 8: Commit**

`git add helper/cover-app helper/res`. Subject: `feat(app): add Compose project with Flippost M3 theme`.

---

### Task 2.2: Data layer: models, parser, client and formatting (P2-A, 2a)

**Files:**
- Create: `app/src/main/java/com/zflip5/modem/data/{Models,SummaryParser,DaemonClient,Format}.kt`
- Test:
  - `app/src/test/java/com/zflip5/modem/data/{SummaryParserTest,DaemonClientTest,FormatTest}.kt`
  - `app/src/test/java/com/zflip5/modem/FakeDaemonClient.kt`
  - `app/src/test/resources/{summary.json,summary_nulls.json,tailscale_running.json}`

**Interfaces:**
- Produces:
  - contract §3.2 and §3.3: the models, `SummaryParser`, both exceptions and `open class DaemonClient`
  - `object Format` with:
    - `fun bytes(b: Long): String`
    - `fun usage(u: UsageInfo): String`
    - `fun mbps(bytesS: Long): String`
    - `fun temp(c: Double?): String`
    - `fun tempLevel(t: TempInfo): TempLevel`
    - `fun bandLabel(band: String?): String?`
    - `fun hotspotSubtitle(h: HotspotInfo): String`
    - `fun devices(n: Int): String`
  - `enum class TempLevel { OK, WARN, HOT }`
- Test fake `open class FakeDaemonClient : DaemonClient("http://fake")`:
  - `gets: MutableList<String>`
  - `posts: MutableList<Pair<String, JSONObject?>>`
  - `getResponses`, `postResponses` (default `JSONObject()`)
  - `failures: MutableMap<String, Exception>`, keyed by the full path including the query
  - `fun fixture(name: String): JSONObject`

- [ ] **Step 1: Write the fixtures**

- `summary.json` is the exact §1.1 sample.
- `summary_nulls.json` is the same, with these values: `signal:null`, `limit_bytes:0`, `left_bytes:null`, `next_reset:null`, `per_client:"unavailable"`, both `phone_*` `null`, `ssid`, `band` and `wifi6` `null`, `wan_ip` and `wan_iface` `null`, `temperature.c:null`, `battery:null`, `tailscale:{"state":"not_running","ip":null}`, `airplane:null`.
- `tailscale_running.json` is the §1.2 sample.

- [ ] **Step 2: Write the failing tests**

```kotlin
@RunWith(RobolectricTestRunner::class) class SummaryParserTest {
  @Test fun parsesFullSample() { val s = SummaryParser.parse(fixture("summary.json"))
    assertEquals(3, s.signal!!.bars); assertEquals("5G", s.signal!!.display)
    assertEquals(12345678901L, s.usage.periodBytes); assertEquals(87654321099L, s.usage.leftBytes)
    assertEquals(2500000L, s.speed.wanDownBytesS); assertEquals("192.168.1.23", s.speed.perClient!![0].ipv4)
    assertEquals(-48, s.speed.perClient!![0].signalDbm); assertEquals(true, s.hotspot.wifi6)
    assertNull(s.hotspot.dualBandBlock); assertEquals(41.2, s.temperature.c!!, 0.0)
    assertEquals("running", s.tailscale.state); assertEquals(false, s.airplane) }
  @Test fun perClientUnavailableAndNullsParse() { val s = SummaryParser.parse(fixture("summary_nulls.json"))
    assertNull(s.speed.perClient); assertNull(s.speed.phoneDownBytesS); assertNull(s.signal)
    assertNull(s.usage.leftBytes); assertNull(s.hotspot.wifi6); assertNull(s.wanIp)
    assertNull(s.temperature.c); assertNull(s.battery); assertNull(s.airplane); assertNull(s.tailscale.ip) }
  @Test fun parsesTailscaleStatus() { val t = SummaryParser.tailscale(fixture("tailscale_running.json"))
    assertEquals("zflip5.tailXXXX.ts.net", t.dnsName); assertEquals(listOf("100.110.36.27","fd7a:115c:a1e0::1"), t.ips)
    assertTrue(t.serveOk); assertNull(t.error) }
}
```

`DaemonClientTest` runs under Robolectric with `com.sun.net.httpserver.HttpServer` on `127.0.0.1:0`:
- `postNon2xxThrowsDaemonExceptionWithReason`: a 409 with `{"error":"clear the allowlist first: dual band stops Wi-Fi scanning","code":409,"reason":"whitelist"}` gives `code==409`, `reason=="whitelist"` and that message.
- `setDualBandSendsBodyAndNoAuthHeader`: expect a `POST` to `/v1/hotspot/dual` with `enabled` and `apply_now` both true and a `null` `Authorization` header. The result is `DualResult(true,"now")`.
- `refusedConnectionThrowsDaemonDown`: `DaemonClient("http://127.0.0.1:1").summary()` throws `DaemonDownException("service not running")`.
- `nonJsonErrorBodyUsesHttpCode`: a 502 with body `boom` gives `message=="HTTP 502"` and `reason==null`.
- `tetherNotAppliedThrows`: 200 `{"applied":false,...}` throws `DaemonException(200,"hotspot did not start","not_applied")`. The request path is `/v1/tether?action=start`.
- `rotateIpPostsCycle`: the path is `/v1/airplane` and the body is `{"mode":"cycle"}`.

`FormatTest` sets `Locale.setDefault(Locale("vi","VN"))` in `@Before` and restores it in `@After`:
- `bytes`: 0 → `"0 B"`, 1_234_567_890 → `"1.2 GB"`, 12_345_678_901 → `"12 GB"`, 87_654_321_099 → `"88 GB"`, 512_000_000 → `"512 MB"`.
- `usage`: limit 1e11 gives `"12 GB / 100 GB"`. Limit 0 gives `"12 GB"`.
- `mbps`: 2_500_000 → `"20.0"`, 300_000 → `"2.4"`, 2_488_000 → `"19.9"`, 0 → `"0.0"`.
- `temp`: 41.2 → `"41.2°"`, `null` → `"—"`.
- `tempLevel`:

  | Input | Result |
  |---|---|
  | SAFE, `safe=true` | OK |
  | WARM, RECOVERY | WARN |
  | HOT, COOLDOWN | HOT |
  | SAFE, `safe=false` | HOT |

- `hotspotSubtitle`:
  - `(true, wifi6=true, "5", 1)` → `"Wi-Fi 6 · 5 GHz · 1 device"`
  - `(wifi6=false, "dual", 5)` → `"Wi-Fi 6 unavailable · 2.4 + 5 GHz · 5 devices"`
  - `(null, null, 0)` → `"0 devices"`
  - inactive → `"Off"`
- `bandLabel`: `"2"` → `"2.4 GHz"`, `"5"` → `"5 GHz"`, `"6"` → `"6 GHz"`, `"dual"` → `"2.4 + 5 GHz"`, `null` → `null`.

- [ ] **Step 3: Run the tests and confirm they fail**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.data.*'`

Expected: `Unresolved reference 'SummaryParser'`.

- [ ] **Step 4: Implement**

- Parser:
  - A private `JSONObject.str(k)` returns `null` when `isNull(k)`.
  - `per_client` parses as `o.opt("per_client") is JSONArray`; any other value becomes `null`.
- Client:
  - `withContext(Dispatchers.IO)` over `HttpURLConnection`, with `connectTimeout = readTimeout = timeoutMs`, and `Content-Type: application/json`.
  - `ConnectException` becomes `DaemonDownException`.
  - An empty 2xx body becomes `JSONObject()`.
  - Timeouts in ms:

    | Method | Timeout |
    |---|---|
    | `summary`, `tailscale` | 3_000 |
    | `tailscaleLogin` | 30_000 |
    | `tailscaleLogout` | 10_000 |
    | `setDualBand` | 40_000 |
    | `tether` | 25_000 |
    | `rotateIp` | 60_000 |

- Format:
  - `String.format(Locale.US, …)` everywhere.
  - `bytes` uses SI base 1000 over `B, kB, MB, GB, TB`. A value under 10 gets 1 decimal; anything else is a whole number.

- [ ] **Step 5: Run the tests and confirm they pass**

Same command. Expected: `BUILD SUCCESSFUL`.

- [ ] **Step 6: Commit**

`git add helper/cover-app/app/src`. Subject: `feat(app): add daemon client, summary parser, and formatting`.

---

### Task 2.3: Summary repository: 1 s polling while visible, stale after 5 s (P2-A, 2a)

**Files:**
- Create: `app/src/main/java/com/zflip5/modem/data/SummaryRepository.kt`
- Test: `app/src/test/java/com/zflip5/modem/data/SummaryRepositoryTest.kt`

**Interfaces:**
- Consumes: `summary()` and `DaemonDownException` (2.2).
- Produces: `SummaryState`, including the `slowAgeS > 30` rule, `writable` and `SummaryRepository`, exactly as in contract §3.3. The initial state is `SummaryState(null, 0L, null, false)`.

- [ ] **Step 1: Write the failing tests**

Every test uses `runTest`, and builds `SummaryRepository(fake, backgroundScope, now = { testScheduler.currentTime })`. The fake counts `get("/v1/summary")` calls.
- `startPollsOncePerSecond`: `start()`, then `advanceTimeBy(3_500)`. Expect 4 calls.
- `startTwiceDoesNotDoublePoll`: `start()` twice, then `advanceTimeBy(2_500)`. Expect 3 calls.
- `stopHaltsPolling`: `start()`, `advanceTimeBy(1_500)`, `stop()`. Record the count `n`, then `advanceTimeBy(5_000)`. Expect still `n`.
- `downOnRefusedThenRecovers`: the first call throws `DaemonDownException(ConnectException())`. Expect `down` true and `summary` null. On the next tick the fixture answers. Expect `down` false, `summary` non-null and `lastOkAtMs == 1000`.
- `errorKeepsLastSummary`: a success, then `IOException("timeout")`. Expect `summary` kept, `error == "timeout"` and `down` false.
- `staleAfterFiveSeconds`: `SummaryState(s, 1_000, null, false)`. Expect `stale(6_000)` false, `stale(6_001)` true, `writable(6_001)` false and `writable(2_000)` true.
- `staleWhenCollectorStuck` (Review Focus 2): with `s.copy(slowAgeS = 31)` and `lastOkAtMs = 1_000`, `stale(1_500)` is true and `writable(1_500)` is false. With `slowAgeS = 30`, `stale(1_500)` is false.
- `refreshNowFetchesImmediately`: call `refreshNow()` without `start()`, then `runCurrent()`. Expect 1 call.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.data.SummaryRepositoryTest'`

Expected: `Unresolved reference 'SummaryRepository'`.

- [ ] **Step 3: Implement**

- The poll loop is `job = scope.launch { while (isActive) { fetchOnce(); delay(1_000) } }`. `start()` returns early when `job?.isActive == true`.
- `fetchOnce` outcomes:
  - success: `SummaryState(s, now(), null, false)`
  - `DaemonDownException`: `copy(down = true, error = "service not running")`
  - any other exception: `copy(error = e.message, down = false)`

- [ ] **Step 4: Run the tests and confirm they pass**

Same command.

- [ ] **Step 5: Commit**

Subject: `feat(app): poll /v1/summary every second while visible`.

---

### Task 2.4: Shared components: optimistic writes, overlays and layout helpers (P2-A, 2a)

**Files:**
- Create in `app/src/main/java/com/zflip5/modem/ui/components/`: `SignalBars.kt`, `GlassCard.kt`, `PillButton.kt`, `StripedBar.kt`, `StaleLabel.kt`, `Optimistic.kt`, `Overlay.kt`, `ScreenScaffold.kt`, `QrCode.kt`, `RotateIpFlow.kt`
- Test: `app/src/test/java/com/zflip5/modem/CoverAsserts.kt` and `…/ui/components/{OptimisticSwitchTest,SignalBarsTest,StaleLabelTest,RotateIpFlowTest,QrCodeTest}.kt`

**Interfaces:**
- Produces (P2-B and P2-C use these exact names):

```kotlin
@Composable fun SignalBars(bars: Int?, modifier: Modifier = Modifier)   // 4 bars 4/7/10/13dp tall, 3dp wide, 2dp gap; unreached Color(0x40D5E7FD); cd "Signal $bars of 4 bars" | "Signal level unknown"
@Composable fun GlassCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) // glass bg, 1dp line border, 18dp radius, padding 9×11dp
@Composable fun PillButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, primary: Boolean = false, destructive: Boolean = false) // min 48dp, 22dp radius; primary = A9CDFB→6FA4F0 gradient
@Composable fun StripedBar(fraction: Float, modifier: Modifier = Modifier) // 9dp, track bg, -45° stripes A9CDFB 6dp / 8DBCF7 6dp, clamp 0..1
fun staleText(state: SummaryState, nowMs: Long): String // down→"Service not running"; null→"Connecting…"; stale→"Updated ${(nowMs-lastOkAtMs)/1000} s ago"; else "Live"
@Composable fun StaleLabel(state: SummaryState, nowMs: Long, modifier: Modifier = Modifier)
@Composable fun rememberNowMs(): Long
val LocalSnackbar: ProvidableCompositionLocal<SnackbarHostState> // staticCompositionLocalOf { error("LocalSnackbar not provided") }
class OptimisticState { val value: Boolean; val busy: Boolean; suspend fun write(target: Boolean, call: suspend () -> Unit) }
@Composable fun rememberOptimistic(server: Boolean): OptimisticState
@Composable fun OptimisticSwitch(checked: Boolean, enabled: Boolean, onWrite: suspend (Boolean) -> Unit, modifier: Modifier = Modifier)
@Composable fun OverlayCard(onDismiss: () -> Unit, content: @Composable ColumnScope.() -> Unit)
@Composable fun ConfirmOverlay(title: String, body: String, confirmLabel: String, onConfirm: () -> Unit, onDismiss: () -> Unit, destructive: Boolean = false)
@Composable fun ScreenScaffold(title: String, onBack: () -> Unit, content: LazyListScope.() -> Unit)
fun qrMatrix(text: String): com.google.zxing.common.BitMatrix
@Composable fun QrCode(text: String, modifier: Modifier = Modifier)
@Composable fun RotateIpFlow(client: DaemonClient, onFinish: () -> Unit)
```

- Test helpers in `CoverAsserts.kt`:
  - `const val COVER = "w352dp-h340dp-land-xhdpi"`
  - `assertFitsWithoutScroll()`, `assertNoTargetInCutout()` and `assertTargetsAtLeast48dp()`, each on `ComposeContentTestRule`
  - `setCover(content)`, which wraps the content in `FlippostTheme` and `LocalSnackbar`, inside a `Box(fillMaxSize)` with a `SnackbarHost` aligned `TopCenter`

**Behavior pinned by the tests:**
- `OptimisticState.write`:
  - It does nothing while `busy`.
  - Otherwise it sets `pending = target` and `busy = true`, fires the haptic `ToggleOn` or `ToggleOff`, then runs `call()`.
  - On a `DaemonException` or `IOException` it clears `pending`, fires `Reject`, and runs `showSnackbar(e.message ?: "write failed")`.
  - On success, `pending` stays until `server == target` or 3 s pass.
  - `value = pending ?: server`.
- `OverlayCard`:
  - A scrim `Color(0xB3141B29)`; a click on it calls `onDismiss`.
  - The card is aligned `TopCenter` with padding 11dp and `heightIn(max = 340.dp - 31.dp - 22.dp)`. It scrolls inside the card only.
  - Buttons are stacked full width. The confirm button has testTag `"confirm_button"` and the cancel button has `"cancel_button"`.
- `ScreenScaffold`:
  - A drizzle background.
  - A 48dp header with a `"←"` back button (48×48dp, contentDescription `"Back"`) and the title.
  - A `LazyColumn` with padding start 11, end 11 and bottom 40dp.
- `RotateIpFlow` states:
  1. Confirm: title `"Rotate IP?"`, body `"Drops connectivity for about 10 to 30 seconds."`, confirm label `"Rotate"`.
  2. Running: `"Rotating IP…"` with a spinner.
  3. Done: `"New IP $new_ip"`, or `"IP unchanged ($old_ip)"` when not changed. A `"Done"` button calls `onFinish`.
  4. Failed: the exception message, with a `"Done"` button.

- [ ] **Step 1: Write the failing tests (Robolectric, `@Config(qualifiers = COVER)`)**

- `OptimisticSwitchTest`, with a fake `LocalHapticFeedback` that records types:
  - `failedWriteRollsBackAndShowsSnackbar`: `DaemonException(409, "clear the allowlist first: dual band stops Wi-Fi scanning", "whitelist")`. Expect `assertIsOff()`, the text displayed, and haptics `[ToggleOn, Reject]`.
  - `successShowsTargetImmediately`: a `CompletableDeferred` write. Expect `assertIsOn()` before it completes.
  - `doubleTapSendsOneWrite`: expect one call.
  - `disabledSwitchIgnoresClicks`: expect no write and `assertIsNotEnabled()`.
- `SignalBarsTest`: `describesBars` (`"Signal 3 of 4 bars"`) and `nullBarsUnknown`.
- `StaleLabelTest`, with no Robolectric:
  - `staleText(SummaryState(s, 1_000, null, false), 8_000) == "Updated 7 s ago"`
  - `down` gives `"Service not running"`
  - no summary gives `"Connecting…"`
  - `nowMs = 3_000` gives `"Live"`
- `RotateIpFlowTest`:
  - `confirmsBeforeRotating`: no post until `"confirm_button"`, then a post to `/v1/airplane` with `{"mode":"cycle"}`.
  - `showsNewIp` and `unchangedIp`.
  - `busyShowsDaemonError`: `DaemonException(409,"airplane busy","busy")` shows `"airplane busy"`.
  - `overlayClearsCutout`.
- `QrCodeTest.encodesLoginUrl`: `qrMatrix("https://login.tailscale.com/a/abc").width >= 21`.

`CoverAsserts` implementation:
- The root height must equal `340.dp` in px, within ±1.
- The cutout is `Rect(rootW-174dp, rootH-31dp, rootW, rootH)`.
- Clickable nodes come from `onAllNodes(hasClickAction(), useUnmergedTree = true)`. Each `touchBoundsInRoot` must not overlap the cutout and must be at least 48×48dp.
- `assertFitsWithoutScroll`: no node has `VerticalScrollAxisRange`, and every node's `Rect(positionInRoot, size)` lies inside the root.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.ui.components.*'`

Expected: `Unresolved reference 'OptimisticSwitch'`.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and confirm they pass**

Same command.

- [ ] **Step 5: Commit**

Subject: `feat(app): add optimistic switch, overlays, and Flippost components`.

---

### Task 2.5: Home option C, navigation, lifecycle and More placeholders (P2-A, 2a)

**Files:**
- Create: `app/src/main/java/com/zflip5/modem/ui/home/HomeScreen.kt`
- Create placeholders: `ui/more/{MoreScreen,DevicesScreen,HotspotScreen,DataScreen,SystemScreen,TailscaleScreen,AlertsScreen,AdvancedScreen,DualBandDialog}.kt`. Each body is `ScreenScaffold(title, onBack) {}`, and `DualBandDialog` is empty. P2-B owns these files from 2b.
- Modify: `CoverKioskActivity.kt`
- Test: `app/src/test/java/com/zflip5/modem/ui/home/HomeScreenTest.kt`

**Interfaces:**
- Produces:

```kotlin
@Composable fun HomeScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onMore: () -> Unit, nowMs: Long = rememberNowMs())
@Composable fun FlippostApp(repo: SummaryRepository, client: DaemonClient)  // theme + LocalSnackbar + SnackbarHost(TopCenter) + NavHost (routes in contract §3.1)
@Composable fun MoreScreen(onOpen: (route: String) -> Unit, onBack: () -> Unit)
@Composable fun DevicesScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onBack: () -> Unit)
// HotspotScreen, DataScreen, SystemScreen, TailscaleScreen, AlertsScreen, AdvancedScreen: same 4 params
@Composable fun DualBandDialog(enable: Boolean, onChoose: (applyNow: Boolean) -> Unit, onDismiss: () -> Unit)
```

**Layout:** a 352×340dp `Column` with `padding(11.dp)` and no scroll.
1. **Header row** (bottom gap 8dp):
   - A 44dp circular avatar with a 3dp border `0x40D5E7FD`.
   - `GlassCard(weight 1)` with the title `"Hotspot · ${ssid ?: "—"}"` (`titleMedium`, 1 line, ellipsis) and the subtitle `Format.hotspotSubtitle(h)`.
   - `OptimisticSwitch` (testTag `"hotspot_switch"`, `enabled = writable`, `onWrite = { client.tether(it); onRefresh() }`).
2. **Chip row** (gap 6dp, bottom 8dp; the chips are not clickable):
   - `SignalBars(signal?.bars)` plus `signal?.display ?: "No signal"`.
   - A 7dp temperature dot from `tempLevel`, plus `Format.temp(c ?: rawC)`.
   - `wanIp ?: "No WAN"`, with `weight(1f, fill=false)` and ellipsis.
   - `"Tailscale"` plus a dot: green when `running`, amber when `needs_login`, `0x40D5E7FD` otherwise.
3. **`GlassCard`** (bottom 9dp):
   - `StaleLabel` and `"↓${mbps(down)} ↑${mbps(up)} Mb/s"`.
   - `"DATA · CYCLE"` and `Format.usage`.
   - `StripedBar(period/limit, or 0)`.
   - `"Today ${bytes(today)}"` and `leftBytes?.let { "${bytes(it)} left" } ?: "No cap"`.
4. **Pill row**, `Row(Modifier.width(176.dp))`, gap 6dp:
   - `PillButton("Rotate IP", enabled = writable)`, which opens `RotateIpFlow` and calls `onRefresh()` on finish.
   - `PillButton("More")`, always enabled.

When `!writable`, the header, chips and card get `alpha(0.5f)`. When there is no summary, every value shows `"—"`.

**Activity:**
- `enableEdgeToEdge()`.
- Hide the system bars with `WindowCompat.getInsetsController`, using `BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE`.
- `repo = SummaryRepository(client, lifecycleScope)`, then `setContent { FlippostApp(repo, client) }`.
- `onStart` calls `repo.start()` and `onStop` calls `repo.stop()`.
- No `setShowWhenLocked`, `setTurnScreenOn` or `FLAG_KEEP_SCREEN_ON`.

- [ ] **Step 1: Write the failing tests**

Use `@Config(qualifiers = COVER)` and `fresh = SummaryState(parse("summary.json"), 100_000, null, false)` with `nowMs = 100_500`.
- `homeFitsWithoutScrolling`, `noTapTargetInCameraCutout` and `everyTapTargetIsAtLeast48dp`.
- `pillsStayInLeft176dp`: for `"Rotate IP"` and `"More"`, `boundsInRoot.right <= 176.dp`.
- `rendersOptionCContent`: shows `"Hotspot · Flippost"`, `"Wi-Fi 6 · 5 GHz · 1 device"`, `"5G"`, `"41.2°"`, `"100.64.12.34"`, `"Tailscale"`, `"↓20.0 ↑2.4 Mb/s"`, `"12 GB / 100 GB"`, `"Today 1.2 GB"` and `"88 GB left"`, and has contentDescription `"Signal 3 of 4 bars"`.
- `nullsRenderPlaceholders`: `summary_nulls.json` shows `"No signal"`, `"No WAN"` and `"No cap"`, and still fits.
- `longValuesStillFit`: SSID `"Flippost-super-long-ssid-32-char"`, IP `"255.255.255.255"` and operator `"Viettel Telecom Long Name"`. It fits and clears the cutout.
- `staleStateGreysOutAndDisablesWrites`: at `nowMs = 107_000`, it shows `"Updated 7 s ago"`; the switch and `"Rotate IP"` are not enabled, and `"More"` is enabled.
- `downShowsServiceNotRunning`.
- `switchTogglesHotspotOptimistically`: `postResponses["/v1/tether?action=stop"]` is `{"applied":true,"action":"stop","active":false}`. Click the switch. Expect off, a post recorded, and `onRefresh` called once.
- `cutoutClearWithOverlayAndSnackbar`.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.ui.home.HomeScreenTest'`

Expected: `Unresolved reference 'HomeScreen'`.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the whole suite and the build**

Run: `./gradlew test assembleRelease && ls -l app/build/outputs/apk/release/app-release.apk`

Expected: `BUILD SUCCESSFUL`, with the APK under 3000000 bytes.

- [ ] **Step 5: Commit**

Subject: `feat(app): add option C home with live summary and navigation`. This hands `AndroidManifest.xml` to P2-C and `ui/more/**` to P2-B.

---

### Task 2.6: `build-apk.sh` on Gradle, Action button, test step and old helper removal (P2-D, 2a, after 2.1)

**Files:**
- Rewrite: `helper/build-apk.sh`, `magisk/action.sh`
- Modify: `tools/run-tests.sh`, with a block after the Go block (ends at :74)
- Modify: `.gitignore`, adding `magisk/app/` and `.kotlin/`
- Delete: `helper/src/`, `helper/AndroidManifest.xml`, `helper/res/values/strings.xml`
- Test: `tests/app-install.test.sh` (new)

**Interfaces:**
- Consumes: `helper/cover-app/app/build/outputs/apk/release/app-release.apk` (2.1).
- Produces:
  - `dist/zflip5-kiosk.apk`
  - `magisk/app/zflip5-kiosk.apk`
  - `magisk/app/versionCode`, a single decimal line
  - stdout `built <path> versionCode=<n>` then `APK_SIGNED_OK`

**`build-apk.sh`:**
- `set -eu`. Keep the `ANDROID_HOME` and `JAVA_HOME` defaults from :9-10 and export them.
- When `dist/debug.keystore` is missing, print `error: dist/debug.keystore missing; restore it from backup (a new key cannot update the installed app)` and exit 1. Delete the keytool generation at :38-42.
- Run `(cd "$HERE/cover-app" && ./gradlew --no-daemon -q assembleRelease)` and copy the APK.
- `apksigner verify --print-certs` must contain the SHA-256 from Global Constraints.
- The size must be under 3000000.
- `VC` comes from `aapt2 dump badging` and must be ≥ 2.
- Copy the APK to `magisk/app/` and write `versionCode`.

**`action.sh` body:**

```sh
am start --user 0 -n com.zflip5.modem/.CoverKioskActivity >/dev/null 2>&1 \
  && echo "Flippost opened" \
  || echo "Could not open Flippost. The module installs the app at boot; reboot once after installing the module."
```

**`run-tests.sh` block:** `echo "=== ANDROID APP (Gradle) ==="`.
- When both `gradlew` and the SDK exist, run `./gradlew --no-daemon -q test` with the defaults exported, and print `PASS gradle test cover-app` or `FAIL gradle test cover-app`.
- Otherwise print `SKIP gradle test cover-app (gradlew or Android SDK absent)`.

- [ ] **Step 1: Write the failing test**

`tests/app-install.test.sh` uses the `pass()`/`fail()` pattern from `tests/magisk.test.sh`. Checks:
- `action.sh opens the app`
- `action.sh holds no tokens`: `! grep -qiE 'token|read-status|radio-control' magisk/action.sh`
- `build-apk.sh uses gradle`: `grep -q gradlew` and no `aapt2" link`
- `old webview sources gone`
- `app has no WebView`: `! grep -rq WebView helper/cover-app/app/src/main`

The script exits with the number of failures.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `bash tests/app-install.test.sh`

Expected: `FAIL - action.sh holds no tokens`.

- [ ] **Step 3: Implement**

Write both scripts, add the `run-tests.sh` block and the `.gitignore` lines, then run `git rm -r helper/src helper/AndroidManifest.xml helper/res/values/strings.xml`.

- [ ] **Step 4: Run the tests and the build**

Run: `bash tests/app-install.test.sh && bash helper/build-apk.sh && cat magisk/app/versionCode && bash tools/run-tests.sh --offline | grep -E 'gradle|OFFLINE_TESTS_OK'`

Expected:
- every check prints `ok`
- `built /Users/adacchi/module-zf5/dist/zflip5-kiosk.apk versionCode=2`, then `APK_SIGNED_OK`, then `2`
- `PASS gradle test cover-app`

- [ ] **Step 5: Commit**

`git add helper/build-apk.sh magisk/action.sh tools/run-tests.sh .gitignore tests/app-install.test.sh`, plus the deletions. Subject: `build(app): build the native APK with Gradle and bundle it in the module`.

---

### Task 2.7: The module installs the bundled app when its versionCode is higher (P2-D, 2a)

**Files:**
- Create: `magisk/app-install.sh`
- Modify: `magisk/service.sh`. Insert directly above the text `# The zip does not preserve the exec bit; ensure the binary is runnable.`
- Test: `tests/app-install.test.sh`

**Interfaces:**
- Consumes: the output of Task 2.6.
- Produces: `sh app-install.sh MODDIR`, with the test seam `ZF5_TMP` (default `/data/local/tmp`). It prints exactly one line and always exits 0:
  - `APP_INSTALL=skip reason=no_bundle`
  - `APP_INSTALL=skip have=<n> want=<n>`
  - `APP_INSTALL=ok have=<n> want=<n>`
  - `APP_INSTALL=fail have=<n> want=<n> rc=<rc>`

**Logic:**
- `WANT` is read from `app/versionCode`. When the APK is missing or `WANT` is not numeric, print `no_bundle`.
- `HAVE=$(pm list packages --show-versioncode com.zflip5.modem | sed -n 's/^package:com\.zflip5\.modem versionCode:\([0-9]*\).*/\1/p')`. An empty result means 0.
- When `HAVE >= WANT`, skip.
- Otherwise copy the APK to `$ZF5_TMP/zf5-app.apk`, `chmod 0644`, run `pm install -r` on it, then `rm -f` the copy. system_server may be denied reads from `/data/adb`, which is why the copy is needed.

**`service.sh` insert:**

```sh
  # Native app: install or update the bundled Flippost APK when it is newer.
  sh "$MODDIR/app-install.sh" "$MODDIR" >> "$LOG" 2>&1
```

- [ ] **Step 1: Write the failing tests**

Stub `pm` in `$TMP/bin`:
- it prints `$PM_LIST` for `list packages`
- it appends `"$*"` to `$TMP/pm.log` for `install`
- it exits with `$PM_RC`

Use a fake `$TMP/mod/app/{zflip5-kiosk.apk,versionCode=2}` and run `PATH="$TMP/bin:$PATH" ZF5_TMP="$TMP/t" sh magisk/app-install.sh "$TMP/mod"`.

| Case | Setup | Expected |
|---|---|---|
| installs when older | `PM_LIST='package:com.zflip5.modem versionCode:1'` | `ok have=1 want=2`, a log line `install -r $TMP/t/zf5-app.apk`, no copy left |
| installs when absent | empty | `ok have=0 want=2` |
| skips when equal | versionCode 2 | `skip have=2 want=2`, empty log |
| skips when newer installed | versionCode 3 | `skip have=3 want=2` |
| ignores lookalike package | `com.zflip5.modem.debug versionCode:9` | `have=0`, then an install |
| reports failure, exits 0 | `PM_RC=1` | `fail have=1 want=2 rc=1`, exit 0 |
| no bundle | no APK | `skip reason=no_bundle` |
| service.sh calls it | | `grep -q app-install.sh magisk/service.sh` |

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `bash tests/app-install.test.sh`

Expected: `FAIL - installs when older`.

- [ ] **Step 3: Implement (shebang `#!/system/bin/sh`)**

- [ ] **Step 4: Run the tests**

Run: `bash tests/app-install.test.sh && bash tests/magisk.test.sh`

Expected: every check `ok`, and `MAGISK_LINT_OK=1`.

- [ ] **Step 5: Commit**

Subject: `feat(module): install the bundled app at boot when it is newer`.

---

### Task 2.8: More list, Devices and Alerts (P2-B, 2b)

**Files:**
- Modify: `ui/more/{MoreScreen,DevicesScreen,AlertsScreen}.kt`
- Test: `…/ui/more/{MoreScreenTest,DevicesScreenTest}.kt`

**Interfaces:**
- Consumes: the Task 2.5 signatures, plus `ScreenScaffold`, `GlassCard`, `Format` and `FakeDaemonClient`.

**Content:**
- **More:** 7 rows, each at least 48dp: Devices, Hotspot, Data, System, Tailscale, Alerts, Advanced. A tap calls `onOpen(<lowercase route>)`.
- **Devices:**
  - one row per client: `ipv4 ?: mac`, `"↓… ↑… Mb/s"` and `"$it dBm"`
  - then `"This phone (approx.)"` when the phone rates are non-null
  - when `perClient == null`: `"Per-device speed unavailable"`, the WAN total row, and `"${hotspot.clients} devices connected"`
- **Alerts:** the read-only text `"Alerts are set on the device in the ntfy config."` (decision 1).

- [ ] **Step 1: Write the failing tests (`@Config(qualifiers = COVER)`)**

- `listsAllSevenScreens`: each label is shown after `performScrollToNode`. A click on Devices records `onOpen("devices")`. 48dp targets.
- `rowsShowClientsAndPhone`: shows `"192.168.1.23"`, `"↓19.9 ↑2.4 Mb/s"`, `"-48 dBm"`, `"This phone (approx.)"` and `"↓0.1 ↑0.0 Mb/s"`.
- `perClientUnavailable`: shows `"Per-device speed unavailable"`, and no phone row.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.ui.more.MoreScreenTest' --tests 'com.zflip5.modem.ui.more.DevicesScreenTest'`

Expected: FAIL, because node `"Devices"` is not found.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and confirm they pass**

Same command.

- [ ] **Step 5: Commit**

Subject: `feat(app): add More list, Devices, and Alerts screens`.

---

### Task 2.9: Hotspot screen and dual-band dialog (P2-B, 2b)

**Files:**
- Modify: `ui/more/HotspotScreen.kt`, `ui/more/DualBandDialog.kt`
- Test: `…/ui/more/{HotspotScreenTest,DualBandDialogTest}.kt`

**Interfaces:**
- Consumes: `setDualBand`, `get`, `post`, `rememberOptimistic`, `OptimisticSwitch`, `ConfirmOverlay` and `OverlayCard`.
- Private helper in this file (`TailscaleScreen.kt` keeps its own copy):

  `@Composable private fun PollWhileStarted(periodMs: Long, block: suspend () -> Unit)`, which runs `repeatOnLifecycle(STARTED) { while (true) { block(); delay(periodMs) } }`.

**Data:** load `GET /v1/hotspot`, `/v1/presets` and `/v1/usbtether`, then poll `/v1/hotspot` every 5 s. Reload after every write.

**Sections:**
1. **Dual band:**
   - `rememberOptimistic(server = hotspot.dualBand)` drives a `Switch` (testTag `"dual_switch"`), with `enabled = writable && (dualBand || dualBandBlock == null)`.
   - Explanation text: `"whitelist"` → `"Clear the allowlist first: dual band stops Wi-Fi scanning."`; `"auto_switch"` → `"Turn off preset auto-switch first: dual band stops Wi-Fi scanning."`.
   - A toggle opens `DualBandDialog`. A choice runs `opt.write(target) { client.setDualBand(target, applyNow); onRefresh() }`. Cancel changes nothing.
2. **Allowlist:**
   - SSIDs with `"Remove"`, plus an `OutlinedTextField` labelled `"SSID"` and `"Add"`. Each change posts `/v1/hotspot/whitelist` `{"ssids":[…]}` (max 16).
   - Disabled while dual band is on, with `"Turn off dual band to use the allowlist."`.
3. **Timed force-on:** `"1 h"`, `"4 h"` and `"Off"` post `/v1/hotspot/override` `{"hours":1|4|0}`. Shows `"On for N min"` from `override_left_s`.
4. **Manual scan:** `"Scan now"` posts `/v1/hotspot/scan`, then GETs `/v1/hotspot`. Lists `nearby` as `ssid  rssi dBm`, marked `"allowlisted"` where it applies.
5. **Presets:**
   - Each row has `"Apply"` (posts `/v1/presets/apply` `{"id"}`) and `"Delete"` (a destructive confirm, then `/v1/presets/delete` `{"id"}`).
   - `"Auto-switch"` is an `OptimisticSwitch` that posts `/v1/presets/auto` `{"on"}`. It is disabled while dual band is on, with `"Turn off dual band to use auto-switch."`.
   - `"Add preset"` opens a form:
     - fields `name`, `ssid`, `passphrase` and `triggers` (comma-separated)
     - security `open`, `wpa2` or `wpa3`
     - bands `"2.4 GHz"`, `"5 GHz"` and `"6 GHz"` only
     - `"Save"` posts `/v1/presets`
6. **USB tethering:** an `OptimisticSwitch` that posts `/v1/usbtether/toggle?action=start|stop`. `applied:false` throws `DaemonException(200,"USB tethering did not change","not_applied")`.

**`DualBandDialog`:**
- Title: `"Turn on dual band?"` or `"Turn off dual band?"`.
- Body when enabling: `"Dual band serves 2.4 GHz and 5 GHz under one name and stops Wi-Fi scanning."`. Body when disabling: `"The hotspot goes back to 5 GHz only."`.
- Buttons: `"Apply now (clients drop about 10 s)"` calls `onChoose(true)`; `"At next start"` calls `onChoose(false)`; `"Cancel"` calls `onDismiss`.

- [ ] **Step 1: Write the failing tests**

Fake GETs:
- `/v1/hotspot`: `{"active":true,"whitelist":[],"nearby":[],"dual_band":false,"wifi6":true}`
- `/v1/presets`: `{"active":"","auto_switch":false,"presets":[]}`
- `/v1/usbtether`: `{"active":false,"ifaces":[]}`

Tests:
- `dualSwitchDisabledWhileAllowlistSet`
- `turningOffDualIsNeverBlocked`
- `applyNowSendsApplyNowTrue`: the last post is `/v1/hotspot/dual` with `enabled` and `apply_now` both true, and the switch is on.
- `atNextStartSendsApplyNowFalse`
- `cancelLeavesSwitchOff`
- `dual409RollsBackWithSnackbar`: the `auto_switch` 409 text is shown and the switch is off.
- `presetBandChoicesExcludeDual`: no `"Dual"` text in the form.
- `usbNotAppliedRollsBack`
- `DualBandDialogTest.dialogClearsCutoutAndTargets`

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.ui.more.HotspotScreenTest' --tests 'com.zflip5.modem.ui.more.DualBandDialogTest'`

Expected: FAIL.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and confirm they pass**

Same command.

- [ ] **Step 5: Commit**

Subject: `feat(app): add Hotspot screen with opt-in dual band`.

---

### Task 2.10: Data and System screens (P2-B, 2b)

**Files:**
- Modify: `ui/more/{DataScreen,SystemScreen}.kt`
- Test: `…/ui/more/{DataScreenTest,SystemScreenTest}.kt`

**Interfaces:**
- Consumes: `RotateIpFlow`, `OptimisticSwitch`, `ConfirmOverlay`, `get`, `post`, `temperature` and `airplane`.

**Data screen:**
- `GET /v1/usage` prefills the form.
- `"Cap (GB)"` is numeric; empty means 0.
- Period: `"Daily"`, `"Weekly"`, `"Monthly"` or `"Manual"`.
- `"Reset time"` is `HH:MM`. `"Reset day"` is 1..28 and shows for monthly only.
- `"Save"` posts `/v1/usage/quota` `{"limit_bytes": GB*1_000_000_000, "period", "reset_time", "reset_day"}`.
- `"Reset data usage"` opens a destructive confirm (`"Reset data usage?"`), then posts `/v1/usage/reset`.

**System screen:**
- `"Bench mode"` posts `/v1/thermal/bench` `{"enabled"}`.
- `"Thermal limits"`: `"Warn °C"` and `"Gate °C"`, then `"Apply"` posts `/v1/thermal/limits`. When the response has `clamped:true`, show `"Clamped to warn ${warn_c}°, gate ${gate_c}°"`.
- `"CPU mode"`: pills `"Auto"`, `"Performance"`, `"Balanced"`, `"Eco"` and `"Off"` post `/v1/cpu/mode`. The current mode comes from `GET /v1/cpu` `requested`, shown as `"Running: $mode ($reason)"`.
- `"Cooldown"` posts `/v1/cooldown` and shows `note`.
- `"Battery cap"`: read-only text `"Battery stops charging at 80%. The module sets this at boot."`.
- `"Airplane mode"` posts `/v1/airplane` `{"mode":"on"|"off"}` with a 60_000 ms timeout.
- `"Rotate IP"` opens `RotateIpFlow`.
- All writes are disabled when not writable.

- [ ] **Step 1: Write the failing tests**

- `saveQuotaSendsBytes`: 120, Monthly, day 1, `00:00` gives `limit_bytes == 120000000000`.
- `emptyCapMeansNoCap`
- `resetNeedsConfirm`
- `cpuModePosts`: `"Eco"` posts `mode == "eco"`.
- `thermalLimitsShowClamp`: `{"warn_c":44,"gate_c":48,"clamped":true}` shows `"Clamped to warn 44°, gate 48°"`.
- `benchSwitchPosts`
- `airplaneSwitchPosts`
- `batteryCapIsReadOnly`
- `writesDisabledWhenStale`

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.ui.more.DataScreenTest' --tests 'com.zflip5.modem.ui.more.SystemScreenTest'`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and confirm they pass**

Same command.

- [ ] **Step 5: Commit**

Subject: `feat(app): add Data and System screens`.

---

### Task 2.11: Tailscale screen with QR login (P2-B, 2b)

**Files:**
- Modify: `ui/more/TailscaleScreen.kt`
- Test: `…/ui/more/TailscaleScreenTest.kt`

**Interfaces:**
- Consumes: `tailscale()`, `tailscaleLogin()`, `tailscaleLogout()`, `QrCode`, `ConfirmOverlay` and `LocalSnackbar`.

**Behavior:**
- Poll `tailscale()` every 2 s while started.
- Rows:
  - `"State"`, `"IP"` and `"MagicDNS"`
  - `"Key expiry"`: the date or `"Never"`, plus `" (expired)"`
  - `"Web UI"`: the URL, plus `"serve missing"` when `!serveOk`
  - the `health` lines
- `not_running` shows `"Tailscale is not running"`. `error` shows its text.
- **`"Log in"`** (when not running):
  - Call `tailscaleLogin()`. With a `loginUrl`, open the full-screen overlay `"login_qr"`: `QrCode(url, Modifier.size(260.dp))`, `"Scan to sign in"`, and a `"Cancel"` pill inside the left 176dp.
  - When the state becomes `running`, close the overlay and show the snackbar `"Tailscale connected"`.
  - After 180 s, close with `"Sign-in timed out"`.
  - Errors:

    | Error | Message |
    |---|---|
    | 409 `needs_machine_auth` | `"Approve this device in the Tailscale admin console."` |
    | 409 `busy` | `"A sign-in is already in progress. Try again in a moment."` |
    | 504 `no_login_url` | `"Tailscale gave no sign-in link. Try again."` |
    | anything else | the daemon message |

- **`"Log out"`** (when running):
  - A destructive `ConfirmOverlay`: title `"Log out of Tailscale?"`, body `"The web UI stops being reachable over the tailnet until you sign in again."`, confirm `"Log out"`.
  - Then `tailscaleLogout()`.

- [ ] **Step 1: Write the failing tests (`composeRule.mainClock`)**

- `statusRows`
- `loginShowsQrThenClosesWhenRunning`: `login_url "https://login.tailscale.com/a/abc"`, then the GET switches to running, then `advanceTimeBy(2_100)`.
- `loginTimesOut`: 181_000 ms.
- `machineAuth409ShowsApprovalMessage`
- `logoutNeedsConfirm`
- `notRunningShowsHint`
- `qrOverlayClearsCutout`

- [ ] **Step 2: Run the test and confirm it fails**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.ui.more.TailscaleScreenTest'`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the test and confirm it passes**

Same command.

- [ ] **Step 5: Commit**

Subject: `feat(app): add Tailscale screen with QR sign-in`.

---

### Task 2.12: Advanced screen (P2-B, 2b)

**Files:**
- Modify: `ui/more/AdvancedScreen.kt`
- Test: `…/ui/more/AdvancedScreenTest.kt`

**Interfaces:**
- Consumes: `get("/v1/status")` (`open_reads`, `open_control`), `tailscale().webUrl`, `QrCode`, `ConfirmOverlay` and `OptimisticSwitch`.

**Controls:**
- `"Restart daemon"`: confirm `"Restart the daemon?"`, then post `/v1/service/restart`, then the snackbar `"Daemon restarting"`.
- `"Reboot phone"`: a destructive confirm (`"Reboot the phone?"`, body `"The hotspot drops for about a minute."`), then post `/v1/device/reboot`.
- `"Open reads"` (testTag `"open_reads_switch"`) posts `/v1/dashboard/open` `{"open"}`.
- `"Open control"` (testTag `"open_control_switch"`) posts `/v1/dashboard/control` `{"open"}`.
- `"Browser pairing"`:
  - Shows `QrCode(webUrl)` (testTag `"pair_qr"`) plus the URL text.
  - When `webUrl == null`: `"Tailscale serve is not set up, so there is no web address to pair."`.
  - When open reads is off, it adds: `"Open reads is off. A browser also needs the read-status token from config.json."`.

- [ ] **Step 1: Write the failing tests**

- `rebootNeedsConfirm`
- `restartNeedsConfirm`
- `openReadsSwitchPosts`: the post has `open == false`.
- `pairingQrUsesWebUrl`
- `openReadsOffShowsTokenNote`
- `everyTargetAtLeast48dp`

- [ ] **Step 2: Run the test and confirm it fails**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.ui.more.AdvancedScreenTest'`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the full suite**

Run: `./gradlew test`

Expected: `BUILD SUCCESSFUL`.

- [ ] **Step 5: Commit**

Subject: `feat(app): add Advanced screen`.

---

### Task 2.13: Home-screen widget with RemoteViews (P2-C, 2b)

**Files:**
- Create:
  - `widget/{FlippostWidget,WidgetRenderer,WidgetRefresh}.kt`
  - the stub `widget/RotateIpActivity.kt` (`class RotateIpActivity : ComponentActivity()`)
  - `res/layout/widget_flippost.xml`, `res/xml/flippost_widget_info.xml`
  - `res/drawable/{widget_bg,widget_chip,widget_pill,widget_bar,widget_avatar_clip}.xml`
- Modify: `app/src/main/AndroidManifest.xml`
- Test: `widget/{WidgetModelTest,FlippostWidgetTest}.kt`

**Interfaces:**
- Consumes: `summary()`, `tether(start)`, `Format.*`, `TempLevel` and `R.drawable.flippost_logo`.
- Produces:

```kotlin
data class WidgetModel(val down: Boolean, val active: Boolean, val devices: String, val wanIp: String, val temp: String, val tempLevel: TempLevel, val data: String, val dataPct: Int)
object WidgetRenderer { fun model(s: Summary?, down: Boolean): WidgetModel; fun render(ctx: Context, s: Summary?, down: Boolean): RemoteViews }
class FlippostWidget : AppWidgetProvider() { companion object { const val ACTION_TOGGLE = "com.zflip5.modem.widget.TOGGLE"; const val ACTION_REFRESH = "com.zflip5.modem.widget.REFRESH"
  internal var client: () -> DaemonClient = { DaemonClient() }; internal var dispatcher: CoroutineDispatcher = Dispatchers.IO } }
object WidgetRefresh { fun schedule(ctx: Context); fun cancel(ctx: Context); fun refreshAll(ctx: Context) }
```

**`model` rules:**
- `down` or no summary gives `down = true`, `active = false` and empty strings.
- Otherwise:
  - `devices = if (active) Format.devices(clients) else "Off"`
  - `wanIp ?: "No WAN"`
  - `temp = Format.temp(c ?: rawC)`
  - `data = Format.usage(usage)`
  - `dataPct = round(100*period/limit)` clamped to 0..100, or 0 with no cap

**`render`:**
- When down: `widget_status` is VISIBLE with `"service not running"`, and `widget_values` is GONE.
- The temperature colour is OK `0xFF9EE6C3`, WARN `0xFFFFD49A` or HOT `0xFFFF9AA6`.
- `setProgressBar(R.id.widget_bar, 100, dataPct, false)`.
- `widget_switch`:
  - `setCompoundButtonChecked(active)`
  - `setOnCheckedChangeResponse(RemoteResponse.fromPendingIntent(pi))`
  - `pi = PendingIntent.getBroadcast(ctx, 1, Intent(ACTION_TOGGLE).setClass(…), FLAG_UPDATE_CURRENT or FLAG_MUTABLE)`. It must be mutable so the system can fill in `EXTRA_CHECKED`, and it stays explicit.
- `widget_rotate` opens `RotateIpActivity` (immutable, request code 2).
- `widget_root` opens `CoverKioskActivity` (immutable).

**Receiver:**
- `onUpdate` and REFRESH: `goAsync()`, then `CoroutineScope(dispatcher).launch`. Run `client().summary()`; any exception means `down = true`. Then `updateAppWidget`, and `finish()` in a `finally`.
- TOGGLE: `goAsync()`, then `client().tether(intent.getBooleanExtra(RemoteViews.EXTRA_CHECKED, false))`. Ignore failures, run the refresh path, and `finish()` in a `finally`.
- `onEnabled` and `onUpdate` call `schedule`. `onDisabled` calls `cancel`.

**`WidgetRefresh`:**
- `schedule`: `setInexactRepeating(ELAPSED_REALTIME, now+INTERVAL_FIFTEEN_MINUTES, INTERVAL_FIFTEEN_MINUTES, refreshPi)`, where `refreshPi` is an immutable REFRESH broadcast.
- `refreshAll`: an explicit REFRESH broadcast.

**Layout:**
- Root `widget_root`: background `widget_bg` (`#222D43`→`#141B29`, 24dp corners), padding 12dp.
- Row 1: a 34dp avatar with `clipToOutline` on an oval, `"Hotspot"` plus `widget_devices`, and the `Switch` `widget_switch`.
- Chips `widget_ip` and `widget_temp`.
- `"DATA"` plus `widget_data`.
- `ProgressBar` `widget_bar`: horizontal, 9dp, solid teal on track. Stripes are skipped, because RemoteViews has no tiled shader.
- `Button` `widget_rotate`: `"Rotate IP"`, at least 48dp.
- `widget_status`: GONE by default.
- Nunito on every TextView.

`flippost_widget_info.xml`: `minWidth="250dp"`, `minHeight="110dp"`, `targetCellWidth="4"`, `targetCellHeight="2"`, `updatePeriodMillis="1800000"`, `initialLayout` and `previewLayout` `@layout/widget_flippost`, `resizeMode="horizontal|vertical"`, `widgetCategory="home_screen"`.

**Manifest:**
- `<receiver android:name=".widget.FlippostWidget" android:exported="true">` with the filters `APPWIDGET_UPDATE`, `TOGGLE` and `REFRESH`, and meta-data `android.appwidget.provider` → `@xml/flippost_widget_info`.
- `<activity android:name=".widget.RotateIpActivity" android:exported="false" android:excludeFromRecents="true" android:theme="@android:style/Theme.Translucent.NoTitleBar"/>`.

- [ ] **Step 1: Write the failing tests**

- `WidgetModelTest` (Robolectric):
  - `downShowsServiceNotRunning`: inflate with `rv.apply(ctx, FrameLayout(ctx))`.
  - `fixtureValues`: `"1 device"`, `"100.64.12.34"`, `"41.2°"`, OK, `"12 GB / 100 GB"`, 12 and active.
  - `noCapShowsUsedOnly`: `"12 GB"` and 0.
  - `hotspotOffShowsOff`.
  - `nullWanIp`.
- `FlippostWidgetTest`, with `client = { fake }` and `dispatcher = Dispatchers.Unconfined`:
  - `toggleCallsTetherWithCheckedExtra`: a stop post, then a summary fetch.
  - `toggleWhenDaemonDownStillFinishes`.
  - `scheduleRepeatsEvery15Minutes`.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.widget.*'`

Expected: `Unresolved reference 'WidgetRenderer'`.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and the build**

Run: `./gradlew test assembleRelease`

- [ ] **Step 5: Commit**

Subject: `feat(widget): add hotspot widget with toggle, IP, temperature, and data`.

---

### Task 2.14: Rotate IP from the widget (P2-C, 2b)

**Files:**
- Modify: `widget/RotateIpActivity.kt`
- Test: `widget/RotateIpActivityTest.kt`

**Interfaces:**
- Consumes: `RotateIpFlow`, `WidgetRefresh.refreshAll`, `FlippostTheme` and `LocalSnackbar`.

`onCreate` sets the content: `FlippostTheme`, a `LocalSnackbar` provider, then `RotateIpFlow(DaemonClient()) { WidgetRefresh.refreshAll(this); finish() }`.

- [ ] **Step 1: Write the failing tests**

- `notExportedAndExcludedFromRecents`: `getActivityInfo` shows `exported == false` and `FLAG_EXCLUDE_FROM_RECENTS`.
- `launchShowsConfirm`: `ActivityScenario.launch` with `createEmptyComposeRule()` shows `"Drops connectivity for about 10 to 30 seconds."`.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `./gradlew testDebugUnitTest --tests 'com.zflip5.modem.widget.RotateIpActivityTest'`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the full suite and the APK build**

Run: `./gradlew test && bash /Users/adacchi/module-zf5/helper/build-apk.sh`

Expected: `BUILD SUCCESSFUL`, `built … versionCode=2`, `APK_SIGNED_OK`.

- [ ] **Step 5: Commit**

Subject: `feat(widget): confirm and run Rotate IP from the widget`.

---

### Task 2.15: Device verification (2c; needs the Phase 1 daemon deployed)

Device serial: `R5CW80J9SVF`. Root commands go only through stdin.

- [ ] **Step 1: Run the host checks**

Run: `bash /Users/adacchi/module-zf5/tools/run-tests.sh --offline | grep -E 'gradle|app-install|OFFLINE_TESTS_OK'`

Expected: `PASS gradle test cover-app` and `PASS app-install.test.sh`.

- [ ] **Step 2: Read the installed version (read-only)**

Run: `adb -s R5CW80J9SVF shell pm list packages --show-versioncode com.zflip5.modem`

Expected: `versionCode:1`.

- [ ] **Step 3: OWNER APPROVAL (installs). Install through the module path**

1. Run `bash tools/package-module.sh --src magisk --out dist/zflip5-modem-module.zip && adb -s R5CW80J9SVF push dist/zflip5-modem-module.zip /data/local/tmp/`.
2. Run `printf 'ksud module install /data/local/tmp/zflip5-modem-module.zip\n' | adb -s R5CW80J9SVF shell su`.
3. **OWNER APPROVAL: reboot.** The owner reboots the phone.
4. Run `printf 'grep APP_INSTALL /data/adb/zflip5-modem/service.log\n' | adb -s R5CW80J9SVF shell su`. Expected: `APP_INSTALL=ok have=1 want=2`.
5. Check `pm list packages`. Expected: `versionCode:2`.

- [ ] **Step 4: Check the cold start**

Run: `adb -s R5CW80J9SVF shell am force-stop com.zflip5.modem && adb -s R5CW80J9SVF shell am start -W -n com.zflip5.modem/.CoverKioskActivity | grep TotalTime`

Expected: under 700.

Then run `adb -s R5CW80J9SVF exec-out screencap -p > /tmp/zf5-home.png`, stripping any bytes before `\x89PNG` if the file does not open. Compare it by eye with `.superpowers/brainstorm/4315-1792640803/content/app-home-c-flippost-v2.html`. Expected: option C, no scroll, and the pills clear of the cutout.

- [ ] **Step 5: Check app-UID auth and live reads**

- The home shows `"Live"` and updates every second.
- The Tailscale screen shows `running` and `100.110.36.27`.
- Advanced shows the tailnet pairing QR.
- Toggle **Open control** off, then on. Neither write shows an `app only` Snackbar.

- [ ] **Step 6: OWNER APPROVAL (bounces the hotspot). Check the widget toggle**

1. The owner adds the widget.
2. Tap it off, then on. Expected: it updates within about 10 s. `printf 'ip link show swlan0\n' | adb -s R5CW80J9SVF shell su` shows `state UP`.
3. On the client, `system_profiler SPAirPortDataType | grep -E 'PHY Mode|Channel|Transmit Rate'` shows `802.11ax`, `149 (5GHz, 80MHz)` and about `1200`. If not, stop and report.

- [ ] **Step 7: OWNER APPROVAL (drops connectivity). Rotate IP from the widget**

1. Expected: `"Rotating IP…"`, then `"New IP <x>"`, where `x` differs from before.
2. The widget IP chip shows `x`.
3. Repeat the PHY check from Step 6.

- [ ] **Step 8: OWNER APPROVAL (restarts the daemon). Check daemon-down handling**

1. Advanced, **Restart daemon**. Within 5 s, the home greys out with `"Updated N s ago"` and the switch is disabled. Within about 15 s, it shows `"Live"` again.
2. The widget shows `"service not running"` in that window, then recovers.

- [ ] **Step 9: Record the results**

Record TotalTime, APK size, the PHY lines and the new IP in the phase report. Nothing to commit.

---

## Phase 3: web UI and remaining removals

### Task 3.1: Serve the web app at `/` and remove the old pages, accent and token leak (P3-A, 3a)

**Files:**
- Create: `daemon/web.go`, `daemon/web/index.html` (shell; 3.2 fills it), `daemon/web_test.go`
- Modify `daemon/server.go`:
  - delete routes `/v1/battery` (:258), `/v1/signal` (:265), `/dashboard` (:314) and `/v1/cover/accent` (:351-354)
  - `/` (:313) now goes to `s.handleRoot`
  - delete `handleBattery` (:532-534), `handleSignal` (:645-647) and `handleCoverAccent` (:1082-1108)
  - delete the field `coverAccent` (:64-67), its store (:124) and `"cover_accent"` (:519)
- Modify `daemon/dashboard.go`:
  - delete `handleDashboard` (:72-81), `htmlWithTokens` and `isHexToken` (:82-122), and `dashboardHTML` (:236-2352)
  - rewrite `handleQR` (:124-142)
  - `webManifest` (:210-223) gets `"start_url": "/"` and `"description": "Hotspot and modem controls for the Z Flip 5"`
- Modify `daemon/config.go`: delete the `Cover` struct, the `Config.Cover` field and `persistCoverAccent` (:143-165).
- Delete `daemon/coverrestore.go` and its call in `daemon/main.go`. `magisk/cleanup.sh` (3.5) takes over the restore.
- Delete: `daemon/cover.go`, `daemon/cover_test.go`, `daemon/dashboard_test.go`, `tests/lunar.test.sh`
- Modify `schemas/config.schema.json`: drop the `cover` property (:70-78).

**Interfaces:**
- Consumes: `s.ts.Status(ctx).WebURL`, `s.openReads` and `writeErr`.
- Produces:
  - `//go:embed web/index.html` → `var webIndexHTML []byte`
  - `func (s *Server) handleRoot(w, r)`: a path other than `/` gets a 404 through `writeErr`. Otherwise it serves the page with `text/html; charset=utf-8` and `Cache-Control: no-store, must-revalidate`, with no token substitution.
  - `func pairURL(r *http.Request, webURL *string, token string) string`:
    - The scheme is `https` when `X-Forwarded-Proto` is `https` or the host contains `.ts.net`; otherwise `http`.
    - For a loopback host (`127.0.0.1`, `localhost` or `[::1]`, with or without a port) with `webURL != nil`, it returns `*webURL`. Otherwise `scheme://host/`.
    - It appends `?token=` only when the token is non-empty.
  - `func (s *Server) pairTarget(r *http.Request) string`: the token is `""` when `openReads` is on, else `cfg.Tokens["read-status"]`. It calls `s.ts.Status` (3 s) only for a loopback host. `handleQR` encodes the result.

- [ ] **Step 1: Write the failing tests in `daemon/web_test.go`**

```go
func TestRootServesWebApp(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	w := do(s, "GET", "/", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	for _, want := range []string{`<link rel="manifest" href="/manifest.webmanifest">`, `<link rel="apple-touch-icon" href="/logo.png">`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
}
func TestRootNeverEmbedsTokens(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.openReads.Store(true)
	s.openControl.Store(true)
	b := do(s, "GET", "/", "").Body.String()
	for _, bad := range []string{strings.Repeat("a", 64), strings.Repeat("c", 64), "EMBEDDED_TOKENS"} {
		if strings.Contains(b, bad) {
			t.Errorf("page leaks %q", bad[:8])
		}
	}
}
func TestRootIsOnlyRoot(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if w := do(s, "GET", "/index.html", ""); w.Code != 404 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}
func TestRemovedPagesAndRoutes404(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	rs, rc := strings.Repeat("a", 64), strings.Repeat("c", 64)
	for _, c := range []struct{ m, p, tok string }{{"GET", "/dashboard", ""}, {"GET", "/v1/signal", rs}, {"GET", "/v1/battery", rs}, {"POST", "/v1/cover/accent", rc}} {
		if w := do(s, c.m, c.p, c.tok); w.Code != 404 {
			t.Errorf("%s %s = %d, want 404", c.m, c.p, w.Code)
		}
	}
}
func TestStatusDropsCoverAccent(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if b := do(s, "GET", "/v1/status", strings.Repeat("a", 64)).Body.String(); strings.Contains(b, "cover_accent") {
		t.Fatal(b)
	}
}
func TestManifestStartsAtRoot(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if b := do(s, "GET", "/manifest.webmanifest", "").Body.String(); !strings.Contains(b, `"start_url": "/"`) || strings.Contains(b, "/dashboard") {
		t.Fatal(b)
	}
}
func TestPairURL(t *testing.T) {
	web, tok := "https://zflip5.tail1.ts.net/", strings.Repeat("a", 64)
	for _, c := range []struct {
		host, xfp string
		web       *string
		tok, want string
	}{
		{"zflip5.tail1.ts.net", "", nil, "", "https://zflip5.tail1.ts.net/"},
		{"zflip5.tail1.ts.net", "", nil, tok, "https://zflip5.tail1.ts.net/?token=" + tok},
		{"127.0.0.1:18080", "", &web, "", web},
		{"127.0.0.1:18080", "", &web, tok, web + "?token=" + tok},
		{"127.0.0.1:18080", "", nil, "", "http://127.0.0.1:18080/"},
		{"phone.lan", "https", nil, "", "https://phone.lan/"},
	} {
		r := httptest.NewRequest("GET", "/v1/qr", nil)
		r.Host = c.host
		if c.xfp != "" {
			r.Header.Set("X-Forwarded-Proto", c.xfp)
		}
		if got := pairURL(r, c.web, c.tok); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}
func TestPairTargetTokenRule(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	r := httptest.NewRequest("GET", "/v1/qr", nil)
	r.Host = "zflip5.tail1.ts.net"
	s.openReads.Store(true)
	if got := s.pairTarget(r); got != "https://zflip5.tail1.ts.net/" {
		t.Errorf("open reads: %q", got)
	}
	s.openReads.Store(false)
	if got := s.pairTarget(r); got != "https://zflip5.tail1.ts.net/?token="+strings.Repeat("a", 64) {
		t.Errorf("closed reads: %q", got)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run 'TestRoot|TestRemovedPages|TestStatusDropsCoverAccent|TestManifestStartsAtRoot|TestPair' ./...`

Expected: `undefined: pairURL`.

- [ ] **Step 3: Implement the routing and removals**

- Keep `deviceSignal` and `parseSignal`.
- The shell page has `<!doctype html>`, a charset meta, a viewport meta, `<title>Flippost</title>`, the two `<link>` lines and `<main id="app"></main>`.
- Run `go mod tidy` only if an import becomes unused. `go-qrcode` stays.

- [ ] **Step 4: Run the daemon suite**

Run: `cd /Users/adacchi/module-zf5/daemon && go vet ./... && go test -race ./...`

Expected: `ok`. `TestUnknownPath404` still passes.

- [ ] **Step 5: Commit**

`git add -A daemon schemas/config.schema.json tests/lunar.test.sh`. Subject: `feat(web): serve the web app at / and drop the cover page and /dashboard`.

---

### Task 3.2: Build the Flippost web app page (P3-A, 3a, after 3.1)

**Files:**
- Modify: `daemon/web/index.html` (one inline `<style>` and one inline `<script>`, no external scripts) and `daemon/web_test.go`
- Create: `tests/web.test.sh`

**Interfaces:**
- Consumes:
  - `GET /v1/summary` and `GET /v1/tailscale` (read only)
  - `POST /v1/hotspot/dual` and `POST /v1/tether?action=…`
  - the GETs `/v1/hotspot`, `/v1/presets`, `/v1/usbtether`, `/v1/clients`, `/v1/usage`, `/v1/thermal`, `/v1/thermal/bench`, `/v1/cpu` and `/v1/qr`
  - the POSTs `/v1/hotspot/{whitelist,scan,override}`, `/v1/presets{,/apply,/delete,/auto}`, `/v1/usbtether/toggle`, `/v1/usage/{quota,reset}`, `/v1/thermal/{limits,bench}`, `/v1/cpu/mode`, `/v1/cooldown`, `/v1/airplane`, `/v1/service/restart` and `/v1/device/reboot`
- Produces pure JS between `/*PURE-START*/` and `/*PURE-END*/`:
  - `fmtBytes(n)`: mirrors `humanBytes` (`usage.go:561`). Under 1000 it returns `"N B"`; otherwise base 1000 with 2 decimals and units `KB`, `MB`, `GB`, `TB`.
  - `fmtRate(bytesPerSec)`: `bits = b*8`. Under 1e6 it returns `Math.round(bits/1e3)+" kbps"`. Under 1e8 it returns `(bits/1e6).toFixed(1)+" Mbps"`. Otherwise `Math.round(bits/1e6)+" Mbps"`.
  - `hotspotLine(h)`: parts joined with `" · "`:
    - `"Wi-Fi 6"` or `"Wi-Fi 6 unavailable"`; omitted when `null`
    - the band label; omitted when `null`
    - `"1 device"` or `"N devices"`
  - `deviceRows(speed)`: `[]` unless `per_client` is an array. Otherwise one `{label: ipv4 ?? mac, down, up}` per client, plus `{label:"This phone (approximate)", …}` when the phone rate is not `null`.
  - `isStale(lastOkMs, nowMs, slowAgeS = 0)`: `nowMs - lastOkMs > 5000 || slowAgeS > 30` (Review Focus 2).
  - `dualBlockText(block)`: the two exact strings from Task 2.9 for `"whitelist"` and `"auto_switch"`, else `""`.
  - the literal `const POLL_MS = 2000;`
  - sections `id="screen-{home,devices,hotspot,data,system,tailscale,alerts,advanced}"`, routed by `location.hash` (`#/devices` and so on)

**Page behavior:**
- **Look:**
  - `DESIGN.md` tokens (`--bg #1b2436` and the rest of its front matter)
  - a drizzle sky, glass cards, gradient pills and a striped bar
  - `font-family: Nunito, ui-rounded, system-ui, sans-serif`, with no download
  - an inline SVG four-bar signal icon with unreached bars dimmed, never `▂▄▆`
  - works at 320 px with a 16 px gutter
- **Home:**
  - signal bars plus `display` and `operator`; `null` shows "No signal data"
  - data used against the cap with the striped bar, "Today X" and "Y left" (left is hidden when `null`)
  - WAN down and up through `fmtRate`
  - `deviceRows`, or "Per-device speed unavailable"
  - the hotspot card: SSID, `hotspotLine`, and a switch that posts `/v1/tether`
  - a **Details** button
- **Polling:** `/v1/summary` every `POLL_MS`, paused while `document.hidden`. When `isStale(lastOk, now, summary.slow_age_s)` is true, values grey out, "Updated N s ago" appears, and writes are disabled.
- **Tokens:**
  - On load, move `?token=` into `localStorage` key `zf5tok`, then remove it with `history.replaceState`.
  - Reads send `Authorization: Bearer <zf5tok>` when the key exists. Writes send `zf5rtok`.
  - A 401 on a write opens a "Control token" prompt that saves `zf5rtok`.
  - Wrap every `localStorage` access in `try/catch`.
- **Errors:** a failed write rolls back and shows the daemon's `error` in a toast.
- **Details screens:**
  - **Devices:** `/v1/clients` merged with `deviceRows`.
  - **Hotspot:** presets (list, apply, delete, upsert), auto-switch, allowlist, timed force-on (0-24 h), scan and USB tethering. The **Dual band** switch is disabled with `dualBlockText(...)`. Its dialog has "Apply now (clients drop about 10 s)", "At next start" and **Cancel**.
  - **Data:** quota, period, reset time and day, and **Reset** with a confirm.
  - **System:** bench, thermal limits, CPU mode, cooldown, airplane, **Rotate IP** (a confirm, then `/v1/airplane {"mode":"cycle"}` with a 60 s timeout, showing `old_ip` → `new_ip`), battery cap as read-only "Set on the phone", and `temperature.state`.
  - **Tailscale:** read-only rows (state, IP, MagicDNS, key expiry, `serve_ok`) and the text "Sign in and sign out from the Flippost app on the phone."
  - **Alerts:** "Alerts are set on the phone."
  - **Advanced:** restart (confirm), reboot (destructive confirm), and the pairing QR from `/v1/qr` with the read header, shown through `URL.createObjectURL`. No open-reads or open-control switches.
- **Service worker:** on load, `navigator.serviceWorker.getRegistrations()` unregisters each one. The page never calls `serviceWorker.register(`.

- [ ] **Step 1: Write the failing Go tests (append to `daemon/web_test.go`)**

```go
var webRouteRe = regexp.MustCompile(`/v1/[a-z0-9/]+[a-z0-9]`)
func webRoutes() []string {
	m := map[string]bool{}
	for _, p := range webRouteRe.FindAllString(string(webIndexHTML), -1) {
		m[p] = true
	}
	var out []string
	for p := range m {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
func TestWebUIRoutesExist(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	rs := webRoutes()
	if len(rs) < 20 {
		t.Fatalf("only %d routes: %v", len(rs), rs)
	}
	for _, p := range rs {
		if w := do(s, "PATCH", p, ""); w.Code == 404 {
			t.Errorf("page calls unregistered %s", p)
		}
	}
}
func TestWebUIForbiddenRoutes(t *testing.T) {
	for _, bad := range []string{"/v1/tailscale/login", "/v1/tailscale/logout", "/v1/dashboard/open", "/v1/dashboard/control", "/v1/signal", "/v1/battery", `"/dashboard"`, "serviceWorker.register("} {
		if strings.Contains(string(webIndexHTML), bad) {
			t.Errorf("page contains %s", bad)
		}
	}
}
func TestWebUIPollsSummary(t *testing.T) {
	for _, want := range []string{"const POLL_MS = 2000;", "/v1/summary", "getRegistrations", "/*PURE-START*/", "/*PURE-END*/", "slow_age_s"} {
		if !strings.Contains(string(webIndexHTML), want) {
			t.Errorf("missing %s", want)
		}
	}
}
func TestWebUIScreens(t *testing.T) {
	for _, n := range []string{"home", "devices", "hotspot", "data", "system", "tailscale", "alerts", "advanced"} {
		if !strings.Contains(string(webIndexHTML), `id="screen-`+n+`"`) {
			t.Errorf("missing screen %s", n)
		}
	}
}
```

- [ ] **Step 2: Write the failing `tests/web.test.sh`**

- Without `node`, it prints `SKIP - node not installed` and exits 0.
- `node --check` must pass on the extracted `<script>`.
- Extract the PURE block and append `module.exports={fmtBytes,fmtRate,hotspotLine,deviceRows,isStale,dualBlockText};`.
- Assert with `node:assert/strict`:

```js
eq(fmtBytes(999), "999 B"); eq(fmtBytes(1500), "1.50 KB"); eq(fmtBytes(1234567890), "1.23 GB"); eq(fmtBytes(87654321099), "87.65 GB");
eq(fmtRate(0), "0 kbps"); eq(fmtRate(12000), "96 kbps"); eq(fmtRate(2500000), "20.0 Mbps"); eq(fmtRate(25000000), "200 Mbps");
eq(hotspotLine({wifi6:true, band:"5", clients:1}), "Wi-Fi 6 · 5 GHz · 1 device");
eq(hotspotLine({wifi6:false, band:"dual", clients:3}), "Wi-Fi 6 unavailable · 2.4 + 5 GHz · 3 devices");
eq(hotspotLine({wifi6:null, band:null, clients:0}), "0 devices");
deq(deviceRows({per_client:"unavailable", phone_down_bytes_s:null, phone_up_bytes_s:null}), []);
deq(deviceRows({per_client:[{mac:"aa:bb:cc:dd:ee:ff", ipv4:null, down_bytes_s:5, up_bytes_s:6, signal_dbm:null}], phone_down_bytes_s:1, phone_up_bytes_s:2}),
    [{label:"aa:bb:cc:dd:ee:ff", down:5, up:6}, {label:"This phone (approximate)", down:1, up:2}]);
eq(isStale(0, 5000), false); eq(isStale(0, 5001), true); eq(isStale(0, 1000, 31), true); eq(isStale(0, 1000, 30), false);
eq(dualBlockText("whitelist"), "Clear the allowlist first: dual band stops Wi-Fi scanning.");
eq(dualBlockText("auto_switch"), "Turn off preset auto-switch first: dual band stops Wi-Fi scanning.");
eq(dualBlockText(null), "");
```

On success it prints `all web tests passed`.

- [ ] **Step 3: Run both and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run TestWebUI ./... ; bash ../tests/web.test.sh`

Expected: "only 0 routes", and a nonzero exit from `web.test.sh`.

- [ ] **Step 4: Write `daemon/web/index.html`**

Invoke `google-writing` first. The strings fixed in this task are used as written.

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -race ./... && bash ../tests/web.test.sh`

Expected: `ok` and `all web tests passed`.

- [ ] **Step 6: Check the page in a browser against a local daemon**

```bash
cd /Users/adacchi/module-zf5/daemon && go build -o /tmp/zf5d . && python3 -c "import json;print(json.dumps({'bind_host':'127.0.0.1','bind_port':18081,'tokens':{'read-status':'a'*64,'radio-control':'c'*64},'thermal':{'warn_c':44,'gate_c':46,'fail_closed':True},'dashboard':{'open_reads':True,'open_control':True},'rate_limits':{'default_per_min':600,'radio_per_min':60}}))" > /tmp/zf5cfg.json && /tmp/zf5d --config /tmp/zf5cfg.json
```

Open `http://127.0.0.1:18081/` at 360 px and 1280 px widths. Expected:
- no horizontal scroll
- "No signal data" or "Per-device speed unavailable" off-device
- every Details screen opens
- no console errors

Stop the daemon afterwards.

- [ ] **Step 7: Commit**

`git add daemon/web/index.html daemon/web_test.go tests/web.test.sh`. Subject: `feat(web): add the Flippost web app with main view and details screens`.

---

### Task 3.3: Finalize the OpenAPI document and pin it to the route table (P3-A, 3a, after 3.2)

**Files:**
- Modify: `schemas/api.openapi.json`
- Create: `daemon/openapi_test.go`

**Interfaces:**
- Consumes: `webRoutes()` (3.2), `postJSON`, `do`, `testCfg`, and the `guardApp` 403 body.
- Produces: `liveRoutes`, the final route list cited by 3.8.

**Content:**
- `info.description`: "Loopback-bound control API for a rooted Z Flip 5 used as a hotspot and modem. Remote browsers reach it only through `tailscale serve`. Writes that start radios are refused in an unsafe thermal state. The phone's own app is trusted by socket UID; Tailscale login and logout are app_only."
- `bearerAuth.description`:
  - scopes `read-status` and `radio-control`
  - tokens are 256-bit or longer, stored 0600 under `/data/adb`, and independently revocable
  - a caller from the `com.zflip5.modem` UID needs no token
- Paths:
  - exactly `liveRoutes`
  - `/v1/hotspot/band` is marked `"deprecated": true`
  - the four app-only operations carry `"x-app-only": true` and `"security": []`
  - every 409 documents `reason`
- Components:
  - `Error` gains an optional `reason`
  - add `Summary`, `SpeedSnap`, `ClientSpeed`, `HotspotStatus`, `TailscaleStatus`, `TailscaleBrief`, `DualRequest` and `DualResult`, with contract field names
  - delete `ThermalHistory*` if present

- [ ] **Step 1: Write the failing test**

```go
var liveRoutes = []string{"/v1/airplane", "/v1/clients", "/v1/cooldown", "/v1/cpu", "/v1/cpu/mode", "/v1/dashboard/control", "/v1/dashboard/open",
	"/v1/device/reboot", "/v1/hotspot", "/v1/hotspot/band", "/v1/hotspot/dual", "/v1/hotspot/override", "/v1/hotspot/scan", "/v1/hotspot/whitelist",
	"/v1/presets", "/v1/presets/apply", "/v1/presets/auto", "/v1/presets/delete", "/v1/qr", "/v1/service/restart", "/v1/status", "/v1/summary",
	"/v1/tailscale", "/v1/tailscale/login", "/v1/tailscale/logout", "/v1/tether", "/v1/thermal", "/v1/thermal/bench", "/v1/thermal/limits",
	"/v1/usage", "/v1/usage/quota", "/v1/usage/reset", "/v1/usbtether", "/v1/usbtether/toggle"}
var removedRoutes = []string{"/v1/health", "/v1/network", "/v1/thermal/history", "/v1/bands", "/v1/prefer5g", "/v1/speedtest", "/v1/sms/recent",
	"/v1/notifications/recent", "/v1/cover/home", "/v1/signal", "/v1/battery", "/v1/cover/accent", "/dashboard"}
var appOnly = []string{"/v1/tailscale/login", "/v1/tailscale/logout", "/v1/dashboard/open", "/v1/dashboard/control"}

func loadSpec(t *testing.T) map[string]any {
	b, err := os.ReadFile("../schemas/api.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func TestOpenAPIPathsMatchRoutes(t *testing.T) {
	paths := loadSpec(t)["paths"].(map[string]any)
	var got []string
	for p := range paths {
		got = append(got, p)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, liveRoutes) {
		t.Fatalf("spec paths\n got %v\nwant %v", got, liveRoutes)
	}
	s := NewServer(testCfg(), fakeCollector{safe: true})
	for _, p := range liveRoutes {
		if w := do(s, "PATCH", p, ""); w.Code == 404 {
			t.Errorf("%s not registered", p)
		}
	}
	for _, p := range removedRoutes {
		if w := do(s, "GET", p, strings.Repeat("a", 64)); w.Code != 404 {
			t.Errorf("%s = %d, want 404", p, w.Code)
		}
	}
}
func TestOpenAPIAppOnlyOps(t *testing.T) {
	paths := loadSpec(t)["paths"].(map[string]any)
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.openControl.Store(true)
	for _, p := range appOnly {
		if paths[p].(map[string]any)["post"].(map[string]any)["x-app-only"] != true {
			t.Errorf("%s lacks x-app-only", p)
		}
		if w := postJSON(s, p, strings.Repeat("c", 64), `{"open":true}`); w.Code != 403 || !strings.Contains(w.Body.String(), `"reason":"app_only"`) {
			t.Errorf("%s token caller: %d %s", p, w.Code, w.Body)
		}
	}
}
func TestOpenAPIDocumentsWebRoutes(t *testing.T) {
	paths := loadSpec(t)["paths"].(map[string]any)
	for _, p := range webRoutes() {
		if _, ok := paths[p]; !ok {
			t.Errorf("web page calls undocumented %s", p)
		}
	}
}
func TestOpenAPINoRemovedScopes(t *testing.T) {
	b, _ := os.ReadFile("../schemas/api.openapi.json")
	for _, bad := range []string{`"sms"`, "SMS", "prefer5g", "speedtest", "cover_accent"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("spec mentions %s", bad)
		}
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -run TestOpenAPI ./...`

Expected: missing paths such as `/v1/cpu/mode`.

- [ ] **Step 3: Edit `schemas/api.openapi.json`**

- [ ] **Step 4: Run the checks**

Run: `cd /Users/adacchi/module-zf5/daemon && go test -race ./... && python3 -m json.tool ../schemas/api.openapi.json >/dev/null && echo JSON_OK`

Expected: `ok` and `JSON_OK`.

- [ ] **Step 5: Commit**

`git add schemas/api.openapi.json daemon/openapi_test.go`. Subject: `docs(api): document every live route and pin the spec to the router`.

---

### Task 3.4: Move the firewall into a module-owned boot step (P3-B, 3a; item 59 part 1)

**Files:**
- Create: `magisk/firewall.sh`, `tests/boot.test.sh`
- Modify `magisk/service.sh`: the first statement inside the `{ … } &` block (:18) is `sh "$MODDIR/firewall.sh"`.
- Modify `tools/dex.sh:46-50`: delete the ws-scrcpy `scrcpy-server.jar` restore and its `ponytail:` comment.

**Interfaces:**
- Produces `magisk/firewall.sh`. It is POSIX `sh` with no `su` and no `MODDIR`, so it can be copied into `service.d`.
  - `sh firewall.sh once` applies the rules once.
  - With no argument, it runs `( while :; do apply; sleep 60; done ) &` and exits.
- Rules, for each `T` in `iptables ip6tables`:

```sh
$T -w -N ZF5_FW 2>/dev/null
$T -w -F ZF5_FW
$T -w -A ZF5_FW -i lo -j RETURN
$T -w -A ZF5_FW -i tailscale0 -j RETURN
$T -w -A ZF5_FW -p tcp --dport 5555 -j DROP
$T -w -A ZF5_FW -p tcp --dport 8000:8088 -j DROP
$T -w -A ZF5_FW -p tcp --dport 8886 -j DROP
$T -w -C INPUT -j ZF5_FW 2>/dev/null || $T -w -I INPUT 1 -j ZF5_FW
```

- [ ] **Step 1: Read the live script to confirm the ports (read-only)**

Run: `printf '%s\n' 'cat /data/adb/service.d/50-zflip-remote.sh' | adb shell su`

Expected: ports within `5555 8000-8088 8886`, the `lo` and `tailscale0` interfaces, and both `iptables` and `ip6tables`. If the script lists anything else, add it to the script and to the test.

- [ ] **Step 2: Write the failing `tests/boot.test.sh` (firewall part)**

- Fake `iptables` and `ip6tables` append `"$(basename $0) $*"` to `$TMP/calls`. They exit 1 on `-C` unless `$TMP/has_jump` exists.
- Run `PATH="$TMP/bin:$PATH" sh magisk/firewall.sh once`.
- For both tools, assert these lines:
  - `-w -F ZF5_FW`
  - the five `-A` lines
  - `-w -I INPUT 1 -j ZF5_FW`
- Touch `has_jump`, rerun, and assert no `-I INPUT`.
- Assert `grep -q firewall.sh magisk/service.sh` and `! grep -q scrcpy-server.jar tools/dex.sh`.
- End with `all boot tests passed`.

- [ ] **Step 3: Run the test and confirm it fails**

Run: `bash /Users/adacchi/module-zf5/tests/boot.test.sh`

Expected: `magisk/firewall.sh` not found.

- [ ] **Step 4: Implement**

- [ ] **Step 5: Run the tests and the lint**

Run: `cd /Users/adacchi/module-zf5 && bash tests/boot.test.sh && bash tests/magisk.test.sh && sh -n magisk/firewall.sh && echo SYNTAX_OK`

- [ ] **Step 6: Apply the script once on the device (no bounce; tell the owner first)**

```bash
adb push /Users/adacchi/module-zf5/magisk/firewall.sh /data/local/tmp/zf5-firewall.sh
printf '%s\n' 'sh /data/local/tmp/zf5-firewall.sh once' 'iptables -w -S ZF5_FW' 'ip6tables -w -S ZF5_FW' 'iptables -w -C INPUT -j ZF5_FW && echo JUMP4_OK' 'ip6tables -w -C INPUT -j ZF5_FW && echo JUMP6_OK' 'rm /data/local/tmp/zf5-firewall.sh' | adb shell su
```

Expected: 5 rules in each chain, `JUMP4_OK` and `JUMP6_OK`, and USB adb still works.

- [ ] **Step 7: Commit**

`git add magisk/firewall.sh magisk/service.sh tests/boot.test.sh tools/dex.sh`. Subject: `feat(magisk): own the adb and remote-port firewall in a module boot step`.

---

### Task 3.5: Restore stock settings and remove the cover watcher, overlay, wallpaper and Termux stack (P3-B, 3a, after 3.4)

**Files:**
- Create: `magisk/cleanup.sh`
- Modify `magisk/service.sh`:
  - delete the animation lines :99-101, keeping `protect_battery` (:98)
  - fix the comment at :96-97
  - delete the wallpaper block :103-111 and the `coverwatch` launch :147-149
  - reword the stay-on comment :137-141
  - after `chmod 700 "$DATADIR"` (:26), add `sh "$MODDIR/cleanup.sh" >> "$LOG" 2>&1`
- Modify `magisk/uninstall.sh`: delete :13-16, rewrite the comment at :2-9, and add the firewall persistence.
- Modify `magisk/module.prop`:
  - `version=v0.2.0`
  - `versionCode=2`
  - `description=Hotspot and modem service for the Z Flip 5: loopback-only API, Wi-Fi 6 hotspot, thermal gate, data meter, native app, and a web UI over Tailscale.`
- Delete: `magisk/coverwatch.sh` (`git rm`), `magisk/system/` (untracked, `rm -rf`), `helper/tether/SetWallpaper.java` (`git rm`)
- Modify `tools/build-tether.sh:13`: drop `SetWallpaper.java` from `SRC`.
- Modify: `tests/boot.test.sh`

**Interfaces:**
- Consumes: `magisk/firewall.sh` (3.4).
- Produces `magisk/cleanup.sh`. Test overrides: `DATADIR` (default `/data/adb/zflip5-modem`), `SERVICED` (default `/data/adb/service.d`) and `DEVTMP` (default `/data/local/tmp`). Steps, in order:
  1. Run `rm -f "$SERVICED/50-zf5-firewall.sh"`.
  2. If `$DATADIR/stock-restored` exists, exit 0.
  3. Set the three animation scales to `1` (`settings put global …`).
  4. `pm enable com.android.systemui/.subscreen.SubHomeActivity` and `pm enable com.sec.android.app.launcher/com.honeyspace.dexservice.SecondaryLauncher`.
  5. For `cover-bright-mode.prev` → `sub_screen_brightness_mode`, then `cover-bright.prev` → `sub_screen_brightness`: when the file holds digits only, run `settings put system <key> <v>`. Delete the file either way.
  6. `rm -f` `cover-home-off`, `temps.json`, `$SERVICED/50-zflip-remote.sh`, `$DEVTMP/scrcpy-server.jar` and `$DEVTMP/zf5-tether.jar`.
  7. `: > "$DATADIR/stock-restored"`.

  It uses no `su` and no `pkill`. The stock wallpaper cannot be restored, because `SetWallpaper` never saved the old one; the module just stops forcing it.
- `uninstall.sh` keeps the stay-on restore (:10-12) and `rm -rf /data/adb/zflip5-modem`. Before the `rm`, it adds:

```sh
MODDIR=${0%/*}
mkdir -p /data/adb/service.d && cp "$MODDIR/firewall.sh" /data/adb/service.d/50-zf5-firewall.sh && chmod 0755 /data/adb/service.d/50-zf5-firewall.sh
```

- [ ] **Step 1: Extend `tests/boot.test.sh`**

- Fake `settings` and `pm`.
- A temp `DATADIR` with `cover-bright.prev=120`, `cover-bright-mode.prev=junk`, `cover-home-off` and `temps.json`.
- A `SERVICED` with `50-zflip-remote.sh` and `50-zf5-firewall.sh`. A `DEVTMP` with both jars.
- Run `cleanup.sh`, then assert the calls are exactly:

```
settings put global window_animation_scale 1
settings put global transition_animation_scale 1
settings put global animator_duration_scale 1
pm enable com.android.systemui/.subscreen.SubHomeActivity
pm enable com.sec.android.app.launcher/com.honeyspace.dexservice.SecondaryLauncher
settings put system sub_screen_brightness 120
```

- Also assert:
  - no `sub_screen_brightness_mode` call
  - all seven files gone, and `stock-restored` present
- Recreate the firewall copy and rerun. Assert no calls and the copy gone.
- Structure checks:
  - `! grep -qE 'animation_scale 0|SetWallpaper|coverwatch|su 1000' magisk/service.sh`
  - `grep -q cleanup.sh magisk/service.sh`
  - `! grep -q SetWallpaper tools/build-tether.sh`
  - the three paths are gone
  - `grep -q 50-zf5-firewall.sh magisk/uninstall.sh`
  - `grep -q '^versionCode=2$' magisk/module.prop`
  - `! grep -qi sms magisk/module.prop`

- [ ] **Step 2: Run the test and confirm it fails**

Run: `bash /Users/adacchi/module-zf5/tests/boot.test.sh`

Expected: `cleanup.sh` not found.

- [ ] **Step 3: Implement the edits and deletions**

- [ ] **Step 4: Run the checks**

Run: `cd /Users/adacchi/module-zf5 && bash tests/boot.test.sh && bash tests/magisk.test.sh && bash tools/build-tether.sh`

Expected: all three pass.

- [ ] **Step 5: Commit**

`git add -A magisk tests/boot.test.sh tools/build-tether.sh helper/tether/SetWallpaper.java`. Subject: `feat(magisk): restore stock settings and drop the cover and remote stacks`.

---

### Task 3.6: Move the ntfy runner into the module and repoint it to `/v1/summary` (P3-C, 3a)

**Files:**
- Create:
  - `magisk/ntfy/alerts.mjs`: from `selfhost/telegram-bot/alerts.js`, without the SMS rule (:99-110)
  - `magisk/ntfy/runner.mjs`: from `selfhost/telegram-bot/index.js`, keeping only `getOk`/`callDaemon`, `publishNtfy`, `pollAlerts`, `pollPowerLoop` and the ntfy-only boot branch (:369-378)
  - `tests/ntfy/alerts.test.mjs` and `tests/ntfy/runner.test.mjs`, ported from `alerts.test.js` and `ntfy.test.js`
- Modify `magisk/ntfy/launch.cjs`:
  - drop `NTFY_ONLY`, `SMS_READ`, `TELEGRAM_TOKEN`, `RADIO_CONTROL` and `AUTOREBOOT_HHMM` (:7, :14)
  - `import(pathToFileURL(__dirname + '/runner.mjs').href)`
- Modify `tools/run-tests.sh`: add `=== NTFY RUNNER ===` running `node --test "$ROOT"/tests/ntfy/`, skipped when `node` is absent.

**Interfaces:**
- Consumes: `summary.signal` and `summary.battery`, plus `/v1/status`, `/v1/usage` and `/v1/hotspot`.
- Produces: `export function detect(prev, snap, cfg = DEFAULTS)`.
  - `snap` holds `{status, usage, signal, battery, hotspot}`.
  - `signal: null` and `battery: null` keep the previous state.
  - The battery rule reads `snap.battery`.
  - The signal key is `(bars === null || bars > 0) ? (display || tech || '?') : 'none'`.
  - With `bars` `null`, the message is `📶 Signal back: 5G, ${operator}.`.
  - `DEFAULTS`, `plain` and `esc` are unchanged.
  - Runner environment: `DAEMON_BASE_URL`, `READ_STATUS`, `NTFY_URL`, `NTFY_TOKEN`, `NTFY_TOPIC`, `DATA_CAP_GB`, `BATTERY_LOW_PCT`, plus `--test-notification`.
  - `pollPowerLoop` reads `/v1/summary` every 3 s. `pollAlerts` reads the four routes every 60 s.

- [ ] **Step 1: Write the failing tests**

Port every existing test that has no SMS input, with battery inputs moved from `status.battery` to `battery`. Add:

```js
test('battery from summary: power lost and restored', () => {
  const m = run([{ battery: { level: 50, plugged: 'ac', temp_c: 30 } }, { battery: { level: 50, plugged: 'unplugged', temp_c: 30 } }, { battery: { level: 49, plugged: 'usb', temp_c: 30 } }]);
  assert.match(m[1][0], /Power lost: on battery at 50%/);
  assert.match(m[2][0], /Power restored \(usb\), 49%/);
});
test('5G SA with bars null is not signal lost', () => {
  const sa = { signal: { display: '5G', tech: 'NR', operator: 'Viettel', bars: null } };
  assert.deepEqual(run([sa, sa, sa, sa]).flat(), []);
});
test('signal null keeps previous state', () => {
  const lte = { signal: { display: '4G', tech: 'LTE', operator: 'Viettel', bars: 3 } };
  assert.deepEqual(run([lte, { signal: null }, { signal: null }, lte]).flat(), []);
});
test('no SMS rule', () => {
  assert.deepEqual(run([{}, { sms: { messages: [{ address: '+1', date: '2', body: 'x' }] } }]).flat(), []);
});
```

`runner.test.mjs`:
- keep the two `--test-notification` multi-server cases
- add `runner polls summary and never the removed routes`: a fake daemon records `req.url` and answers summary `{"signal":null,"battery":{"level":80,"plugged":"ac","temp_c":30}}`. Spawn with `DAEMON_BASE_URL`, `READ_STATUS=test`, `NTFY_URL`, `NTFY_TOKEN=t` and `NTFY_TOPIC=zf5-modem`, wait 1500 ms, then kill. Assert `/v1/summary` was polled and no path starts with `/v1/signal`, `/v1/battery` or `/v1/sms/recent`.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd /Users/adacchi/module-zf5 && node --test tests/ntfy/`

Expected: `Cannot find module …/magisk/ntfy/alerts.mjs`.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `node --test tests/ntfy/ && node --check magisk/ntfy/launch.cjs && echo LAUNCH_OK`

Expected: `# fail 0` and `LAUNCH_OK`.

- [ ] **Step 5: Commit**

`git add magisk/ntfy tests/ntfy tools/run-tests.sh`. Subject: `feat(ntfy): ship the alert runner in the module and read /v1/summary`.

---

### Task 3.7: Remove the bots, relay and Mac scripts for removed features (P3-C, 3a, after 3.6)

**Files:**
- Delete (`git rm -r`):
  - `selfhost/telegram-bot/`, `selfhost/bot/`
  - `relay/`, `fixtures/discord/`
  - `tools/test-discord-relay.sh`, `tools/probe-controls.sh`, `tools/collect-metrics.sh`
  - `tests/probe-controls.test.sh`, `tests/collect-metrics.test.sh`

  Task 1.3 already deleted the Shortcuts files.
- Modify `selfhost/docker-compose.yml`: the header (:1-7) says ntfy only; delete the `telegram` (:29-37) and `bot` (:39-47) services.
- Modify `selfhost/README.md`: rewrite it for ntfy only. Invoke `google-writing` first.
- Modify `tools/run-tests.sh`: delete `=== DISCORD RELAY ===` (:87-96) and the `DEVICE_TESTS` lines (:31-32).
- Modify `.gitignore`: delete `selfhost/bot/.env` and `selfhost/telegram-bot/.env`.
- Create: `tests/removed.test.sh`

- [ ] **Step 1: Write the failing `tests/removed.test.sh`**

It asserts these paths are absent:
- `selfhost/telegram-bot`, `selfhost/bot`
- `relay`, `fixtures/discord`
- `tools/{test-discord-relay,replay-shortcuts,probe-controls,collect-metrics}.sh`
- `tests/{shortcuts,probe-controls,collect-metrics}.test.sh`

It also checks:
- `! grep -qE 'telegram|bot:' selfhost/docker-compose.yml`
- `grep -q 'ntfy:' selfhost/docker-compose.yml`
- `! grep -qE 'DISCORD|collect-metrics|probe-controls' tools/run-tests.sh`
- `! grep -q telegram-bot .gitignore`

On success it prints `all removed tests passed`.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `bash /Users/adacchi/module-zf5/tests/removed.test.sh`

Expected: FAIL, naming `selfhost/telegram-bot`.

- [ ] **Step 3: Delete the files and make the edits**

- [ ] **Step 4: Run the checks**

Run: `cd /Users/adacchi/module-zf5 && bash tests/removed.test.sh && docker compose -f selfhost/docker-compose.yml config -q 2>/dev/null; echo compose_rc=$? && bash tools/run-tests.sh --offline | tail -3`

Expected:
- `all removed tests passed`
- `compose_rc=0`, or nonzero only when docker is absent
- the only allowed FAIL lines are `validate-contracts`, which waits on 3.8 and 3.9

- [ ] **Step 5: Commit**

`git add -A selfhost relay fixtures tools tests .gitignore`. Subject: `chore: remove the Telegram and Discord bots, the relay, and dead tooling`.

---

### Task 3.8: Rewrite the docs for the native app, web UI and slimmer module (P3-D, 3a)

Invoke `google-writing` first, and lint with `python3 /Users/adacchi/.claude/skills/google-writing/scripts/style_lint.py <files>`.

**Files:**
- Delete:
  - `docs/{bands,discord,discord-bot,telegram-bot,shortcuts,scrcpy-remote,dashboard-design-prompt}.md`
  - `docs/assets/dashboard-{home,network,system}.png`
  - `dwkpnx6.md`
- Rewrite `docs/dashboard.md`. It covers:
  - the web UI at the tailnet `/`, with a main view and Details
  - PWA install
  - the pairing QR: URL `/`, with a token only when open reads is off
  - the open reads and open control switches live only in the app's **Advanced** screen
  - Tailscale is read-only on the web, and login and logout are app_only
- Rewrite `docs/ntfy.md`:
  - drop Telegram, Discord and SMS
  - the runner is `magisk/ntfy/{launch.cjs,runner.mjs,alerts.mjs}`, and it ships in the module
  - tests run with `node --test tests/ntfy/`
  - signal and battery come from `/v1/summary`
- Edit `docs/architecture.md` lines 5, 15, 16, 31, 37-43, 61-64 and 73-84:
  - describe the three front ends, app-UID authentication, `/v1/summary` and the module firewall
  - drop relay, Discord, prefer-5G and SMS
- Edit `docs/threat-model.md` lines 5, 7, 12, 16, 18-25 and 42-43. It must contain:
  - `loopback`
  - `unsafe thermal`
  - the sentence "Tailscale login and logout are owner-only: the daemon allows them only from the app's UID (`app_only`)."
  - that UID 0 is never trusted, because `tailscale serve` traffic arrives as UID 0
  - that port 5555 stays firewalled to `lo` and `tailscale0`
- Edit `docs/install.md` lines 4, 10-13, 19 and 27-33:
  - build with `bash helper/build-apk.sh`, `bash tools/build-tether.sh` and `bash tools/build-daemon.sh`, then `cp dist/zflip5-modemd magisk/daemon/zflip5-modemd`, then `bash tools/package-module.sh --src magisk --out dist/zflip5-modem-module.zip`
  - install with `ksud module install <zip>`
  - the first boot installs the app
- Edit `docs/rollback.md:22`:
  - `modemd.prev`
  - rebuilding the WebView kiosk from git history with a higher versionCode
  - "uninstall keeps `/data/adb/service.d/50-zf5-firewall.sh`"
- Edit `README.md` lines 9-55, 75-77, 90-93 and 111-114.
- Edit `PRODUCT.md` lines 13, 21 and 29.
- Edit `DESIGN.md`: remove the speedtest at 130, 150, 171 and 208, and change "352px cover screen" to "app, widget, and web UI" in the overview and description.

**Interfaces:**
- Consumes: `liveRoutes` (3.3), the runner names (3.6), `firewall.sh`, `cleanup.sh` and `50-zf5-firewall.sh` (3.4, 3.5), and the web screen names (3.2).

- [ ] **Step 1: Run the stale-reference check**

Run: `cd /Users/adacchi/module-zf5 && grep -rnE 'coverwatch|/dashboard([^/a-z]|$)|SetWallpaper|[Tt]elegram|[Dd]iscord|/v1/(sms|signal|battery|prefer5g|speedtest|bands|network|health|notifications|cover)|ws-scrcpy|[Ss]hortcuts|ingress|speedtest' docs README.md PRODUCT.md DESIGN.md --exclude-dir=superpowers`

Expected: matches in more than 10 files.

- [ ] **Step 2: Delete, rewrite and edit the files under Files**

- [ ] **Step 3: Rerun the check, plus the token and link checks**

Run the Step 1 command again, then:

```bash
for t in loopback 'unsafe thermal' owner-only app_only; do grep -qF "$t" docs/threat-model.md && echo "TOKEN_OK $t"; done; grep -rhoE '\]\(([^)#]+\.md)' README.md docs/*.md | sed 's/](//' | sort -u
```

Expected: no stale output, four `TOKEN_OK` lines, and every printed link resolves.

- [ ] **Step 4: Lint**

Run: `python3 /Users/adacchi/.claude/skills/google-writing/scripts/style_lint.py README.md PRODUCT.md DESIGN.md docs/*.md`

Fix each finding, or record why it stands in the commit body.

- [ ] **Step 5: Commit**

`git add -A docs README.md PRODUCT.md DESIGN.md dwkpnx6.md`. Subject: `docs: describe the native app, web UI, and slimmer module`.

---

### Task 3.9: Update the contract tokens (P3-E, 3b, after 3.8)

**Files:**
- Modify `tools/validate-contracts.py:22`: change `REQUIRED_TOKENS` from `["unsafe thermal", "owner-only", "ephemeral", "loopback"]` to `["unsafe thermal", "owner-only", "loopback", "app_only"]`. `FORBIDDEN` (:23) is unchanged.
- Modify `tests/validate-contracts.test.sh:23`: use the same four tokens.

**Interfaces:**
- Consumes: the tokens in `docs/threat-model.md` (3.8) and `app_only` in `schemas/api.openapi.json` (3.3).

- [ ] **Step 1: Add a failing negative case**

Copy `schemas/` and `docs/` to `$TMP/neg`, and strip every `app_only` with `sed -i ''` on macOS or `sed -i` on GNU. Run the validator on the copy. Assert `policy_present:app_only=0` and a nonzero exit.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `bash /Users/adacchi/module-zf5/tests/validate-contracts.test.sh`

Expected: FAIL on `app_only`.

- [ ] **Step 3: Edit both token lists**

- [ ] **Step 4: Run the contract tests and the validator**

Run: `cd /Users/adacchi/module-zf5 && bash tests/validate-contracts.test.sh && python3 tools/validate-contracts.py --schemas schemas --docs docs`

Expected: `all validate-contracts tests passed` and `CONTRACTS_OK=1`.

- [ ] **Step 5: Commit**

`git add tools/validate-contracts.py tests/validate-contracts.test.sh`. Subject: `test(contracts): require app_only and drop the ephemeral token`.

---

### Task 3.10: Verify on the Mac, on the device and from a tailnet browser (3c, orchestrator)

This task writes no code. Fix forward in the owning task's files.

- [ ] **Step 1: Run the offline suites**

Run: `cd /Users/adacchi/module-zf5 && (cd daemon && go test -race ./...) && bash tools/run-tests.sh --offline | tail -4 && (cd helper/cover-app && ./gradlew test assembleRelease -q) && echo GRADLE_OK`

Expected: `ok`, `failures=0`, `OFFLINE_TESTS_OK=1` and `GRADLE_OK`.

- [ ] **Step 2: Build the module zip**

Run: `bash helper/build-apk.sh && bash tools/build-tether.sh && bash tools/build-daemon.sh && cp dist/zflip5-modemd magisk/daemon/zflip5-modemd && bash tools/package-module.sh --src magisk --out dist/zflip5-modem-module.zip && bash tools/lint-magisk-module.sh dist/zflip5-modem-module.zip | tail -1 && unzip -l dist/zflip5-modem-module.zip | grep -cE 'coverwatch|system/app|SetWallpaper'`

Expected: `MAGISK_LINT_OK=1` and `0`.

- [ ] **Step 3: Capture the pre-install state (read-only, plus a backup copy)**

Run: `printf '%s\n' 'ls /data/adb/service.d/' 'ls -l /data/local/tmp/scrcpy-server.jar' 'cp /data/adb/modules/zflip5_modem/daemon/zflip5-modemd /data/adb/zflip5-modem/modemd.prev' 'ps -A -o PID,UID,ARGS | grep -E "^ *[0-9]+ +10325 "' | adb shell su`

Expected: `50-zflip-remote.sh` is listed, `modemd.prev` is saved, and the UID 10325 processes are listed.

- [ ] **Step 4: OWNER APPROVAL (installs). Install the zip**

Run: `adb push dist/zflip5-modem-module.zip /data/local/tmp/zf5.zip && printf '%s\n' 'ksud module install /data/local/tmp/zf5.zip' 'rm /data/local/tmp/zf5.zip' | adb shell su`

Expected: success.

- [ ] **Step 5: OWNER APPROVAL (reboot)**

Run: `adb reboot && adb wait-for-device && sleep 90`

- [ ] **Step 6: Check the boot results (read-only)**

Run: `printf '%s\n' 'settings get global window_animation_scale' 'settings get global transition_animation_scale' 'settings get global animator_duration_scale' 'ls /data/adb/service.d/' 'ls /data/local/tmp/scrcpy-server.jar /data/adb/zflip5-modem/cover-home-off /data/adb/zflip5-modem/cover-bright.prev 2>&1' 'ls /data/adb/zflip5-modem/stock-restored' 'iptables -w -C INPUT -j ZF5_FW && echo FW4_OK' 'ip6tables -w -C INPUT -j ZF5_FW && echo FW6_OK' 'dumpsys package com.android.systemui | grep -c "subscreen.SubHomeActivity"' 'pidof zflip5-modemd' | adb shell su`

Expected:
- `1` three times
- no `50-zflip-remote.sh`
- three `No such file` lines
- `stock-restored` listed
- `FW4_OK` and `FW6_OK`
- a count of at least 1
- one PID

- [ ] **Step 7: OWNER APPROVAL (stops processes). Stop the leftover Termux services**

Run `printf '%s\n' 'kill <PID> <PID> …' | adb shell su` with the exact ws-scrcpy, relay and frontdoor PIDs from Step 3. Never use `pkill -f` with a multi-word pattern.

After the owner confirms the list, remove the Termux service directories, including `/data/data/com.termux/files/home/ws-scrcpy`, with `rm -rf` piped the same way.

Expected: no UID 10325 listener on ports 8000-8088 or 8886.

- [ ] **Step 8: Check the firewall**

- From the Mac on the hotspot: `nc -z -G 3 <swlan0 IPv4> 5555; echo rc=$?`. Expected: nonzero.
- From a tailnet peer, when adb TCP listens: `nc -z -G 3 100.110.36.27 5555`. Expected: `rc=0`.

- [ ] **Step 9: Check the web UI from a tailnet browser (open reads and open control on)**

```bash
curl -s -o /dev/null -w '%{http_code} %{content_type}\n' "$TS_URL"
curl -s -o /dev/null -w '%{http_code}\n' "$TS_URL"dashboard
curl -s -X POST "$TS_URL"v1/tailscale/login
curl -s -X POST "$TS_URL"v1/tailscale/logout
curl -s -X POST "$TS_URL"v1/dashboard/open -d '{"open":false}'
curl -s -w ' %{time_total}\n' "$TS_URL"v1/summary | tail -c 40
```

Expected: `200 text/html; charset=utf-8`, `404`, three `"reason":"app_only"` bodies, and a summary ending in `"airplane":…}`.

In a browser:
- the main view shows signal, data, device rows (or "Per-device speed unavailable") and the hotspot switch
- every Details screen loads
- Tailscale has no login control

- [ ] **Step 10: Check the hotspot PHY on a client**

Run: `system_profiler SPAirPortDataType | grep -E 'PHY Mode|Channel|Transmit Rate' | head -3`

Expected: `802.11ax`, `149 (5GHz, 80MHz)` and about `1200`. If the client reports 802.11ac or 802.11n, stop and report.

- [ ] **Step 11: Report the results with the open decisions from Shared Contract §5**

---

## Self-Review

1. **Spec coverage:** every Keep, Remove, Cleanup, Design, Verification and Rollback item maps to a task. These were fixed while checking:
   - Item 12 has no code to remove (decision 13).
   - The spec wants the Alerts and battery-cap settings editable. They stay read-only until the owner confirms (decision 1).
   - The "no WebView" goal had no check, so Task 2.6 gains one.
   - The `ephemeral` token is kept until 3.9 drops it.
   - The double deletion of the Shortcuts files (1.3 and 3.7) is fixed: 3.7 only asserts they are absent.
2. **Step scan:**
   - The `postJSON` move happens once (1.1).
   - 3.1 names the exact cover leftovers to delete.
   - The `stubStart` and `tsFake` harnesses are described, not transcribed.
   - The commit trailers appear once, in Global Constraints. This also fixes the Phase 2 `-m` trailers, which were indented and so not parsed as trailers.
3. **Type consistency:** these contract changes are applied:
   - `parseStations(b []byte)`
   - the named `RateLimits` type
   - `open class DaemonClient` and `SummaryState.stale` with `slowAgeS`
   - the `"more"` route
   - `rotateIp(): JSONObject`
   - `isStale(lastOkMs, nowMs, slowAgeS)`

   Identifiers checked against the code: `do`, `testCfg`, `fakeCollector`, `newTestHC`, `LoadConfig`, `writeConfigAtomic`, `persistHotspotWhitelist`, `thermalSmoother.push` and `median`, `applyMu`, `SetAuto`, `AutoOn`, `statusRedacted`, `airplaneBusy`, `openControl`, `apUp`, `hotspotSettle`, `hotspotPollWait`, `UsageTracker.Report` and `Sample`, and `webManifest` `start_url`.
4. **Review Focus:** five lines, and each test sits in its owning task: 1.2, 2.3 + 3.2, 1.15, 1.7, and 1.5 + 1.15. One device-only risk is noted with its check in 1.16.
5. **Proportion:** the test code stays as assertions. Implementation bodies appear only for fixed shell or copy: 1.4 and the 3.4 rules.