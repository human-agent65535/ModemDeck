# ModemDeck Web

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
