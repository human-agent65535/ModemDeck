#!/bin/sh

set -eu

tls_directory=${MODEMDECK_WEB_TLS_DIRECTORY:-/var/lib/modemdeck/tls}
runtime_certificate=${MODEMDECK_WEB_TLS_CERTIFICATE:-/tmp/modemdeck-web-cert.pem}
runtime_key=${MODEMDECK_WEB_TLS_KEY:-/tmp/modemdeck-web-key.pem}
refresh_seconds=${MODEMDECK_WEB_TLS_REFRESH_SECONDS:-5}

selected_bundle() {
    source_value=$(tr -d '[:space:]' <"${tls_directory}/source")
    case "$source_value" in
        auto)
            printf '%s\n' "${tls_directory}/automatic-server.pem"
            ;;
        user)
            printf '%s\n' "${tls_directory}/user.pem"
            ;;
        *)
            printf 'modemdeck-web: invalid TLS source %s\n' "$source_value" >&2
            return 1
            ;;
    esac
}

bundle_checksum() {
    bundle_path=$(selected_bundle)
    [ -r "$bundle_path" ] || {
        printf 'modemdeck-web: TLS bundle is not readable: %s\n' "$bundle_path" >&2
        return 1
    }
    cksum "${tls_directory}/source" "$bundle_path" | cksum | awk '{ print $1 ":" $2 }'
}

install_material() {
    bundle_path=$(selected_bundle)
    next_certificate="${runtime_certificate}.next"
    next_key="${runtime_key}.next"
    awk '
        /^-----BEGIN CERTIFICATE-----$/ { certificate = 1 }
        certificate { print }
        /^-----END CERTIFICATE-----$/ { certificate = 0 }
    ' "$bundle_path" >"$next_certificate"
    awk '
        /^-----BEGIN .*PRIVATE KEY-----$/ { private_key = 1 }
        private_key { print }
        /^-----END .*PRIVATE KEY-----$/ { private_key = 0 }
    ' "$bundle_path" >"$next_key"
    [ -s "$next_certificate" ] && [ -s "$next_key" ] || {
        printf 'modemdeck-web: selected TLS bundle is incomplete: %s\n' "$bundle_path" >&2
        return 1
    }
    chmod 0444 "$next_certificate"
    chmod 0400 "$next_key"
    mv "$next_certificate" "$runtime_certificate"
    mv "$next_key" "$runtime_key"
}

install_material
nginx -t
nginx "$@" &
nginx_pid=$!

stop_nginx() {
    kill -QUIT "$nginx_pid" 2>/dev/null || true
}
trap stop_nginx INT TERM QUIT

last_checksum=$(bundle_checksum)
while kill -0 "$nginx_pid" 2>/dev/null; do
    sleep "$refresh_seconds" &
    wait $! || true
    next_checksum=$(bundle_checksum) || continue
    [ "$next_checksum" = "$last_checksum" ] && continue

    previous_certificate="${runtime_certificate}.previous"
    previous_key="${runtime_key}.previous"
    cp "$runtime_certificate" "$previous_certificate"
    cp "$runtime_key" "$previous_key"
    install_material
    if nginx -t; then
        rm -f "$previous_certificate" "$previous_key"
        nginx -s reload
        last_checksum=$next_checksum
        printf '%s\n' 'modemdeck-web: reloaded the selected TLS certificate'
    else
        mv "$previous_certificate" "$runtime_certificate"
        mv "$previous_key" "$runtime_key"
        printf '%s\n' 'modemdeck-web: rejected an invalid TLS certificate update' >&2
    fi
done

wait "$nginx_pid"
