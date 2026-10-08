# ModemDeck Web（中文）

ModemDeck 通信工作区的 Vue 3 和 TypeScript 客户端。

## 开发

    npm ci
    npm run dev

Vite 服务器默认将 `/api` 代理到本地开发 API `http://127.0.0.1:8080`。可通过
`VITE_API_PROXY_TARGET` 覆盖目标地址。

确定性的开发数据必须显式启用，生产构建永远不会使用这些数据：

    VITE_MODEMDECK_FIXTURE=1 npm run dev

固定数据模式会在应用中显示明确标记。

## 检查

    npm run typecheck
    npm test
    npm run lint
    npm run build

真实通话使用同源 Cookie 和现有通话租约，在 HTTPS 入口上建立 WSS 音频连接，
连接复用 HTTPS 入口。AudioWorklet 保留浏览器回声消除、自动增益和噪声抑制，
按 16 kHz 单声道、20 ms 分帧；独立 Worker 使用固定版本 `libopus-wasm` 编解码，
支持没有原生 Opus WebCodecs 的浏览器。浏览器需要安全上下文、AudioWorklet、
WebAssembly 和 Web Locks；不支持时会明确报告通话音频失败。
编解码 Worker 为延迟加载，带独立体积预算，首屏不加载 WASM。
完整协议见 [通话 WSS 音频](../docs/call-audio-websocket.md)。随 Web 构建分发的
许可证位于 [call-audio-licenses.txt](public/call-audio-licenses.txt)。
部署 CSP 必须允许同源 Worker、同源 WSS 与 `wasm-unsafe-eval`；不需要一般的
`unsafe-eval`。现有服务器录音仍使用共享 PCM 通路，重连不会创建新录音。

---

# ModemDeck Web (English)

Vue 3 and TypeScript client for the ModemDeck communication workspace.

## Development

    npm ci
    npm run dev

The Vite server proxies /api to the local development API at
http://127.0.0.1:8080 by default. Override the
target with VITE_API_PROXY_TARGET.

Deterministic development data is opt-in and never used by production builds:

    VITE_MODEMDECK_FIXTURE=1 npm run dev

Fixture mode is visibly labelled in the application.

## Checks

    npm run typecheck
    npm test
    npm run lint
    npm run build

Live call audio uses same-origin session cookies and the existing call lease,
with WSS on the HTTPS ingress. AudioWorklet keeps
browser echo cancellation, automatic gain and noise suppression, resamples to
16 kHz mono and produces 20 ms frames. A lazily loaded, pinned `libopus-wasm`
Worker handles raw Opus on browsers without native Opus WebCodecs support.
Secure context, AudioWorklet, WebAssembly and Web Locks are required; unsupported
browsers report a media failure. The codec has a separate bundle budget and is
never loaded at startup. See the [WSS protocol](../docs/call-audio-websocket.md)
and the distributed [licenses](public/call-audio-licenses.txt).
CSP must allow same-origin workers/WSS and `wasm-unsafe-eval`, without general
`unsafe-eval`. Server recording still uses the shared PCM path; reconnecting
does not create another recording.
