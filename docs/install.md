# Install and update

## Prerequisites

- A rooted Z Flip 5 (SM-F731B) running KernelSU. The module works best on
  [OnigiriDeX](https://github.com/Kinue72/OnigiriDeX), which includes
  KernelSU. Root trips Knox, which degrades Samsung Pay and Secure Folder.
- A host with `adb`, Go, and a JDK with the Android build tools.

## Build

```sh
bash helper/build-apk.sh
bash tools/build-tether.sh
bash tools/build-daemon.sh
cp dist/zflip5-modemd magisk/daemon/zflip5-modemd
bash tools/package-module.sh --src magisk --out dist/zflip5-modem-module.zip
bash tools/lint-magisk-module.sh dist/zflip5-modem-module.zip
```

The lint script prints `MAGISK_LINT_OK=1` on success.

## Install

```sh
adb push dist/zflip5-modem-module.zip /data/local/tmp/
printf '%s\n' 'ksud module install /data/local/tmp/zflip5-modem-module.zip' | adb shell su
adb reboot
```

On first boot, `service.sh` generates scoped tokens in
`/data/adb/zflip5-modem/config.json` (mode `0600`) and `app-install.sh`
installs the Flippost app. The app is not launched automatically; open it
from the launcher.

## Reach the web UI

- Over USB, run `adb forward tcp:18080 tcp:18080` and open
  `http://127.0.0.1:18080/`.
- Over your tailnet, follow [Tailscale](tailscale.md).

## Update

Rebuild, repackage, and reinstall the zip, then reboot. The first boot after
an update installs the app when the bundled `versionCode` is higher. Your
config and tokens persist. To regenerate the tokens, delete
`/data/adb/zflip5-modem/config.json`.

## Troubleshoot

- **The daemon is not running.** Read `/data/adb/zflip5-modem/service.log`
  and confirm the binary exists at
  `/data/adb/modules/zflip5_modem/daemon/zflip5-modemd`.
- **A 401 appears everywhere.** The token is wrong. Read `config.json` again.
- **Writes return 409.** The thermal gate refused the write because the phone
  is warm.
