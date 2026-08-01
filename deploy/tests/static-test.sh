#!/bin/sh

set -eu

tests_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH='' cd -- "${tests_dir}/../.." && pwd)
test_root=$(mktemp -d)

cleanup() {
    rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'static-test: %s\n' "$*" >&2
    exit 1
}

sh -n "${repo_dir}/install.sh"

git -C "$repo_dir" check-ignore -q .env ||
    fail "generated Compose environment is not ignored"
git -C "$repo_dir" check-ignore -q data/modemdeck.db ||
    fail "application data is not ignored"
git -C "$repo_dir" check-ignore -q secrets/admin-password ||
    fail "generated secrets are not ignored"
git -C "$repo_dir" check-ignore -q deploy/device-assignments.json ||
    fail "common real assignment path is not ignored"

MODEMDECK_SETTINGS_KEY_FILE=/dev/null \
MODEMDECK_DATA_DIR="${test_root}/data" \
MODEMDECK_API_VERSION=api-test \
MODEMDECK_WEB_VERSION=web-test \
MODEMDECK_HARDWARE_VERSION=hardware-test \
docker compose \
    --project-directory "$repo_dir" \
    -f "${repo_dir}/docker-compose.yml" \
    config >"${test_root}/compose.yml"

MODEMDECK_SETTINGS_KEY_FILE=/dev/null \
MODEMDECK_DATA_DIR="${test_root}/data" \
MODEMDECK_ASSIGNMENT_FILE="${repo_dir}/deploy/advanced-assignment.example.json" \
MODEMDECK_HOST_PROC_ROOT=/proc \
MODEMDECK_HOST_DBUS_SOCKET=/var/run/docker.sock \
docker compose \
    --project-directory "$repo_dir" \
    -f "${repo_dir}/docker-compose.yml" \
    -f "${repo_dir}/docker-compose.advanced.yml" \
    config >"${test_root}/advanced-compose.yml"

printf '%s\n' \
    'eyJhbGciOiJIUzI1NiJ9X19tb2RlbWRlY2tfZGVwbG95bWVudF90ZXN0X3Rva2VuX19sb25nX2Vub3VnaF9mb3JfdmFsaWRhdGlvbg' \
    >"${test_root}/cloudflare-token"
printf '%s\n' 'test-cloudflare-turn-token' \
    >"${test_root}/cloudflare-turn-token"
MODEMDECK_SETTINGS_KEY_FILE=/dev/null \
MODEMDECK_DATA_DIR="${test_root}/data" \
MODEMDECK_CLOUDFLARE_TOKEN_FILE="${test_root}/cloudflare-token" \
docker compose \
    --project-directory "$repo_dir" \
    -f "${repo_dir}/docker-compose.yml" \
    -f "${repo_dir}/docker-compose.cloudflare.yml" \
    config >"${test_root}/cloudflare-compose.yml"

MODEMDECK_SETTINGS_KEY_FILE=/dev/null \
MODEMDECK_DATA_DIR="${test_root}/data" \
MODEMDECK_CLOUDFLARE_TOKEN_FILE="${test_root}/cloudflare-token" \
MODEMDECK_CLOUDFLARE_TURN_KEY_ID=0123456789abcdef0123456789abcdef \
MODEMDECK_CLOUDFLARE_TURN_TOKEN_FILE="${test_root}/cloudflare-turn-token" \
docker compose \
    --project-directory "$repo_dir" \
    -f "${repo_dir}/docker-compose.yml" \
    -f "${repo_dir}/docker-compose.cloudflare.yml" \
    -f "${repo_dir}/docker-compose.cloudflare-turn.yml" \
    config >"${test_root}/cloudflare-turn-compose.yml"

MODEMDECK_SETTINGS_KEY_FILE=/dev/null \
MODEMDECK_DATA_DIR="${test_root}/data" \
MODEMDECK_UPDATER_TOKEN_FILE=/dev/null \
MODEMDECK_DEPLOYMENT_DIR="${repo_dir}" \
MODEMDECK_UPDATER_IMAGE_REF="ghcr.io/human-agent65535/modemdeck-updater:v1.9.3@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
docker compose \
    --project-directory "$repo_dir" \
    -f "${repo_dir}/docker-compose.yml" \
    -f "${repo_dir}/docker-compose.ota.yml" \
    --profile ota \
    config >"${test_root}/ota-compose.yml"

extract_service() {
    service_name=$1
    source_file=$2
    awk -v header="  ${service_name}:" '
        $0 == header {
            printing = 1
        }
        printing && $0 != header &&
            ($0 ~ /^  [A-Za-z0-9_-]+:$/ || $0 ~ /^[^ ]/) {
            exit
        }
        printing { print }
    ' "$source_file"
}

extract_service hardware "${test_root}/compose.yml" \
    >"${test_root}/hardware.yml"
extract_service api "${test_root}/compose.yml" \
    >"${test_root}/app.yml"
extract_service modemdeck "${test_root}/compose.yml" \
    >"${test_root}/web.yml"
extract_service api "${test_root}/cloudflare-compose.yml" \
    >"${test_root}/cloudflare-app.yml"
extract_service api "${test_root}/cloudflare-turn-compose.yml" \
    >"${test_root}/cloudflare-turn-app.yml"
extract_service cloudflared "${test_root}/cloudflare-compose.yml" \
    >"${test_root}/cloudflared.yml"
extract_service api "${test_root}/ota-compose.yml" \
    >"${test_root}/ota-app.yml"
extract_service updater "${test_root}/ota-compose.yml" \
    >"${test_root}/updater.yml"

grep -Fq 'image: modemdeck-hardware:hardware-test' \
    "${test_root}/hardware.yml" ||
    fail "hardware does not retain an independent image version"
grep -Fq 'image: modemdeck:api-test' "${test_root}/app.yml" ||
    fail "API does not retain an independent image version"
grep -Fq 'image: modemdeck-web:web-test' "${test_root}/web.yml" ||
    fail "Web does not retain an independent image version"

hardware_hash() {
    MODEMDECK_SETTINGS_KEY_FILE=/dev/null \
    MODEMDECK_DATA_DIR="${test_root}/data" \
    MODEMDECK_API_VERSION=$1 \
    MODEMDECK_WEB_VERSION=$2 \
    MODEMDECK_HARDWARE_VERSION=$3 \
    docker compose \
        --project-directory "$repo_dir" \
        -f "${repo_dir}/docker-compose.yml" \
        config --hash hardware
}

hardware_hash_before=$(hardware_hash api-one web-one hardware-one)
hardware_hash_after_app=$(hardware_hash api-two web-two hardware-one)
[ "$hardware_hash_before" = "$hardware_hash_after_app" ] ||
    fail "API/Web image versions alter the Hardware service hash"
hardware_hash_after_hardware=$(hardware_hash api-two web-two hardware-two)
[ "$hardware_hash_before" != "$hardware_hash_after_hardware" ] ||
    fail "Hardware image version does not alter the Hardware service hash"

grep -Fq 'network_mode: host' "${test_root}/hardware.yml" ||
    fail "hardware does not use host networking"
grep -Fq 'privileged: true' "${test_root}/hardware.yml" ||
    fail "hardware is not explicitly privileged"
grep -Fq 'target: /var/lib/ModemManager' "${test_root}/hardware.yml" ||
    fail "hardware ModemManager state is not persistent"
grep -Fq 'target: /run/modemdeck' "${test_root}/hardware.yml" ||
    fail "hardware Agent state/socket volume is missing"
grep -Fq 'target: /etc/modemdeck/media-bindings.json' \
    "${test_root}/hardware.yml" ||
    fail "hardware modem audio bindings mount is missing"
grep -Fq '/run/dbus:rw' "${test_root}/hardware.yml" ||
    fail "hardware private D-Bus tmpfs is missing"
grep -Fq '/run:rw' "${test_root}/hardware.yml" ||
    fail "hardware private runtime tmpfs is missing"
for hardware_mount in '/dev' '/sys' '/run/udev'; do
    grep -Fq "source: ${hardware_mount}" "${test_root}/hardware.yml" ||
        fail "hardware mount is missing: ${hardware_mount}"
done
if grep -A5 -F 'source: /sys' "${test_root}/hardware.yml" |
    grep -Fq 'read_only: true'; then
    fail "hardware sysfs mount is read-only; QMI raw_ip cannot be configured"
fi

grep -Fq 'cap_drop:' "${test_root}/app.yml" ||
    fail "application capability drop is missing"
grep -Fq -- '- ALL' "${test_root}/app.yml" ||
    fail "application does not drop every capability"
grep -Fq 'read_only: true' "${test_root}/app.yml" ||
    fail "application root filesystem is not read-only"
grep -Fq '/api/v1/health/ready' "${test_root}/app.yml" ||
    fail "application healthcheck does not use readiness"
grep -Fq 'condition: service_healthy' "${test_root}/app.yml" ||
    fail "application does not wait for healthy hardware"
grep -Fq 'target: /run/modemdeck' "${test_root}/app.yml" ||
    fail "application Agent socket mount is missing"
grep -Fq 'MODEMDECK_LISTEN_ADDRESS: 0.0.0.0:8080' \
    "${test_root}/app.yml" ||
    fail "application API does not listen on private port 8080"
grep -Fq 'expose:' "${test_root}/app.yml" ||
    fail "application API does not declare its private port"
grep -Fq -- '- "8080"' "${test_root}/app.yml" ||
    fail "application API private port is not 8080"
if grep -Fq 'ports:' "${test_root}/app.yml"; then
    fail "application API is published to the host"
fi

grep -Fq 'MODEMDECK_UPDATER_URL:' "${test_root}/ota-app.yml" ||
    fail "application API does not expose the private updater client configuration"
grep -Fq 'source: modemdeck_updater_token' "${test_root}/ota-app.yml" ||
    fail "application API does not receive updater authentication as a secret"
if grep -Fq 'source: /var/run/docker.sock' "${test_root}/ota-app.yml"; then
    fail "application API has direct Docker socket access"
fi

grep -Fq 'image: ghcr.io/human-agent65535/modemdeck-updater:v1.9.3@sha256:' \
    "${test_root}/updater.yml" ||
    fail "updater does not use the immutable configured image reference"
grep -Fq 'source: /var/run/docker.sock' "${test_root}/updater.yml" ||
    fail "updater is missing its explicit Docker control-plane mount"
grep -Fq 'target: /var/run/docker.sock' "${test_root}/updater.yml" ||
    fail "updater Docker socket target is incorrect"
grep -Fq 'read_only: true' "${test_root}/updater.yml" ||
    fail "updater root filesystem is not read-only"
grep -Fq -- '- ALL' "${test_root}/updater.yml" ||
    fail "updater does not drop every Linux capability"
grep -Fq 'no-new-privileges:true' "${test_root}/updater.yml" ||
    fail "updater permits privilege escalation"
if grep -Fq 'ports:' "${test_root}/updater.yml"; then
    fail "updater control plane is published to the host"
fi
if grep -Fq 'MODEMDECK_CURRENT_VERSION:' "${test_root}/updater.yml"; then
    fail "overall release changes alter the retained updater service"
fi

for forbidden in \
    'network_mode: host' \
    'privileged: true' \
    'source: /dev' \
    'source: /sys' \
    'source: /run/udev' \
    'source: /run/dbus' \
    'target: /var/lib/ModemManager'
do
    if grep -Fq "$forbidden" "${test_root}/app.yml"; then
        fail "application contains forbidden hardware access: ${forbidden}"
    fi
done

grep -Fq 'target: web-runtime' "${test_root}/web.yml" ||
    fail "Web gateway does not build the Nginx runtime"
grep -Fq '127.0.0.1' "${test_root}/web.yml" ||
    fail "Web gateway is not restricted to host loopback"
grep -Fq 'target: 7577' "${test_root}/web.yml" ||
    fail "Web gateway does not publish HTTPS port 7577"
grep -Fq 'protocol: tcp' "${test_root}/web.yml" ||
    fail "Web gateway does not publish HTTPS over TCP"
grep -Fq 'protocol: udp' "${test_root}/web.yml" ||
    fail "Web gateway does not publish HTTP/3 over UDP"
grep -Fq 'MODEMDECK_WEB_HTTPS_PORT: "7577"' "${test_root}/web.yml" ||
    fail "Web gateway does not advertise its published HTTP/3 port"
grep -Fq -- '- "7575"' "${test_root}/web.yml" ||
    fail "Web gateway does not expose Cloudflare API port 7575"
grep -Fq -- '- "7576"' "${test_root}/web.yml" ||
    fail "Web gateway does not expose Cloudflare Web port 7576"
if grep -Fq 'published: "7575"' "${test_root}/web.yml"; then
    fail "API-only port 7575 is published to the host"
fi
if grep -Fq 'published: "7576"' "${test_root}/web.yml"; then
    fail "Cloudflare Web port 7576 is published to the host"
fi
grep -Fq 'target: /var/lib/modemdeck/tls' "${test_root}/web.yml" ||
    fail "Web gateway cannot read the managed TLS certificate"
grep -Fq 'condition: service_healthy' "${test_root}/web.yml" ||
    fail "Web gateway does not wait for the API"
grep -Fq 'read_only: true' "${test_root}/web.yml" ||
    fail "Web gateway root filesystem is not read-only"
grep -Fq -- '- ALL' "${test_root}/web.yml" ||
    fail "Web gateway does not drop every capability"

grep -Fq 'include /tmp/modemdeck-origin-7575.conf;' \
    "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not load the conditional API origin listener"
grep -Fq 'include /tmp/modemdeck-origin-7576.conf;' \
    "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not load the conditional Web origin listener"
grep -Fq 'listen 7577 ssl default_server;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not listen on HTTPS Web port 7577"
grep -Fq 'listen 7577 quic reuseport;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not listen for HTTP/3 on Web port 7577"
grep -Fq 'http2 on;' "${repo_dir}/web/nginx.conf" ||
    fail "HTTPS Web listener does not enable HTTP/2"
grep -Fq 'include /tmp/modemdeck-http3.conf;' "${repo_dir}/web/nginx.conf" ||
    fail "HTTPS Web listener does not advertise HTTP/3"
grep -Fq 'error_page 497 =308 https://$http_host$request_uri;' \
    "${repo_dir}/web/nginx.conf" ||
    fail "HTTPS Web listener does not upgrade plain HTTP requests"
grep -Fq 'map $http_x_forwarded_proto $cloudflare_https_redirect {' \
    "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not classify Cloudflare HTTP requests"
cloudflare_redirect_count=$(
    grep -Fc 'if ($cloudflare_https_redirect) {' \
        "${repo_dir}/web/nginx.conf" || true
)
[ "$cloudflare_redirect_count" -eq 2 ] ||
    fail "Cloudflare API and Web listeners do not both enforce HTTPS"
cloudflare_redirect_target_count=$(
    grep -Fc 'return 308 https://$host$request_uri;' \
        "${repo_dir}/web/nginx.conf" || true
)
[ "$cloudflare_redirect_target_count" -eq 2 ] ||
    fail "Cloudflare listeners do not preserve host and request URI on HTTPS redirect"
grep -Fq 'return 404;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not reject non-API paths on port 7575"
grep -Fq 'server api:8080 resolve;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not target the private API port"
grep -Fq 'proxy_pass http://modemdeck_api;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not proxy API requests"
grep -Fq 'proxy_buffering off;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx would buffer API event streams"
connection_header_count=$(grep -Fc 'proxy_set_header Connection "";' "${repo_dir}/web/nginx.conf" || true)
[ "$connection_header_count" -eq 3 ] ||
    fail "Nginx API proxies do not consistently preserve upstream keepalive"
if grep -Fq 'proxy_set_header Upgrade ' "${repo_dir}/web/nginx.conf"; then
    fail "Nginx carries an unused WebSocket upgrade header"
fi
if grep -Fq 'map $http_upgrade ' "${repo_dir}/web/nginx.conf"; then
    fail "Nginx carries an unused WebSocket connection map"
fi
grep -Fq 'MODEMDECK_WEB_TLS_DIRECTORY' "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint does not read the managed Web certificate directory"
grep -Fq '"${tls_directory}/automatic-server.pem"' \
    "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint cannot select the automatic Web certificate"
grep -Fq '"${tls_directory}/user.pem"' "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint cannot select an uploaded Web certificate"
grep -Fq 'nginx -s reload' "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint does not hot-reload certificate changes"
grep -Fq 'MODEMDECK_WEB_HTTPS_PORT' "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint does not use the published HTTP/3 port"
grep -Fq 'h3=":%s"; ma=86400' "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint does not generate the HTTP/3 Alt-Svc header"
grep -Fq 'cloudflare-origin.pem' "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint does not observe the optional Origin CA bundle"
grep -Fq "'listen %s default_server;" "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint cannot keep disabled origins on HTTP"
grep -Fq "'listen %s ssl default_server;" "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint cannot enable HTTPS origins"
grep -Fq "'http2 on;'" "${repo_dir}/scripts/nginx-entrypoint.sh" ||
    fail "Nginx entrypoint does not enable HTTP/2 with Origin TLS"
grep -Fq 'https://127.0.0.1:7577/api/v1/health/live' \
    "${test_root}/web.yml" ||
    fail "Web health does not use the invariant local HTTPS listener"
grep -Fq 'https://127.0.0.1:7577/api/v1/health/live' \
    "${repo_dir}/Dockerfile" ||
    fail "Web image health does not use the invariant local HTTPS listener"

if grep -Fq 'MODEMDECK_CLOUDFLARE_PUBLIC_URL' \
    "${test_root}/cloudflare-app.yml"
then
    fail "application still depends on a static Cloudflare public URL"
fi
grep -Fq 'MODEMDECK_CLOUDFLARE_READY_URL: http://cloudflared:2000/ready' \
    "${test_root}/cloudflare-app.yml" ||
    fail "Cloudflare readiness URL does not reach the application"
grep -Fq 'MODEMDECK_CLOUDFLARE_ORIGIN_PROBE_URLS: https://modemdeck:7575/api/v1/health/live,https://modemdeck:7576/api/v1/health/live' \
    "${test_root}/cloudflare-app.yml" ||
    fail "Cloudflare origin TLS activation does not verify both HTTP/2 listeners"
if grep -Fq 'MODEMDECK_CLOUDFLARE_TURN_' \
    "${test_root}/cloudflare-app.yml"
then
    fail "Tunnel-only Compose unexpectedly enables Cloudflare TURN"
fi
grep -Fq 'MODEMDECK_CLOUDFLARE_TURN_KEY_ID: 0123456789abcdef0123456789abcdef' \
    "${test_root}/cloudflare-turn-app.yml" ||
    fail "Cloudflare TURN key ID does not reach the application"
grep -Fq 'MODEMDECK_CLOUDFLARE_TURN_TOKEN_FILE: /run/secrets/cloudflare_turn_token' \
    "${test_root}/cloudflare-turn-app.yml" ||
    fail "application does not read the TURN token from a Compose secret"
grep -Fq 'source: cloudflare_turn_token' \
    "${test_root}/cloudflare-turn-app.yml" ||
    fail "application TURN token secret is not mounted"
grep -Fq 'condition: service_healthy' "${test_root}/cloudflared.yml" ||
    fail "cloudflared does not wait for the Web gateway"
grep -Fq '      modemdeck:' "${test_root}/cloudflared.yml" ||
    fail "cloudflared is not ordered after the Web gateway"
grep -Fq '/run/secrets/cloudflare_tunnel_token' \
    "${test_root}/cloudflared.yml" ||
    fail "cloudflared does not read its token from a Compose secret"
grep -Fq '127.0.0.1:2000' "${test_root}/cloudflared.yml" ||
    fail "cloudflared readiness does not use the metrics endpoint"
grep -Fq 'cloudflare/cloudflared:2026.7.3@sha256:e39ee8da81ad5e05d77f38d2f51c60ca51bf2a8450ac3abab50c17fdb91d91bf' \
    "${test_root}/cloudflared.yml" ||
    fail "cloudflared image is not pinned by digest"
if grep -Fq 'ports:' "${test_root}/cloudflared.yml"; then
    fail "cloudflared publishes a host port"
fi
if grep -Fq 'container_name:' "${test_root}/cloudflared.yml"; then
    fail "cloudflared uses a fixed container name that blocks manual-connector migration"
fi

for advanced_view in '/run/host-proc' '/run/host-dbus' 'assignments.json'; do
    if grep -Fq "$advanced_view" "${test_root}/compose.yml"; then
        fail "simple Compose leaks an advanced-only host view: ${advanced_view}"
    fi
done

extract_service hardware "${test_root}/advanced-compose.yml" \
    >"${test_root}/advanced-hardware.yml"
extract_service api "${test_root}/advanced-compose.yml" \
    >"${test_root}/advanced-app.yml"

grep -Fq 'MODEMDECK_HARDWARE_MODE: advanced' \
    "${test_root}/advanced-hardware.yml" ||
    fail "advanced mode does not reach the hardware runtime"
grep -Fq -- '--assignments' "${test_root}/advanced-hardware.yml" ||
    fail "advanced assignment command is missing"
grep -Fq 'target: /etc/modemdeck/device-assignments.json' \
    "${test_root}/advanced-hardware.yml" ||
    fail "advanced assignment file mount is missing"
grep -Fq 'target: /run/host-proc' "${test_root}/advanced-hardware.yml" ||
    fail "advanced host process view is missing"
grep -Fq 'target: /run/host-dbus/system_bus_socket' \
    "${test_root}/advanced-hardware.yml" ||
    fail "advanced host D-Bus view is missing"
for advanced_mount in \
    '/etc/modemdeck/device-assignments.json' \
    '/run/host-proc' \
    '/run/host-dbus/system_bus_socket'
do
    if grep -Fq "$advanced_mount" "${test_root}/advanced-app.yml"; then
        fail "application received advanced hardware access: ${advanced_mount}"
    fi
done

grep -Fq 'do not configure, stop, restart' "${repo_dir}/deploy/README.md" ||
    fail "advanced host-ownership boundary is unclear"
grep -Fq 'does not prescribe or apply that host policy' \
    "${repo_dir}/deploy/README.md" ||
    fail "advanced operator responsibility is unclear"
if grep -Eq -- '--filter-policy|ID_MM_DEVICE_' "${repo_dir}/deploy/README.md"; then
    fail "advanced deployment guide prescribes host ModemManager policy"
fi

printf '%s\n' "static-test: ok"
