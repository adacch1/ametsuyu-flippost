# Rollback and uninstall

The module changes no persistent device state beyond its own module directory
and `/data/adb/zflip5-modem`. It never edits radio firmware, thermal config,
vendor files, or SIM and eSIM settings.

## Disable the module

In the KernelSU manager, switch the module off and reboot. The daemon stops.

## Uninstall the module

```sh
printf '%s\n' 'ksud module uninstall zflip5_modem' | adb shell su
adb reboot
```

`uninstall.sh` restores the stay-on setting and wipes `/data/adb/zflip5-modem`
(config, tokens, and logs). Uninstall keeps `/data/adb/service.d/50-zf5-firewall.sh`,
so the firewall stays active after removal. Delete that file yourself if you
want the rules gone.

## Roll back the daemon

Before you replace the daemon binary, keep the old one as
`/data/adb/zflip5-modem/modemd.prev`:

```sh
printf '%s\n' 'BIN=/data/adb/modules/zflip5_modem/daemon/zflip5-modemd' \
  'cp "$BIN" /data/adb/zflip5-modem/modemd.prev' | adb shell su
```

To roll back, copy `modemd.prev` over the binary with the rename-first steps in
the README, then run `pkill -x zflip5-modemd`. The watchdog restarts it.

## Go back to the WebView kiosk

The older WebView kiosk is gone from the tree. To restore it, rebuild the kiosk
from git history with a higher `versionCode` than the installed app, then
install that APK with `pm install -r`. Android refuses a lower `versionCode`.

## Rotate a leaked token

Edit `/data/adb/zflip5-modem/config.json` (root, `0600`). Replace the token
with a new 64-character hex value, or delete the file to regenerate all tokens
on the next boot. Then restart the daemon:

```sh
printf '%s\n' 'pkill -x zflip5-modemd' | adb shell su
```

## Recover from a bad boot

If a module change ever blocks boot, boot to safe mode to disable all modules,
then remove the module.
