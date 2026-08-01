#!/bin/sh

set -eu

tests_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
source_repo=$(CDPATH='' cd -- "${tests_dir}/../.." && pwd)
test_root=$(mktemp -d)
real_stat=$(command -v stat)

cleanup() {
    rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'install-behavior-test: %s\n' "$*" >&2
    exit 1
}

file_mode() {
    if "$real_stat" -c %a "$1" >/dev/null 2>&1; then
        "$real_stat" -c %a "$1"
    else
        "$real_stat" -f %Lp "$1"
    fi
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
cp "${source_repo}/docker-compose.cloudflare.yml" \
    "${fixture}/docker-compose.cloudflare.yml"
cp "${source_repo}/docker-compose.cloudflare-turn.yml" \
    "${fixture}/docker-compose.cloudflare-turn.yml"
cp "${source_repo}/docker-compose.ota.yml" \
    "${fixture}/docker-compose.ota.yml"
cp "${source_repo}/Dockerfile" "${fixture}/Dockerfile"
cp "${source_repo}/hardware/Dockerfile" "${fixture}/hardware/Dockerfile"
cp "${source_repo}/hardware/config/media-bindings.empty.json" \
    "${fixture}/hardware/config/media-bindings.empty.json"
cp "${source_repo}/scripts/prepare-modemdeck-data.sh" \
    "${fixture}/scripts/prepare-modemdeck-data.sh"
cp "${source_repo}/deploy/advanced-assignment.example.json" \
    "${fixture}/deploy/advanced-assignment.example.json"
cp "${source_repo}/deploy/release-manifest.json" \
    "${fixture}/deploy/release-manifest.json"
cp "${source_repo}/VERSION" "${fixture}/VERSION"
cp "${source_repo}/.gitignore" "${fixture}/.gitignore"
chmod 0755 \
    "${fixture}/install.sh" \
    "${fixture}/scripts/prepare-modemdeck-data.sh"
git -C "$fixture" init --quiet
git -C "$fixture" add .
git -C "$fixture" \
    -c user.name=ModemDeck \
    -c user.email=modemdeck@example.invalid \
    commit --quiet -m fixture

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
printf 'chown|%s\n' "$*" >>"${MODEMDECK_TEST_COMMAND_LOG}"
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
    pull)
        if [ -n "${MODEMDECK_TEST_PULL_FAILURE:-}" ] &&
            [ "${2:-}" = "$MODEMDECK_TEST_PULL_FAILURE" ]
        then
            exit 1
        fi
        exit 0
        ;;
    inspect)
        container_id=
        for argument in "$@"; do
            container_id=$argument
        done
        case "$*" in
            *'{{.State.Running}}'*)
                printf '%s\n' true
                exit 0
                ;;
            *com.docker.compose.config-hash*)
                case "$container_id" in
                    hardware-id) printf '%s\n' hardware-hash ;;
                    app-id) printf '%s\n' api-hash ;;
                    web-id) printf '%s\n' modemdeck-hash ;;
                    cloudflared-id) printf '%s\n' cloudflared-hash ;;
                    updater-id) printf '%s\n' updater-hash ;;
                esac
                exit 0
                ;;
        esac
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
    image)
        image_name=
        for argument in "$@"; do
            image_name=$argument
        done
        if [ "${2:-}" = inspect ] &&
            [ -n "${MODEMDECK_TEST_MISSING_IMAGE:-}" ] &&
            [ "$image_name" = "$MODEMDECK_TEST_MISSING_IMAGE" ]
        then
            exit 1
        fi
        case "$*" in
            *RepoDigests*)
                image_repository=${image_name%:*}
                printf '%s@sha256:%064d\n' "$image_repository" 1
                ;;
        esac
        exit 0
        ;;
    *)
        exit 0
        ;;
esac

action=
hash_service=
previous_argument=
for argument in "$@"; do
    if [ "$previous_argument" = --hash ]; then
        hash_service=$argument
    fi
    case "$argument" in
        version|config|ps|build|run|up|down|start|stop|logs)
            action=$argument
            ;;
    esac
    previous_argument=$argument
done
case "$action" in
    version|build|run|start|stop|logs)
        exit 0
        ;;
    config)
        if [ -n "$hash_service" ]; then
            printf '%s %s-hash\n' "$hash_service" "$hash_service"
        fi
        ;;
    ps)
        service=
        for argument in "$@"; do
            service=$argument
        done
        if [ -e "${MODEMDECK_TEST_DOCKER_UP_MARKER}" ]; then
            case "$service" in
                hardware) printf '%s\n' hardware-id ;;
                api) printf '%s\n' app-id ;;
                modemdeck) printf '%s\n' web-id ;;
                cloudflared) printf '%s\n' cloudflared-id ;;
                updater) printf '%s\n' updater-id ;;
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
    "${test_root}/data/recordings/call_existing" \
    "${test_root}/data/tls" \
    "${test_root}/secrets" \
    "${test_root}/systemctl"
printf '%s\n' database-before >"${test_root}/data/modemdeck.db"
printf '%s\n' recording-before \
    >"${test_root}/data/recordings/call_existing/segment.opus"
printf '%s\n' user-certificate-before >"${test_root}/data/tls/user.crt"
printf '%s\n' settings-key-before >"${test_root}/secrets/settings"
chmod 0770 \
    "${test_root}/data/recordings" \
    "${test_root}/data/recordings/call_existing" \
    "${test_root}/data/tls"
chmod 0660 \
    "${test_root}/data/recordings/call_existing/segment.opus" \
    "${test_root}/data/tls/user.crt"

# Persistent application state must never contain links that can escape the
# data directory during a privileged ownership normalization.
printf '%s\n' outside-before >"${test_root}/outside"
ln -s "${test_root}/outside" "${test_root}/data/escape"
if common_env \
    "${fixture}/scripts/prepare-modemdeck-data.sh" "${test_root}/data" \
    >"${test_root}/prepare-link-output.log" 2>&1
then
    fail "data preparation accepted a nested symbolic link"
fi
grep -qx 'outside-before' "${test_root}/outside" ||
    fail "data preparation changed a symbolic-link target outside the data directory"
grep -Fq 'application data must not contain symbolic links' \
    "${test_root}/prepare-link-output.log" ||
    fail "data preparation did not explain the symbolic-link rejection"
rm -f -- "${test_root}/data/escape"

# A failed simple startup must return host service state to its exact baseline.
: >"${test_root}/commands.log"
write_unit_state ModemManager.service enabled 1
write_unit_state modemdeck-agent.service masked 0
if common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=fail \
    "${fixture}/install.sh" \
        --git \
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
[ "$(file_mode "${test_root}/data/recordings")" = 700 ] ||
    fail "recording root was not normalized to mode 0700"
[ "$(file_mode "${test_root}/data/recordings/call_existing")" = 700 ] ||
    fail "recording call directory was not normalized to mode 0700"
[ "$(file_mode "${test_root}/data/recordings/call_existing/segment.opus")" = 600 ] ||
    fail "recording file was not normalized to mode 0600"
[ "$(file_mode "${test_root}/data/tls")" = 750 ] ||
    fail "TLS directory was not normalized to mode 0750"
[ "$(file_mode "${test_root}/data/tls/user.crt")" = 640 ] ||
    fail "TLS file was not normalized to mode 0640"
grep -Eq "^chown\\|-h [^ ]+ ${test_root}/data( |$)" \
    "${test_root}/commands.log" ||
    fail "data ownership normalization can dereference symbolic links"

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
        --git \
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
        --git \
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

# Cloudflare is an installer-owned optional service. The connector works
# without TURN, while the API hostname is discovered from active ingress.
printf '%s\n' \
    'eyJhbGciOiJIUzI1NiJ9X19tb2RlbWRlY2tfZGVwbG95bWVudF90ZXN0X3Rva2VuX19sb25nX2Vub3VnaF9mb3JfdmFsaWRhdGlvbg' \
    >"${test_root}/cloudflare-token-input"
printf '%s\n' 'test-cloudflare-turn-token' \
    >"${test_root}/cloudflare-turn-token-input"
printf '%s\n' \
    'MODEMDECK_CLOUDFLARE_HOSTNAME=stale.example.com' \
    'MODEMDECK_CLOUDFLARE_PUBLIC_URL=https://stale.example.com' \
    >>"${fixture}/.env"
: >"${test_root}/commands.log"
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    MODEMDECK_HOST_DBUS_SOCKET=/var/run/docker.sock \
    "${fixture}/install.sh" \
        --git \
        --mode advanced \
        --assignment-file "${test_root}/assignments.json" \
        --cloudflare-token-file "${test_root}/cloudflare-token-input" \
        --allow-dirty \
        >"${test_root}/cloudflare-only-output.log" 2>&1
if [ -e "${fixture}/secrets/cloudflare-turn-token" ]; then
    fail "Tunnel-only installation created a TURN token secret"
fi
if grep -Fq 'docker-compose.cloudflare-turn.yml' \
    "${test_root}/commands.log"
then
    fail "Tunnel-only installation enabled the TURN Compose override"
fi
grep -Fq 'TURN:          disabled' \
    "${test_root}/cloudflare-only-output.log" ||
    fail "Tunnel-only installation did not report TURN as disabled"

: >"${test_root}/commands.log"
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    MODEMDECK_HOST_DBUS_SOCKET=/var/run/docker.sock \
    "${fixture}/install.sh" \
        --git \
        --mode advanced \
        --assignment-file "${test_root}/assignments.json" \
        --cloudflare-turn-key-id 0123456789abcdef0123456789abcdef \
        --cloudflare-turn-token-file \
            "${test_root}/cloudflare-turn-token-input" \
        --allow-dirty \
        >"${test_root}/cloudflare-output.log" 2>&1
grep -Fq 'MODEMDECK_CLOUDFLARE_ENABLED=true' "${fixture}/.env" ||
    fail "Cloudflare enablement was not persisted"
if grep -Eq '^MODEMDECK_CLOUDFLARE_(HOSTNAME|PUBLIC_URL)=' \
    "${fixture}/.env"
then
    fail "obsolete static Cloudflare hostname settings were retained"
fi
grep -Fq \
    'MODEMDECK_CLOUDFLARE_TURN_KEY_ID=0123456789abcdef0123456789abcdef' \
    "${fixture}/.env" ||
    fail "Cloudflare TURN key ID was not persisted"
grep -Fq 'MODEMDECK_WEB_IMAGE=modemdeck-web' "${fixture}/.env" ||
    fail "Web gateway image was not persisted"
grep -qx \
    'eyJhbGciOiJIUzI1NiJ9X19tb2RlbWRlY2tfZGVwbG95bWVudF90ZXN0X3Rva2VuX19sb25nX2Vub3VnaF9mb3JfdmFsaWRhdGlvbg' \
    "${fixture}/secrets/cloudflare-tunnel-token" ||
    fail "Cloudflare token was not normalized into its file secret"
[ "$(file_mode "${fixture}/secrets/cloudflare-tunnel-token")" = 440 ] ||
    fail "Cloudflare token does not use mode 0440"
grep -qx 'test-cloudflare-turn-token' \
    "${fixture}/secrets/cloudflare-turn-token" ||
    fail "Cloudflare TURN token was not normalized into its file secret"
[ "$(file_mode "${fixture}/secrets/cloudflare-turn-token")" = 440 ] ||
    fail "Cloudflare TURN token does not use mode 0440"
if grep -Fq \
    'eyJhbGciOiJIUzI1NiJ9X19tb2RlbWRlY2tfZGVwbG95bWVudF90ZXN0X3Rva2Vu' \
    "${test_root}/cloudflare-output.log"
then
    fail "Cloudflare token leaked into installer output"
fi
if grep -Fq 'test-cloudflare-turn-token' \
    "${test_root}/cloudflare-output.log"
then
    fail "Cloudflare TURN token leaked into installer output"
fi
if grep -Eq '^docker\|compose .* build( |$)' \
    "${test_root}/commands.log"
then
    fail "TURN configuration rebuilt unchanged component images"
fi
grep -Eq '^docker\|compose .* up .* --force-recreate .* api( |$)' \
    "${test_root}/commands.log" ||
    fail "TURN configuration did not update the API container"
grep -Eq '^docker\|compose .* up .* --force-recreate .* cloudflared( |$)' \
    "${test_root}/commands.log" ||
    fail "Tunnel configuration did not update cloudflared"
if grep -Eq '^docker\|compose .* up .* hardware( |$)' \
    "${test_root}/commands.log"
then
    fail "TURN configuration restarted the hardware container"
fi
grep -Fq 'cloudflared' "${test_root}/commands.log" ||
    fail "installer did not wait for cloudflared"
grep -Fq 'docker-compose.cloudflare-turn.yml' "${test_root}/commands.log" ||
    fail "TURN installation did not enable the TURN Compose override"

# TURN can be disabled without removing the Tunnel connector or its secrets.
: >"${test_root}/commands.log"
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    MODEMDECK_HOST_DBUS_SOCKET=/var/run/docker.sock \
    "${fixture}/install.sh" \
        --git \
        --mode advanced \
        --assignment-file "${test_root}/assignments.json" \
        --disable-cloudflare-turn \
        --allow-dirty \
        >"${test_root}/cloudflare-turn-disabled-output.log" 2>&1
grep -Fq 'MODEMDECK_CLOUDFLARE_ENABLED=true' "${fixture}/.env" ||
    fail "disabling TURN also disabled the Tunnel connector"
grep -qx 'MODEMDECK_CLOUDFLARE_TURN_KEY_ID=' "${fixture}/.env" ||
    fail "disabling TURN did not clear TURN enablement"
if grep -Fq 'docker-compose.cloudflare-turn.yml' \
    "${test_root}/commands.log"
then
    fail "disabled TURN remained in the Compose stack"
fi
grep -Fq 'docker-compose.cloudflare.yml' "${test_root}/commands.log" ||
    fail "disabling TURN removed the Tunnel Compose override"
grep -qx 'test-cloudflare-turn-token' \
    "${fixture}/secrets/cloudflare-turn-token" ||
    fail "disabling TURN destroyed the persisted TURN token"
grep -Fq 'TURN:          disabled' \
    "${test_root}/cloudflare-turn-disabled-output.log" ||
    fail "disabled TURN was not reported"

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
        --git \
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

# A release that changes only API/Web inputs must retain the running hardware
# image and container. VERSION is consumed by both application images but not
# by the independently versioned hardware image.
hardware_version_before=$(sed -n \
    's/^MODEMDECK_HARDWARE_VERSION=//p' "${fixture}/.env")
printf '%s\n' 'selective-v2' >"${fixture}/VERSION"
git -C "$fixture" add VERSION
git -C "$fixture" \
    -c user.name=ModemDeck \
    -c user.email=modemdeck@example.invalid \
    commit --quiet -m api-web-update
: >"${test_root}/commands.log"
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    "${fixture}/install.sh" \
        --git \
        --mode simple \
        --version selective-v2 \
        >"${test_root}/selective-update-output.log" 2>&1
grep -Eq '^docker\|compose .* build api modemdeck( |$)' \
    "${test_root}/commands.log" ||
    fail "API/Web update did not build exactly the changed images"
if grep -Eq '^docker\|compose .* build .*hardware' \
    "${test_root}/commands.log"
then
    fail "API/Web update rebuilt the hardware image"
fi
if grep -Eq '^docker\|compose .* up .* hardware( |$)' \
    "${test_root}/commands.log"
then
    fail "API/Web update restarted the hardware container"
fi
grep -Eq '^docker\|compose .* up .* --no-deps .*--force-recreate .* api( |$)' \
    "${test_root}/commands.log" ||
    fail "API/Web update did not target the API container independently"
grep -Eq '^docker\|compose .* up .* --no-deps .*--force-recreate .* modemdeck( |$)' \
    "${test_root}/commands.log" ||
    fail "API/Web update did not target the Web container independently"
grep -Fq 'Containers:    hardware=retain, api=update, web=update' \
    "${test_root}/selective-update-output.log" ||
    fail "API/Web update plan did not report retained hardware"
[ "$(sed -n 's/^MODEMDECK_HARDWARE_VERSION=//p' "${fixture}/.env")" \
    = "$hardware_version_before" ] ||
    fail "API/Web update changed the retained hardware image tag"
grep -qx 'MODEMDECK_API_VERSION=selective-v2' "${fixture}/.env" ||
    fail "API component version was not advanced"
grep -qx 'MODEMDECK_WEB_VERSION=selective-v2' "${fixture}/.env" ||
    fail "Web component version was not advanced"

# External hardware configuration is not part of the Git diff or Compose
# service hash. Persist its baseline, then verify a same-path content change
# force-recreates Hardware without rebuilding its image.
cp "${fixture}/hardware/config/media-bindings.empty.json" \
    "${test_root}/media-bindings.json"
: >"${test_root}/commands.log"
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    "${fixture}/install.sh" \
        --git \
        --mode simple \
        --version selective-v2 \
        --media-bindings-file "${test_root}/media-bindings.json" \
        >"${test_root}/media-bindings-baseline-output.log" 2>&1
printf '\n' >>"${test_root}/media-bindings.json"
: >"${test_root}/commands.log"
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    "${fixture}/install.sh" \
        --git \
        --mode simple \
        --version selective-v2 \
        --media-bindings-file "${test_root}/media-bindings.json" \
        >"${test_root}/media-bindings-update-output.log" 2>&1
if grep -Eq '^docker\|compose .* build( |$)' \
    "${test_root}/commands.log"
then
    fail "external media-binding update rebuilt a component image"
fi
grep -Eq '^docker\|compose .* up .* --force-recreate .* hardware( |$)' \
    "${test_root}/commands.log" ||
    fail "external media-binding update did not replace Hardware"

# If an independently retained image was pruned, rebuild that component from
# current source under the requested version instead of silently reusing its
# now-missing historical tag.
: >"${test_root}/commands.log"
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    MODEMDECK_TEST_MISSING_IMAGE="modemdeck-hardware:${hardware_version_before}" \
    "${fixture}/install.sh" \
        --git \
        --mode simple \
        --version recovered-v3 \
        >"${test_root}/missing-image-output.log" 2>&1
grep -Eq '^docker\|compose .* build hardware( |$)' \
    "${test_root}/commands.log" ||
    fail "missing retained Hardware image was not rebuilt"
grep -Eq '^docker\|compose .* up .* --force-recreate .* hardware( |$)' \
    "${test_root}/commands.log" ||
    fail "rebuilt missing Hardware image did not replace its container"
if grep -Eq '^docker\|compose .* build .*(api|modemdeck)' \
    "${test_root}/commands.log"
then
    fail "missing Hardware image rebuilt an unchanged application image"
fi
grep -qx 'MODEMDECK_HARDWARE_VERSION=recovered-v3' "${fixture}/.env" ||
    fail "rebuilt missing Hardware image retained its unavailable old tag"

# The explicit escape hatch rebuilds every image and force-recreates the full
# stack, including Hardware/ModemManager.
: >"${test_root}/commands.log"
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    "${fixture}/install.sh" \
        --git \
        --mode simple \
        --version full-v2 \
        --rebuild-all \
        >"${test_root}/full-rebuild-output.log" 2>&1
grep -Eq '^docker\|compose .* build hardware api modemdeck( |$)' \
    "${test_root}/commands.log" ||
    fail "--rebuild-all did not build every component image"
grep -Eq '^docker\|compose .* up .* --force-recreate( |$)' \
    "${test_root}/commands.log" ||
    fail "--rebuild-all did not force-recreate the deployment"
grep -Fq 'Update plan:   rebuild and recreate every container' \
    "${test_root}/full-rebuild-output.log" ||
    fail "--rebuild-all did not report its disruptive update plan"
for component_key in \
    MODEMDECK_API_VERSION \
    MODEMDECK_WEB_VERSION \
    MODEMDECK_HARDWARE_VERSION
do
    grep -qx "${component_key}=full-v2" "${fixture}/.env" ||
        fail "--rebuild-all did not advance $component_key"
done

# Without --git, the installer pulls the published release and pins every
# ModemDeck image to the digest returned by the registry. The updater is part
# of this release deployment; source builds remain opt-in above.
printf '%s\n' '1.9.3' >"${fixture}/VERSION"
cp "${fixture}/.env" "${test_root}/before-required-updater.env"
: >"${test_root}/commands.log"
if common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    MODEMDECK_TEST_PULL_FAILURE=ghcr.io/human-agent65535/modemdeck-updater:v1.9.3 \
    "${fixture}/install.sh" \
        --mode simple \
        --version v1.9.3 \
        >"${test_root}/missing-updater-output.log" 2>&1
then
    fail "release deployment accepted a missing updater image"
fi
cmp -s "${fixture}/.env" "${test_root}/before-required-updater.env" ||
    fail "missing updater image changed the installed Compose environment"

: >"${test_root}/commands.log"
common_env \
    MODEMDECK_TEST_DOCKER_HEALTH=healthy \
    "${fixture}/install.sh" \
        --mode simple \
        --version v1.9.3 \
        >"${test_root}/release-output.log" 2>&1
for release_image in \
    ghcr.io/human-agent65535/modemdeck:v1.9.3 \
    ghcr.io/human-agent65535/modemdeck-web:v1.9.3 \
    ghcr.io/human-agent65535/modemdeck-hardware:v1.9.3 \
    ghcr.io/human-agent65535/modemdeck-updater:v1.9.3
do
    grep -Fq "docker|pull ${release_image}" "${test_root}/commands.log" ||
        fail "default release deployment did not pull ${release_image}"
done
if grep -Eq '^docker\|compose .* build( |$)' "${test_root}/commands.log"; then
    fail "default release deployment built a local image"
fi
grep -Fq 'Deployment:    release' "${test_root}/release-output.log" ||
    fail "default installer mode did not report release deployment"
grep -Eq '^MODEMDECK_API_IMAGE_REF=ghcr.io/human-agent65535/modemdeck:v1.9.3@sha256:[0-9]{64}$' \
    "${fixture}/.env" ||
    fail "default release deployment did not pin the API digest"
grep -qx 'MODEMDECK_UPDATER_URL=http://updater:8081' "${fixture}/.env" ||
    fail "default release deployment did not enable the updater control plane"

printf '%s\n' "install-behavior-test: ok"
