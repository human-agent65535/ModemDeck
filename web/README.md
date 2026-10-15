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
采集直接使用 48 kHz 单声道、960 样本/20 ms 分帧，无平台手写重采样；MD12 时间戳仍为 16 kHz 时间基准。同源 WASM 内的官方 NetEq 和 Opus 由单个
AudioWorklet 拥有；48 kHz 设备渲染按实际需求拉取，缓冲、PLC 与时间伸缩由 NetEq
统一处理，不叠加固定 TTL、前置播放队列或平台重预填。浏览器需要安全上下文、AudioWorklet、
WebAssembly 和 Web Locks；不支持时会明确报告通话音频失败。
WASM 为延迟加载，独立 gzip 预算 300 KiB，首屏不加载。32 MiB 是固定内存预留，
不是实测峰值或最低需求。同步 WASM 编译仅在 Worklet 构造/setup 阶段发生，
渲染回调不 fetch/compile。发送资源上限为 2 秒；正常批量到达不按帧年龄删包。
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
browser echo cancellation, automatic gain and noise suppression, and encodes
48 kHz mono directly as 960-sample/20 ms frames. MD12 timestamps retain their
16 kHz time base; there is no platform capture resampler. One AudioWorklet owns the pinned official
NetEq and Opus WASM. Native 48k render demand drives its receiver, which alone
owns jitter buffering, PLC and time stretching; there is no additional fixed
TTL, PCM playback queue or platform rebuffer offset.
Secure context, AudioWorklet, WebAssembly and Web Locks are required; unsupported
browsers report a media failure. The shared WASM has a separate 300 KiB gzip
budget and is never loaded at startup. Its fixed 32 MiB reservation is not a
measured peak or minimum. Synchronous WASM compilation happens during worklet
construction/setup; rendering callbacks never fetch or compile. The sender has
a 2-second resource bound, without discarding normal batches by frame age. See the [WSS protocol](../docs/call-audio-websocket.md)
and the distributed [licenses](public/call-audio-licenses.txt).
CSP must allow same-origin workers/WSS and `wasm-unsafe-eval`, without general
`unsafe-eval`. Server recording still uses the shared PCM path; reconnecting
does not create another recording.

## 合成浏览器验证 / Synthetic browser verification

    npm run build
    node scripts/prepare-call-audio-browser-check.mjs /private/tmp/modemdeck-production-neteq-web

将生成目录作为 localhost HTTP 根目录打开。页面复用实际生产 Worklet/WASM 与 MD12
封装，仅使用 OfflineAudioContext 合成输入，覆盖正常批次、单次/交替100ms 延迟及
回绕；不申请麦克风、不连接业务 WebSocket。它证明实际浏览器执行与质量统计，
不证明实时线程期限、真实设备声学或生产通话效果。

Serve the generated directory as a localhost HTTP root. The page uses actual
production Worklet/WASM and MD12 framing with synthetic OfflineAudioContext
input: normal batches, single/alternating 100ms delays and wrapping. It requests
no microphone or business WebSocket. Offline execution and quality counters do
not establish realtime deadlines, device acoustics or production call quality.

The production build also executes the emitted Worklet ES module and its actual
WASM in a scope without `self`, `location`, `URL`, `performance` or shared memory,
matching the verified WebKit AudioWorklet globals. The synchronous official
`wasmBinary` + `locateFile` entry initializes during processor setup; it performs
no fetch or compilation in `process`. The separate browser page exercises the
same emitted assets with a real OfflineAudioContext. Neither offline check
establishes realtime deadline or microphone acoustic performance.
