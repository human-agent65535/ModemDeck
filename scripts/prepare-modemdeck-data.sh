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
unexpected_link=$(find "$data_dir" -xdev -type l -print -quit)
if [ -n "$unexpected_link" ]; then
    echo "application data must not contain symbolic links: $unexpected_link" >&2
    exit 1
fi

# Do not dereference a link even if an entry is replaced between the validation
# above and chown. The installer quiesces the application container while this
# script runs; -h also protects against an out-of-process race.
find "$data_dir" -xdev -exec chown -h "$uid:$gid" {} +
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

tls_tree="${data_dir}/tls"
if [ -e "$tls_tree" ]; then
    [ -d "$tls_tree" ] && [ ! -L "$tls_tree" ] || {
        echo "TLS data path must be a real directory: $tls_tree" >&2
        exit 1
    }
else
    install -d -o "$uid" -g "$gid" -m 0750 "$tls_tree"
fi
find "$tls_tree" -xdev -type d -exec chmod 0750 {} +
find "$tls_tree" -xdev -type f -exec chmod 0640 {} +
if [ -f "${tls_tree}/automatic-ca.pem" ] &&
    [ ! -L "${tls_tree}/automatic-ca.pem" ]; then
    chmod 0600 "${tls_tree}/automatic-ca.pem"
fi

printf 'prepared %s for uid=%s gid=%s\n' "$data_dir" "$uid" "$gid"
