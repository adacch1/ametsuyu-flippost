# PRODUCT.md

## Register
product — the dashboard serves a task (operating a phone-as-modem); design must disappear into the instrument-panel job.

## What this is
`zflip5-modem-module`: a rooted Samsung Galaxy Z Flip 5 (SM-F731B) repurposed as a dedicated 5G/LTE modem + Wi-Fi hotspot. A Magisk module runs a root Go daemon (loopback-only HTTP API, bearer-token scopes) that serves a self-contained admin dashboard and enforces safety policy (clamped thermal gate, reversible CPU tuning, SSID-whitelist hotspot auto-toggle).

## Users
Exactly one: the device owner, a technical power user. No onboarding funnel, no marketing surface, no anonymous traffic.

## Primary surfaces
1. **Cover-screen kiosk** (WebView, ~352×339 CSS px usable, touch): the everyday surface, opened via the Magisk Action button. Comfort here wins every tradeoff.
2. **Desktop/phone browser over Tailscale**: occasional deeper admin (thermal limits, SSID whitelist).

## Product personality
Instrument panel. Dark-only, quiet, dense where data earns it, zero decoration that doesn't convey state. Numbers are the interface.

## Hard constraints
- Single self-contained HTML string compiled into the Go binary: no CDN, no external fonts, no build step.
- API is loopback-only with scoped tokens (`read-status`, `radio-control`, `sms`); the page fetches with a locally-stored token.
- Rate limits are real (120/min default): polling must stay well under.
- Safety invariants (thermal gate clamp, no public bind) are product features, not implementation details — the UI must never suggest disabling them.

## Anti-references
Consumer SaaS marketing pages, onboarding tours, gradient-hero aesthetics, light mode.

## Accessibility posture
Dark theme with high-contrast ink (#e6edf3 on #0b0d10); touch targets ≥44px on the kiosk; single known sighted user, but WCAG AA contrast is kept anyway because the cover screen is often read outdoors.
