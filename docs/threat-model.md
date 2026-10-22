# Threat model

## Assets

1. **Root radio control.** Tether, band, airplane mode, and service restart.
2. **Scoped tokens.** `read-status` and `radio-control`.
3. **Tailscale sign-in.** Login and logout change which tailnet owns the phone.

## Attackers and mitigations

### A1 - Another app on the phone
It shares `127.0.0.1`, so the loopback bind is not a boundary. Reading or
controlling the daemon requires the correct scoped token.
Tailscale login and logout are owner-only: the daemon allows them only from the app's UID (`app_only`).

### A2 - A tailnet browser or a stolen token
A tailnet browser reaches the daemon through `tailscale serve`, so the daemon
sees the request as UID 0. The daemon never trusts UID 0 and never trusts
UID 2000 (adb forward) for `app_only` routes. A tailnet browser never gets a
Tailscale login URL and cannot sign Tailscale out. Tailnet ACLs decide who
reaches the page, and the open reads and open control switches default to off.
To revoke a leaked token, see [Rollback](rollback.md).

### A3 - Passive network and CGNAT peers
No public inbound: the default bind is loopback, and remote reach is a private
Tailscale tunnel. Some carriers route a public IPv6 prefix to the phone. The
module firewall keeps port 5555 (adb) firewalled to `lo` and `tailscale0`, for
IPv4 and IPv6, and does the same for the remote-screen ports.

### A4 - Thermal abuse through the control API
The daemon refuses radio and tether writes whenever the thermal gate reports an
**unsafe thermal** state. The gate fails closed: if the daemon cannot read
the temperature, it refuses writes.

## Forbidden

- No weakening of the thermal gate.
- No public or all-interfaces bind.
- No new network listener.
- No IMEI, baseband, SIM, eSIM, or carrier-provisioning changes.
- No broad SELinux permissive mode.

## Residual risks

- Root trips Knox (`warranty_bit=1`), so Samsung Pay and Secure Folder degrade.
- A stolen `radio-control` token can toggle the hotspot and radio until you
  rotate it. The thermal gate and rate limits bound the damage.
