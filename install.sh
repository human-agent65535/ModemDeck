#!/bin/sh

set -eu
umask 077

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$script_dir
env_file="${repo_dir}/.env"
compose_file="${repo_dir}/docker-compose.yml"
advanced_compose_file="${repo_dir}/docker-compose.advanced.yml"
cloudflare_compose_file="${repo_dir}/docker-compose.cloudflare.yml"
cloudflare_turn_compose_file="${repo_dir}/docker-compose.cloudflare-turn.yml"
assignment_example="${repo_dir}/deploy/advanced-assignment.example.json"

mode_arg=
assignment_arg=
bind_address_arg=
port_arg=
tls_hosts_arg=
media_bindings_arg=
version_arg=
cloudflare_token_arg=
cloudflare_hostname_arg=
cloudflare_turn_key_id_arg=
cloudflare_turn_token_arg=
disable_cloudflare=false
disable_cloudflare_turn=false
rebuild_all=false
allow_dirty=false
check_only=false
cloudflare_enabled=false
cloudflare_turn_enabled=false

work_dir=
env_work=
transaction_state=
rollback_state_file=
compose_ready=false
compose_started=false
api_quiesced=false
services_changed=false
baseline_created=false
installation_complete=false
legacy_cloudflared_id=
deployment_was_running=false

usage() {
    cat <<'EOF'
Usage: sudo ./install.sh [options]

Build and run ModemDeck as three Docker containers, plus an optional Cloudflare
Tunnel connector. No Go, Node.js,
ModemManager, D-Bus, NetworkManager, or Polkit build packages are installed on
the host.

Modes:
  simple    Default for a new installation. Stop, disable, and mask the host
            ModemManager and legacy modemdeck-agent services, then let the
            hardware container discover all modems.
  advanced  Manage only explicitly assigned devices. Host services, udev,
            Polkit, and firewall rules remain entirely operator-managed; any
            ownership conflict fails closed.

Options:
  --mode MODE             simple or advanced
  --assignment-file FILE  Required in advanced mode; never copied into Git
  --bind-address ADDRESS  Host address for the HTTPS Web UI (default: 127.0.0.1)
  --port PORT             HTTPS Web UI port (default: 7577)
  --tls-hosts LIST        Comma-separated certificate DNS names and IPs
  --media-bindings-file FILE
                          Optional explicit modem audio bindings JSON
  --cloudflare-token-file FILE
                          Enable cloudflared using a remotely-managed Tunnel
                          token read from FILE
  --cloudflare-turn-key-id ID
                          Enable Cloudflare Web and iOS call relay using a
                          Cloudflare Realtime TURN key ID
  --cloudflare-turn-token-file FILE
                          TURN API token read from FILE
  --disable-cloudflare-turn
                          Disable TURN while keeping the Tunnel connector
  --disable-cloudflare    Disable the installed Tunnel connector and iOS pairing
  --version TAG           Docker image tag (default: current Git revision)
  --rebuild-all           Rebuild every image and force-recreate every container;
                          this interrupts ModemManager and attached modems
  --allow-dirty           Allow deployment from a modified Git checkout
  --check                 Read-only validation; build or change nothing
  -h, --help              Show this help

Nginx exposes three isolated listeners: HTTP port 7575 is the Tunnel API origin,
HTTP port 7576 is the Tunnel Web origin, and HTTPS port 7577 serves the local
Web UI. Users manage Cloudflare ingress rules themselves. Only HTTPS port 7577
is published to the host. The Go API listens only on port 8080 inside the
private Compose network. iOS pairing is available only while the configured
public API route is connected. The installer preserves application data,
settings secrets, and the Tunnel token. On a first installation, open the local
HTTPS Web UI and complete Quick Start to create the administrator username and
password.
EOF
}

log() {
    printf '\n==> %s\n' "$*"
}

warn() {
    printf 'install.sh: warning: %s\n' "$*" >&2
}

fail() {
    printf 'install.sh: %s\n' "$*" >&2
    exit 1
}

compose() {
    if [ "$mode" = advanced ]; then
        if [ "$cloudflare_enabled" = true ]; then
            if [ "$cloudflare_turn_enabled" = true ]; then
                docker compose \
                    --project-directory "$repo_dir" \
                    --env-file "$env_work" \
                    -f "$compose_file" \
                    -f "$advanced_compose_file" \
                    -f "$cloudflare_compose_file" \
                    -f "$cloudflare_turn_compose_file" \
                    "$@"
            else
                docker compose \
                    --project-directory "$repo_dir" \
                    --env-file "$env_work" \
                    -f "$compose_file" \
                    -f "$advanced_compose_file" \
                    -f "$cloudflare_compose_file" \
                    "$@"
            fi
        else
            docker compose \
                --project-directory "$repo_dir" \
                --env-file "$env_work" \
                -f "$compose_file" \
                -f "$advanced_compose_file" \
                "$@"
        fi
    else
        if [ "$cloudflare_enabled" = true ]; then
            if [ "$cloudflare_turn_enabled" = true ]; then
                docker compose \
                    --project-directory "$repo_dir" \
                    --env-file "$env_work" \
                    -f "$compose_file" \
                    -f "$cloudflare_compose_file" \
                    -f "$cloudflare_turn_compose_file" \
                    "$@"
            else
                docker compose \
                    --project-directory "$repo_dir" \
                    --env-file "$env_work" \
                    -f "$compose_file" \
                    -f "$cloudflare_compose_file" \
                    "$@"
            fi
        else
            docker compose \
                --project-directory "$repo_dir" \
                --env-file "$env_work" \
                -f "$compose_file" \
                "$@"
        fi
    fi
}

restore_service_state() {
    restore_file=$1
    [ -f "$restore_file" ] || return 0
    restore_failed=0

    while IFS='|' read -r record unit present enabled_state was_active was_masked
    do
        [ "$record" = unit ] || continue
        [ "$present" = 1 ] || continue

        systemctl unmask "$unit" >/dev/null 2>&1 || restore_failed=1
        case "$enabled_state" in
            enabled)
                systemctl enable "$unit" >/dev/null 2>&1 || restore_failed=1
                ;;
            enabled-runtime)
                systemctl enable --runtime "$unit" >/dev/null 2>&1 ||
                    restore_failed=1
                ;;
            disabled|masked|masked-runtime)
                systemctl disable "$unit" >/dev/null 2>&1 || restore_failed=1
                ;;
            static|indirect|generated|transient|alias|linked|linked-runtime)
                ;;
            *)
                warn "unsupported saved enable state for $unit: $enabled_state"
                restore_failed=1
                ;;
        esac

        if [ "$was_active" = 1 ]; then
            systemctl start "$unit" >/dev/null 2>&1 || restore_failed=1
        else
            systemctl stop "$unit" >/dev/null 2>&1 || restore_failed=1
        fi

        if [ "$was_masked" = 1 ]; then
            case "$enabled_state" in
                masked-runtime)
                    systemctl mask --runtime "$unit" >/dev/null 2>&1 ||
                        restore_failed=1
                    ;;
                *)
                    systemctl mask "$unit" >/dev/null 2>&1 || restore_failed=1
                    ;;
            esac
        fi
    done <"$restore_file"
    systemctl daemon-reload >/dev/null 2>&1 || restore_failed=1
    [ "$restore_failed" -eq 0 ]
}

finish() {
    finish_status=$1
    trap - 0 HUP INT TERM

    if [ "$finish_status" -ne 0 ] && [ "$installation_complete" != true ]; then
        if [ "$api_quiesced" = true ] && [ "$compose_ready" = true ]; then
            warn "data preparation failed; restarting the previous application"
            compose start api >/dev/null 2>&1 || true
            api_quiesced=false
        fi
        if [ "$compose_started" = true ] && [ "$compose_ready" = true ]; then
            if [ "$deployment_was_running" = true ]; then
                warn "Docker update failed; retaining the existing deployment and previous Compose environment"
            else
                warn "Docker startup failed; removing the incomplete deployment"
                compose down --remove-orphans >/dev/null 2>&1 || true
            fi
        fi
        rollback_succeeded=true
        if [ "$services_changed" = true ] && [ -n "$transaction_state" ]; then
            warn "restoring host service state from before this invocation"
            if ! restore_service_state "$transaction_state"; then
                rollback_succeeded=false
                warn "host service rollback was incomplete; recovery state was retained at $rollback_state_file"
            fi
        fi
        if [ "$rollback_succeeded" = true ] &&
            [ -n "${rollback_state_file:-}" ]
        then
            rm -f -- "$rollback_state_file"
        fi
        if [ "$rollback_succeeded" = true ] &&
            [ "$baseline_created" = true ] &&
            [ -n "${service_state_file:-}" ]
        then
            rm -f -- "$service_state_file"
        fi
    fi

    if [ -n "$work_dir" ] && [ -d "$work_dir" ]; then
        rm -rf -- "$work_dir"
    fi
    exit "$finish_status"
}

trap 'finish $?' 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

while [ "$#" -gt 0 ]; do
    case "$1" in
        --mode)
            [ "$#" -ge 2 ] || fail "--mode requires a value"
            mode_arg=$2
            shift 2
            ;;
        --assignment-file)
            [ "$#" -ge 2 ] || fail "--assignment-file requires a value"
            assignment_arg=$2
            shift 2
            ;;
        --bind-address)
            [ "$#" -ge 2 ] || fail "--bind-address requires a value"
            bind_address_arg=$2
            shift 2
            ;;
        --port)
            [ "$#" -ge 2 ] || fail "--port requires a value"
            port_arg=$2
            shift 2
            ;;
        --tls-hosts)
            [ "$#" -ge 2 ] || fail "--tls-hosts requires a value"
            tls_hosts_arg=$2
            shift 2
            ;;
        --media-bindings-file)
            [ "$#" -ge 2 ] || fail "--media-bindings-file requires a value"
            media_bindings_arg=$2
            shift 2
            ;;
        --cloudflare-token-file)
            [ "$#" -ge 2 ] || fail "--cloudflare-token-file requires a value"
            cloudflare_token_arg=$2
            shift 2
            ;;
        --cloudflare-hostname)
            [ "$#" -ge 2 ] || fail "--cloudflare-hostname requires a value"
            cloudflare_hostname_arg=$2
            shift 2
            ;;
        --cloudflare-turn-key-id)
            [ "$#" -ge 2 ] || fail "--cloudflare-turn-key-id requires a value"
            cloudflare_turn_key_id_arg=$2
            shift 2
            ;;
        --cloudflare-turn-token-file)
            [ "$#" -ge 2 ] ||
                fail "--cloudflare-turn-token-file requires a value"
            cloudflare_turn_token_arg=$2
            shift 2
            ;;
        --disable-cloudflare)
            disable_cloudflare=true
            shift
            ;;
        --disable-cloudflare-turn)
            disable_cloudflare_turn=true
            shift
            ;;
        --version)
            [ "$#" -ge 2 ] || fail "--version requires a value"
            version_arg=$2
            shift 2
            ;;
        --rebuild-all)
            rebuild_all=true
            shift
            ;;
        --allow-dirty)
            allow_dirty=true
            shift
            ;;
        --check)
            check_only=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            fail "unknown option: $1"
            ;;
    esac
done

[ "$(uname -s)" = Linux ] || fail "the deployment host must run Linux"
if [ "$check_only" != true ] && [ "$(id -u)" -ne 0 ]; then
    fail "run installation as root: sudo ./install.sh"
fi

for required_file in \
    "$compose_file" \
    "$cloudflare_compose_file" \
    "$cloudflare_turn_compose_file" \
    "${repo_dir}/Dockerfile" \
    "${repo_dir}/hardware/Dockerfile" \
    "${repo_dir}/hardware/config/media-bindings.empty.json" \
    "${repo_dir}/scripts/prepare-modemdeck-data.sh"
do
    [ -f "$required_file" ] ||
        fail "required repository file is missing: $required_file"
done

for command_name in \
    awk base64 cat chmod chown cksum cp date dd dirname docker find grep id install \
    mktemp mv readlink rm sed sleep stat tr uname
do
    command -v "$command_name" >/dev/null 2>&1 ||
        fail "required host command is unavailable: $command_name"
done

docker info >/dev/null 2>&1 || fail "Docker Engine is not reachable"
docker compose version >/dev/null 2>&1 ||
    fail "the Docker Compose plugin is unavailable"

case "$(uname -m)" in
    x86_64|amd64)
        target_arch=amd64
        ;;
    aarch64|arm64)
        target_arch=arm64
        ;;
    *)
        fail "unsupported host architecture: $(uname -m)"
        ;;
esac

read_env_value() {
    read_key=$1
    [ -f "$env_file" ] || return 1
    awk -v key="$read_key" '
        index($0, key "=") == 1 {
            value = substr($0, length(key) + 2)
            sub(/\r$/, "", value)
            first = substr(value, 1, 1)
            last = substr(value, length(value), 1)
            if (length(value) >= 2 &&
                ((first == "\"" && last == "\"") ||
                 (first == "\047" && last == "\047"))) {
                value = substr(value, 2, length(value) - 2)
            }
            print value
            found = 1
            exit
        }
        END { if (!found) exit 1 }
    ' "$env_file"
}

env_or_default() {
    env_key=$1
    env_fallback=$2
    if env_value=$(read_env_value "$env_key"); then
        printf '%s' "$env_value"
    else
        printf '%s' "$env_fallback"
    fi
}

absolute_path() {
    case "$1" in
        /*) printf '%s' "$1" ;;
        *) printf '%s/%s' "$repo_dir" "$1" ;;
    esac
}

mode=simple
[ -z "$mode_arg" ] || mode=$mode_arg
case "$mode" in
    simple|advanced) ;;
    *) fail "--mode must be simple or advanced" ;;
esac

assignment_path=${MODEMDECK_ASSIGNMENT_FILE:-$(env_or_default MODEMDECK_ASSIGNMENT_FILE "")}
[ -z "$assignment_arg" ] || assignment_path=$assignment_arg
if [ "$mode" = advanced ]; then
    [ -f "$advanced_compose_file" ] ||
        fail "advanced Compose override is missing: $advanced_compose_file"
    [ -n "$assignment_path" ] ||
        fail "advanced mode requires --assignment-file FILE"
    assignment_path=$(absolute_path "$assignment_path")
else
    [ -z "$assignment_arg" ] ||
        fail "--assignment-file is valid only with --mode advanced"
    assignment_path=$assignment_example
fi

bind_address=${MODEMDECK_BIND_ADDRESS:-$(env_or_default MODEMDECK_BIND_ADDRESS 127.0.0.1)}
port=${MODEMDECK_PORT:-$(env_or_default MODEMDECK_PORT 7577)}
tls_hosts=${MODEMDECK_TLS_HOSTS:-$(env_or_default MODEMDECK_TLS_HOSTS localhost,127.0.0.1,::1)}
app_uid=${MODEMDECK_UID:-$(env_or_default MODEMDECK_UID 10001)}
app_gid=${MODEMDECK_GID:-$(env_or_default MODEMDECK_GID 10001)}
agent_gid=${MODEMDECK_AGENT_GID:-$(env_or_default MODEMDECK_AGENT_GID 10002)}
image_name=${MODEMDECK_IMAGE:-$(env_or_default MODEMDECK_IMAGE modemdeck)}
web_image=${MODEMDECK_WEB_IMAGE:-$(env_or_default MODEMDECK_WEB_IMAGE modemdeck-web)}
hardware_image=${MODEMDECK_HARDWARE_IMAGE:-$(env_or_default MODEMDECK_HARDWARE_IMAGE modemdeck-hardware)}
settings_key_path=${MODEMDECK_SETTINGS_KEY_FILE:-$(env_or_default MODEMDECK_SETTINGS_KEY_FILE ./secrets/settings-key)}
data_path=${MODEMDECK_DATA_DIR:-$(env_or_default MODEMDECK_DATA_DIR ./data)}
media_bindings_path=${MODEMDECK_MEDIA_BINDINGS_FILE:-$(env_or_default MODEMDECK_MEDIA_BINDINGS_FILE ./hardware/config/media-bindings.empty.json)}
cloudflare_enabled=${MODEMDECK_CLOUDFLARE_ENABLED:-$(env_or_default MODEMDECK_CLOUDFLARE_ENABLED false)}
cloudflare_token_path=${MODEMDECK_CLOUDFLARE_TOKEN_FILE:-$(env_or_default MODEMDECK_CLOUDFLARE_TOKEN_FILE ./secrets/cloudflare-tunnel-token)}
cloudflare_turn_key_id=${MODEMDECK_CLOUDFLARE_TURN_KEY_ID:-$(env_or_default MODEMDECK_CLOUDFLARE_TURN_KEY_ID "")}
cloudflare_turn_token_path=${MODEMDECK_CLOUDFLARE_TURN_TOKEN_FILE:-$(env_or_default MODEMDECK_CLOUDFLARE_TURN_TOKEN_FILE ./secrets/cloudflare-turn-token)}
cloudflared_version=${MODEMDECK_CLOUDFLARED_VERSION:-$(env_or_default MODEMDECK_CLOUDFLARED_VERSION 2026.7.3)}
cloudflared_digest=${MODEMDECK_CLOUDFLARED_DIGEST:-$(env_or_default MODEMDECK_CLOUDFLARED_DIGEST sha256:e39ee8da81ad5e05d77f38d2f51c60ca51bf2a8450ac3abab50c17fdb91d91bf)}

[ -z "$bind_address_arg" ] || bind_address=$bind_address_arg
[ -z "$port_arg" ] || port=$port_arg
[ -z "$tls_hosts_arg" ] || tls_hosts=$tls_hosts_arg
[ -z "$media_bindings_arg" ] || media_bindings_path=$media_bindings_arg

case "$bind_address" in
    ""|*[!A-Za-z0-9._-]*)
        fail "--bind-address must be one IPv4 address or hostname"
        ;;
esac
case "$port" in
    ""|*[!0-9]*)
        fail "--port must be numeric"
        ;;
esac
[ "$port" -ge 1 ] && [ "$port" -le 65535 ] ||
    fail "--port must be between 1 and 65535"
for numeric_value in "$app_uid" "$app_gid" "$agent_gid"; do
    case "$numeric_value" in
        ""|*[!0-9]*)
            fail "MODEMDECK_UID, MODEMDECK_GID, and MODEMDECK_AGENT_GID must be numeric"
            ;;
    esac
    [ "$numeric_value" -gt 0 ] ||
        fail "ModemDeck service IDs must be non-root"
done
[ "$app_gid" != "$agent_gid" ] ||
    fail "MODEMDECK_GID and MODEMDECK_AGENT_GID must remain separate"

settings_key_file=$(absolute_path "$settings_key_path")
data_dir=$(absolute_path "$data_path")
media_bindings_file=$(absolute_path "$media_bindings_path")
cloudflare_token_file=$(absolute_path "$cloudflare_token_path")
cloudflare_turn_token_file=$(absolute_path "$cloudflare_turn_token_path")

case "$cloudflare_enabled" in
    true|false) ;;
    *) fail "MODEMDECK_CLOUDFLARE_ENABLED must be true or false" ;;
esac
if [ "$disable_cloudflare" = true ]; then
    [ -z "$cloudflare_token_arg" ] &&
        [ -z "$cloudflare_hostname_arg" ] &&
        [ -z "$cloudflare_turn_key_id_arg" ] &&
        [ -z "$cloudflare_turn_token_arg" ] ||
        fail "--disable-cloudflare cannot be combined with Cloudflare enable options"
    cloudflare_enabled=false
elif [ -n "$cloudflare_token_arg" ] ||
    [ -n "$cloudflare_turn_key_id_arg" ] ||
    [ -n "$cloudflare_turn_token_arg" ]
then
    cloudflare_enabled=true
fi
if [ "$disable_cloudflare_turn" = true ]; then
    [ -z "$cloudflare_turn_key_id_arg" ] &&
        [ -z "$cloudflare_turn_token_arg" ] ||
        fail "--disable-cloudflare-turn cannot be combined with TURN enable options"
    cloudflare_turn_key_id=
else
    [ -z "$cloudflare_turn_key_id_arg" ] ||
        cloudflare_turn_key_id=$cloudflare_turn_key_id_arg
    if [ -n "$cloudflare_turn_key_id" ] ||
        [ -n "$cloudflare_turn_token_arg" ]
    then
        cloudflare_turn_enabled=true
    fi
fi
if [ "$cloudflare_enabled" != true ]; then
    cloudflare_turn_enabled=false
fi

validate_cloudflare_hostname() {
    printf '%s\n' "$1" | awk -F. '
        length($0) < 1 || length($0) > 253 || NF < 2 { exit 1 }
        {
            for (part = 1; part <= NF; part++) {
                label = $part
                if (length(label) < 1 || length(label) > 63 ||
                    label !~ /^[A-Za-z0-9-]+$/ ||
                    label ~ /^-/ || label ~ /-$/) {
                    exit 1
                }
            }
        }
    '
}

validate_cloudflare_token_file() {
    token_file=$1
    [ -r "$token_file" ] ||
        fail "Cloudflare Tunnel token file is not readable: $token_file"
    [ -f "$token_file" ] && [ ! -L "$token_file" ] ||
        fail "Cloudflare Tunnel token must be a regular non-symlink file: $token_file"
    token_size=$(stat -c %s "$token_file" 2>/dev/null ||
        stat -f %z "$token_file")
    [ "$token_size" -le 4097 ] ||
        fail "Cloudflare Tunnel token file exceeds 4097 bytes"
    awk '
        {
            sub(/\r$/, "")
            if ($0 == "") next
            if (seen || length($0) < 80 || length($0) > 4096 ||
                $0 !~ /^[A-Za-z0-9_-]+$/) {
                exit 1
            }
            seen = 1
        }
        END { if (!seen) exit 1 }
    ' "$token_file" ||
        fail "Cloudflare Tunnel token file does not contain one valid token"
}

validate_cloudflare_turn_token_file() {
    token_file=$1
    [ -r "$token_file" ] ||
        fail "Cloudflare TURN token file is not readable: $token_file"
    [ -f "$token_file" ] && [ ! -L "$token_file" ] ||
        fail "Cloudflare TURN token must be a regular non-symlink file: $token_file"
    token_size=$(stat -c %s "$token_file" 2>/dev/null ||
        stat -f %z "$token_file")
    [ "$token_size" -le 4097 ] ||
        fail "Cloudflare TURN token file exceeds 4097 bytes"
    awk '
        {
            sub(/\r$/, "")
            if ($0 == "") next
            if (seen || length($0) > 4096 ||
                $0 ~ /[[:space:][:cntrl:]]/) {
                exit 1
            }
            seen = 1
        }
        END { if (!seen) exit 1 }
    ' "$token_file" ||
        fail "Cloudflare TURN token file does not contain one valid token"
}

cloudflare_token_source=$cloudflare_token_file
cloudflare_turn_token_source=$cloudflare_turn_token_file
if [ -n "$cloudflare_hostname_arg" ]; then
    validate_cloudflare_hostname "$cloudflare_hostname_arg" ||
        fail "--cloudflare-hostname must be a valid DNS hostname"
    warn "--cloudflare-hostname is no longer required; the API hostname is discovered from the active Tunnel ingress"
fi
if [ "$cloudflare_enabled" = true ]; then
    if [ -n "$cloudflare_token_arg" ]; then
        cloudflare_token_source=$(absolute_path "$cloudflare_token_arg")
    fi
    validate_cloudflare_token_file "$cloudflare_token_source"
fi
if [ "$cloudflare_turn_enabled" = true ]; then
    printf '%s\n' "$cloudflare_turn_key_id" |
        grep -Eq '^[0-9A-Fa-f]{32}$' ||
        fail "enabling Cloudflare TURN requires a 32-character key ID"
    if [ -n "$cloudflare_turn_token_arg" ]; then
        cloudflare_turn_token_source=$(
            absolute_path "$cloudflare_turn_token_arg"
        )
    fi
    validate_cloudflare_turn_token_file "$cloudflare_turn_token_source"
fi
printf '%s\n' "$cloudflared_version" |
    grep -Eq '^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$' ||
    fail "MODEMDECK_CLOUDFLARED_VERSION is not a valid Docker tag"
printf '%s\n' "$cloudflared_digest" |
    grep -Eq '^sha256:[0-9a-f]{64}$' ||
    fail "MODEMDECK_CLOUDFLARED_DIGEST is not a valid image digest"

state_dir=${MODEMDECK_INSTALL_STATE_DIR:-/var/lib/modemdeck-installer}
case "$state_dir" in
    /*) ;;
    *) fail "MODEMDECK_INSTALL_STATE_DIR must be absolute" ;;
esac
service_state_file="${state_dir}/host-services.state"
rollback_state_file="${state_dir}/host-services.rollback"

sys_root=${MODEMDECK_HOST_SYS_ROOT:-/sys}
dev_root=${MODEMDECK_HOST_DEV_ROOT:-/dev}
proc_root=${MODEMDECK_HOST_PROC_ROOT:-/proc}
udev_root=${MODEMDECK_HOST_UDEV_ROOT:-/run/udev}
host_dbus_socket=${MODEMDECK_HOST_DBUS_SOCKET:-/run/dbus/system_bus_socket}
for host_directory in "$sys_root" "$dev_root" "$proc_root" "$udev_root"; do
    [ -d "$host_directory" ] ||
        fail "required host view is unavailable: $host_directory"
done
if [ "$mode" = advanced ]; then
    [ -S "$host_dbus_socket" ] ||
        fail "advanced mode requires the host system D-Bus socket: $host_dbus_socket"
fi

validate_assignment() {
    assignment_file=$1
    [ -r "$assignment_file" ] ||
        fail "assignment file is not readable: $assignment_file"
    [ -f "$assignment_file" ] && [ ! -L "$assignment_file" ] ||
        fail "assignment file must be a regular non-symlink file: $assignment_file"
    [ -s "$assignment_file" ] ||
        fail "assignment file is empty: $assignment_file"
    assignment_size=$(stat -c %s "$assignment_file" 2>/dev/null ||
        stat -f %z "$assignment_file")
    [ "$assignment_size" -le 1048576 ] ||
        fail "assignment file exceeds 1 MiB: $assignment_file"
    if grep -q 'REPLACE_WITH_' "$assignment_file"; then
        fail "assignment file still contains example placeholders"
    fi
    grep -Eq '"version"[[:space:]]*:[[:space:]]*1' "$assignment_file" ||
        fail "assignment file must declare JSON version 1"
    grep -Eq '"assignments"[[:space:]]*:' "$assignment_file" ||
        fail "assignment file must contain assignments"
}

if [ "$mode" = advanced ]; then
    validate_assignment "$assignment_path"
fi

[ -r "$media_bindings_file" ] ||
    fail "media bindings file is not readable: $media_bindings_file"
[ -f "$media_bindings_file" ] && [ ! -L "$media_bindings_file" ] ||
    fail "media bindings file must be a regular non-symlink file: $media_bindings_file"
media_bindings_size=$(stat -c %s "$media_bindings_file" 2>/dev/null ||
    stat -f %z "$media_bindings_file")
[ "$media_bindings_size" -le 65536 ] ||
    fail "media bindings file exceeds 64 KiB: $media_bindings_file"

git_command() {
    GIT_OPTIONAL_LOCKS=0 \
        git -c "safe.directory=${repo_dir}" -C "$repo_dir" "$@"
}

vcs_ref=unknown
derived_version=
checkout_dirty=false
if command -v git >/dev/null 2>&1 &&
    git_command rev-parse --is-inside-work-tree >/dev/null 2>&1
then
    vcs_ref=$(git_command rev-parse HEAD)
    derived_version=$(git_command rev-parse --short=8 HEAD)
    if [ -n "$(git_command status --porcelain --untracked-files=normal)" ]; then
        checkout_dirty=true
        if [ "$allow_dirty" != true ]; then
            fail "the Git checkout has uncommitted changes; commit them or pass --allow-dirty"
        fi
        derived_version="${derived_version}-dirty"
    fi
fi

if [ -n "$version_arg" ]; then
    version=$version_arg
elif [ -n "${MODEMDECK_VERSION:-}" ]; then
    version=$MODEMDECK_VERSION
elif [ -n "$derived_version" ]; then
    version=$derived_version
else
    version="local-$(date -u +%Y%m%d%H%M%S)"
fi
printf '%s\n' "$version" |
    grep -Eq '^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$' ||
    fail "resolved version is not a valid Docker tag: $version"

build_date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/modemdeck-install.XXXXXX")
env_work="${work_dir}/compose.env"
transaction_state="${work_dir}/host-services.before"
changed_paths_file="${work_dir}/changed-paths"

installed_version=$(env_or_default MODEMDECK_VERSION "")
installed_vcs_ref=$(env_or_default MODEMDECK_VCS_REF "")
installed_cloudflare_enabled=$(env_or_default MODEMDECK_CLOUDFLARE_ENABLED false)
installed_cloudflare_turn_key_id=$(
    env_or_default MODEMDECK_CLOUDFLARE_TURN_KEY_ID ""
)
installed_assignment_fingerprint=$(
    env_or_default MODEMDECK_ASSIGNMENT_FINGERPRINT ""
)
installed_media_bindings_fingerprint=$(
    env_or_default MODEMDECK_MEDIA_BINDINGS_FINGERPRINT ""
)
api_version=$(env_or_default MODEMDECK_API_VERSION "$installed_version")
web_version=$(env_or_default MODEMDECK_WEB_VERSION "$installed_version")
hardware_version=$(env_or_default MODEMDECK_HARDWARE_VERSION "$installed_version")

file_fingerprint() {
    cksum "$1" | awk '{ print $1 ":" $2 }'
}

assignment_fingerprint=
if [ "$mode" = advanced ]; then
    assignment_fingerprint=$(file_fingerprint "$assignment_path")
fi
media_bindings_fingerprint=$(file_fingerprint "$media_bindings_file")

build_api=false
build_web=false
build_hardware=false
selective_images=false

if [ "$rebuild_all" = true ] || [ ! -f "$env_file" ]; then
    build_api=true
    build_web=true
    build_hardware=true
elif printf '%s\n' "$installed_vcs_ref" |
    grep -Eq '^[0-9a-f]{40}$' &&
    printf '%s\n' "$vcs_ref" |
        grep -Eq '^[0-9a-f]{40}$' &&
    git_command cat-file -e "${installed_vcs_ref}^{commit}" 2>/dev/null
then
    selective_images=true
    git_command diff --no-renames --name-only "$installed_vcs_ref" -- \
        >"$changed_paths_file"
    if [ "$checkout_dirty" = true ]; then
        git_command ls-files --others --exclude-standard \
            >>"$changed_paths_file"
    fi
    while IFS= read -r changed_path; do
        case "$changed_path" in
            .dockerignore)
                build_api=true
                build_web=true
                build_hardware=true
                continue
                ;;
            Dockerfile|VERSION|LICENSE|NOTICE.md|THIRD_PARTY_NOTICES.md)
                build_api=true
                build_web=true
                continue
                ;;
        esac
        case "$changed_path" in
            go.mod|go.sum|cmd/modemdeck|cmd/modemdeck/*|internal|internal/*|\
            scripts/docker-entrypoint.sh)
                build_api=true
                ;;
        esac
        case "$changed_path" in
            web|web/*|scripts/nginx-entrypoint.sh)
                build_web=true
                ;;
        esac
        case "$changed_path" in
            agent|agent/*|hardware|hardware/*)
                build_hardware=true
                ;;
        esac
    done <"$changed_paths_file"
else
    warn "the previous deployed Git revision is unavailable; rebuilding every image"
    build_api=true
    build_web=true
    build_hardware=true
fi

validate_component_version() {
    printf '%s\n' "$1" |
        grep -Eq '^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$'
}

if [ "$build_api" = true ] || ! validate_component_version "$api_version"; then
    api_version=$version
    build_api=true
fi
if [ "$build_web" = true ] || ! validate_component_version "$web_version"; then
    web_version=$version
    build_web=true
fi
if [ "$build_hardware" = true ] ||
    ! validate_component_version "$hardware_version"
then
    hardware_version=$version
    build_hardware=true
fi

if ! docker image inspect "${image_name}:${api_version}" >/dev/null 2>&1; then
    api_version=$version
    build_api=true
fi
if ! docker image inspect "${web_image}:${web_version}" >/dev/null 2>&1; then
    web_version=$version
    build_web=true
fi
if ! docker image inspect \
    "${hardware_image}:${hardware_version}" >/dev/null 2>&1
then
    hardware_version=$version
    build_hardware=true
fi

if [ -f "$env_file" ]; then
    cp "$env_file" "$env_work"
else
    printf '%s\n' \
        '# Generated by install.sh; contains local deployment paths, not secrets.' \
        >"$env_work"
fi

upsert_env() {
    upsert_key=$1
    upsert_value=$2
    next_env="${work_dir}/compose.env.next"
    awk -v key="$upsert_key" -v value="$upsert_value" '
        index($0, key "=") == 1 {
            if (!written) {
                print key "=" value
                written = 1
            }
            next
        }
        { print }
        END {
            if (!written) print key "=" value
        }
    ' "$env_work" >"$next_env"
    mv "$next_env" "$env_work"
}

remove_env() {
    remove_key=$1
    next_env="${work_dir}/compose.env.next"
    awk -v key="$remove_key" '
        index($0, key "=") != 1 { print }
    ' "$env_work" >"$next_env"
    mv "$next_env" "$env_work"
}

upsert_env MODEMDECK_HARDWARE_MODE "$mode"
upsert_env MODEMDECK_ASSIGNMENT_FILE "$assignment_path"
upsert_env MODEMDECK_IMAGE "$image_name"
upsert_env MODEMDECK_WEB_IMAGE "$web_image"
upsert_env MODEMDECK_HARDWARE_IMAGE "$hardware_image"
upsert_env MODEMDECK_VERSION "$version"
upsert_env MODEMDECK_API_VERSION "$api_version"
upsert_env MODEMDECK_WEB_VERSION "$web_version"
upsert_env MODEMDECK_HARDWARE_VERSION "$hardware_version"
upsert_env MODEMDECK_BUILD_DATE "$build_date"
upsert_env MODEMDECK_VCS_REF "$vcs_ref"
upsert_env MODEMDECK_BIND_ADDRESS "$bind_address"
upsert_env MODEMDECK_PORT "$port"
upsert_env MODEMDECK_TLS_HOSTS "$tls_hosts"
upsert_env MODEMDECK_UID "$app_uid"
upsert_env MODEMDECK_GID "$app_gid"
upsert_env MODEMDECK_AGENT_GID "$agent_gid"
upsert_env MODEMDECK_SETTINGS_KEY_FILE "$settings_key_file"
upsert_env MODEMDECK_DATA_DIR "$data_dir"
upsert_env MODEMDECK_MEDIA_BINDINGS_FILE "$media_bindings_file"
upsert_env MODEMDECK_ASSIGNMENT_FINGERPRINT "$assignment_fingerprint"
upsert_env MODEMDECK_MEDIA_BINDINGS_FINGERPRINT "$media_bindings_fingerprint"
upsert_env MODEMDECK_SECURE_COOKIES true
upsert_env MODEMDECK_CLOUDFLARE_ENABLED "$cloudflare_enabled"
remove_env MODEMDECK_CLOUDFLARE_HOSTNAME
remove_env MODEMDECK_CLOUDFLARE_PUBLIC_URL
upsert_env MODEMDECK_CLOUDFLARE_TOKEN_FILE "$cloudflare_token_file"
upsert_env MODEMDECK_CLOUDFLARE_TURN_KEY_ID "$cloudflare_turn_key_id"
upsert_env MODEMDECK_CLOUDFLARE_TURN_TOKEN_FILE "$cloudflare_turn_token_file"
upsert_env MODEMDECK_CLOUDFLARED_VERSION "$cloudflared_version"
upsert_env MODEMDECK_CLOUDFLARED_DIGEST "$cloudflared_digest"
if [ "$mode" = advanced ]; then
    upsert_env MODEMDECK_HOST_PROC_ROOT "$proc_root"
    upsert_env MODEMDECK_HOST_DBUS_SOCKET "$host_dbus_socket"
fi
compose_ready=true

log "Validating deployment"
printf 'Source:        %s\n' "$repo_dir"
printf 'Mode:          %s\n' "$mode"
printf 'Architecture:  linux/%s\n' "$target_arch"
printf 'Images:        %s:%s, %s:%s, %s:%s\n' \
    "$image_name" "$api_version" \
    "$web_image" "$web_version" \
    "$hardware_image" "$hardware_version"
if [ "$rebuild_all" = true ]; then
    printf '%s\n' 'Update plan:   rebuild and recreate every container'
elif [ "$selective_images" = true ]; then
    printf '%s\n' 'Update plan:   rebuild only changed component images'
else
    printf '%s\n' 'Update plan:   full image build (no prior component baseline)'
fi
printf 'Web UI:        https://%s:%s\n' "$bind_address" "$port"
printf '%s\n' 'API origin:    http://modemdeck:7575 (Compose only, non-API paths return 404)'
printf '%s\n' 'Web origin:    http://modemdeck:7576 (Compose only)'
printf '%s\n' 'API upstream:  http://api:8080 (Compose only)'
printf 'Data:          %s\n' "$data_dir"
printf 'Media config:  %s\n' "$media_bindings_file"
if [ "$cloudflare_enabled" = true ]; then
    printf '%s\n' 'Cloudflare:    enabled (ingress managed in Cloudflare)'
    if [ "$cloudflare_turn_enabled" = true ]; then
        printf '%s\n' 'TURN:          enabled (Cloudflare Realtime)'
    else
        printf '%s\n' 'TURN:          disabled'
    fi
else
    printf '%s\n' 'Cloudflare:    disabled (iOS pairing unavailable)'
fi
if [ "$mode" = advanced ]; then
    printf 'Assignments:   %s\n' "$assignment_path"
fi
compose config --quiet

existing_hardware_id=$(compose ps -q hardware 2>/dev/null || true)
existing_api_id=$(compose ps -q api 2>/dev/null || true)
existing_web_id=$(compose ps -q modemdeck 2>/dev/null || true)
existing_cloudflared_id=$(docker ps -aq \
    --filter 'label=com.docker.compose.project=modemdeck' \
    --filter 'label=com.docker.compose.service=cloudflared' 2>/dev/null |
    sed -n '1p')
if [ -n "$existing_hardware_id" ] &&
    [ -n "$existing_api_id" ] &&
    [ -n "$existing_web_id" ]
then
    deployment_was_running=true
fi

service_needs_update() {
    update_service=$1
    update_container_id=$2
    [ -n "$update_container_id" ] || return 0

    update_container_running=$(docker inspect --format \
        '{{.State.Running}}' "$update_container_id" 2>/dev/null || true)
    [ "$update_container_running" = true ] || return 0

    current_config_hash=$(docker inspect --format \
        '{{index .Config.Labels "com.docker.compose.config-hash"}}' \
        "$update_container_id" 2>/dev/null || true)
    desired_hash_line=$(compose config --hash "$update_service" 2>/dev/null) ||
        fail "Docker Compose cannot calculate the $update_service configuration hash"
    desired_config_hash=$(printf '%s\n' "$desired_hash_line" |
        awk -v service="$update_service" '
            $1 == service && NF == 2 { print $2; found = 1; exit }
            END { if (!found) exit 1 }
        ') ||
        fail "Docker Compose returned an invalid $update_service configuration hash"
    [ -n "$current_config_hash" ] &&
        [ "$current_config_hash" = "$desired_config_hash" ] &&
        return 1
    return 0
}

update_hardware=$build_hardware
update_api=$build_api
update_web=$build_web
update_cloudflared=false
remove_cloudflared=false

if service_needs_update hardware "$existing_hardware_id"; then
    update_hardware=true
fi
if service_needs_update api "$existing_api_id"; then
    update_api=true
fi
if service_needs_update modemdeck "$existing_web_id"; then
    update_web=true
fi
if [ "$cloudflare_enabled" = true ]; then
    if service_needs_update cloudflared "$existing_cloudflared_id"; then
        update_cloudflared=true
    fi
elif [ -n "$existing_cloudflared_id" ]; then
    remove_cloudflared=true
    update_web=true
fi

# File-secret contents and external bind-file contents are not represented in
# Compose's service hash. Explicit replacement options therefore select the
# consumers that must reload those files.
if { [ -n "$installed_assignment_fingerprint" ] &&
        [ "$installed_assignment_fingerprint" != "$assignment_fingerprint" ]; } ||
    { [ -n "$installed_media_bindings_fingerprint" ] &&
        [ "$installed_media_bindings_fingerprint" != "$media_bindings_fingerprint" ]; }
then
    update_hardware=true
fi
[ -z "$cloudflare_token_arg" ] || update_cloudflared=true
if [ "$installed_cloudflare_enabled" != "$cloudflare_enabled" ]; then
    update_api=true
fi
if [ "$installed_cloudflare_turn_key_id" != "$cloudflare_turn_key_id" ]; then
    update_api=true
fi
if [ -n "$cloudflare_turn_key_id_arg" ] ||
    [ -n "$cloudflare_turn_token_arg" ] ||
    [ "$disable_cloudflare_turn" = true ] ||
    [ "$disable_cloudflare" = true ]
then
    update_api=true
fi

if [ "$rebuild_all" = true ]; then
    update_hardware=true
    update_api=true
    update_web=true
    [ "$cloudflare_enabled" = true ] && update_cloudflared=true
fi

plan_action() {
    if [ "$1" = true ]; then
        printf '%s' update
    else
        printf '%s' retain
    fi
}

printf 'Containers:    hardware=%s, api=%s, web=%s' \
    "$(plan_action "$update_hardware")" \
    "$(plan_action "$update_api")" \
    "$(plan_action "$update_web")"
if [ "$cloudflare_enabled" = true ]; then
    printf ', cloudflared=%s\n' "$(plan_action "$update_cloudflared")"
elif [ "$remove_cloudflared" = true ]; then
    printf '%s\n' ', cloudflared=remove'
else
    printf '%s\n' ', cloudflared=disabled'
fi

if [ "$cloudflare_enabled" = true ]; then
    legacy_cloudflared_id=$(docker ps -aq \
        --filter 'name=^/modemdeck-cloudflared$' 2>/dev/null |
        sed -n '1p')
    if [ -n "$legacy_cloudflared_id" ]; then
        existing_project=$(docker inspect --format \
            '{{index .Config.Labels "com.docker.compose.project"}}' \
            "$legacy_cloudflared_id" 2>/dev/null || true)
        existing_service=$(docker inspect --format \
            '{{index .Config.Labels "com.docker.compose.service"}}' \
            "$legacy_cloudflared_id" 2>/dev/null || true)
        if [ "$existing_project" = modemdeck ] &&
            [ "$existing_service" = cloudflared ]; then
            legacy_cloudflared_id=
        else
            printf '%s\n' \
                'Tunnel migration: the existing manual connector will remain running until its Compose replacement is healthy.'
        fi
    fi
fi

unit_load_state() {
    systemctl show "$1" --property=LoadState --value 2>/dev/null ||
        printf '%s\n' not-found
}

unit_enabled_state() {
    enabled_output=$(systemctl is-enabled "$1" 2>/dev/null || true)
    [ -n "$enabled_output" ] || enabled_output=disabled
    printf '%s\n' "$enabled_output"
}

unit_is_active() {
    systemctl is-active --quiet "$1" >/dev/null 2>&1
}

unit_is_present() {
    [ "$(unit_load_state "$1")" != not-found ]
}

if [ "$mode" = simple ]; then
    command -v systemctl >/dev/null 2>&1 ||
        fail "simple mode requires systemctl to manage host modem services"
    systemd_runtime_dir=${MODEMDECK_SYSTEMD_RUNTIME_DIR:-/run/systemd/system}
    [ -d "$systemd_runtime_dir" ] ||
        fail "simple mode requires systemd as the host service manager"
    for managed_unit in ModemManager.service modemdeck-agent.service; do
        if unit_is_present "$managed_unit"; then
            printf 'Host service:  %s (%s, %s)\n' \
                "$managed_unit" \
                "$(unit_enabled_state "$managed_unit")" \
                "$(systemctl is-active "$managed_unit" 2>/dev/null || true)"
        fi
    done
fi

managed_gateway_id=$(compose ps -q modemdeck 2>/dev/null || true)
port_hex=$(printf '%04X' "$port")
port_is_listening=false
for socket_table in "${proc_root}/net/tcp" "${proc_root}/net/tcp6"; do
    [ -r "$socket_table" ] || continue
    if awk -v suffix=":${port_hex}" '
        NR > 1 && $4 == "0A" &&
        substr($2, length($2) - length(suffix) + 1) == suffix {
            found = 1
        }
        END { exit(found ? 0 : 1) }
    ' "$socket_table"; then
        port_is_listening=true
        break
    fi
done
if [ "$port_is_listening" = true ] &&
    [ -z "$managed_gateway_id" ]; then
    fail "TCP port $port is already in use by another host process"
fi

if [ "$check_only" = true ]; then
    if [ "$mode" = simple ]; then
        log "Read-only check passed; host services would be stopped, disabled, and masked"
    else
        log "Read-only check passed; host services were not changed"
    fi
    exit 0
fi

prepare_secret() {
    secret_file=$1
    random_bytes=$2
    description=$3
    secret_directory=$(dirname -- "$secret_file")

    install -d -o root -g root -m 0700 "$secret_directory"
    if [ -e "$secret_file" ] || [ -L "$secret_file" ]; then
        [ -f "$secret_file" ] && [ ! -L "$secret_file" ] ||
            fail "$description must be a regular non-symlink file: $secret_file"
        [ -s "$secret_file" ] || fail "$description is empty: $secret_file"
        chown root:"$app_gid" "$secret_file"
        chmod 0440 "$secret_file"
        return
    fi

    raw_secret="${work_dir}/secret.raw"
    encoded_secret="${work_dir}/secret.encoded"
    dd if=/dev/urandom of="$raw_secret" bs="$random_bytes" count=1 2>/dev/null
    base64 "$raw_secret" | tr -d '\r\n' >"$encoded_secret"
    printf '\n' >>"$encoded_secret"
    install -o root -g "$app_gid" -m 0440 "$encoded_secret" "$secret_file"
    rm -f -- "$raw_secret" "$encoded_secret"
}

prepare_secret "$settings_key_file" 32 "settings encryption key"

prepare_cloudflare_token() {
    token_source=$1
    token_destination=$2
    token_owner=$3
    token_group=$4
    token_name=$5
    token_directory=$(dirname -- "$token_destination")
    normalized_token="${work_dir}/${token_name}"

    tr -d '\r\n' <"$token_source" >"$normalized_token"
    printf '\n' >>"$normalized_token"
    install -d -o root -g root -m 0700 "$token_directory"
    install -o "$token_owner" -g "$token_group" -m 0440 \
        "$normalized_token" \
        "$token_destination"
}

if [ "$cloudflare_enabled" = true ]; then
    prepare_cloudflare_token \
        "$cloudflare_token_source" \
        "$cloudflare_token_file" \
        65532 \
        65532 \
        cloudflare-tunnel-token
fi
if [ "$cloudflare_turn_enabled" = true ]; then
    prepare_cloudflare_token \
        "$cloudflare_turn_token_source" \
        "$cloudflare_turn_token_file" \
        root \
        "$app_gid" \
        cloudflare-turn-token
fi

set --
[ "$build_hardware" != true ] || set -- "$@" hardware
[ "$build_api" != true ] || set -- "$@" api
[ "$build_web" != true ] || set -- "$@" modemdeck
if [ "$#" -gt 0 ]; then
    log "Building selected component images in Docker"
    printf 'Build images:  %s\n' "$*"
    compose build "$@"
else
    log "No component image rebuild is required"
fi
if [ "$mode" = advanced ]; then
    log "Validating advanced device assignments with the hardware image"
    compose run --rm --no-deps \
        --entrypoint /usr/local/bin/modemdeck-device-owner \
        hardware \
        validate \
        --config /etc/modemdeck/device-assignments.json
fi

if [ -L "$data_dir" ]; then
    fail "application data directory must not be a symlink: $data_dir"
fi
if [ "$update_api" = true ]; then
    if [ -n "$existing_api_id" ]; then
        log "Quiescing the application before preparing persistent data"
        compose stop api
        api_quiesced=true
    fi
    log "Preparing persistent application data"
    env MODEMDECK_UID="$app_uid" MODEMDECK_GID="$app_gid" \
        "${repo_dir}/scripts/prepare-modemdeck-data.sh" "$data_dir"
    if [ "$api_quiesced" = true ]; then
        compose start api
        api_quiesced=false
    fi
else
    log "Application image and data configuration are unchanged"
fi

capture_service_state() {
    capture_file=$1
    capture_status=$2
    {
        printf 'version|1\n'
        printf 'status|%s\n' "$capture_status"
        for capture_unit in ModemManager.service modemdeck-agent.service; do
            capture_present=0
            capture_enabled=disabled
            capture_active=0
            capture_masked=0
            if unit_is_present "$capture_unit"; then
                capture_present=1
                capture_enabled=$(unit_enabled_state "$capture_unit")
                if unit_is_active "$capture_unit"; then
                    capture_active=1
                fi
                case "$capture_enabled" in
                    masked|masked-runtime) capture_masked=1 ;;
                esac
            fi
            printf 'unit|%s|%s|%s|%s|%s\n' \
                "$capture_unit" \
                "$capture_present" \
                "$capture_enabled" \
                "$capture_active" \
                "$capture_masked"
        done
    } >"$capture_file"
    chmod 0600 "$capture_file"
}

validate_service_state() {
    validate_file=$1
    [ -f "$validate_file" ] && [ ! -L "$validate_file" ] ||
        fail "installer service state must be a regular file: $validate_file"
    [ "$(stat -c %u "$validate_file")" = 0 ] ||
        fail "installer service state must be owned by root: $validate_file"
    [ "$(stat -c %g "$validate_file")" = 0 ] ||
        fail "installer service state must use root group ownership: $validate_file"
    [ "$(stat -c %a "$validate_file")" = 600 ] ||
        fail "installer service state must use mode 0600: $validate_file"
    grep -qx 'version|1' "$validate_file" ||
        fail "installer service state has an unsupported format: $validate_file"
    for validate_unit in ModemManager.service modemdeck-agent.service; do
        grep -q "^unit|${validate_unit}|" "$validate_file" ||
            fail "installer service state is incomplete: $validate_file"
    done
}

state_status() {
    awk -F'|' '$1 == "status" { print $2; exit }' "$1"
}

mark_state_installed() {
    mark_file=$1
    mark_next="${work_dir}/host-services.installed"
    awk -F'|' '
        $1 == "status" { print "status|installed"; next }
        { print }
    ' "$mark_file" >"$mark_next"
    install -o root -g root -m 0600 "$mark_next" "$mark_file"
}

if [ "$mode" = simple ]; then
    install -d -o root -g root -m 0700 "$state_dir"
    if [ -e "$rollback_state_file" ]; then
        validate_service_state "$rollback_state_file"
        [ "$(state_status "$rollback_state_file")" = pending ] ||
            fail "installer rollback state is not pending: $rollback_state_file"
        log "Recovering host services from an interrupted deployment"
        restore_service_state "$rollback_state_file" ||
            fail "could not recover host services; retained $rollback_state_file"
        rm -f -- "$rollback_state_file"
    fi
    if [ -e "$service_state_file" ]; then
        validate_service_state "$service_state_file"
        existing_status=$(state_status "$service_state_file")
        case "$existing_status" in
            installed) ;;
            pending)
                log "Recovering host services from an interrupted installation"
                restore_service_state "$service_state_file" ||
                    fail "could not recover host services; retained $service_state_file"
                rm -f -- "$service_state_file"
                ;;
            *)
                fail "installer service state has invalid status: $existing_status"
                ;;
        esac
    fi

    capture_service_state "$transaction_state" pending
    install -o root -g root -m 0600 "$transaction_state" "$rollback_state_file"
    if [ ! -e "$service_state_file" ]; then
        install -o root -g root -m 0600 "$transaction_state" "$service_state_file"
        baseline_created=true
    fi

    services_changed=true
    log "Handing host modem ownership to the hardware container"
    for managed_unit in ModemManager.service modemdeck-agent.service; do
        unit_is_present "$managed_unit" || continue
        systemctl stop "$managed_unit"
        systemctl disable "$managed_unit" >/dev/null 2>&1 || true
        if ! systemctl mask "$managed_unit"; then
            if [ "$managed_unit" = ModemManager.service ]; then
                fail "could not mask $managed_unit; simple mode cannot safely prevent host modem ownership"
            fi
            warn "$managed_unit is stopped and disabled but its local unit file prevents masking"
            continue
        fi
        case "$(unit_enabled_state "$managed_unit")" in
            masked|masked-runtime) ;;
            *) fail "$managed_unit did not enter a masked state" ;;
        esac
        if unit_is_active "$managed_unit"; then
            fail "$managed_unit remained active after stop"
        fi
    done
    systemctl daemon-reload
fi

if [ "$mode" = simple ]; then
    managed_hardware_id=$(compose ps -q hardware 2>/dev/null || true)
    device_conflicts="${work_dir}/device-conflicts"
    : >"$device_conflicts"
    for process_dir in "${proc_root}"/[0-9]*; do
        [ -d "$process_dir/fd" ] || continue
        process_id=${process_dir##*/}
        if [ -n "$managed_hardware_id" ] &&
            [ -r "$process_dir/cgroup" ] &&
            grep -q "$managed_hardware_id" "$process_dir/cgroup"
        then
            continue
        fi
        for descriptor in "$process_dir"/fd/*; do
            [ -e "$descriptor" ] || [ -L "$descriptor" ] || continue
            descriptor_target=$(readlink "$descriptor" 2>/dev/null || true)
            case "$descriptor_target" in
                "$dev_root"/cdc-wdm*|"$dev_root"/ttyUSB*|"$dev_root"/ttyACM*|\
                "$dev_root"/wwan*|"$dev_root"/mhi_*|"$dev_root"/qcqmi*)
                    process_command=$(tr '\000' ' ' <"$process_dir/cmdline" 2>/dev/null || true)
                    printf '%s|%s|%s\n' \
                        "$process_id" "$descriptor_target" "$process_command" \
                        >>"$device_conflicts"
                    ;;
            esac
        done
    done
    if [ -s "$device_conflicts" ]; then
        cat "$device_conflicts" >&2
        fail "cellular device nodes are still open; refusing conflicting ownership"
    fi
fi

wait_for_healthy() {
    health_service=$1
    health_attempts=$2
    health_id=$(compose ps -q "$health_service")
    [ -n "$health_id" ] ||
        fail "Docker Compose did not create service: $health_service"
    health_attempt=0
    health_status=starting
    while [ "$health_attempt" -lt "$health_attempts" ]; do
        health_status=$(docker inspect \
            --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \
            "$health_id" 2>/dev/null || printf '%s' missing)
        case "$health_status" in
            healthy)
                return 0
                ;;
            exited|dead|missing|unhealthy)
                break
                ;;
        esac
        health_attempt=$((health_attempt + 1))
        sleep 2
    done
    compose logs --tail=150 "$health_service" >&2 || true
    fail "$health_service did not become healthy (status: $health_status)"
}

if [ "$rebuild_all" = true ]; then
    log "Force-recreating the all-Docker deployment"
    compose_started=true
    compose up \
        --detach \
        --no-build \
        --force-recreate \
        --remove-orphans
elif [ "$deployment_was_running" != true ]; then
    log "Starting the all-Docker deployment"
    compose_started=true
    compose up --detach --no-build --remove-orphans
else
    log "Applying selected container updates"
    selected_update=false
    if [ "$update_hardware" = true ]; then
        selected_update=true
        compose_started=true
        compose up \
            --detach \
            --no-build \
            --no-deps \
            --force-recreate \
            --remove-orphans \
            hardware
        wait_for_healthy hardware 60
    fi
    if [ "$update_api" = true ]; then
        selected_update=true
        compose_started=true
        compose up \
            --detach \
            --no-build \
            --no-deps \
            --force-recreate \
            --remove-orphans \
            api
        wait_for_healthy api 60
    fi
    if [ "$update_web" = true ]; then
        selected_update=true
        compose_started=true
        compose up \
            --detach \
            --no-build \
            --no-deps \
            --force-recreate \
            --remove-orphans \
            modemdeck
        wait_for_healthy modemdeck 60
    fi
    if [ "$cloudflare_enabled" = true ] &&
        [ "$update_cloudflared" = true ]
    then
        selected_update=true
        compose_started=true
        compose up \
            --detach \
            --no-build \
            --no-deps \
            --force-recreate \
            --remove-orphans \
            cloudflared
        wait_for_healthy cloudflared 60
    fi
    if [ "$selected_update" != true ]; then
        printf '%s\n' 'No running container needs replacement.'
    fi
fi

wait_for_healthy hardware 60
wait_for_healthy api 60
wait_for_healthy modemdeck 60
if [ "$cloudflare_enabled" = true ]; then
    wait_for_healthy cloudflared 60
fi

if [ -n "$legacy_cloudflared_id" ] &&
    [ "$(docker inspect --format '{{.State.Running}}' \
        "$legacy_cloudflared_id" 2>/dev/null || true)" = true ]; then
    log "Stopping the superseded manual Cloudflare Tunnel connector"
    docker stop --time 20 "$legacy_cloudflared_id" >/dev/null ||
        warn "the superseded manual connector is still running: $legacy_cloudflared_id"
fi

repo_uid=$(stat -c %u "$repo_dir")
repo_gid=$(stat -c %g "$repo_dir")
install -o "$repo_uid" -g "$repo_gid" -m 0600 "$env_work" "$env_file"

if [ "$mode" = simple ] && [ "$baseline_created" = true ]; then
    mark_state_installed "$service_state_file"
fi
if [ "$mode" = simple ]; then
    rm -f -- "$rollback_state_file"
fi

installation_complete=true
services_changed=false

log "Installation complete"
printf 'Web UI:         https://%s:%s\n' "$bind_address" "$port"
if [ "$cloudflare_enabled" = true ]; then
    printf '%s\n' 'iOS API:        auto-discovered from Tunnel ingress'
fi
printf 'Hardware mode:  %s\n' "$mode"
printf '%s\n' 'First install: complete Web Quick Start to create the administrator account.'
printf '%s\n' \
    'Application data, Agent ownership state, ModemManager state, and secrets are persistent.'
