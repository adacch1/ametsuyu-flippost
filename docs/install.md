# Install & update

## Prerequisites
- Rooted Z Flip 5 (SM-F731B) with Magisk (confirmed 30700). Bootloader unlocked;
  Knox is tripped by root (accepted — Samsung Pay / Secure Folder degrade).
- Host with `adb` and the Go toolchain (to build the daemon binary).

## Build
```
bash tools/build-daemon.sh                       # -> dist/zflip5-modemd (static arm64)
cp dist/zflip5-modemd magisk/daemon/zflip5-modemd # ship the binary in the module
bash tools/package-module.sh --src magisk --out dist/zflip5-modem-module.zip
bash tools/lint-magisk-module.sh dist/zflip5-modem-module.zip   # MAGISK_LINT_OK=1
```

## Install
```
adb push dist/zflip5-modem-module.zip /sdcard/
adb shell su -c 'magisk --install-module /sdcard/zflip5-modem-module.zip'
adb reboot
```
On first boot `service.sh` generates scoped tokens into
`/data/adb/zflip5-modem/config.json` (mode `0600`).

## Get your tokens (owner, once)
```
adb shell su -c 'cat /data/adb/zflip5-modem/config.json'   # read-status / sms / radio-control
```
Store them in the iPhone Shortcut variable / Discord relay config — never inline.

## Reach it
- Local: `adb forward tcp:18080 tcp:18080 && curl -H "Authorization: Bearer $T" http://127.0.0.1:18080/v1/status`
- Remote: enable Tailscale (docs/tailscale.md); iPhone (docs/shortcuts.md); Discord (docs/discord.md).

## Update
Re-build, re-package, re-install the zip, reboot. The config (and tokens) persist
across updates; delete `/data/adb/zflip5-modem/config.json` to regenerate tokens.

## Troubleshooting
- **Daemon not running**: `adb shell su -c 'cat /data/adb/zflip5-modem/service.log'`.
  Confirm the binary is at `/data/adb/modules/zflip5_modem/daemon/zflip5-modemd`.
- **DEFEX kills the daemon**: it execs through `/system/bin/sh` and is static (no
  libc); if a denial still appears, copy the binary to an allowed path and point
  `$BIN` there — do NOT switch SELinux to permissive.
- **Helper "sleeping apps"**: exclude the module/helper from battery optimization.
- **401 everywhere**: wrong token — re-read config.json; tokens are 64 hex chars.
- **Writes return 409**: thermal gate — the device is warm; that is intended.
