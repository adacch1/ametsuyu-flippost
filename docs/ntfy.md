# ntfy alerts

[ntfy](https://ntfy.sh) is a small publish and subscribe notification server.
The module ships a device-local runner that reads the daemon and publishes
alerts to your ntfy servers. Your phones and watches subscribe to the topic
`zf5-modem`.

## What the runner reports

The runner polls the daemon on loopback and sends an alert when one of these
changes: temperature, battery level, power loss or return, data cap, signal,
hotspot state, or an unreachable daemon. Signal and battery come from
`/v1/summary`. The first poll only records state, and later changes raise
alerts. The runner never writes to the daemon.

## Runner files

The runner ships in the module under `/data/adb/modules/zflip5_modem/ntfy/`:

- `launch.cjs` reads the daemon config and `ntfy.json`, sets the environment,
  and starts the runner. Credentials never appear in arguments or logs.
- `runner.mjs` polls the daemon and publishes to every server.
- `alerts.mjs` holds the alert rules.

The boot script `/data/adb/service.d/60-zf5-ntfy.sh` (source
`magisk/ntfy-service.sh`) restarts the runner when it stops. It needs the
Termux Node runtime and starts only when `ntfy.json` exists.

## Configure the runner

Create `/data/adb/zflip5-modem/ntfy.json` with mode `0600`:

```json
{"servers": [{"url": "https://ntfy.example.net", "token": "tk_example"}]}
```

Use a write-only publisher token for each server. Do not commit this file to
Git. The runner logs to `/data/adb/zflip5-modem/ntfy.log`, which the boot
script keeps bounded.

Each server receives every alert independently with a 15-second timeout. A
failed publish writes a log line and does not block the other servers. The runner
does not queue failed publishes.

## Host your own server

To run a private server, follow the [self-hosted ntfy guide](../selfhost/README.md).
The server denies all access by default, so create a user and a token first.

## Subscribe from a phone

1. Install the ntfy app from the App Store, Google Play, or F-Droid.
2. In the app settings, set the default server to your server URL and add the
   access token.
3. Subscribe to the topic `zf5-modem`.

## Publish a test message

```sh
curl -H "Authorization: Bearer $NTFY_TOKEN" \
     -H "Title: test" -H "Priority: high" \
     -d "hello from the modem" \
     https://ntfy.example.net/zf5-modem
```

## Run the tests

From the repository root, run:

```sh
node --test tests/ntfy/
```

## Limits

A device-local runner can report a stalled daemon while Android and the
network still work. It cannot report its own power or network loss. Use an
external check for that.
