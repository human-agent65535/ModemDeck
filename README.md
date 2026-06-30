# VoHive

[![License: PolyForm Noncommercial 1.0.0](https://img.shields.io/badge/License-PolyForm--Noncommercial--1.0.0-blue.svg)](https://polyformproject.org/licenses/noncommercial/1.0.0)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](go.mod)
[![Vue 3](https://img.shields.io/badge/Vue-3-42b883?logo=vue.js)](web/package.json)

> 面向高通 4G/LTE/5G 模组（Quectel EC20/EC25/EC21/EG25/EM20 等）的综合管理与代理服务平台。

VoHive 把模组热插拔管理、SOCKS5/HTTP 代理编排、短信收发、VoWiFi/IMS 通话、eSIM 全生命周期管理整合到一个服务里,并提供一套现代化的响应式 Web 管理后台。

## 核心特性

| 模块 | 说明 |
| --- | --- |
| 多模组并发管理 | USB 热插拔自动发现(ttyUSB 等)、多设备实时状态监控 |
| 轻量级代理引擎 | 内建 SOCKS5 / HTTP 代理内核,支持多实例并发;基于 `SO_BINDTODEVICE` 按设备网卡严格绑定出站流量 |
| 通信与短信中心 | 统一界面/API 处理 AT 短信收发、会话与联系人管理、USSD 交互,短信落库可查 |
| eSIM 管理 | 通过 AT 指令通道直接管理 eSIM 芯片,支持 Profile 下载、启用/停用、重命名、删除 |
| 全渠道通知 | 重要短信及系统告警可推送至 Telegram、Email、PushPlus、Bark、飞书(Lark/Feishu)、QQ 等 |
| 多架构构建 | 原生支持 amd64 / arm64 / arm7 跨平台编译,路由器到边缘节点均可部署 |

## 典型应用场景

- **私有 IP 代理池**:单主机挂载多张物理 SIM 卡或多张 eSIM,每张网卡对应独立的 SOCKS5/HTTP 实例,组建自己的移动网络代理。
- **统一接码/验证码中心**:Web 界面或 API 并行收发多卡短信,并通过 Webhook/Bot 实时推送到个人终端。
- **VoWiFi 零信号通信**:地下室、弱覆盖场景下,借助宽带网络隧道建立 IMS 连接,保证业务不掉线。

## 架构与技术栈

- **Backend**:Go 1.26+(Gin、GORM、Viper、euicc-go)
- **Frontend**:Vue 3 + Vite + TailwindCSS + Element Plus
- **Database**:SQLite(`vohive.db`)
- **CI/CD**:GitHub Actions 自动化多架构 Docker 镜像构建与发布


## 免责声明

- **用途定位**:本项目主要面向个人学习、技术研究与功能测试场景,不建议直接用于生产环境或关键业务系统;由此产生的部署及使用风险由使用者自行承担。
- **非官方项目**:VoHive 为第三方独立开发的开源软件,与 Quectel(高通模组厂商)、高通公司及其他任何模组/芯片厂商均无官方关联、授权或合作关系,亦不对模组硬件本身的功能、质量或安全性负责。
- **合规使用**:使用本项目搭建的服务时,请自行确保符合所在地区的法律法规及电信运营商的服务条款,不得用于任何违法违规用途。因违规使用造成的一切法律责任由使用者自行承担,与本项目作者及贡献者无关。
- **无担保**:本软件按"现状"提供,不附带任何明示或暗示的担保,包括但不限于适销性、特定用途适用性及不侵权担保。因使用或无法使用本软件(含数据丢失、设备异常、业务中断等)造成的任何直接或间接损失,作者及贡献者不承担任何责任。

## License

本项目基于 [PolyForm Noncommercial License 1.0.0](LICENSE) 开源,**仅限非商业用途**:可自由查看、使用、修改、分发源码用于个人学习、研究、测试等非商业场景;**禁止任何形式的商业使用**(包括但不限于销售、提供付费服务、用于盈利性产品或业务)。如需商业授权,请联系作者另行协商。
