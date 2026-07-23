#!/bin/sh

set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "prepare-modemdeck-data.sh must run as root" >&2
    exit 1
fi

data_dir=${1:-}
uid=${MODEMDECK_UID:-10001}
gid=${MODEMDECK_GID:-10001}

case "$data_dir" in
    ""|/)
        echo "refusing unsafe data directory: ${data_dir:-<empty>}" >&2
        exit 1
        ;;
esac

for value in "$uid" "$gid"; do
    case "$value" in
        ""|*[!0-9]*)
            echo "MODEMDECK_UID and MODEMDECK_GID must be numeric" >&2
            exit 1
            ;;
    esac
done

install -d -o "$uid" -g "$gid" -m 0770 "$data_dir"
find "$data_dir" -xdev -exec chown "$uid:$gid" {} +
find "$data_dir" -xdev -type d -exec chmod u+rwx,g+rwx,o-rwx {} +

printf 'prepared %s for uid=%s gid=%s\n' "$data_dir" "$uid" "$gid"
