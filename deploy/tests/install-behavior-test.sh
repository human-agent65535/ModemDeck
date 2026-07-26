#!/bin/sh

set -eu

tests_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
source_repo=$(CDPATH='' cd -- "${tests_dir}/../.." && pwd)
test_root=$(mktemp -d)

cleanup() {
    rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'install-behavior-test: %s\n' "$*" >&2
    exit 1
}

fixture="${test_root}/repo"
mkdir -p \
    "${fixture}/deploy" \
    "${fixture}/hardware/config" \
    "${fixture}/scripts" \
    "${test_root}/bin" \
    "${test_root}/dev" \
    "${test_root}/proc/net" \
    "${test_root}/run/systemd/system" \
    "${test_root}/run/udev" \
    "${test_root}/state" \
    "${test_root}/sys" \
    "${test_root}/tmp"
cp "${source_repo}/install.sh" "${fixture}/install.sh"
cp "${source_repo}/docker-compose.yml" "${fixture}/docker-compose.yml"
cp "${source_repo}/docker-compose.advanced.yml" \
    "${fixture}/docker-compose.advanced.yml"
cp "${source_repo}/Dockerfile" "${fixture}/Dockerfile"
cp "${source_repo}/hardware/Dockerfile" "${fixture}/hardware/Dockerfile"
cp "${source_repo}/hardware/config/media-bindings.empty.json" \
    "${fixture}/hardware/config/media-bindings.empty.json"
cp "${source_repo}/scripts/prepare-modemdeck-data.sh" \
    "${fixture}/scripts/prepare-modemdeck-data.sh"
cp "${source_repo}/deploy/advanced-assignment.example.json" \
    "${fixture}/deploy/advanced-assignment.example.json"
chmod 0755 \
    "${fixture}/install.sh" \
    "${fixture}/scripts/prepare-modemdeck-data.sh"

cat >"${test_root}/proc/net/tcp" <<'EOF'
  sl  local_address rem_address   st
EOF
cp "${test_root}/proc/net/tcp" "${test_root}/proc/net/tcp6"

cat >"${test_root}/bin/uname" <<'EOF'
#!/bin/sh
case "${1:-}" in
    -s) printf '%s\n' Linux ;;
    -m) printf '%s\n' x86_64 ;;
    *) printf '%s\n' Linux ;;
esac
EOF

cat >"${test_root}/bin/id" <<'EOF'
#!/bin/sh
case "${1:-}" in
    -u|-g) printf '%s\n' 0 ;;
    *) printf '%s\n' 0 ;;
esac
EOF

cat >"${test_root}/bin/chown" <<'EOF'
#!/bin/sh
exit 0
EOF

cat >"${test_root}/bin/install" <<'EOF'
#!/bin/sh
directory=false
mode=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -d)
            directory=true
            shift
            ;;
        -o|-g)
            shift 2
            ;;
        -m)
            mode=$2
            shift 2
            ;;
        --)
            shift
            break
            ;;
        -*)
            shift
            ;;
        *)
            break
            ;;
    esac
done
if [ "$directory" = true ]; then
    for path in "$@"; do
        mkdir -p -- "$path"
        [ -z "$mode" ] || chmod "$mode" "$path"
    done
    exit 0
fi
[ "$#" -eq 2 ] || exit 64
cp "$1" "$2"
[ -z "$mode" ] || chmod "$mode" "$2"
EOF

cat >"${test_root}/bin/stat" <<'EOF'
#!/bin/sh
case "${1:-}:${2:-}" in
    -c:%s)
        wc -c <"$3" | tr -d ' '
        ;;
    -c:%u|-c:%g)
        printf '%s\n' 0
        ;;
    -c:%a)
        printf '%s\n' 600
        ;;
    *)
        exit 64
        ;;
esac
EOF

cat >"${test_root}/bin/docker" <<'EOF'
#!/bin/sh
printf 'docker|%s\n' "$*" >>"${MODEMDECK_TEST_COMMAND_LOG}"
case "${1:-}" in
    info)
        exit 0
        ;;
    inspect)
        container_id=
        for argument in "$@"; do
            container_id=$argument
        done
        if [ "${MODEMDECK_TEST_DOCKER_HEALTH:-healthy}" = fail ] &&
            [ "$container_id" = hardware-id ]
        then
            printf '%s\n' unhealthy
        else
            printf '%s\n' healthy
        fi
        exit 0
        ;;
    compose)
        shift
        ;;
    *)
        exit 0
        ;;
esac

action=
for argument in "$@"; do
    case "$argument" in
        version|config|ps|build|run|up|down|logs)
            action=$argument
            break
            ;;
    esac
done
case "$action" in
    version|config|build|run|logs)
        exit 0
        ;;
    ps)
        service=
        for argument in "$@"; do
            service=$argument
        done
        if [ -e "${MODEMDECK_TEST_DOCKER_UP_MARKER}" ]; then
            case "$service" in
                hardware) printf '%s\n' hardware-id ;;
                modemdeck) printf '%s\n' app-id ;;
            esac
        fi
        ;;
    up)
        : >"${MODEMDECK_TEST_DOCKER_UP_MARKER}"
        ;;
    down)
        rm -f -- "${MODEMDECK_TEST_DOCKER_UP_MARKER}"
        ;;
esac
EOF

cat >"${test_root}/bin/systemctl" <<'EOF'
#!/bin/sh
printf 'systemctl|%s\n' "$*" >>"${MODEMDECK_TEST_COMMAND_LOG}"
command_name=${1:-}
shift || true
unit=
runtime=false
for argument in "$@"; do
    case "$argument" in
        --quiet|--property=*|--value) ;;
        --runtime) runtime=true ;;
        *) unit=$argument ;;
    esac
done
state_file="${MODEMDECK_TEST_SYSTEMCTL_STATE_DIR}/${unit}"

load_state() {
    if [ ! -f "$state_file" ]; then
        present=0
        enabled=disabled
        active=0
        return
    fi
    # The test owns these files and writes only fixed shell assignments.
    . "$state_file"
}

save_state() {
    {
        printf 'present=%s\n' "$present"
        printf 'enabled=%s\n' "$enabled"
        printf 'active=%s\n' "$active"
    } >"$state_file"
}

case "$command_name" in
    daemon-reload)
        exit 0
        ;;
esac
load_state
case "$command_name" in
    show)
        if [ "$present" = 1 ]; then
            printf '%s\n' loaded
        else
            printf '%s\n' not-found
        fi
        ;;
    is-enabled)
        printf '%s\n' "$enabled"
        [ "$present" = 1 ]
        ;;
    is-active)
        if [ "$active" = 1 ]; then
            printf '%s\n' active
            exit 0
        fi
        printf '%s\n' inactive
        exit 3
        ;;
    stop)
        active=0
        save_state
        ;;
    start)
        active=1
        save_state
        ;;
    disable)
        enabled=disabled
        save_state
        ;;
    enable)
        if [ "$runtime" = true ]; then
            enabled=enabled-runtime
        else
            enabled=enabled
        fi
        save_state
        ;;
    unmask)
        enabled=disabled
        save_state
        ;;
    mask)
        if [ "${MODEMDECK_TEST_MASK_FAILURE_UNIT:-}" = "$unit" ]; then
            exit 1
        fi
        if [ "$runtime" = true ]; then
            enabled=masked-runtime
        else
            enabled=masked
        fi
        save_state
        ;;
    *)
        exit 64
        ;;
esac
EOF
chmod 0755 "${test_root}/bin/"*

write_unit_state() {
    unit_name=$1
    unit_enabled=$2
    unit_active=$3
    {
        printf 'present=1\n'
        printf 'enabled=%s\n' "$unit_enabled"
        printf 'active=%s\n' "$unit_active"
    } >"${test_root}/systemctl/${unit_name}"
}

common_env() {
    env \
        PATH="${test_root}/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
        TMPDIR="${test_root}/tmp" \
        MODEMDECK_TEST_COMMAND_LOG="${test_root}/commands.log" \
        MODEMDECK_TEST_DOCKER_UP_MARKER="${test_root}/docker-up" \
        MODEMDECK_TEST_SYSTEMCTL_STATE_DIR="${test_root}/systemctl" \
        MODEMDECK_HOST_SYS_ROOT="${test_root}/sys" \
        MODEMDECK_HOST_DEV_ROOT="${test_root}/dev" \
        MODEMDECK_HOST_PROC_ROOT="${test_root}/proc" \
        MODEMDECK_HOST_UDEV_ROOT="${test_root}/run/udev" \
        MODEMDECK_SYSTEMD_RUNTIME_DIR="${test_root}/run/systemd/system" \
        MODEMDECK_INSTALL_STATE_DIR="${test_root}/state/install" \
        MODEMDECK_DATA_DIR="${test_root}/data" \
        MODEMDECK_SETTINGS_KEY_FILE="${test_root}/secrets/settings" \
        "$@"
}

mkdir -p \
    "${test_root}/data/tls" \
    "${test_root}/secrets" \
    "${test_root}/systemctl"
printf '%s\n' database-before >"${test_root}/data/modemdeck.db"
printf '%s\n' user-certificate-before >"${test_root}/data/tls/user.crt"
printf '%s\n' settings-key-before >"${test_root}/secrets/settings"

# A failed simple startup must return host service state to its exact baseline.
: >"${test_root}/commands.log"
write_unit_state ModemManager.service enabled 1
write_unit_state modemdeck-agent.service masked 0
if common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=fail \
    "${fixture}/install.sh" \
        --mode simple \
        --allow-dirty \
        >"${test_root}/simple-output.log" 2>&1
then
    fail "simple failure fixture unexpectedly succeeded"
fi
grep -qx 'enabled=enabled' \
    "${test_root}/systemctl/ModemManager.service" ||
    fail "simple rollback did not restore ModemManager enable state"
grep -qx 'active=1' "${test_root}/systemctl/ModemManager.service" ||
    fail "simple rollback did not restart ModemManager"
grep -qx 'enabled=masked' \
    "${test_root}/systemctl/modemdeck-agent.service" ||
    fail "simple rollback changed the legacy Agent mask state"
grep -qx 'active=0' "${test_root}/systemctl/modemdeck-agent.service" ||
    fail "simple rollback changed the legacy Agent active state"
grep -Fq 'docker|compose ' "${test_root}/commands.log" ||
    fail "simple fixture did not reach Docker Compose"
grep -Eq '^docker\|compose .* down( |$)' "${test_root}/commands.log" ||
    fail "simple failure did not remove the incomplete deployment"
[ ! -e "${test_root}/state/install/host-services.state" ] ||
    fail "successful simple rollback retained baseline state"
[ ! -e "${test_root}/state/install/host-services.rollback" ] ||
    fail "successful simple rollback retained transaction state"
[ ! -e "${fixture}/.env" ] ||
    fail "failed simple installation persisted Compose environment"

# Advanced mode must never mutate host services and must preserve local state.
rm -f -- "${test_root}/docker-up"
: >"${test_root}/commands.log"
rm -f -- "${test_root}/systemctl/modemdeck-agent.service"
write_unit_state ModemManager.service enabled 1
cat >"${test_root}/assignments.json" <<'EOF'
{
  "version": 1,
  "host": {
    "proc_root": "/run/host-proc",
    "system_bus_address": "unix:path=/run/host-dbus/system_bus_socket"
  },
  "assignments": [
    {
      "id": "modem-bay-a",
      "required_at_startup": false,
      "match": {
        "usb": {
          "vendor_id": "2c7c",
          "product_id": "0125",
          "serial": "SERIAL-001"
        }
      }
    }
  ]
}
EOF
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    MODEMDECK_HOST_DBUS_SOCKET=/var/run/docker.sock \
    "${fixture}/install.sh" \
        --mode advanced \
        --assignment-file "${test_root}/assignments.json" \
        --allow-dirty \
        >"${test_root}/advanced-output.log" 2>&1

if grep -q '^systemctl|' "${test_root}/commands.log"; then
    fail "advanced mode inspected or mutated a host service"
fi
grep -qx 'enabled=enabled' \
    "${test_root}/systemctl/ModemManager.service" ||
    fail "advanced mode changed ModemManager enable state"
grep -qx 'active=1' "${test_root}/systemctl/ModemManager.service" ||
    fail "advanced mode changed ModemManager active state"
grep -Fq 'run --rm --no-deps --entrypoint /usr/local/bin/modemdeck-device-owner' \
    "${test_root}/commands.log" ||
    fail "advanced mode skipped authoritative assignment validation"
grep -Fq 'MODEMDECK_HARDWARE_MODE=advanced' "${fixture}/.env" ||
    fail "advanced mode was not persisted"
grep -Fq "MODEMDECK_ASSIGNMENT_FILE=${test_root}/assignments.json" \
    "${fixture}/.env" ||
    fail "advanced assignment path was not persisted"
[ ! -e "${test_root}/state/install/host-services.state" ] ||
    fail "advanced mode created host service state"

for preserved_file in \
    "${test_root}/data/modemdeck.db:database-before" \
    "${test_root}/data/tls/user.crt:user-certificate-before" \
    "${test_root}/secrets/settings:settings-key-before"
do
    preserved_path=${preserved_file%%:*}
    preserved_value=${preserved_file#*:}
    grep -qx "$preserved_value" "$preserved_path" ||
        fail "advanced installation replaced persistent file: $preserved_path"
done

# A repeated install must keep the same database, TLS, and secret contents.
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    MODEMDECK_HOST_DBUS_SOCKET=/var/run/docker.sock \
    "${fixture}/install.sh" \
        --mode advanced \
        --assignment-file "${test_root}/assignments.json" \
        --allow-dirty \
        >"${test_root}/advanced-repeat-output.log" 2>&1
grep -qx 'user-certificate-before' "${test_root}/data/tls/user.crt" ||
    fail "repeat installation replaced the user certificate"
grep -qx 'database-before' "${test_root}/data/modemdeck.db" ||
    fail "repeat installation replaced the database"
grep -qx 'settings-key-before' "${test_root}/secrets/settings" ||
    fail "repeat installation replaced the settings key"

# A successful simple install requires host ModemManager to be masked. A
# stopped and disabled legacy Agent may retain its local unit file when systemd
# cannot replace that file with a mask.
: >"${test_root}/commands.log"
write_unit_state ModemManager.service enabled 1
write_unit_state modemdeck-agent.service disabled 1
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    MODEMDECK_TEST_MASK_FAILURE_UNIT=modemdeck-agent.service \
    "${fixture}/install.sh" \
        --mode simple \
        --allow-dirty \
        >"${test_root}/simple-success-output.log" 2>&1
grep -qx 'enabled=masked' \
    "${test_root}/systemctl/ModemManager.service" ||
    fail "simple success did not mask ModemManager"
grep -qx 'enabled=disabled' \
    "${test_root}/systemctl/modemdeck-agent.service" ||
    fail "simple success did not disable the legacy Agent"
for simple_unit in ModemManager.service modemdeck-agent.service; do
    grep -qx 'active=0' "${test_root}/systemctl/${simple_unit}" ||
        fail "simple success did not stop $simple_unit"
done
grep -qx 'status|installed' \
    "${test_root}/state/install/host-services.state" ||
    fail "simple success did not retain the original host service state"
[ ! -e "${test_root}/state/install/host-services.rollback" ] ||
    fail "simple success retained a completed rollback transaction"
grep -Fq 'MODEMDECK_HARDWARE_MODE=simple' "${fixture}/.env" ||
    fail "simple mode was not persisted"
grep -qx 'user-certificate-before' "${test_root}/data/tls/user.crt" ||
    fail "simple installation replaced the user certificate"
grep -qx 'database-before' "${test_root}/data/modemdeck.db" ||
    fail "simple installation replaced the database"

printf '%s\n' "install-behavior-test: ok"
