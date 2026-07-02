# Remote screen: control the Z Flip from iOS and Android on the hotspot

Goal: view and control the modem phone's screen from any device — **including
iOS** — where every device is on the modem's own Wi-Fi hotspot.

The catch: plain `scrcpy` is a desktop app and has **no iOS client**. The
cross-platform answer is **ws-scrcpy** — it mirrors the device in a *browser*
over WebSocket, so iOS Safari and Android Chrome both work, no app install.

## Topology

```
   iPhone / Android  ─┐
   (hotspot Wi-Fi)    │  browser → http://<host>:8000
                      ▼
              ws-scrcpy (a small always-on host on the hotspot LAN,
                         or the selfhost server, or Termux on the phone)
                      │  adb
                      ▼
              Z Flip 5  (adb over TCP, port 5555)
```

`scrcpy` reads the framebuffer, so this works even with the inner panel removed
(mirrors whatever display is active — pin the cover with `cmd device_state
state 0`, or spin a virtual display; see the display notes).

## Setup

1. **Enable adb over TCP on the Z Flip** (root):
   ```sh
   adb tcpip 5555            # over USB once, OR on-device as root:
   setprop service.adb.tcp.port 5555 && stop adbd && start adbd
   ```
   The phone's hotspot IP is its `swlan0` address (usually `192.168.x.1`); clients
   reach adb at `<phone-hotspot-ip>:5555`.

2. **Run ws-scrcpy** on a host that can reach the phone (its hotspot IP, or its
   tailnet IP). Easiest is Docker on a small LAN host / the selfhost server:
   ```sh
   docker run -d --name ws-scrcpy -p 8000:8000 --restart unless-stopped \
     xaymar/ws-scrcpy    # or build from github.com/NetrisTV/ws-scrcpy
   # then, inside the container or on the host, register the device once:
   adb connect <phone-hotspot-ip>:5555
   ```
   Or run it in **Termux on the phone itself** (`pkg install nodejs`,
   `npx ws-scrcpy`) and connect ws-scrcpy to `127.0.0.1:5555` — no adb over the
   LAN at all (most private option).

3. **Open from any device** on the hotspot: `http://<ws-scrcpy-host>:8000`, pick
   the device, and you get a live, controllable screen in the browser — iOS and
   Android alike.

## For Android clients specifically
Native `scrcpy` also works from an Android client (via Termux `pkg install
scrcpy` or a laptop): `adb connect <phone-ip>:5555 && scrcpy`. ws-scrcpy is the
one path that additionally covers **iOS**, so use it as the common denominator.

## Security — read before enabling

adb over TCP is the exposure here, not scrcpy:

- **adb keeps its RSA-key auth over TCP** — a new host is `unauthorized` until its
  key is accepted on-device. But once a key is authorized, that host has full
  **root adb** (this is a rooted phone). Treat an authorized key as root access.
- **Don't leave `:5555` open on a shared hotspot.** Anyone on the hotspot who
  gets a key authorized owns the phone. Prefer, in order:
  1. **Termux on-device** ws-scrcpy → adb stays on `127.0.0.1`, nothing on the LAN.
  2. **Bind adb to the tailnet**, not the hotspot LAN, and only let tailnet peers
     connect (the phone already runs tailscaled for the daemon).
  3. If it must be on the hotspot LAN, put **ws-scrcpy behind a reverse proxy with
     auth** (Caddy/nginx basic-auth + TLS) and firewall `:5555` to the ws-scrcpy
     host only.
- Turn adb-tcp **off when not in use**: `setprop service.adb.tcp.port -1 && stop
  adbd && start adbd` (or `adb usb`).

This is why the module does **not** auto-enable adb-over-TCP: it's a deliberate,
owner-only, on-demand step.
