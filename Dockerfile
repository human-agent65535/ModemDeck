# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:22.17.1-bookworm-slim@sha256:2fa754a9ba4d7adbd2a51d182eaabbe355c82b673624035a38c0d42b08724854
ARG GO_IMAGE=golang:1.26.3-bookworm@sha256:386d475a660466863d9f8c766fec64d7fdad3edac2c6a05020c09534d71edb4b
ARG RUNTIME_IMAGE=alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

FROM ${NODE_IMAGE} AS web-builder

ENV NODE_OPTIONS=--max-old-space-size=1536
WORKDIR /workspace/web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --include=dev --no-audit --no-fund

COPY web/ ./
RUN npm run build


FROM --platform=${TARGETPLATFORM} ${GO_IMAGE} AS app-builder

ARG TARGETOS
ARG TARGETARCH
ARG BUILD_DATE=unknown
ARG VCS_REF=unknown

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=1
WORKDIR /workspace

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends libopus-dev=1.3.1-3

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY cmd/modemdeck/ ./cmd/modemdeck/
COPY internal/ ./internal/
COPY VERSION ./
RUN rm -rf ./internal/webapp/dist \
    && mkdir -p ./internal/webapp/dist
COPY --from=web-builder /workspace/web/dist/ ./internal/webapp/dist/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    app_version="v$(tr -d '\r\n' < VERSION)" \
    && test "${app_version}" != "v" \
    && \
    GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -mod=readonly -tags=netgo,osusergo -trimpath -buildvcs=false \
    -ldflags="-s -w -linkmode=external -extldflags=-static \
      -X main.version=${app_version} -X main.commit=${VCS_REF} -X main.buildDate=${BUILD_DATE}" \
    -o /out/modemdeck ./cmd/modemdeck


FROM scratch AS modemdeck-artifact

COPY --from=app-builder /out/modemdeck /modemdeck


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

ENV MODEMDECK_LISTEN_ADDRESS=0.0.0.0:7575 \
    MODEMDECK_DATABASE_PATH=/var/lib/modemdeck/modemdeck.db \
    MODEMDECK_RECORDINGS_PATH=/data/recordings \
    MODEMDECK_AGENT_SOCKET=/run/modemdeck/agent.sock \
    MODEMDECK_TLS_DIRECTORY=/var/lib/modemdeck/tls \
    MODEMDECK_TLS_HOSTS=localhost,127.0.0.1,::1 \
    MODEMDECK_SECURE_COOKIES=true \
    MODEMDECK_HEALTHCHECK_URL=https://127.0.0.1:7575/api/v1/health

USER ${MODEMDECK_UID}:${MODEMDECK_GID}
WORKDIR /var/lib/modemdeck

EXPOSE 7575
STOPSIGNAL SIGTERM

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q --no-check-certificate -T 3 -O /dev/null "${MODEMDECK_HEALTHCHECK_URL}" || exit 1

ENTRYPOINT ["/usr/local/bin/modemdeck-entrypoint"]
