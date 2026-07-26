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

MODEMDECK_ADMIN_PASSWORD_FILE=/dev/null \
MODEMDECK_SETTINGS_KEY_FILE=/dev/null \
MODEMDECK_DATA_DIR="${test_root}/data" \
docker compose \
    --project-directory "$repo_dir" \
    -f "${repo_dir}/docker-compose.yml" \
    config >"${test_root}/compose.yml"

MODEMDECK_ADMIN_PASSWORD_FILE=/dev/null \
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

awk '
    /^  hardware:/ { printing = 1 }
    /^  modemdeck:/ { printing = 0 }
    printing { print }
' "${test_root}/compose.yml" >"${test_root}/hardware.yml"

awk '
    /^  modemdeck:/ { printing = 1 }
    /^[^ ]/ && printing { printing = 0 }
    printing { print }
' "${test_root}/compose.yml" >"${test_root}/app.yml"

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

for advanced_view in '/run/host-proc' '/run/host-dbus' 'assignments.json'; do
    if grep -Fq "$advanced_view" "${test_root}/compose.yml"; then
        fail "simple Compose leaks an advanced-only host view: ${advanced_view}"
    fi
done

awk '
    /^  hardware:/ { printing = 1 }
    /^  modemdeck:/ { printing = 0 }
    printing { print }
' "${test_root}/advanced-compose.yml" >"${test_root}/advanced-hardware.yml"

awk '
    /^  modemdeck:/ { printing = 1 }
    /^[^ ]/ && printing { printing = 0 }
    printing { print }
' "${test_root}/advanced-compose.yml" >"${test_root}/advanced-app.yml"

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
