# Rollback & uninstall

The module changes no persistent device state beyond its own module dir and
`/data/adb/zflip5-modem`. It never edits radio firmware, thermal config, vendor
files, or SIM/eSIM. Any runtime radio probe is reverted by the daemon/tools.

## Disable (keep installed)
Magisk Manager → Modules → toggle off → reboot. The daemon stops; nothing else
changes.

## Uninstall (full removal)
```
adb shell su -c 'magisk --remove-modules'        # or remove in Magisk Manager
adb reboot
```
`uninstall.sh` runs on removal and wipes `/data/adb/zflip5-modem` (config +
tokens + logs), so no secrets linger.

## Revoke / rotate a leaked token
Edit `/data/adb/zflip5-modem/config.json` (root, `0600`) — replace the affected
token with a fresh 64-hex value, or delete the whole file to regenerate all three
on next boot. Then update the iPhone/Discord side. A rotated token invalidates the
old one immediately on daemon restart:
```
adb shell su -c 'pkill -x zflip5-modemd'         # watchdog restarts it
```

## Recover from a bad boot
If a module change ever prevented boot (it should not — `service.sh` never blocks
boot), boot to Magisk safe mode (hold volume-down during boot) to disable all
modules, then remove the zip.

## Verify clean state
```
adb shell su -c 'ls /data/adb/modules/zflip5_modem 2>/dev/null; ls /data/adb/zflip5-modem 2>/dev/null'
```
Both absent after uninstall = fully removed.
