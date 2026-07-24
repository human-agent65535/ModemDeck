# ModemDeck

[![License: PolyForm Noncommercial 1.0.0](https://img.shields.io/badge/License-PolyForm--Noncommercial--1.0.0-blue.svg)](LICENSE)

ModemDeck is a self-hosted console for cellular calls, messages, contacts, and
attached modem lines. It is inspired by
[VoHive](https://github.com/iniwex5/vohive) and is maintained as an independent
rewrite rather than an official VoHive successor. The communication-first
interface also takes design cues from Google Voice.

## Features

- contacts, SMS conversations, call history, dialing, DTMF, answer, reject,
  and hangup;
- multiple modem lines without an artificial product-level line limit;
- global and per-line receive-calls or do-not-disturb policies;
- Telegram bots with encrypted credentials and explicit line scopes;
- browser WebRTC audio using Opus, explicit host PCM bindings, and Ogg Opus
  call recording;
- per-line traffic usage and HTTP/SOCKS5 proxies bound to modem bearers;
- modem inventory and configuration through ModemManager and a dedicated Linux
  host agent.

## Design

The Web application runs in an unprivileged container and owns authentication,
the UI, communication workflows, and SQLite data. It does not receive `/dev`,
the host D-Bus socket, host networking, or Linux capabilities.

The host agent is the only component that talks to ModemManager and hardware.
The application reaches it through a permission-restricted Unix socket.
Unsupported hardware operations fail explicitly; ModemDeck does not guess a
device path or silently switch control backends.

## Build

Builds and checks use the pinned Docker toolchains. Go, Node.js, npm, C
toolchains, and libopus development packages are not required on the host.

```sh
make check
make build
```

`make check` runs the Go, Host Agent, Web, Compose, and Dockerfile checks.
`make build` writes release artifacts to `dist/`.

## Status

Software capability does not imply support for every modem, firmware, SIM, or
carrier. Voice media requires an exact ModemManager audio port mapped to an
explicit PCM binding. Unknown devices remain unsupported instead of receiving
guessed vendor commands.

Technical notes live under [`docs/`](docs/), including the
[architecture](docs/architecture.md), [call and WebRTC validation
plan](docs/call-webrtc-capability-and-test-plan.md), [device
configuration](docs/device-configuration.md), and [VoLTE extension
boundary](docs/volte-extension.md).

## License

ModemDeck is distributed under the
[PolyForm Noncommercial License 1.0.0](LICENSE). The original VoHive notice and
project relationship are recorded in [NOTICE.md](NOTICE.md).
