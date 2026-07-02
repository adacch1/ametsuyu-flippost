# 4G/5G bands — Vietnam, and what "unlock all bands" actually means

## TL;DR

Every radio access technology is **already enabled** on this device, including
5G NR. `GET /v1/bands` reports it:

```json
{ "allowed_types": "GPRS|EDGE|UMTS|HSDPA|HSUPA|HSPA|LTE|HSPA+|GSM|LTE_CA|NR",
  "nr_enabled": true, "is_max": true, "current_tech": "LTE_CA", "current_band": 3 }
```

`is_max: true` means the RAT mask permits 2G/3G/4G/**5G-NR** — the modem is free
to select any band it supports. There is nothing to "unlock" at the layer this
module is allowed to touch.

## Why the module does NOT force a per-band mask

"Unlock all bands" is often imagined as writing a band bitmask into the modem
(the `*#0011#` / `*#2263#` service menus, or a QMI `NAS SET_BAND_PREFERENCE`).
This module deliberately does **not** do that:

- It is a **baseband/modem configuration write**, which the project's safety
  guarantees forbid (no IMEI/baseband/SIM modification, no carrier-provisioning
  bypass). A bad band mask can drop you to no-service or brick the radio until a
  modem reflash.
- It is **unnecessary**. With no band lock set (the default), the modem already
  scans and uses every band it supports and the network broadcasts. Band 3 in
  the sample above is simply the serving cell the network selected — not a lock.

So the safe, effective definition of "unlock all bands, maximize speed and
stability" here is: **all RATs enabled (done) + prefer NR when thermals allow +
carrier aggregation on (done)**. Beyond that, band selection is the network's job.

## Vietnam band reference (SM-F731B supports these)

| Tech | Bands commonly deployed in VN | Notes |
|---|---|---|
| LTE (4G) | B1 (2100), B3 (1800), B7 (2600), B8 (900), B40 (2300), B38/B41 (2600 TDD) | Viettel/Vinaphone/Mobifone; B3 + B1 + B7 carry most capacity, aggregated (LTE-CA) |
| 5G NR | n78 (3500 TDD), n1 (2100), n3 (1800), n28 (700), n41 (2600), n77 (3700) | n78 is the main VN mid-band; NSA anchored on LTE, some SA |

The Z Flip 5 (Snapdragon 8 Gen 2 / X70 modem) supports all of the above; the
limiting factor is coverage + SIM provisioning, not this module.

## Maximize speed / stability — the levers that ARE safe

1. **Keep NR enabled** — already true (`nr_enabled: true`). `POST /v1/prefer5g`
   nudges NR preference and is **thermally gated** (refused when HOT/COOLDOWN).
2. **Carrier aggregation** — on (`LTE_CA`, `carrier_aggregation: true` in
   `/v1/signal`), aggregates multiple LTE bands for throughput.
3. **Thermal headroom = stability.** The single biggest cause of throughput
   collapse on a phone-as-modem is thermal throttling. Keep it cool (airflow, no
   direct sun, `POST /v1/cooldown` when hot). The module never disables thermal
   mitigation — that is the safety line and also what keeps sustained speed.
4. **Good placement / external antenna** — band selection and MIMO depend on
   signal; `/v1/signal` (RSRP/RSRQ/SINR) tells you when to reposition.

## Verify on device

```sh
su -c 'cmd phone get-allowed-network-types-for-users'   # should list ...LTE_CA|NR
curl -s -H "Authorization: Bearer $READ_STATUS" http://127.0.0.1:18080/v1/bands
curl -s -H "Authorization: Bearer $READ_STATUS" http://127.0.0.1:18080/v1/signal
```

If NR is ever missing from `allowed_types`, restore the full RAT set (owner, once):

```sh
su -c 'cmd phone set-allowed-network-types-for-users NR|LTE_CA|LTE|HSPA+|HSPA|HSUPA|HSDPA|UMTS|EDGE|GPRS|GSM USER'
```

This is a **RAT** allowance (official Android API), not a modem band write.
