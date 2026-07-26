# ModemDeck（中文）

[![许可证：PolyForm Noncommercial 1.0.0](https://img.shields.io/badge/License-PolyForm--Noncommercial--1.0.0-blue.svg)](LICENSE)

ModemDeck 是一个自托管控制台，用于管理蜂窝通话、消息、联系人、流量和已连接的
模组线路。项目的早期产品构想受到
[VoHive](https://github.com/iniwex5/vohive) 启发，我们在此感谢其作者；
ModemDeck 是独立项目，与 VoHive 没有官方关系，也未获得其背书。

## 功能

- 多线路仪表盘、线路标签、默认线路以及每位联系人的首选线路。
- 支持按线路回复、已读状态、重连对账和 SSE 实时更新的短信会话。
- 通话历史与控制、DTMF、全局和单线路来电策略，以及在媒体路径经过验证后提供的
  录音控制。
- 每条线路的数据用量、活动连接详情，以及绑定到模组承载的 HTTP/SOCKS5 代理。
- ModemManager 可提供时显示 SIM/eSIM 身份、卡槽状态和脱敏 EID。
- 分别显示归属运营商和当前服务运营商身份，包括漫游状态。
- 对设备、联系人和代理等重要操作使用无障碍的应用内确认对话框。
- 使用加密凭据并具有明确线路作用域的 Telegram 机器人。

## 界面

| 场景 | 桌面端 | 手机端 |
| --- | --- | --- |
| 多线路仪表盘 | ![桌面端多线路仪表盘，包含合成活动状态和拨号器](docs/images/readme-dashboard.png) | ![手机端多线路仪表盘，包含合成最近活动](docs/images/readme-dashboard-mobile.png) |
| 按线路区分的消息 | ![桌面端合成短信会话，包含线路标签和直接回复](docs/images/readme-messages.png) | ![手机端合成短信会话，包含联系人操作和直接回复](docs/images/readme-messages-mobile.png) |
| 通话历史和录音 | ![桌面端合成通话详情，包含录音播放和拨号器](docs/images/readme-calls.png) | ![手机端合成通话详情，包含录音播放](docs/images/readme-calls-mobile.png) |
| 网络详情 | ![桌面端合成模组网络设置](docs/images/readme-network-settings.png) | ![手机端合成模组网络设置](docs/images/readme-network-settings-mobile.png) |

## 架构

Web 应用运行在非特权容器中，负责身份验证、通信工作流和 SQLite 数据。专用的
Linux 主机代理是唯一与 ModemManager 和硬件通信的组件。应用通过权限受限的
Unix 套接字访问代理；应用容器不会获得 `/dev`、主机 D-Bus 套接字、主机网络
或 Linux capabilities。

不支持的硬件操作会明确失败。ModemDeck 不会猜测设备路径，也不会静默切换控制
后端。

## 硬件兼容性

### Quectel EC2x USB

Linux 基线使用 Quectel EC2x USB、`qmi_wwan`、ModemManager 和
`usbnet=0`。命令定义见 Quectel
[EC2x/EG2x/EG9x/EM05 QCFG AT 命令手册 V1.0](https://www.quectel.com/content/uploads/2024/02/Quectel_EC2xEG2xEG9xEM05_Series_QCFG_AT_Commands_Manual_V1.0.pdf)。

已验证的 USB 身份和接口配置为：

```text
AT+QCFG="usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,0
                         |      | | | | | | |
                         |      | | | | | | +-- UAC：禁用
                         |      | | | | | +---- ADB：禁用
                         |      | | | | +------ USB 网络接口：启用
                         |      | | | +-------- Modem 端口：启用
                         |      | | +---------- AT 端口：启用
                         |      | +------------ NMEA 端口：启用
                         |      +-------------- 诊断端口：启用
                         +--------------------- VID:PID 2c7c:0125
```

该配置自动保存，重启模组后生效。换到另一台主机时配置仍然保留。它只改变 USB
描述符和接口，不会安装驱动或改变硬件型号。

`usbcfg` 倒数第二位控制 ADB；网络协议由 `usbnet` 单独选择：

```text
AT+QCFG="usbnet",0   # RmNet/QMI
AT+QCFG="usbnet",1   # ECM / USB 以太网
AT+QCFG="usbnet",2   # MBIM
AT+QCFG="usbnet",3   # RNDIS
```

Linux 已验证 `usbnet=0`。macOS 没有 Quectel QMI 驱动。要使用直连 USB
以太网，先在支持 AT 命令的主机上改为 `usbnet=1`，再重启模组。第三方已验证
QDC507 的 ECM 模式可用于 macOS 和 iPadOS。这只证明网络接口可用，不代表 AT、
短信或语音功能。[Apple 文档](https://support.apple.com/zh-cn/108894)列出了
iPadOS 的 USB 转以太网支持。Windows 是否可用取决于 QMI、ECM 或 MBIM 驱动。

语音媒体还需要启用 UAC：

```text
AT+QCFG="usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,1
```

枚举出 UAC 接口不代表固件已经提供通话音频路由。

### QDC507 固件与 VoLTE

系统只为 `QDC507GLEFM21` 加载 VoLTE 配置。ModemManager 通过生产级 AT
D-Bus 接口执行配置，不需要调试模式。配置成功不等于 IMS 已注册，也不能证明
实时通话的承载或音频路径。

QDC507 使用定制固件。不要刷入标准 EC25/EG25 固件。当前设备可枚举 UAC，
但 `AT+QPCMV=1,2` 返回 `ERROR`。拨号、接听和挂断可用，通话音频不可用。
浏览器双向语音需要经过验证的主机媒体端点。

### eSIM/eUICC

Quectel
[eSIM AT 命令手册 V1.0.0](https://forums.quectel.com/uploads/short-url/A4rEKqqtf17nlInpzz8jIhF06GN.pdf)
定义了 profile 查询、启用、停用、删除、改名和下载。手册没有将 QDC507 列为
适用型号。当前硬件的只读测试结果如下：

| 指令或属性 | 结果 |
| --- | --- |
| `AT+QESIM=?` | `ERROR` |
| `AT+QESIM="eid"` | `ERROR` |
| `AT+QESIM="list"` 及卡槽参数 | `ERROR` |
| `AT+QCCID` | 可读取，未记录号码 |
| ModemManager 1.24.0 的 EID、SIM 类型和 eSIM 状态 | 未报告 |

`AT+CMEE=2` 也未返回详细 eUICC 错误。测试没有修改任何 profile。当前固件没有
可用的 `QESIM` 管理路径，QDC507 因此不支持 eSIM profile 管理。VoWiFi 尚未验证。

## 构建

构建和检查使用固定版本的 Docker 工具链；主机无需安装 Go、Node.js、npm、
C 工具链或 libopus 开发包。

```sh
make check
make build
```

`make check` 执行 Go、主机代理、Web、Compose 和 Dockerfile 检查。
`make build` 将发布产物写入 `dist/`。

## 运行

```sh
docker compose up -d
```

容器部署默认提供 HTTPS。自动模式会创建本地 CA 和叶证书；请从设置中下载并
信任该 CA。用户上传的证书仍由用户管理，过期时不会被自动替换。

## 许可证

ModemDeck 按
[PolyForm Noncommercial License 1.0.0](LICENSE) 分发。项目与归档分支说明
记录在 [NOTICE.md](NOTICE.md) 中。

---

# ModemDeck (English)

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

| Scene | Desktop | Mobile |
| --- | --- | --- |
| Multi-line dashboard | ![Desktop dashboard with synthetic activity, two modem lines, and the dialer](docs/images/readme-dashboard.png) | ![Mobile dashboard with synthetic recent activity](docs/images/readme-dashboard-mobile.png) |
| Line-aware messages | ![Desktop synthetic SMS conversation with line labels and direct replies](docs/images/readme-messages.png) | ![Mobile synthetic SMS conversation with contact actions and direct replies](docs/images/readme-messages-mobile.png) |
| Call history and recording | ![Desktop synthetic call details with recording playback and the dialer](docs/images/readme-calls.png) | ![Mobile synthetic call details with recording playback](docs/images/readme-calls-mobile.png) |
| Network details | ![Desktop synthetic modem network settings](docs/images/readme-network-settings.png) | ![Mobile synthetic modem network settings](docs/images/readme-network-settings-mobile.png) |

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
[PolyForm Noncommercial License 1.0.0](LICENSE). Project and archive-branch
information is recorded in [NOTICE.md](NOTICE.md).
