#!/bin/sh

set -eu

exec /usr/local/bin/modemdeck \
    --listen "${MODEMDECK_LISTEN_ADDRESS}" \
    --database "${MODEMDECK_DATABASE_PATH}" \
    --agent-socket "${MODEMDECK_AGENT_SOCKET}" \
    --tls-directory "${MODEMDECK_TLS_DIRECTORY}" \
    --tls-hosts "${MODEMDECK_TLS_HOSTS}" \
    "$@"
