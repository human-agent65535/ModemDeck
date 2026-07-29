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

prepare_private_tree() {
    private_tree=$1
    [ -e "$private_tree" ] || return 0
    [ -d "$private_tree" ] && [ ! -L "$private_tree" ] || {
        echo "private data path must be a real directory: $private_tree" >&2
        exit 1
    }
    find "$private_tree" -xdev -type d -exec chmod 0700 {} +
    find "$private_tree" -xdev -type f -exec chmod 0600 {} +
}

prepare_private_tree "${data_dir}/recordings"
prepare_private_tree "${data_dir}/tls"

printf 'prepared %s for uid=%s gid=%s\n' "$data_dir" "$uid" "$gid"
