#!/bin/sh

set -eu

usage() {
    cat >&2 <<'EOF'
usage: install-modemdeck-agent.sh [PREBUILT_AGENT_BINARY]

Installs a prebuilt Linux modemdeck-agent and its systemd unit. This script
does not build source or install packages.
EOF
}

if [ "$#" -gt 1 ]; then
    usage
    exit 2
fi

if [ "$(id -u)" -ne 0 ]; then
    echo "install-modemdeck-agent.sh must run as root" >&2
    exit 1
fi

if [ "$(uname -s)" != "Linux" ]; then
    echo "modemdeck-agent is a Linux host service" >&2
    exit 1
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "${script_dir}/.." && pwd)
binary=${1:-"${repo_dir}/dist/modemdeck-agent"}
agent_gid=${MODEMDECK_AGENT_GID:-10002}

for command in getent groupadd install systemctl systemd-tmpfiles; do
    if ! command -v "$command" >/dev/null 2>&1; then
        echo "required runtime command is unavailable: $command" >&2
        exit 1
    fi
done

case "$agent_gid" in
    ""|*[!0-9]*)
        echo "MODEMDECK_AGENT_GID must be numeric" >&2
        exit 1
        ;;
esac

if [ ! -f "$binary" ] || [ -L "$binary" ] || [ ! -x "$binary" ]; then
    echo "agent binary must be an executable regular file, not a symlink: $binary" >&2
    exit 1
fi

if ! "$binary" -h >/dev/null 2>&1; then
    echo "agent binary cannot execute on this host: $binary" >&2
    exit 1
fi

if group_entry=$(getent group modemdeck); then
    existing_gid=$(printf '%s\n' "$group_entry" | cut -d: -f3)
    if [ "$existing_gid" != "$agent_gid" ]; then
        echo "modemdeck group uses gid $existing_gid, expected $agent_gid" >&2
        exit 1
    fi
else
    if getent group "$agent_gid" >/dev/null; then
        echo "gid $agent_gid is already assigned to another group" >&2
        exit 1
    fi
    groupadd --system --gid "$agent_gid" modemdeck
fi

install -d -o root -g modemdeck -m 0750 /etc/modemdeck
install -D -o root -g root -m 0755 \
    "$binary" /usr/local/bin/modemdeck-agent
install -D -o root -g root -m 0644 \
    "${repo_dir}/packaging/systemd/modemdeck-agent.tmpfiles.conf" \
    /usr/lib/tmpfiles.d/modemdeck.conf
install -D -o root -g root -m 0644 \
    "${repo_dir}/packaging/systemd/modemdeck-agent.service" \
    /etc/systemd/system/modemdeck-agent.service

systemd-tmpfiles --create /usr/lib/tmpfiles.d/modemdeck.conf
systemctl daemon-reload
systemctl enable modemdeck-agent.service
systemctl restart modemdeck-agent.service

printf 'modemdeck-agent installed; set MODEMDECK_AGENT_GID=%s for Docker Compose\n' "$agent_gid"
printf '%s\n' \
    'media remains disabled unless /etc/modemdeck/agent.env explicitly sets MODEMDECK_MEDIA_BINDINGS_FILE'
