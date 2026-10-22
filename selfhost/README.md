# Self-hosted ntfy

Run a private [ntfy](https://ntfy.sh) server to receive push notifications from
the Z Flip 5 modem, such as thermal and data-cap alerts. The server depends on
no third-party cloud. The phone daemon stays bound to loopback, and the only
path in is your tailnet.

For the alert path on the phone, see [ntfy.md](../docs/ntfy.md).

## Prerequisites

- A Linux host with Docker and Docker Compose.
- The host joined to the same tailnet as the phone. Run `tailscale up`.

## Start the server

1. Open `ntfy/server.yml` and set `base-url` to the tailnet IP of this host.
2. Start the container:

   ```sh
   docker compose up -d ntfy
   ```

The container publishes port 8080 on `127.0.0.1` only. To reach it from other
devices on the tailnet, run `tailscale serve` on the host.

## Create an admin user and token

The server denies all access by default. Create a user and an access token:

```sh
docker exec -it zf5-ntfy ntfy user add --role=admin zf5
docker exec -it zf5-ntfy ntfy token add zf5
```

The first command prompts you for a password. The second command prints a
token that starts with `tk_`. To use a non-admin user, grant it the topic:

```sh
docker exec -it zf5-ntfy ntfy access zf5 zf5-modem rw
```

## Subscribe to alerts

1. Install the ntfy app on your phone or desktop.
2. Add the server `http://<host-tailnet-ip>:8080`.
3. Subscribe to the topic `zf5-modem`.
4. In the server settings, enter the access token.

## Security notes

- ntfy has no public inbound port. It listens on loopback and is reachable only
  through your tailnet.
- Access tokens stay local. Rotate a token with `ntfy token remove`, then
  create a new one.
