# Private ingress — Tailscale (stubbed deploy)

The phone is behind carrier CGNAT (no public inbound), so remote access is a
mesh VPN, never a port-forward. The module runs **userspace** `tailscaled`
(no TUN/root-net changes) and exposes **only** the loopback daemon port to your
tailnet with `tailscale serve`. The default `ingress.mode` is `loopback`, which
skips all of this.

## One-time setup (your secret — not committed)

1. Download the **arm64** `tailscaled` and `tailscale` binaries and place them in
   the module at `magisk/tailscale/` (they ship inside the module zip).
2. Generate a Tailscale **auth key** (tailnet admin → Settings → Keys). Write it
   to the phone at `/data/adb/zflip5-modem/tailscale.authkey` (mode `0600`). The
   module consumes it once on boot and deletes it; it is never logged.
3. Set `ingress.mode` to `tailscale` in `/data/adb/zflip5-modem/config.json`.
4. Reboot (or restart the module). `service.sh` brings up userspace tailscaled,
   runs `tailscale up`, and `tailscale serve`s the loopback port.

## Result

- iPhone Shortcuts reach `https://zflip5.<tailnet>.ts.net` (see docs/shortcuts.md).
- The Discord relay host joins the same tailnet and reaches the daemon privately
  (see docs/discord.md).
- The daemon still binds loopback only; `tailscale serve` is the sole bridge, and
  the tailnet ACL governs who may reach it.

## Verify no public exposure

On the device: `su -c 'ss -ltn'` must show the daemon on `127.0.0.1` only — never
a public / all-interfaces address. `tools/security-check.sh --device` asserts
this once the module is installed.
