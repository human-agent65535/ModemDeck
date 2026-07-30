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
MODEMDECK_SETTINGS_KEY_FILE=/dev/null \
MODEMDECK_DATA_DIR="${test_root}/data" \
MODEMDECK_CLOUDFLARE_TOKEN_FILE="${test_root}/cloudflare-token" \
MODEMDECK_CLOUDFLARE_PUBLIC_URL=https://mobile.example.com \
docker compose \
    --project-directory "$repo_dir" \
    -f "${repo_dir}/docker-compose.yml" \
    -f "${repo_dir}/docker-compose.cloudflare.yml" \
    config >"${test_root}/cloudflare-compose.yml"

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
extract_service cloudflared "${test_root}/cloudflare-compose.yml" \
    >"${test_root}/cloudflared.yml"

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
if grep -Fq 'published: "7575"' "${test_root}/web.yml"; then
    fail "API-only port 7575 is published to the host"
fi
grep -Fq 'target: /var/lib/modemdeck/tls' "${test_root}/web.yml" ||
    fail "Web gateway cannot read the managed TLS certificate"
grep -Fq 'condition: service_healthy' "${test_root}/web.yml" ||
    fail "Web gateway does not wait for the API"
grep -Fq 'read_only: true' "${test_root}/web.yml" ||
    fail "Web gateway root filesystem is not read-only"
grep -Fq -- '- ALL' "${test_root}/web.yml" ||
    fail "Web gateway does not drop every capability"

grep -Fq 'listen 7575 default_server;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not listen on API-only port 7575"
grep -Fq 'listen 7577 ssl default_server;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not listen on HTTPS Web port 7577"
grep -Fq 'return 404;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not reject non-API paths on port 7575"
grep -Fq 'server api:8080 resolve;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not target the private API port"
grep -Fq 'proxy_pass http://modemdeck_api;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx does not proxy API requests"
grep -Fq 'proxy_buffering off;' "${repo_dir}/web/nginx.conf" ||
    fail "Nginx would buffer API event streams"

grep -Fq 'MODEMDECK_CLOUDFLARE_PUBLIC_URL: https://mobile.example.com' \
    "${test_root}/cloudflare-app.yml" ||
    fail "Cloudflare public URL does not reach the application"
grep -Fq 'MODEMDECK_CLOUDFLARE_READY_URL: http://cloudflared:2000/ready' \
    "${test_root}/cloudflare-app.yml" ||
    fail "Cloudflare readiness URL does not reach the application"
grep -Fq 'condition: service_healthy' "${test_root}/cloudflared.yml" ||
    fail "cloudflared does not wait for the Web gateway"
grep -Fq '      modemdeck:' "${test_root}/cloudflared.yml" ||
    fail "cloudflared is not ordered after the Web gateway"
grep -Fq '/run/secrets/cloudflare_tunnel_token' \
    "${test_root}/cloudflared.yml" ||
    fail "cloudflared does not read its token from a Compose secret"
grep -Fq '127.0.0.1:2000' "${test_root}/cloudflared.yml" ||
    fail "cloudflared readiness does not use the metrics endpoint"
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
