# Flashing BKLYN-Kalama-Gki-Kernel on this rig

This is a headless, battery-less SM-F731B with the inner screen removed, Magisk root, unlocked bootloader, and the zflip5 modem module running. The kernel zip is an **AnyKernel3** package: it contains only a prebuilt GKI `Image`, so no building is required — you swap the kernel inside your existing Magisk-patched `boot` image.

Assumed paths:

- Zip: `/Users/adacchi/Downloads/BKLYN-Kalama-Gki-Kernel-1.4.2-Enforcing-BK-KSU.zip`
- Repo: `/Users/adacchi/module-zf5`

---

## 0. Preflight

Run these on the Mac. Everything must succeed before you touch a partition.

```sh
adb devices -l              # device listed
adb shell getprop sys.boot_completed   # 1
adb shell su -c id          # uid=0(root)
adb shell uname -r          # 5.15.148-android13-8...
```

Power rule: this phone has no battery. Flash only on a stable bench supply. Do not run speedtests/load while flashing.

---

## 1. Extract the kernel Image

```sh
mkdir -p /tmp/bk-kernel
unzip -o "/Users/adacchi/Downloads/BKLYN-Kalama-Gki-Kernel-1.4.2-Enforcing-BK-KSU.zip" Image -d /tmp/bk-kernel
file /tmp/bk-kernel/Image    # expect: Linux kernel ARM64 boot executable Image
```

---

## 2. Backup the current boot partition

Pull the **live** image, not a stock firmware image. The live partition already contains Magisk's ramdisk patch, which you must preserve.

```sh
adb shell su -c 'dd if=/dev/block/by-name/boot of=/data/local/tmp/boot-backup.img bs=4096'
adb shell su -c 'dd if=/dev/block/by-name/init_boot of=/data/local/tmp/init_boot-backup.img bs=4096'
adb pull /data/local/tmp/boot-backup.img ~/boot-backup.img
adb pull /data/local/tmp/init_boot-backup.img ~/init_boot-backup.img
```

Verify the local backup is non-trivial:

```sh
ls -lh ~/boot-backup.img ~/init_boot-backup.img
```

---

## 3. Build the new boot image on the phone

Push the kernel and repack using Magisk's `magiskboot` on the device. This preserves the ramdisk and Magisk.

On this device Samsung DEFEX blocks exec of `/data/adb/magisk/magiskboot` from the su shell, so copy it to `/data/local/tmp` first:

```sh
adb push /tmp/bk-kernel/Image /data/local/tmp/Image
adb shell su -c 'cp /data/adb/magisk/magiskboot /data/local/tmp/mbk && chmod 755 /data/local/tmp/mbk'
adb shell su -c 'cd /data/local/tmp && \
  ./mbk unpack boot-backup.img && \
  cp Image kernel && \
  ./mbk repack boot-backup.img new-boot.img'
```

Confirm `new-boot.img` exists and is similar in size to the original:

```sh
adb shell su -c 'ls -lh /data/local/tmp/boot-backup.img /data/local/tmp/new-boot.img'
```

Do not touch `init_boot` or `vendor_boot`. They carry ramdisk and vendor modules.

---

## 4. Flash boot

```sh
adb shell su -c 'dd if=/data/local/tmp/new-boot.img of=/dev/block/by-name/boot bs=4096'
adb shell sync
adb reboot
```

---

## 5. Verify after boot (headless)

Wait ~60-90s, then:

```sh
adb wait-for-device
adb shell getprop sys.boot_completed     # 1
adb shell uname -r                        # should now be the BKLYN build, still 5.15.x
adb shell su -c id                        # uid=0(root) — Magisk alive

# modem + hotspot still up
adb shell su -c 'ip -br link | grep -E "swlan0|rmnet_data0"'
adb shell su -c 'dumpsys wifi | grep -m1 "channel_frequency"'
```

Module/bench checks (token from `/data/adb/zflip5-modem/config.json`):

```sh
adb forward tcp:18080 tcp:18080
curl -sS -H "Authorization: Bearer <read-status-token>" http://127.0.0.1:18080/v1/thermal/bench
curl -sS -H "Authorization: Bearer <read-status-token>" http://127.0.0.1:18080/v1/status
```

Bench should still be `enabled:true` (config persists, daemon reapplies after boot). Run a speedtest only after confirming the baseband is attached.

---

## 6. Rollback

If anything fails after the flash:

```sh
adb push ~/boot-backup.img /data/local/tmp/boot-backup.img
adb shell su -c 'dd if=/data/local/tmp/boot-backup.img of=/dev/block/by-name/boot bs=4096 && sync'
adb reboot
```

If the phone cannot boot to adb, use Download mode:

1. Power off.
2. Hold **Volume Up + Volume Down**.
3. Plug USB into the Mac.
4. Odin/heimdall sees the device even without a screen.

Build an Odin-flashable tar from the backup on the Mac:

```sh
cd ~
cp boot-backup.img boot.img
tar -H ustar -cf boot-restore.tar boot.img
```

Flash `boot-restore.tar` in Odin's AP slot. For a complete recovery, use the stock F731BXXS5DYB3 firmware.

---

## 7. Real risks

- **Most likely failure**: vendor modules (Wi-Fi/modem) don't load because the kernel branch is older than the Feb 2025 firmware. This is not a brick; it's a functional modem failure. Revert with Section 6.
- **Brick of boot partition**: power loss during `dd` on a battery-less device. Recover via Download/Odin.
- **Magisk loss**: only if you accidentally flash a stock image instead of the repacked live image.
- **No screen**: boot failure is invisible; rely on `adb` and the PC within the first two minutes.

KernelSU is baked into this Image. Install the KernelSU Manager APK if you want a second root manager; Magisk remains active.
