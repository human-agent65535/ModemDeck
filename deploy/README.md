# Deployment

## Prerequisites

ModemDeck requires Linux on x86_64 or arm64, Docker Engine, and the Docker
Compose plugin. Simple mode additionally requires systemd. The host does not
need Go, Node.js, a C toolchain, or its own ModemManager installation.

Run the installer from a stable source release. Use `./install.sh --help` to
review every option before changing an existing deployment.

## Quick start

The default simple mode takes exclusive ownership of the host's cellular
modems and disables conflicting host modem services. On a dedicated host, run:

```sh
sudo ./install.sh
```

When installation finishes, open `https://localhost:7577` on the host. The
first visitor creates the administrator username and password through Quick
Start. Later visits use the normal login screen. Changing the administrator
password under **Settings > System** revokes all existing login sessions.

`install.sh` creates `.env`, secrets, application data, and root-only installer
state at runtime. They are machine-local and must not be committed.
Keep real assignment JSON outside the repository; `deploy/` also ignores
`device-assignments.json` and `*.local.json` as a last-line safeguard.

Re-running the installer preserves SQLite data, settings secrets, certificates,
and saved Cloudflare credentials.

## Deployment modes

### Simple mode

Simple mode stops, disables, and masks host modem services, then gives the
hardware container normal ModemManager discovery. It never mounts host `/proc`
or the host system D-Bus.

### Advanced mode

Advanced mode manages only ModemDeck's assigned devices. The installer and
Compose stack do not configure, stop, restart, or otherwise manage the host
ModemManager, udev rules, filter policy, Polkit, firewall/port rules, or
services. The operator must ensure that every assigned device is left unclaimed
by host software.

## Service boundary

The base stack has three services:

- `modemdeck` runs Nginx with three isolated listeners. Compose-only HTTP
  `7575` proxies `/api/*` and returns 404 for every other path. Compose-only
  HTTP `7576` serves the Web UI and same-origin API to Cloudflare Tunnel.
  HTTPS `7577` serves the local Web UI and is the only listener published to
  the host. Plain HTTP sent to `7577` is redirected to HTTPS on the same
  address.
- `api` runs the Go HTTP API on `8080` inside the Compose network. It has
  no published host port.
- `hardware` owns the modem and host data plane.

## Upgrades

On an existing deployment, `install.sh` compares the previously deployed Git
revision, resolved Compose service hashes, and fingerprints of external
assignment/media-binding files. It builds and replaces only affected services,
using independent image tags for API, Web, and Hardware. API, Web,
cloudflared, or TURN-only changes retain the running Hardware/ModemManager
container. Agent/Hardware inputs, Hardware mode, device assignments, and media
bindings select Hardware for replacement. `--rebuild-all` is the explicit
escape hatch that rebuilds every image and force-recreates the complete stack.

## HTTPS certificates

The administrator-facing **Web certificate** setting manages only HTTPS
listener `7577`. The API container atomically stores either the automatic
certificate or an uploaded PEM certificate chain and private key in the
persistent TLS directory. The Nginx container mounts that directory read-only,
detects source changes, and reloads the selected certificate without a
container restart. Cloudflare edge certificates are outside this setting.

`--bind-address` and `--port` control only the host-published local HTTPS Web
UI, which binds to host loopback by default. They do not change Cloudflare
origins or appear in an iOS QR payload.

## Cloudflare Tunnel and Realtime TURN

Cloudflare Tunnel and Realtime TURN are installer options, not editable
application settings. Create a remotely-managed Tunnel and configure its
public hostnames as needed. The available origins are the API-only
`http://modemdeck:7575` listener and the Web
`http://modemdeck:7576` listener. The connector requires only a Tunnel token:

```sh
sudo ./install.sh \
  --cloudflare-token-file /root/modemdeck-cloudflare.token
```

For Cloudflare Web and iOS call media, also create a Realtime TURN key and add
its credentials:

```sh
sudo ./install.sh \
  --cloudflare-token-file /root/modemdeck-cloudflare.token \
  --cloudflare-turn-key-id REPLACE_WITH_TURN_KEY_ID \
  --cloudflare-turn-token-file /root/modemdeck-cloudflare-turn.token
```

The installer enables `docker-compose.cloudflare.yml` and, when TURN is
configured, `docker-compose.cloudflare-turn.yml`. Credentials are copied into
restricted file secrets. Both Tunnel origins are reachable from the connector;
the user decides which ingress rules Cloudflare publishes. The Go API
discovers every pathless ingress whose service is `http://modemdeck:7575` and
verifies each public route. Pairing uses one verified address selected by the
user. Tunnel changes are scanned at startup and while running without
reinstalling; administrators can also rescan manually. With the connector
disabled or no verified API route, users may revoke an existing credential but
cannot create one.

Paired clients receive short-lived relay-only ICE configurations, while the
long-lived TURN API token remains available only to the API container.
`--disable-cloudflare-turn` removes TURN from the running stack while retaining
the Tunnel connector and persisted credentials. `--disable-cloudflare` removes
the connector and disables new iOS pairing while retaining application data.

There is no LAN discovery or LAN endpoint in an iOS QR payload. Every iOS
pairing code contains one selected Cloudflare API HTTPS origin, while multiple
API ingresses may coexist. An administrator permits pairing per account; the
permitted user creates and revokes their own credential. Creating a code leaves
the credential pending until its first authenticated iOS API request; closing
the QR does not cancel that wait. The credential remains valid until the user
or an administrator revokes it.

## Advanced device assignments

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

## Persistent mounts

Both modes use separate persistent volumes for `/run/modemdeck` and
`/var/lib/ModemManager`. The application receives only a read-only
`/run/modemdeck` mount; it never receives hardware state, host devices, host
D-Bus, or host process visibility.
