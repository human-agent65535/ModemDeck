# ModemDeck

[![License: PolyForm Noncommercial 1.0.0](https://img.shields.io/badge/License-PolyForm--Noncommercial--1.0.0-blue.svg)](LICENSE)

ModemDeck is a self-hosted console for cellular calls, messages, contacts,
traffic, and attached modem lines. Early product ideas were inspired by
[VoHive](https://github.com/iniwex5/vohive), whose author we thank; ModemDeck is
independent, has no official relationship with that project, and is not
endorsed by it.

## Features

- Multi-line dashboard, line labels, a default line, and per-contact preferred
  lines.
- SMS conversations with line-aware replies, read state, reconnect
  reconciliation, and real-time updates over SSE.
- Call history and control, DTMF, global and per-line incoming-call policies,
  and recording controls when a verified media path is available.
- Per-line data usage, active connection details, and HTTP/SOCKS5 proxies bound
  to modem bearers.
- SIM and eSIM identity, slot status, and masked EID when exposed by
  ModemManager.
- Separate home and serving operator identity, including roaming state.
- Accessible in-app confirmation dialogs for consequential device, contact,
  and proxy actions.
- Telegram bots with encrypted credentials and explicit line scopes.

## Interface

| Multi-line dashboard | Line-aware messages |
| --- | --- |
| ![Dashboard with two synthetic modem lines, activity, and dialer](docs/images/readme-dashboard.png) | ![Synthetic SMS conversation with line labels and reply routing](docs/images/readme-messages.png) |
| Call history and recording | Network details and confirmation |
| ![Synthetic call details with recording playback and dialer](docs/images/readme-calls.png) | ![Synthetic modem network settings with an in-app confirmation dialog](docs/images/readme-network-settings.png) |

## Architecture

The Web application runs in an unprivileged container and owns authentication,
communication workflows, and SQLite data. A dedicated Linux host agent is the
only component that talks to ModemManager and hardware. The application reaches
it through a permission-restricted Unix socket and does not receive `/dev`, the
host D-Bus socket, host networking, or Linux capabilities.

Unsupported hardware operations fail explicitly. ModemDeck does not guess a
device path or silently switch control backends.

## Hardware compatibility

### Quectel EC2x USB

The Linux baseline uses Quectel EC2x USB, `qmi_wwan`, ModemManager, and
`usbnet=0`. Command definitions are in Quectel's
[EC2x/EG2x/EG9x/EM05 QCFG AT Commands Manual V1.0](https://www.quectel.com/content/uploads/2024/02/Quectel_EC2xEG2xEG9xEM05_Series_QCFG_AT_Commands_Manual_V1.0.pdf).

The validated USB identity and interface configuration is:

```text
AT+QCFG="usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,0
                         |      | | | | | | |
                         |      | | | | | | +-- UAC: disabled
                         |      | | | | | +---- ADB: disabled
                         |      | | | | +------ USB network: enabled
                         |      | | | +-------- modem port: enabled
                         |      | | +---------- AT port: enabled
                         |      | +------------ NMEA port: enabled
                         |      +-------------- diagnostic port: enabled
                         +--------------------- VID:PID 2c7c:0125
```

This setting is saved automatically and takes effect after a module restart. It
stays with the module when moved to another host. It changes USB descriptors
and interfaces only; it does not install drivers or change the hardware model.

The penultimate `usbcfg` value controls ADB. `usbnet` selects the network
protocol separately:

```text
AT+QCFG="usbnet",0   # RmNet/QMI
AT+QCFG="usbnet",1   # ECM / USB Ethernet
AT+QCFG="usbnet",2   # MBIM
AT+QCFG="usbnet",3   # RNDIS
```

Linux is validated with `usbnet=0`. macOS has no Quectel QMI driver. For direct
USB Ethernet, change the module to `usbnet=1` on an AT-capable host, then
restart it. A third party has validated QDC507 ECM networking on macOS and
iPadOS. This confirms the network interface only, not AT, messaging, or voice.
[Apple documents](https://support.apple.com/en-us/108894) iPadOS USB Ethernet
support. Windows support depends on its QMI, ECM, or MBIM driver.

Voice media also requires UAC:

```text
AT+QCFG="usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,1
```

An enumerated UAC interface does not prove that call audio is routed to it.

### QDC507 firmware and VoLTE

The system loads the VoLTE configuration only for `QDC507GLEFM21`.
ModemManager applies it through the production AT D-Bus interface without
debug mode. A successful configuration does not prove IMS registration, the
live-call bearer, or an audio path.

QDC507 uses custom firmware. Do not flash standard EC25/EG25 firmware onto it.
The current device exposes UAC, but `AT+QPCMV=1,2` returns `ERROR`. Dial,
answer, and hangup work; call audio does not. Browser bidirectional voice
requires a validated host media endpoint.

### eSIM/eUICC

Quectel's
[eSIM AT Commands Manual V1.0.0](https://forums.quectel.com/uploads/short-url/A4rEKqqtf17nlInpzz8jIhF06GN.pdf)
defines profile listing, enabling, disabling, deletion, renaming, and download.
It does not list QDC507 as an applicable model. Read-only results on the current
hardware are:

| Command or property | Result |
| --- | --- |
| `AT+QESIM=?` | `ERROR` |
| `AT+QESIM="eid"` | `ERROR` |
| `AT+QESIM="list"` and slot variants | `ERROR` |
| `AT+QCCID` | readable; identifier not recorded |
| EID, SIM type, and eSIM state from ModemManager 1.24.0 | not reported |

`AT+CMEE=2` did not return a detailed eUICC error. The test changed no profile.
The current firmware has no usable `QESIM` management path, so QDC507 eSIM
profile management is unsupported. VoWiFi is not yet validated.

## Build

Builds and checks use pinned Docker toolchains; Go, Node.js, npm, C toolchains,
and libopus development packages are not required on the host.

```sh
make check
make build
```

`make check` runs the Go, host-agent, Web, Compose, and Dockerfile checks.
`make build` writes release artifacts to `dist/`.

## Run

```sh
docker compose up -d
```

Container deployments serve HTTPS by default. Automatic mode creates a local
CA and leaf certificate; download and trust that CA from Settings. Uploaded
certificates remain user-managed and are not replaced automatically when they
expire.

## License

ModemDeck is distributed under the
[PolyForm Noncommercial License 1.0.0](LICENSE). Required notices and project
attribution are recorded in [NOTICE.md](NOTICE.md).
