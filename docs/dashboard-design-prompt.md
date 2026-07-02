# Claude Design prompt — Z Flip 5 Modem dashboard

Paste the block below into Claude Design (claude.ai/design). It is self-contained;
the "Reference data model" section binds every UI number to a real daemon field so
the generated screens map 1:1 onto what the device already serves.

Design reference image: the dark monitoring-dashboard mockup (metric cards +
circular resource rings + an Integrations screen with per-service toggles).

---

## PROMPT

Design a **dark, mobile-first admin dashboard for a phone-as-modem** (Samsung
Galaxy Z Flip 5). It is a local status console for a rooted Android device being
used as a 5G/LTE hotspot. Match the visual language of the reference image:
near-black background (#0b0d10), raised card surfaces (#161b22) with 1px hairline
borders (#21262d) and ~10px radius, SF-style system font, generous negative space,
one accent per metric, a bottom tab bar. Feel: calm, precise, glanceable.

Build **two screens** plus a bottom tab bar (`Overview` · `Integrations`).

### Screen 1 — Overview

1. **Header row:** title "📶 Z Flip 5 Modem" on the left; on the right a small
   network badge showing connection type (e.g. `LTE`, `LTE_CA`, `5G`) and a status
   dot (green = thermal SAFE, amber = WARM, red = HOT/COOLDOWN).

2. **Hero: Mobile-data ring (the centerpiece).** A large circular progress ring —
   like the CPU/Memory/Disk rings in the reference, but bigger and centered. It
   shows **this month's mobile data against a 512 GB monthly cap**:
   - Center: percent of cap used (e.g. `42%`), with used amount below it
     (`215 GB of 512 GB`).
   - Sweep fill colored by headroom: teal/green under 70%, amber 70–90%,
     red over 90%.
   - Small caption under the ring: `Today 6.4 GB · This week 48 GB`.
   - The cap (512 GB) is a fixed reference line; render the remaining arc as a
     faint track.

3. **Stat cards (2-up grid):**
   - **Battery** — big `level%`, subcaption `plugged · temp_c°C` (e.g.
     `ac · 32.7°C`).
   - **Temperature** — big `temp_max_c°C`; subcaption `battery <battery_c>°C ·
     <safe?>`; turn the value amber at ≥44°C (warn) and red at ≥46°C (gate).
   - **CPU** — see the per-core treatment below.
   - **Memory** — big `mem_used_pct%` with a thin horizontal fill bar.

4. **CPU card, per-core view (hero-adjacent).** Show a compact **per-core
   utilization strip**: one small vertical bar or mini-ring per core (this device
   has 8 cores), each 0–100%, so it reads "which cores are busy" at a glance.
   Header shows load average `5m <cpu_load5>` and `<cpu_cores> cores`.
   > DATA NOTE for the engineer: per-core % is **not yet served** — the daemon
   > currently exposes only load averages (`cpu_load1/5/15`) and `cpu_cores`.
   > Design the 8-bar strip anyway; back it with a new `per_core_pct` array
   > (see "Backend gaps"). Until then bars can bind to a single load-derived value.

5. **State strip (full-width card):**
   - `Thermal policy` → `SAFE / WARM / HOT / COOLDOWN` with a matching dot.
   - `Ingress` → read-only status `loopback` or `tailscale` with a green dot.
     **No toggle** — Tailscale is managed by tailscaled; this is informational.

6. Footer: `updated <local time>`, auto-refreshing every 5s. A one-line error slot
   under the content (red, e.g. token missing / endpoint 429).

### Screen 2 — Integrations

Mirror the reference "Integrations & Webhooks" screen: a scrollable list of
service cards, each with icon, name, one-line description, and a **toggle**.

- **Discord** — the one active integration. Card: Discord glyph, "Discord",
  description "Relay status + control commands to your Discord server", and a
  **toggle switch** (on/off) exactly like the reference toggles. Below it a compact
  read-only line: last relay status / channel.
- **Tailscale** — show as a **connected, managed** card with a "Managed" pill
  instead of a toggle (it's wired through tailscaled; nothing to switch here).
- Leave room for future integrations (empty-state hint at the bottom).

### Layout & platform constraints (important)

- **Two form factors from one layout.** The page renders in a WebView kiosk on
  BOTH the Z Flip 5 **cover screen (~720×748, very small)** and the **unfolded
  main screen (~1080×2640)**. Design fluid/responsive: single column and larger
  ring on the narrow cover screen; the 2-up card grid on the wider screen. No
  horizontal scroll ever.
- **Ship as a single self-contained HTML file**: inline CSS + vanilla JS, **no
  framework, no build step, no external fonts/CDN/network requests** other than the
  loopback API. This is dropped verbatim into a Go string constant
  (`daemon/dashboard.go`) and served from the device. Prefer semantic HTML +
  CSS `conic-gradient`/SVG for the rings (no chart libraries).
- **Auth & data:** on load, read a bearer token from `?token=` in the URL, save it
  to `localStorage` under `zf5tok`, then strip it from the URL. All fetches send
  `Authorization: Bearer <token>`. Poll every 5s. All endpoints are loopback
  `http://127.0.0.1:18080` (cleartext localhost is allowed).
- Dark only. Cover-screen legibility first: large numerals, high contrast.

### Reference data model (bind the UI to these real fields)

All read endpoints are `GET`, token-gated with scope `read-status`, served at
`http://127.0.0.1:18080`.

`GET /v1/status` →
```json
{
  "health":  { "cpu_load1": 4.33, "cpu_load5": 3.39, "cpu_load15": 2.1,
               "cpu_cores": 8, "mem_used_pct": 53,
               "temp_battery_c": 32.7, "temp_max_c": 45.7, "temp_max_zone": "..." },
  "thermal": { "battery_c": 32.7, "temp_max_c": 45.7, "safe": true },
  "policy_state": "WARM",                       // SAFE | WARM | HOT | COOLDOWN
  "network": { "type": "LTE_CA", "override": "", "nr_state": "NONE" },  // nr_state != NONE => show 5G
  "battery": { "level": 100, "temp_c": 32.7, "plugged": "ac" }
}
```

`GET /v1/usage` →
```json
{
  "today_bytes": 6871947673, "week_bytes": 51539607552, "month_bytes": 230000000000,
  "today_human": "6.4 GB", "week_human": "48 GB", "month_human": "214 GB",
  "source": "rmnet_data"
}
```
Ring math: `used = month_bytes`, `cap = 512 * 1024^3`, `pct = used / cap`.

Thermal thresholds (for card coloring): `warn_c = 44`, `gate_c = 46`.

### Backend gaps to surface (for the engineer, not the design)

- **Per-core CPU %**: add a `per_core_pct: number[]` (len = `cpu_cores`) to
  `/v1/health` and `/v1/status`, computed from `/proc/stat` deltas.
- **Discord toggle**: needs a new write endpoint (e.g. `POST /v1/integrations/discord
  {"enabled":bool}`) with a **write-scoped** token — the dashboard token is
  read-only. Until wired, render the toggle optimistic/disabled-with-tooltip.

### Deliverables

- A single self-contained `dashboard.html` (inline CSS/JS, no deps).
- Both screens + the bottom tab bar, responsive across cover and main screens.
- Realistic sample data matching the schema above so the preview looks live.
