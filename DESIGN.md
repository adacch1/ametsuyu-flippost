---
name: Z Flip 5 Modem Dashboard
description: Rainy-dusk skin from the Flippost logo, tuned for a 352px cover screen
colors:
  bg: "#1b2436"
  sky-top: "#2b3850"
  surface: "rgba(213,231,253,.07)"
  surface-inset: "rgba(14,20,33,.45)"
  line: "rgba(213,231,253,.15)"
  line-soft: "rgba(213,231,253,.08)"
  ink: "#fef7ee"
  ink-2: "#c9d7ea"
  ink-3: "#9eb1cb"
  teal: "#a9cdfb"
  teal-2: "#6fa4f0"
  green: "#9ee6c3"
  amber: "#ffd49a"
  red: "#ff9aa6"
  violet: "#c4b5ff"
  blush: "#fbd3d0"
  track: "rgba(213,231,253,.12)"
  on-teal: "#1b2436"
typography:
  data-hero:
    fontFamily: "Nunito, ui-rounded, system-ui, Roboto, sans-serif"
    fontSize: "clamp(34px, 11vw, 46px)"
    fontWeight: 700
    lineHeight: 1
    letterSpacing: "-0.03em"
    fontFeature: "'tnum' 1"
  stat:
    fontFamily: "Nunito, ui-rounded, system-ui, Roboto, sans-serif"
    fontSize: "30px"
    fontWeight: 700
    lineHeight: 1
    letterSpacing: "-0.02em"
    fontFeature: "'tnum' 1"
  body:
    fontFamily: "Nunito, ui-rounded, system-ui, Roboto, sans-serif"
    fontSize: "13.5px"
    fontWeight: 500
    lineHeight: 1.4
  label:
    fontFamily: "Nunito, ui-rounded, system-ui, Roboto, sans-serif"
    fontSize: "10.5px"
    fontWeight: 600
    letterSpacing: "0.07em"
  mono:
    fontFamily: "ui-monospace, Menlo, monospace"
    fontSize: "10.5px"
    fontWeight: 500
rounded:
  sm: "12px"
  md: "22px"
  lg: "30px"
  pill: "999px"
spacing:
  card-pad: "14px"
  gap: "10px"
components:
  button-commit:
    backgroundColor: "{colors.teal}"
    textColor: "{colors.on-teal}"
    rounded: "{rounded.sm}"
    padding: "13px"
    height: "46px"
  button-ghost:
    backgroundColor: "{colors.surface-inset}"
    textColor: "{colors.teal}"
    rounded: "{rounded.sm}"
    padding: "13px"
    height: "46px"
  button-stateful-on:
    backgroundColor: "{colors.green}"
    textColor: "{colors.on-teal}"
    rounded: "{rounded.sm}"
    padding: "13px"
    height: "46px"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "{spacing.card-pad}"
  input:
    backgroundColor: "{colors.surface-inset}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "11px 12px"
    height: "44px"
---

# Design System: Z Flip 5 Modem Dashboard

## 1. Overview

**Creative North Star: "The Instrument Panel"**

This is not an app that *has* data — the data *is* the interface. A rooted phone is doing a machine's job (5G modem, Wi-Fi hotspot), and its dashboard should read like the gauge cluster of that machine: a near-black ground, a few numbers sized to be read at a glance, and color spent only where it reports state. The primary surface is a folded phone's cover screen — roughly **352×339 CSS px, touched with a thumb, often outdoors** — so every choice bends toward legibility at arm's length and one-handed reach.

The system is deliberately quiet. There is one brand accent (teal); everything else is either neutral ink or a semantic status color. Depth comes from stacked dark surfaces and hairline borders, never shadows or blur. A colored dot means something is *live*; if it's decoration, it's deleted. The largest type on any screen is a datum — a percentage, a temperature, a throughput — and the chrome around it recedes into muted grey labels.

It explicitly **rejects** the consumer-SaaS playbook this kind of surface usually attracts: no gradient hero, no onboarding tour, no light mode, no marketing gloss, no big friendly primary button competing with the readings. If it looks like a landing page, it's wrong.

**Key Characteristics:**
- Dark-only, near-black ground (#0b0d10); depth by tonal layering, not shadow.
- One accent (teal) + a strict semantic status set (green/amber/red); violet reserved for the CPU lane.
- Numbers are the largest, boldest, tabular type; labels are small, muted, uppercase.
- Touch targets ≥44px; the 5-tab bar is fixed to the bottom (thumb zone).
- Color and dots carry *state*, never decoration.

## 2. Colors

A monochrome dark ground carrying one teal accent and a reserved semantic-status set — the palette of a gauge, not a brand campaign.

### Primary
- **Signal Teal** (#3fb8af): the single brand accent. Owns the data ring, the memory/throughput bars, ghost-button text, active nav, and input focus. It marks "this is the reading / the current thing," never decoration.

### Secondary
Reserved *semantic status* colors. These are not accents to reach for; each means exactly one state.
- **Go Green** (#3fb950): healthy / on — battery OK, hotspot up, policy SAFE, "turn on" actions.
- **Warn Amber** (#e3a008): elevated but safe — warming temp, WARM policy, open-reads active.
- **Limit Red** (#f0524e): the danger line — hot temp, the thermal-gate ceiling, "turn off," open-control active.

### Tertiary
- **CPU Violet** (#8a7dff): the one data lane that isn't teal. Used *only* for the per-core CPU bars, so load reads as its own channel distinct from the data/throughput teal.

### Neutral
- **Ground** (#0b0d10): app background; the near-black the whole panel sits on.
- **Surface** (#161b22): card background — the first step up from ground.
- **Inset Surface** (#1b212a): fields, ghost buttons, speedtest cells — a step deeper *into* a card.
- **Hairline** (#21262d) / **Soft Divider** (#1a1f27): 1px card borders and inner row rules.
- **Track** (#242b34): the unfilled portion of rings and bars.
- **Ink** (#e6edf3): primary text. **Ink-2** (#9aa5b1): secondary values/captions. **Ink-3** (#7d8794): uppercase micro-labels — held at 4.7:1 on Surface so it survives AA outdoors.

### Named Rules
**The One Accent Rule.** Teal is the only brand accent. Green/amber/red are *status*, violet is the *CPU lane*, and nothing else earns a hue. A fourth decorative accent (there was once a stray blue) is prohibited — it dilutes every color that's supposed to mean something.

**The Status-Only Color Rule.** If a color isn't reporting a live state, it's grey. A red that doesn't mean danger and a green that doesn't mean healthy are both bugs.

## 3. Typography

**Display / Body / Label Font:** system-ui stack (`system-ui, -apple-system, Roboto, sans-serif`). "Be Vietnam Pro" is named as the intended face but is **not shipped** — the dashboard is a single self-contained HTML string compiled into the Go binary, so no webfont loads; the system sans is the real face.
**Mono Font:** `ui-monospace, Menlo, monospace` — IP addresses and the header WAN line only.

**Character:** One neutral sans doing every job, separated by weight and size rather than family. Tabular figures (`font-feature-settings:"tnum"`) everywhere numbers change, so digits don't jitter as values tick.

### Hierarchy
- **Data Hero** (700, clamp 34–46px, −0.03em): the one big reading per view — the data-usage ring percentage. Only one per screen.
- **Stat** (700, 30px, −0.02em): card headline numbers — battery %, temperature, memory %, CPU load. Tabular.
- **Speed cell** (700, 24px): the three throughput/ping figures in the speedtest.
- **Body** (500, 13.5px, 1.4): state rows, captions, helper text. Ink or Ink-2.
- **Label** (600, 10.5px, uppercase, 0.05–0.09em tracking): card headers, field labels, section kickers. Always Ink-3.

### Named Rules
**The Numbers-Are-The-Interface Rule.** The largest, heaviest type on any screen is always a datum, never a heading or a button label. Chrome shrinks so the reading can grow.

## 4. Elevation

**Flat. No shadows anywhere.** Depth is built entirely from a three-step tonal stack — Ground (#0b0d10) → Surface (#161b22) → Inset Surface (#1b212a) — separated by 1px hairline borders (#21262d). A control that sits "inside" a card (input, ghost button, speedtest cell) uses Inset Surface to read as recessed; a card reads as raised simply by being lighter than the ground it floats on. The only blur in the system is the bottom nav's `backdrop-filter`, so content scrolls softly under a fixed tab bar — a functional frosting, not decoration.

### Named Rules
**The No-Shadow Rule.** Shadows are forbidden. If a surface needs to feel higher, step its background one tone lighter and give it a hairline border. A drop shadow on this ground reads as a 2014 app.

## 5. Components

Every interactive element is at least 44px tall (cover-screen thumb target) and states are conveyed by color/fill, never by shadow.

### Buttons
- **Shape:** gently rounded (8–9px radius), full-width within a card, ≥46px tall, weight 700.
- **Commit (primary):** solid **Signal Teal** fill (#3fb8af) with dark teal ink (#04211f). Reserved for actions that *write and persist* — "Apply thermal limits", "Save whitelist".
- **Ghost (trigger):** Inset Surface (#1b212a) with a hairline border and teal text. For *occasional* actions that shouldn't shout — "Run speedtest", "Rotate IP". Same footprint as commit, a fraction of the volume.
- **Stateful:** fill *is* the state — **Go Green** when the press turns something on ("Turn hotspot ON"), **Limit Red** when the press turns it off. The label states the action a press performs.
- **Mini (`.minibtn`):** Inset Surface + hairline + teal text, ~34–44px, for paired secondary actions and toggles ("Scan now", "Turn on / Turn off").
- **Focus:** 2px teal outline, inset. **Disabled:** 0.5 opacity.

### Cards / Containers
- **Corner Style:** 12px radius.
- **Background:** Surface (#161b22) on the Ground; **Border:** 1px Hairline (#21262d); **Shadow Strategy:** none (see Elevation).
- **Internal Padding:** 14px (12px on ≤420px-tall screens). Cards stack with a 10px gap. **Never nest a card in a card.**
- **Header:** a small uppercase Ink-3 label, optionally trailed by a single **status dot** (see below) — nothing else.

### Inputs / Fields
- **Style:** Inset Surface (#1b212a), 1px Hairline border, 8px radius, ≥44px tall, 14px text.
- **Focus:** border shifts to teal (no glow). Labels are Ink-3 uppercase above the field.

### Navigation
- **Style:** fixed bottom bar, 5 tabs, `backdrop-filter: blur(14px)` over a translucent ground, 1px top hairline. Each tab is an icon + a 10px label.
- **States:** default Ink-3; **active** = Ink label with a teal icon. Tab is the whole ≥60px column (thumb target).

### Status Dot (signature)
- An 8px circle in a card header or state row. Colors map 1:1 to state: green (good) / amber (warn) / red (danger) / #30363d (off). It is the system's core state vocabulary — and it **only** appears where a value is live.

### Data Ring & Bars (signature)
- **Ring:** an SVG `stroke-dasharray` arc, teal on a #242b34 track, rounded cap, animating `stroke-dashoffset`. Carries the one hero datum (monthly data, battery). Center holds the Data-Hero number.
- **Bars:** memory/throughput fill teal; **CPU per-core** bars fill violet — the only place violet appears — so load reads as its own channel.

## 6. Do's and Don'ts

### Do:
- **Do** keep the biggest type a number. The reading grows; the label shrinks.
- **Do** spend teal on the current/primary reading and on `:focus`; keep it under ~10% of any screen.
- **Do** use green/amber/red *only* as status, and pair every status color with a word (SAFE/WARM, on/off), never color alone.
- **Do** convey depth with the three-tone surface stack + 1px hairlines.
- **Do** give every touch target ≥44px and keep primary actions reachable at the bottom (thumb zone).
- **Do** use one filled teal *commit* button per card; make every other action a ghost or mini.

### Don't:
- **Don't** introduce a fourth accent. Blue is banned — it once crept onto the speedtest button and diluted the whole palette (see The One Accent Rule).
- **Don't** put a colored dot, stripe, or accent anywhere it doesn't report live state. Decoration that doesn't convey state is deleted.
- **Don't** stack multiple loud, differently-colored "primary" buttons in one card — one teal/stateful primary, ghosts around it.
- **Don't** add shadows, glows, or glassmorphism (the nav blur is the sole, functional exception).
- **Don't** drift toward the consumer-SaaS look this surface attracts: no gradient hero, no onboarding tour, no light mode, no marketing gloss. If it looks like a landing page, it's wrong.
- **Don't** ship gray body text that fails 4.5:1 — Ink-3 is floored at 4.7:1 on Surface because the cover screen is read outdoors.
