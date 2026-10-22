# PRODUCT.md

## Register
product — the dashboard serves a task (operating a phone-as-modem); design must disappear into the instrument-panel job.

## What this is
`zflip5-modem-module`: a rooted Samsung Galaxy Z Flip 5 (SM-F731B) repurposed as a dedicated 5G/LTE modem and Wi-Fi hotspot. A KernelSU module runs a root Go daemon (loopback-only HTTP API, bearer-token scopes) that enforces safety policy (clamped thermal gate, reversible CPU tuning, SSID-list hotspot auto-toggle) and serves a web UI. A native app and a home-screen widget are the everyday surfaces.

## Users
Exactly one: the device owner, a technical power user. No onboarding funnel, no marketing surface, no anonymous traffic.

## Primary surfaces
1. **Native app and widget** (Kotlin, Compose, touch): the everyday surface. Comfort here wins every tradeoff.
2. **Web UI over Tailscale** (one self-contained page at `/`, installable as a PWA): occasional remote checks and hotspot control.

## Product personality
Instrument panel. Dark-only, quiet, dense where data earns it, zero decoration that doesn't convey state. Numbers are the interface.

## Hard constraints
- The web UI is a single self-contained HTML page compiled into the Go binary: no CDN, no external fonts, no build step.
- API is loopback-only with scoped tokens (`read-status`, `radio-control`); the app authenticates by UID, and the web page fetches with a locally-stored token.
- Rate limits are real (600/min default): polling must stay well under.
- Safety invariants (thermal gate clamp, no public bind) are product features, not implementation details — the UI must never suggest disabling them.

## Anti-references
Consumer SaaS marketing pages, onboarding tours, gradient-hero aesthetics, light mode.

## Accessibility posture
Dark theme with high-contrast ink (#e6edf3 on #0b0d10); touch targets ≥44px in the app; single known sighted user, but WCAG AA contrast is kept anyway because the phone is often read outdoors.
