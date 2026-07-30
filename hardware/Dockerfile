# syntax=docker/dockerfile:1.7

ARG DEBIAN_IMAGE=debian:trixie-slim@sha256:020c0d20b9880058cbe785a9db107156c3c75c2ac944a6aa7ab59f2add76a7bd
ARG GO_IMAGE=golang:1.26.5-trixie@sha256:4ee9ffa999b4583ce281939cdff828763083610292f252279a0cee77473bd9a7

FROM --platform=${BUILDPLATFORM} ${GO_IMAGE} AS agent-builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

ENV GOTOOLCHAIN=local
WORKDIR /workspace/agent

COPY agent/go.mod agent/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY agent/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -mod=readonly -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/modemdeck-agent ./cmd/modemdeck-agent \
    && CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -mod=readonly -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/modemdeck-call-watchdog ./cmd/modemdeck-call-watchdog


FROM --platform=${BUILDPLATFORM} ${GO_IMAGE} AS ownership-builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

ENV GOTOOLCHAIN=local
WORKDIR /workspace/hardware/ownership

COPY hardware/ownership/go.mod hardware/ownership/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY hardware/ownership/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -mod=readonly -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/modemdeck-device-owner .


FROM ${DEBIAN_IMAGE} AS modemmanager-builder

ARG MODEMMANAGER_DEBIAN_VERSION=1.24.0-1+deb13u1

ENV DEBIAN_FRONTEND=noninteractive
WORKDIR /source

RUN sed -i 's/^Types: deb$/Types: deb deb-src/' \
        /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        dpkg-dev \
    && apt-get build-dep -y "modemmanager=${MODEMMANAGER_DEBIAN_VERSION}" \
    && apt-get source "modemmanager=${MODEMMANAGER_DEBIAN_VERSION}" \
    && rm -rf /var/lib/apt/lists/*

RUN source_dir="$(find /source -mindepth 1 -maxdepth 1 \
        -type d -name 'modemmanager-*' -print -quit)" \
    && test -n "${source_dir}" \
    && multiarch="$(dpkg-architecture -qDEB_HOST_MULTIARCH)" \
    && meson setup /build "${source_dir}" \
        --prefix=/usr \
        --libdir="lib/${multiarch}" \
        --buildtype=release \
        -Dudev=true \
        -Dat_command_via_dbus=true \
        -Dpolkit=no \
        -Dsystemd_suspend_resume=false \
        -Dsystemd_journal=false \
        -Dtests=false \
        -Dman=false \
        -Dgtk_doc=false \
        -Dvapi=false \
        -Dintrospection=false \
    && meson compile -C /build \
    && DESTDIR=/out meson install -C /build \
    && install -d /out/usr/share/modemdeck \
    && { \
        printf '%s\n' \
            "debian_release=13" \
            "debian_codename=trixie" \
            "debian_source_version=${MODEMMANAGER_DEBIAN_VERSION}" \
            "upstream_version=1.24.0" \
            "at_command_via_dbus=true" \
            "polkit=no" \
            "udev=true" \
            "systemd_suspend_resume=false" \
            "systemd_journal=false" \
            "libqmi_build_version=$(dpkg-query -W -f='${Version}' libqmi-glib5)" \
            "libmbim_build_version=$(dpkg-query -W -f='${Version}' libmbim-glib4)"; \
       } > /out/usr/share/modemdeck/modemmanager-build-options \
    && rm -rf \
        /out/etc/dbus-1 \
        /out/lib/udev \
        /out/usr/lib/systemd \
        /out/usr/share/dbus-1/system-services


FROM ${DEBIAN_IMAGE} AS runtime

ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG VCS_REF=unknown
ARG MODEMMANAGER_DEBIAN_VERSION=1.24.0-1+deb13u1

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        alsa-utils \
        bash \
        ca-certificates \
        curl \
        dbus-bin \
        dbus-daemon \
        dbus-system-bus-common \
        jq \
        libglib2.0-0t64 \
        libgudev-1.0-0 \
        libmbim-glib4 \
        libmbim-proxy \
        libmm-glib0 \
        libqmi-glib5 \
        libqmi-proxy \
        libqrtr-glib0 \
        ppp \
        util-linux \
    && rm -rf /var/lib/apt/lists/*

COPY --from=modemmanager-builder /out/usr/sbin/ModemManager /usr/sbin/ModemManager
COPY --from=modemmanager-builder /out/usr/bin/mmcli /usr/bin/mmcli
COPY --from=modemmanager-builder /out/usr/lib/ /usr/lib/
COPY --from=modemmanager-builder /out/usr/share/ModemManager/ /usr/share/ModemManager/
COPY --from=modemmanager-builder /out/usr/share/modemdeck/ /usr/share/modemdeck/
COPY --from=modemmanager-builder /out/etc/ModemManager/ /etc/ModemManager/
COPY --from=agent-builder /out/modemdeck-agent /usr/local/bin/modemdeck-agent
COPY --from=agent-builder /out/modemdeck-call-watchdog \
    /usr/local/bin/modemdeck-call-watchdog
COPY --from=ownership-builder /out/modemdeck-device-owner \
    /usr/local/bin/modemdeck-device-owner
COPY --chmod=0755 hardware/bin/modemdeck-hardware-entrypoint \
    /usr/local/bin/modemdeck-hardware-entrypoint
COPY --chmod=0755 hardware/bin/modemdeck-hardware-healthcheck \
    /usr/local/bin/modemdeck-hardware-healthcheck
COPY hardware/dbus/modemdeck-system.conf /etc/dbus-1/modemdeck-system.conf

RUN ldd /usr/sbin/ModemManager | tee /tmp/modemmanager-ldd \
    && ! grep -q 'not found' /tmp/modemmanager-ldd \
    && ! grep -Eq 'libpolkit|libsystemd' /tmp/modemmanager-ldd \
    && /usr/sbin/ModemManager --version | grep -Fq '1.24.0' \
    && test "$(dpkg-query -W -f='${Version}' libmm-glib0)" \
        = "${MODEMMANAGER_DEBIAN_VERSION}" \
    && dpkg --compare-versions \
        "$(dpkg-query -W -f='${Version}' libqmi-glib5)" ge 1.36 \
    && dpkg --compare-versions \
        "$(dpkg-query -W -f='${Version}' libmbim-glib4)" ge 1.32 \
    && for package in systemd systemd-sysv udev network-manager polkitd policykit-1; do \
         ! dpkg-query -W -f='${db:Status-Abbrev}' "${package}" 2>/dev/null \
             | grep -q '^ii'; \
       done \
    && test -x /usr/local/bin/modemdeck-agent \
    && test -x /usr/local/bin/modemdeck-call-watchdog \
    && test -x /usr/local/bin/modemdeck-device-owner \
    && install -d -m 0700 /var/lib/ModemManager \
    && install -d -m 0770 /run/dbus \
    && install -d -m 0770 /run/modemdeck \
    && install -d -m 0770 /run/modemdeck/tmp \
    && rm -f /tmp/modemmanager-ldd

LABEL org.opencontainers.image.title="ModemDeck hardware runtime" \
      org.opencontainers.image.description="Private D-Bus, custom ModemManager, explicit device ownership, and ModemDeck hardware agent" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.base.name="debian:trixie-slim" \
      org.modemdeck.modemmanager.debian-source="${MODEMMANAGER_DEBIAN_VERSION}" \
      org.modemdeck.modemmanager.at-command-via-dbus="true" \
      org.modemdeck.modemmanager.polkit="no"

ENV MODEMDECK_HARDWARE_MODE=simple \
    MODEMDECK_AGENT_SOCKET=/run/modemdeck/agent.sock \
    MODEMDECK_AGENT_SOCKET_MODE=0660 \
    MODEMDECK_AGENT_SOCKET_GID=10002 \
    MODEMDECK_AGENT_HEARTBEAT_FILE=/run/modemdeck/agent-heartbeat.json \
    MODEMDECK_BEARER_STATE_FILE=/run/modemdeck/bearers.json \
    MODEMDECK_NETWORK_STATE_FILE=/run/modemdeck/network.json \
    MODEMDECK_DEVICE_OWNER_STATUS=/run/modemdeck/device-owner-status.json \
    MODEMDECK_DEVICE_OWNER_MAX_AGE_SECONDS=30 \
    MODEMDECK_DBUS_CONFIG=/etc/dbus-1/modemdeck-system.conf \
    MODEMDECK_DBUS_SOCKET=/run/dbus/system_bus_socket \
    MODEMDECK_MM_STATE_DIR=/var/lib/ModemManager \
    MODEMDECK_MM_LOG_LEVEL=INFO \
    MODEMDECK_STARTUP_TIMEOUT_SECONDS=20 \
    MODEMDECK_SHUTDOWN_TIMEOUT_SECONDS=10 \
    MODEMDECK_WATCHDOG_TIMEOUT_SECONDS=3 \
    MODEMDECK_WATCHDOG_POLL_MILLISECONDS=250 \
    MODEMDECK_WATCHDOG_CLEANUP_TIMEOUT_SECONDS=15 \
    TMPDIR=/run/modemdeck/tmp

VOLUME ["/var/lib/ModemManager", "/run/modemdeck"]

STOPSIGNAL SIGTERM

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/modemdeck-hardware-healthcheck"]

ENTRYPOINT ["/usr/local/bin/modemdeck-hardware-entrypoint"]
CMD ["--mode", "simple"]
