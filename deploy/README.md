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
  Web UI and is the only listener published to the host.
- `api` runs the Go HTTP API on `8080` inside the Compose network. It has
  no published host port.
- `hardware` owns the modem and host data plane.

Cloudflare Tunnel is an installer option, not an editable application setting.
Create a remotely-managed Tunnel, configure its public-hostname service as
`http://modemdeck:7575`, and store its token in a regular root-readable file. Then
run:

```sh
sudo ./install.sh \
  --cloudflare-token-file /root/modemdeck-cloudflare.token \
  --cloudflare-hostname deck.example.com
```

The installer copies the token into `secrets/cloudflare-tunnel-token` with
restricted permissions and enables `docker-compose.cloudflare.yml`. The
connector uses HTTP to reach the API-only Nginx listener over the private
Compose network; Cloudflare provides the public HTTPS API used by iOS. It does
not publish the Web UI. The Go API checks connector readiness before issuing an
iOS pairing credential. With the override disabled or the connector
disconnected, users may revoke an existing credential but cannot create one.

There is no LAN discovery or LAN endpoint in an iOS QR payload. Every iOS
client uses the single installation-managed Cloudflare HTTPS origin. An
administrator permits pairing per account; the permitted user creates and
revokes their own credential.

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
