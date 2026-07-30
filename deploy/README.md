# Deployment files

`install.sh` creates `.env`, secrets, application data, and root-only installer
state at runtime. They are machine-local and must not be committed.
Keep real assignment JSON outside the repository; `deploy/` also ignores
`device-assignments.json` and `*.local.json` as a last-line safeguard.

Simple mode stops, disables, and masks host modem services, then gives the
hardware container normal ModemManager discovery. It never mounts host `/proc`
or the host system D-Bus.

Advanced mode manages only ModemDeck's assigned devices. The installer and
Compose stack do not configure, stop, restart, or otherwise manage the host
ModemManager, udev rules, filter policy, Polkit, firewall/port rules, or
services. The operator must ensure that every assigned device is left unclaimed
by host software.

The base stack has three services:

- `modemdeck` runs Nginx with two isolated listeners. Compose-only HTTP `7575`
  proxies `/api/*` and returns 404 for every other path. HTTPS `7577` serves the
  Web UI and is the only listener published to the host. Plain HTTP sent to
  `7577` is redirected to HTTPS on the same address.
- `api` runs the Go HTTP API on `8080` inside the Compose network. It has
  no published host port.
- `hardware` owns the modem and host data plane.

The administrator-facing **Web certificate** setting manages only HTTPS
listener `7577`. The API container atomically stores either the automatic
certificate or an uploaded PEM certificate chain and private key in the
persistent TLS directory. The Nginx container mounts that directory read-only,
detects source changes, and reloads the selected certificate without a
container restart. Cloudflare edge certificates are outside this setting.

Cloudflare Tunnel and Realtime TURN are installer options, not editable
application settings. Create a remotely-managed Tunnel and configure its
public hostnames as needed. The available origins are the API-only
`http://modemdeck:7575` listener and the Web
`https://modemdeck:7577` listener. With the default self-signed Web
certificate, enable **No TLS Verify** in that Published Application's origin
TLS settings. The connector requires only a Tunnel token:

```sh
sudo ./install.sh \
  --cloudflare-token-file /root/modemdeck-cloudflare.token
```

For iOS call media, also create a Realtime TURN key and add its credentials:

```sh
sudo ./install.sh \
  --cloudflare-token-file /root/modemdeck-cloudflare.token \
  --cloudflare-turn-key-id REPLACE_WITH_TURN_KEY_ID \
  --cloudflare-turn-token-file /root/modemdeck-cloudflare-turn.token
```

The installer enables `docker-compose.cloudflare.yml` and, when TURN is
configured, `docker-compose.cloudflare-turn.yml`. Credentials are copied into
restricted file secrets. Both Nginx listeners are reachable from the connector;
the user decides which ingress rules Cloudflare publishes. The Go API
automatically selects the unique pathless ingress whose service is
`http://modemdeck:7575` and verifies that public route before issuing a pairing
credential. Tunnel configuration changes are picked up without reinstalling.
The iOS settings page periodically refreshes both API and Web ingress hostnames.
With the override disabled, an ambiguous API ingress, or a disconnected
connector, users may revoke an existing credential but cannot create one.
Paired clients receive short-lived relay-only ICE configurations, while the
long-lived TURN API token remains available only to the API container.
`--disable-cloudflare-turn` removes TURN from the running stack while retaining
the Tunnel connector and persisted credentials.

There is no LAN discovery or LAN endpoint in an iOS QR payload. Every iOS
client uses the single Cloudflare API HTTPS origin discovered from the active
Tunnel ingress. An administrator permits pairing per account; the permitted
user creates and revokes their own credential.

Copy `advanced-assignment.example.json` outside the repository, replace every
placeholder with a stable USB serial or physical port path, and run:

```sh
sudo ./install.sh \
  --mode advanced \
  --assignment-file /etc/modemdeck/device-assignments.json
```

The advanced override alone mounts the assignment file, host `/proc`, and the
host system D-Bus socket into the hardware container. The device owner validates
each assignment, rejects ambiguous or host-owned devices, and reports only the
assigned kernel ports to the private ModemManager instance. A conflict or a
required device that is not ready makes startup fail closed.

An assignment file is not a host-side partition. Before starting advanced mode,
the operator must configure the host so its software does not own or hold open
any assigned device. ModemDeck does not prescribe or apply that host policy. It
only checks the resulting process and D-Bus state; a conflict keeps the hardware
container unhealthy instead of stealing or sharing the device.

Both modes use separate persistent volumes for `/run/modemdeck` and
`/var/lib/ModemManager`. The application receives only a read-only
`/run/modemdeck` mount; it never receives hardware state, host devices, host
D-Bus, or host process visibility.
