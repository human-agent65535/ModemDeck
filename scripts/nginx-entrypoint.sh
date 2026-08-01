#!/bin/sh

set -eu

tls_directory=${MODEMDECK_WEB_TLS_DIRECTORY:-/var/lib/modemdeck/tls}
runtime_certificate=${MODEMDECK_WEB_TLS_CERTIFICATE:-/tmp/modemdeck-web-cert.pem}
runtime_key=${MODEMDECK_WEB_TLS_KEY:-/tmp/modemdeck-web-key.pem}
runtime_http3_config=${MODEMDECK_WEB_HTTP3_CONFIG:-/tmp/modemdeck-http3.conf}
origin_bundle=${MODEMDECK_CLOUDFLARE_ORIGIN_TLS_BUNDLE:-${tls_directory}/cloudflare-origin.pem}
runtime_origin_certificate=${MODEMDECK_CLOUDFLARE_ORIGIN_TLS_CERTIFICATE:-/tmp/modemdeck-origin-cert.pem}
runtime_origin_key=${MODEMDECK_CLOUDFLARE_ORIGIN_TLS_KEY:-/tmp/modemdeck-origin-key.pem}
runtime_origin_api_config=${MODEMDECK_CLOUDFLARE_ORIGIN_API_CONFIG:-/tmp/modemdeck-origin-7575.conf}
runtime_origin_web_config=${MODEMDECK_CLOUDFLARE_ORIGIN_WEB_CONFIG:-/tmp/modemdeck-origin-7576.conf}
https_port=${MODEMDECK_WEB_HTTPS_PORT:-7577}
refresh_seconds=${MODEMDECK_WEB_TLS_REFRESH_SECONDS:-5}

case "$https_port" in
    ""|*[!0-9]*)
        printf 'modemdeck-web: HTTPS port must be numeric\n' >&2
        exit 1
        ;;
esac
[ "$https_port" -ge 1 ] && [ "$https_port" -le 65535 ] || {
    printf 'modemdeck-web: HTTPS port must be between 1 and 65535\n' >&2
    exit 1
}

printf 'add_header Alt-Svc '\''h3=":%s"; ma=86400'\'' always;\n' \
    "$https_port" >"$runtime_http3_config"

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

local_bundle_checksum() {
    bundle_path=$(selected_bundle)
    [ -r "$bundle_path" ] || {
        printf 'modemdeck-web: TLS bundle is not readable: %s\n' "$bundle_path" >&2
        return 1
    }
    cksum "${tls_directory}/source" "$bundle_path" | cksum | awk '{ print $1 ":" $2 }'
}

configuration_checksum() {
    local_checksum=$(local_bundle_checksum) || return 1
    if [ -e "$origin_bundle" ]; then
        [ -f "$origin_bundle" ] && [ -r "$origin_bundle" ] || {
            printf 'modemdeck-web: Cloudflare origin TLS bundle is not a readable regular file: %s\n' \
                "$origin_bundle" >&2
            return 1
        }
        origin_checksum=$(cksum "$origin_bundle") || return 1
    else
        origin_checksum=cloudflare-origin-tls:disabled
    fi
    printf '%s\n%s\n' "$local_checksum" "$origin_checksum" |
        cksum | awk '{ print $1 ":" $2 }'
}

extract_material() {
    bundle_path=$1
    certificate_path=$2
    key_path=$3
    next_certificate="${certificate_path}.next"
    next_key="${key_path}.next"
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
        rm -f "$next_certificate" "$next_key"
        printf 'modemdeck-web: TLS bundle is incomplete: %s\n' "$bundle_path" >&2
        return 1
    }
    chmod 0444 "$next_certificate"
    chmod 0400 "$next_key"
    mv "$next_certificate" "$certificate_path"
    mv "$next_key" "$key_path"
}

install_local_material() {
    extract_material "$(selected_bundle)" "$runtime_certificate" "$runtime_key"
}

write_origin_listener() {
    port=$1
    destination=$2
    next_destination="${destination}.next"
    if [ -e "$origin_bundle" ]; then
        {
            printf 'listen %s ssl default_server;\n' "$port"
            printf '%s\n' 'http2 on;'
            printf 'ssl_certificate %s;\n' "$runtime_origin_certificate"
            printf 'ssl_certificate_key %s;\n' "$runtime_origin_key"
        } >"$next_destination"
    else
        printf 'listen %s default_server;\n' "$port" >"$next_destination"
    fi
    chmod 0444 "$next_destination"
    mv "$next_destination" "$destination"
}

install_origin_material() {
    if [ -e "$origin_bundle" ]; then
        [ -f "$origin_bundle" ] && [ -r "$origin_bundle" ] || {
            printf 'modemdeck-web: Cloudflare origin TLS bundle is not a readable regular file: %s\n' \
                "$origin_bundle" >&2
            return 1
        }
        extract_material \
            "$origin_bundle" \
            "$runtime_origin_certificate" \
            "$runtime_origin_key" || return 1
    fi
    write_origin_listener 7575 "$runtime_origin_api_config"
    write_origin_listener 7576 "$runtime_origin_web_config"
    if [ ! -e "$origin_bundle" ]; then
        rm -f "$runtime_origin_certificate" "$runtime_origin_key"
    fi
}

runtime_files() {
    printf '%s\n' \
        "$runtime_certificate" \
        "$runtime_key" \
        "$runtime_origin_certificate" \
        "$runtime_origin_key" \
        "$runtime_origin_api_config" \
        "$runtime_origin_web_config"
}

backup_runtime() {
    runtime_files | while IFS= read -r file; do
        rm -f "${file}.previous" "${file}.missing"
        if [ -e "$file" ]; then
            cp "$file" "${file}.previous"
        else
            : >"${file}.missing"
        fi
    done
}

restore_runtime() {
    runtime_files | while IFS= read -r file; do
        if [ -e "${file}.missing" ]; then
            rm -f "$file"
        elif [ -e "${file}.previous" ]; then
            mv "${file}.previous" "$file"
        fi
        rm -f "${file}.previous" "${file}.missing"
    done
}

clear_runtime_backup() {
    runtime_files | while IFS= read -r file; do
        rm -f "${file}.previous" "${file}.missing"
    done
}

install_local_material
install_origin_material
nginx -t
nginx "$@" &
nginx_pid=$!

stop_nginx() {
    kill -QUIT "$nginx_pid" 2>/dev/null || true
}
trap stop_nginx INT TERM QUIT

last_checksum=$(configuration_checksum)
while kill -0 "$nginx_pid" 2>/dev/null; do
    sleep "$refresh_seconds" &
    wait $! || true
    next_checksum=$(configuration_checksum) || continue
    [ "$next_checksum" = "$last_checksum" ] && continue

    backup_runtime
    if install_local_material && install_origin_material && nginx -t; then
        if nginx -s reload; then
            clear_runtime_backup
            last_checksum=$next_checksum
            printf '%s\n' 'modemdeck-web: reloaded TLS listener configuration'
            continue
        fi
    fi
    restore_runtime
    nginx -t && nginx -s reload || true
    printf '%s\n' 'modemdeck-web: rejected an invalid TLS listener update' >&2
done

wait "$nginx_pid"
