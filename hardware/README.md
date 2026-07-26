# ModemDeck hardware runtime

This directory builds the privileged hardware-side image. It is separate from
the unprivileged ModemDeck application image and does not change host packages.

## Process and write boundaries

PID 1 directly supervises these container-local processes:

1. a private `dbus-daemon`;
2. a custom ModemManager;
3. the repository's `modemdeck-agent`;
4. `modemdeck-device-owner` in advanced mode only.

It does not run systemd, udevd, NetworkManager, or Polkit. D-Bus activation is
disabled because the private bus configuration has no service directories.
If any supervised process exits, PID 1 terminates the remaining process groups
and returns a failure status.

The root filesystem is designed to be read-only. Only these runtime locations
are writable:

| Path | Mount | Purpose |
| --- | --- | --- |
| `/var/lib/ModemManager` | dedicated persistent volume | ModemManager state |
| `/run/dbus` | private tmpfs | private system-bus socket |
| `/run/modemdeck` | dedicated persistent/shared volume | Agent socket, device-owner status, bearer ownership, network ownership, and temporary files |

`TMPDIR` is `/run/modemdeck/tmp`. PID 1 performs a write probe against all
three locations before starting D-Bus. Agent state is explicitly passed as
`/run/modemdeck/bearers.json` and `/run/modemdeck/network.json`.

The application container may receive `/run/modemdeck/agent.sock`; it must not
receive the private D-Bus socket, `/dev`, sysfs, host udev state, or the host
visibility mounts used by advanced mode.

## Pinned ModemManager build

Builder and runtime stages use the pinned Debian 13 `trixie-slim` multi-platform
image. The build accepts only Debian stable/security source version
`modemmanager 1.24.0-1+deb13u1`; it fails instead of silently moving to another
version when that source is unavailable. It compiles for the target
architecture with:

```text
udev=true
at_command_via_dbus=true
polkit=no
systemd_suspend_resume=false
systemd_journal=false
```

The runtime asserts ModemManager 1.24.0, `libqmi >= 1.36`, and
`libmbim >= 1.32`. It also fails the image build if ModemManager has an
unresolved dependency, links to Polkit or systemd, or installs a systemd,
udevd, NetworkManager, or Polkit daemon package. Exact source and dependency
versions are recorded in:

```text
/usr/share/modemdeck/modemmanager-build-options
```

## Audio bindings

Compose mounts the host path in `MODEMDECK_MEDIA_BINDINGS_FILE` read-only at
`/etc/modemdeck/media-bindings.json`. The checked-in default contains no
bindings, so the Agent does not advertise browser call audio without a verified
media endpoint. `install.sh --media-bindings-file FILE` selects an explicit
deployment file.

Each binding maps the exact `audio_port` reported by ModemManager to either an
`alsa-pcm` device name or a `char-pcm` device node:

```json
{
  "bindings": [
    {
      "audio_port": "usb:REPLACE_WITH_PHYSICAL_PORT",
      "backend": "alsa-pcm",
      "endpoint": "hw:CARD=REPLACE_WITH_CARD,DEV=0"
    }
  ]
}
```

Bindings are hardware-specific deployment data and should remain outside the
repository.

## Simple mode

Simple mode leaves ModemManager automatic discovery enabled:

```sh
modemdeck-hardware-entrypoint --mode simple
```

It does not start the device owner and never reads host `/proc` or the host
system D-Bus. Device ownership outside ModemDeck remains the user's
responsibility.

## Advanced mode

Advanced mode always starts ModemManager with `--no-auto-scan` and requires an
explicit assignment file:

```sh
modemdeck-hardware-entrypoint \
  --mode advanced \
  --assignments /etc/modemdeck/device-assignments.json
```

Start from
[`config/device-assignments.example.json`](config/device-assignments.example.json).
Each assignment uses one stable physical selector:

- an exact physical path below `/sys/devices`; or
- USB VID/PID plus exactly one USB serial or physical `/devices/...` port path.

Kernel ordinals such as `ttyUSB0` are discovered ports, never assignment
identities. `modemdeck-device-owner inventory` prints the stable values visible
inside the hardware container. `required_at_startup: false` permits a device
to be absent at startup and added later without making a no-modem runtime
unhealthy.

Assignments control only which devices the private ModemDeck ModemManager
receives. They do not configure, coordinate, inhibit, restart, or otherwise
change any external device manager, udev policy, or port assignment.

At startup and on each poll, the owner resolves the assignments against real
sysfs objects and host udev records. After a short settle period it reports the
matched ports through the real ModemManager 1.24
`ReportKernelEvent(a{sv})` method, using only the documented `action`,
`subsystem`, `name`, and shared physical `uid` fields. Runtime disappearance
reports corresponding `remove` events. There is no generated
`--initial-kernel-events` file and no synthetic fallback.

The owner fails closed when:

- one assignment matches multiple physical devices;
- multiple assignments match one physical device;
- a required startup device is missing;
- a matched port or host udev record is unavailable;
- another process has an assigned device node open;
- an external hardware manager exports an assigned physical path or port;
- advanced host visibility cannot be read reliably.

### Advanced-only host visibility

Advanced mode needs the following read-only host views:

```text
host /proc                 -> /run/host-proc
host system bus directory -> /run/host-dbus
host /run/udev             -> /run/udev
```

Keep the container's normal private PID namespace; bind host `/proc` at the
separate path instead of using `--pid=host`. The host system bus address in the
assignment file defaults to
`unix:path=/run/host-dbus/system_bus_socket`.

Reading external process descriptors for conflict detection requires the
advanced hardware container to add `CAP_SYS_PTRACE`. Simple mode neither needs
that capability nor receives the external visibility mounts.

The separate host `/proc` and system-bus views exist only for conflict
detection. Host D-Bus access is limited to read-only `NameHasOwner` and
`GetManagedObjects` calls with service autostart disabled. The owner never
changes external processes, D-Bus services, device policy, or udev state.
Finding an assigned device already open or exported is a fatal external
ownership conflict; ModemDeck reports it and leaves resolution to the user.
Simple mode needs none of these views.

## Health

The image healthcheck validates:

1. the private D-Bus socket and ModemManager bus owner;
2. a fresh, ready device-owner status in advanced mode;
3. a real HTTP `GET /v1/health` over the Agent Unix socket;
4. HTTP 200, `status=ok`, `provider.available=true`, and a non-empty
   `provider.boot_epoch`.

No physical modem is required for the runtime itself to be healthy.

## Build and test

Run static, supervisor, and device-owner tests:

```sh
./hardware/tests/run.sh
```

Build an amd64+arm64 OCI archive:

```sh
./hardware/build-image.sh
```

Build and load one native image, then verify a read-only root filesystem with
only the declared writable mounts:

```sh
./hardware/build-image.sh \
  --platform linux/amd64 \
  --output load \
  --tag modemdeck-hardware:dev

./hardware/tests/runtime-readonly-test.sh modemdeck-hardware:dev

./hardware/tests/runtime-advanced-readonly-test.sh modemdeck-hardware:dev
```
