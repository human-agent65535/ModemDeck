# syntax=docker/dockerfile:1.19@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

ARG NODE_IMAGE=node:24.18.0-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d
ARG GO_IMAGE=golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651
ARG RUNTIME_IMAGE=alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ARG NGINX_IMAGE=nginx:1.30.4-alpine-slim@sha256:ddde39c6e51f02fde7410c2e9c234cf2d0a4c7bdbbe176aeb37d8ad7ab4eb58c

FROM ${NODE_IMAGE} AS web-builder

ENV NODE_OPTIONS=--max-old-space-size=1536
WORKDIR /workspace/web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --include=dev --no-audit --no-fund

COPY VERSION /workspace/VERSION
COPY web/ ./
RUN npm run build


FROM ${GO_IMAGE} AS go-toolchain

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=1
WORKDIR /workspace

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends libopus-dev=1.3.1-3


FROM go-toolchain AS app-builder

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY cmd/modemdeck/ ./cmd/modemdeck/
COPY internal/ ./internal/
COPY VERSION ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    app_version="v$(tr -d '\r\n' < VERSION)" \
    && test "${app_version}" != "v" \
    && \
    GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -mod=readonly -tags=netgo,osusergo -trimpath -buildvcs=false \
    -ldflags="-s -w -linkmode=external -extldflags=-static \
      -X main.version=${app_version}" \
    -o /out/modemdeck ./cmd/modemdeck


FROM scratch AS modemdeck-artifact

COPY --from=app-builder /out/modemdeck /modemdeck


FROM ${NGINX_IMAGE} AS web-runtime

ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG VCS_REF=unknown

COPY --chown=101:101 web/nginx.conf /etc/nginx/nginx.conf
COPY --chown=101:101 --chmod=0755 scripts/nginx-entrypoint.sh /usr/local/bin/modemdeck-web-entrypoint
COPY --from=web-builder --chown=101:101 /workspace/web/dist/ /usr/share/nginx/html/
COPY LICENSE NOTICE.md THIRD_PARTY_NOTICES.md /usr/share/licenses/modemdeck/

LABEL org.opencontainers.image.title="ModemDeck Web" \
      org.opencontainers.image.description="ModemDeck Web frontend and API reverse proxy" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}"

USER 101:101

EXPOSE 7575 7576 7577/tcp 7577/udp
STOPSIGNAL SIGQUIT

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -T 3 --no-check-certificate -O /dev/null https://127.0.0.1:7577/api/v1/health/live || exit 1

ENTRYPOINT ["/usr/local/bin/modemdeck-web-entrypoint"]
CMD ["-g", "daemon off;"]


FROM ${RUNTIME_IMAGE} AS runtime

ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG VCS_REF=unknown
ARG MODEMDECK_UID=10001
ARG MODEMDECK_GID=10001

RUN apk add --no-cache ca-certificates=20260611-r0 \
    && addgroup -S -g "${MODEMDECK_GID}" modemdeck \
    && adduser -S -D -H -h /nonexistent -s /sbin/nologin \
        -u "${MODEMDECK_UID}" -G modemdeck modemdeck \
    && mkdir -p /var/lib/modemdeck /run/modemdeck /tmp \
    && ln -s /var/lib/modemdeck /data \
    && chown -R "${MODEMDECK_UID}:${MODEMDECK_GID}" /var/lib/modemdeck /tmp \
    && chmod 0770 /var/lib/modemdeck \
    && chmod 0750 /run/modemdeck

COPY --from=app-builder /out/modemdeck /usr/local/bin/modemdeck
COPY --chmod=0755 scripts/docker-entrypoint.sh /usr/local/bin/modemdeck-entrypoint
COPY LICENSE NOTICE.md THIRD_PARTY_NOTICES.md /usr/share/licenses/modemdeck/

LABEL org.opencontainers.image.title="ModemDeck" \
      org.opencontainers.image.description="Self-hosted cellular communications console" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}"

ENV MODEMDECK_LISTEN_ADDRESS=0.0.0.0:8080 \
    MODEMDECK_DATABASE_PATH=/var/lib/modemdeck/modemdeck.db \
    MODEMDECK_RECORDINGS_PATH=/data/recordings \
    MODEMDECK_AGENT_SOCKET=/run/modemdeck/agent.sock \
    MODEMDECK_SECURE_COOKIES=true \
    MODEMDECK_HEALTHCHECK_URL=http://127.0.0.1:8080/api/v1/health

USER ${MODEMDECK_UID}:${MODEMDECK_GID}
WORKDIR /var/lib/modemdeck

EXPOSE 8080
STOPSIGNAL SIGTERM

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -T 3 -O /dev/null "${MODEMDECK_HEALTHCHECK_URL}" || exit 1

ENTRYPOINT ["/usr/local/bin/modemdeck-entrypoint"]
