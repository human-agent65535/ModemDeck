#!/bin/sh

set -eu

violations="$(
    git ls-files | awk '
        BEGIN { IGNORECASE = 1 }
        /^web\/src\/assets\/(notifications|ringtones|tones)\/[^\/]+\.ogg$/ {
            next
        }
        /^internal\/tlsmanager\/cloudflare_origin_roots\.pem$/ {
            next
        }
        /(^|\/)(data|logs|secrets|recordings|private|scratch|backups|one-off|migration|migrations)\// ||
        /(^|\/)cmd\/modemdeck-(import-legacy-telegram|migrate)\// ||
        /(^|\/)internal\/(legacymigrate|legacytelegram)\// ||
        /(^|\/)integration\/legacy_/ ||
        /\.(db|db-journal|db-shm|db-wal|sqlite|sqlite3|dump|backup|bak|sql\.gz)$/ ||
        /\.(key|pem|p12|pfx|jks|keystore)$/ ||
        /\.(wav|flac|ogg|opus)$/ {
            print
        }
    '
)"

if [ -n "${violations}" ]; then
    printf '%s\n' "repository hygiene check rejected tracked private or one-off artifacts:" >&2
    printf '%s\n' "${violations}" >&2
    exit 1
fi
