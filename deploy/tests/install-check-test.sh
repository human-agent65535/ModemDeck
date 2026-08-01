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
    printf 'install-check-test: %s\n' "$*" >&2
    exit 1
}

mkdir -p \
    "${test_root}/bin" \
    "${test_root}/dev" \
    "${test_root}/proc/net" \
    "${test_root}/run/systemd/system" \
    "${test_root}/run/udev" \
    "${test_root}/state" \
    "${test_root}/sys" \
    "${test_root}/tmp"

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

cat >"${test_root}/bin/docker" <<'EOF'
#!/bin/sh
printf 'docker|%s\n' "$*" >>"${MODEMDECK_TEST_COMMAND_LOG}"
case "${1:-}:${2:-}" in
    info:) exit 0 ;;
    compose:version) exit 0 ;;
    compose:config) exit 0 ;;
    compose:ps) exit 0 ;;
esac
exit 0
EOF

cat >"${test_root}/bin/systemctl" <<'EOF'
#!/bin/sh
printf 'systemctl|%s\n' "$*" >>"${MODEMDECK_TEST_COMMAND_LOG}"
case "${1:-}" in
    show)
        printf '%s\n' not-found
        exit 0
        ;;
    is-enabled)
        printf '%s\n' disabled
        exit 1
        ;;
    is-active)
        printf '%s\n' inactive
        exit 3
        ;;
esac
printf 'mutating systemctl call reached during --check: %s\n' "$*" >&2
exit 97
EOF
chmod 0755 "${test_root}/bin/"*

command_log="${test_root}/commands.log"
: >"$command_log"
state_dir="${test_root}/state/install"
data_dir="${test_root}/data"
settings_file="${test_root}/secrets/settings"

env \
    PATH="${test_root}/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
    TMPDIR="${test_root}/tmp" \
    MODEMDECK_TEST_COMMAND_LOG="$command_log" \
    MODEMDECK_HOST_SYS_ROOT="${test_root}/sys" \
    MODEMDECK_HOST_DEV_ROOT="${test_root}/dev" \
    MODEMDECK_HOST_PROC_ROOT="${test_root}/proc" \
    MODEMDECK_HOST_UDEV_ROOT="${test_root}/run/udev" \
    MODEMDECK_SYSTEMD_RUNTIME_DIR="${test_root}/run/systemd/system" \
    MODEMDECK_INSTALL_STATE_DIR="$state_dir" \
    MODEMDECK_DATA_DIR="$data_dir" \
    MODEMDECK_SETTINGS_KEY_FILE="$settings_file" \
    "${repo_dir}/install.sh" \
        --check \
        --git \
        --allow-dirty \
        >"${test_root}/output.log"

grep -Fq 'Read-only check passed' "${test_root}/output.log" ||
    fail "installer did not report a successful read-only check"

if grep -Eq \
    '^systemctl\|(stop|start|disable|enable|mask|unmask|daemon-reload)( |$)' \
    "$command_log"
then
    fail "--check invoked a mutating systemctl command"
fi
if grep -Eq '^docker\|compose (build|up|down)( |$)' "$command_log"; then
    fail "--check invoked a mutating Docker Compose command"
fi

[ ! -e "$state_dir" ] ||
    fail "--check created installer state"
[ ! -e "$data_dir" ] ||
    fail "--check created application data"
[ ! -e "$settings_file" ] ||
    fail "--check created the settings secret"

if find "${test_root}/tmp" -mindepth 1 -print -quit | grep -q .; then
    fail "--check left temporary files behind"
fi

printf '%s\n' "install-check-test: ok"
