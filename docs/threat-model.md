# Threat model — zflip5-modem-module

## Assets (crown jewels)

1. **SMS** — carries 2FA/OTP, so disclosure enables account takeover.
2. **Root radio control** — tether, network-type preference, service restart.
3. **Scoped tokens** — `read-status`, `sms`, `radio-control`.

## Attackers & mitigations

### A1 — Any other app installed on the phone (primary on-device)
It shares `127.0.0.1`, so the loopback bind is not a boundary. Reaching SMS or
radio control requires the correct scoped token **and** crossing the root-broker
Unix socket, which uses `SO_PEERCRED` and a named-verb allowlist (typed args, no
free-form shell strings → no shell injection). Read paths are owner-only via
token; the `sms` scope is separate and independently revocable.

### A2 — Attacker who reaches the public Discord relay, or steals a token (primary off-device)
- The relay holds **no `sms` token** and cannot read messages — structurally, not
  by policy alone.
- Every interaction is Ed25519-verified with a ≤5 s timestamp window and nonce
  dedup; the owner user-ID allowlist is default-deny.
- Sensitive replies are **ephemeral**.
- One-command revoke/rotate kill-switch invalidates a leaked token.
- Out-of-band alert fires on SMS reads and on state-changing commands.

### A3 — Passive network / CGNAT peers
No public inbound: default bind is loopback; remote reach is a private Tailscale
tunnel governed by tailnet ACLs, never a port-forward.

### A4 — Physical/thermal abuse via the control API
Radio/tether writes are refused whenever the thermal gate reports an
**unsafe thermal** state. The gate is PRIMARY on battery temp + `/sys/class/thermal`
and **fails closed**: if temperature cannot be read, writes are refused. "Prefer
5G" is a safe **prefer/recover NR** preference, never a force.

## Forbidden (hard scope guardrails)

- No bypass of thermal mitigation; no disabling or editing of thermal daemons or
  vendor thermal files; no forcing NR while thermal is unsafe.
- No public / all-interfaces bind by default.
- SMS is pull-only, redacted by default, owner-only, rate-limited; there is no
  default forwarding of messages to any channel, and never over Discord.
- No IMEI/baseband/SIM/eSIM/carrier-provisioning changes; no modem firmware
  patching.
- No broad SELinux permissive mode — only a narrow, daemon-scoped `sepolicy.rule`
  added when a real on-device denial proves it necessary.

## Residual risks (accepted)

- Root trips Knox (`warranty_bit=1`); Samsung Pay / Secure Folder degrade.
- A stolen `radio-control` token could toggle tether/5G preference until revoked;
  the thermal gate and rate limits bound the blast radius, and out-of-band alerts
  make it observable.
