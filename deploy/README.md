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

The default install uses published images. Use `sudo ./install.sh --git` to
build the current checkout instead.

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

The release stack has four services:

- `modemdeck` runs Nginx with three isolated listeners. Compose-only `7575`
  proxies `/api/*` and returns 404 for every other path. Compose-only `7576`
  serves the Web UI and same-origin API to Cloudflare Tunnel. They use HTTP by
  default and switch together to verified HTTPS with HTTP/2 only while a
  Cloudflare Origin CA certificate is installed.
  HTTPS `7577` serves the local Web UI and is the only listener published to
  the host. It accepts HTTP/1.1 and HTTP/2 over TCP plus HTTP/3 over UDP, and
  advertises the host port selected by `--port`. Plain HTTP sent over TCP to
  `7577` is redirected to HTTPS on the same address.
- `api` runs the Go HTTP API on `8080` inside the Compose network. It has
  no published host port.
- `hardware` owns the modem and host data plane.
- `updater` applies release updates and is the only service with Docker socket
  access. It has no published host port.

## Upgrades

Published-image deployments update from **Settings > About** and replace only
changed containers. Hardware changes require confirmation.

For `--git` deployments, `install.sh` compares the previously deployed Git
revision, resolved Compose service hashes, and fingerprints of external
assignment/media-binding files. It builds and replaces only affected services,
using independent image tags for API, Web, and Hardware. API, Web,
cloudflared, or TURN-only changes retain the running Hardware/ModemManager
container. Agent/Hardware inputs, Hardware mode, device assignments, and media
bindings select Hardware for replacement. `--rebuild-all` is the explicit
escape hatch that rebuilds every image and force-recreates the complete stack.

## Apple Push (APNs and PushKit)

The API can send standard APNs alerts for incoming SMS and PushKit VoIP pushes
for incoming calls. It uses each iPhone registration's own `development` or
`production` environment, so a development-signed app and a TestFlight build
can coexist. Incoming calls suppressed by the effective do-not-disturb policy
are not sent to CallKit.

Provider credentials are server secrets. Do not commit a real Bundle ID, Apple
Team ID, Key ID, or `.p8` file. The default published deployment reads
`${MODEMDECK_DATA_DIR}/apple-push/config.json` through the API's existing data
mount; copy [`apple-push.example.json`](apple-push.example.json) there and
replace every placeholder locally. A relative `private_key_file` is resolved
beside `config.json`:

```text
data/apple-push/
├── config.json
└── AuthKey_local.p8
```

Both files must be readable by the API container's configured
`MODEMDECK_UID` (default `10001`) and should not be readable by other users.
Restart `modemdeck-api` after creating or changing them. With no config file,
push delivery remains disabled and the rest of ModemDeck starts normally.

A custom deployment can instead provide all four environment variables to the
API container: `MODEMDECK_APNS_TEAM_ID`, `MODEMDECK_APNS_KEY_ID`,
`MODEMDECK_APNS_BUNDLE_ID`, and `MODEMDECK_APNS_PRIVATE_KEY_FILE`. Partial or
mixed file/environment configuration fails closed at startup. The token-based
Apple provider key is used only to create short-lived provider JWTs; registered
device tokens and the client-reported Bundle ID remain in SQLite. ModemDeck
clears an individual token only when APNs reports that exact token as invalid.

## Release images

Pushing an annotated stable tag that exactly matches `v$(cat VERSION)` starts
the `Publish release images` GitHub Actions workflow. It builds Linux `amd64`
and `arm64` variants and publishes application packages under the repository
owner's GHCR namespace:

- `ghcr.io/OWNER/modemdeck:vX.Y.Z` for the API
- `ghcr.io/OWNER/modemdeck-web:vX.Y.Z` for the Web gateway
- `ghcr.io/OWNER/modemdeck-updater:vX.Y.Z` for the updater
- `ghcr.io/OWNER/modemdeck-hardware:vX.Y.Z` for the Hardware runtime

The workflow uses the repository `GITHUB_TOKEN`; it needs no registry secret.
Each image also receives a `sha-COMMIT` tag and GitHub build-provenance
attestation. Deployments and update tooling must resolve and retain the
published digest instead of following a mutable tag. The workflow deliberately
does not publish `latest`.

To backfill images for an existing release that contains the updater workflow
and release manifest, run it manually with `release_tag` set to that tag. Select
`include_hardware` only when the backfill also needs a Hardware baseline. The
workflow checks out the tagged commit, verifies that its `VERSION` matches,
requires the existing tag to be annotated, and builds only that historical
source. Never move or recreate a published release tag.

`VERSION` is the only ModemDeck release version. The release workflow compares
each container's runtime inputs with the previous stable tag. Changed containers
are rebuilt; unchanged multi-architecture manifests are copied to the new tag
without rebuilding. The updater resolves that shared tag for every container
and compares immutable digests, so only containers whose image actually changed
are replaced.

Verify package visibility after its first publication. Public packages support
anonymous device pulls; if a package is private, either change it to public in
its GitHub package settings or provision a device credential with
`read:packages`.

Release images are build artifacts, not authority for an unattended Hardware
restart. API and Web may be updated together after staging both digests;
Hardware replacement remains an explicit maintenance operation because it
restarts the private D-Bus, ModemManager, and Agent data plane.

## HTTPS certificates

The administrator-facing **Web certificate** setting manages only HTTPS
listener `7577`. The API container atomically stores either the automatic
certificate or an uploaded PEM certificate chain and private key in the
persistent TLS directory. The Nginx container mounts that directory read-only,
detects source changes, and reloads the selected certificate without a
container restart. Cloudflare edge certificates are outside this setting.

The administrator-facing **Cloudflare Origin TLS** setting is separate. With
no saved certificate, `7575` and `7576` remain the existing private HTTP
origins. Saving a current Cloudflare Origin CA PEM certificate and matching
private key switches both listeners to HTTPS with HTTP/2. The API returns
success only after both listeners present that exact certificate over HTTP/2;
if activation fails, it removes the bundle and returns an error. Removing an
active bundle switches both listeners back to HTTP. The certificate
must cover every currently discovered API and Web Tunnel hostname. Invalid,
expired, mismatched, or non-Cloudflare material is rejected before it changes
the active listeners. An installed certificate that later expires remains
selected and is reported as expired rather than silently changing the origin
protocol. This setting never changes listener `7577`.

`--bind-address` and `--port` control only the host-published local HTTPS Web
UI, which binds to host loopback by default. They do not change Cloudflare
origins or appear in an iOS QR payload. To use HTTP/3 outside the host, allow
the selected port over both TCP and UDP in the host firewall; the installer
does not modify firewall policy.

## Cloudflare Tunnel and Realtime TURN

Cloudflare Tunnel and Realtime TURN are installer options. Create a
remotely-managed Tunnel and configure its public hostnames as needed. The
default origins are the API-only `http://modemdeck:7575` listener and the Web
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
discovers every pathless ingress for the HTTP or HTTPS forms of the built-in
`7575` and `7576` origins and verifies each public API route. Pairing uses one
verified address selected by the user. Tunnel changes are scanned at startup and while running without
reinstalling; administrators can also rescan manually. With the connector
disabled or no verified API route, users may revoke an existing credential but
cannot create one.

Cloudflare's edge protocol and the connector-to-origin protocol are separate.
The remotely managed connector token can read its assigned routes but cannot
rewrite them. After uploading an Origin CA certificate, the administrator must
change the two published application services to
`https://modemdeck:7575` and `https://modemdeck:7576`, enable `http2Origin`
and `matchSNItoHost`, and keep `noTLSVerify` disabled. The External Access page
keeps connector reachability in the Tunnel card and certificate state in the
Origin TLS card. Reverse those Tunnel changes before removing the certificate.
The installer and Web application do not request a broader Tunnel-edit API
credential.

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
