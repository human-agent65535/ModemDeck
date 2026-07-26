# ModemDeck Web（中文）

ModemDeck 通信工作区的 Vue 3 和 TypeScript 客户端。

## 开发

    npm ci
    npm run dev

Vite 服务器默认将 `/api` 代理到 `http://127.0.0.1:7575`。可通过
`VITE_API_PROXY_TARGET` 覆盖目标地址。

确定性的开发数据必须显式启用，生产构建永远不会使用这些数据：

    VITE_MODEMDECK_FIXTURE=1 npm run dev

固定数据模式会在应用中显示明确标记。

## 检查

    npm run typecheck
    npm run lint
    npm run build

首个生产版本未经身份验证并且只读。它使用以 `/api/v1` 为根路径的 GET
端点；只存在于固定数据模式中的交互绝不会发送到生产 API。

---

# ModemDeck Web (English)

Vue 3 and TypeScript client for the ModemDeck communication workspace.

## Development

    npm ci
    npm run dev

The Vite server proxies /api to http://127.0.0.1:7575 by default. Override the
target with VITE_API_PROXY_TARGET.

Deterministic development data is opt-in and never used by production builds:

    VITE_MODEMDECK_FIXTURE=1 npm run dev

Fixture mode is visibly labelled in the application.

## Checks

    npm run typecheck
    npm run lint
    npm run build

The first production slice is unauthenticated and read-only. It consumes the
GET endpoints rooted at /api/v1; fixture-only interactions are never sent to
the production API.
