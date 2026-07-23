#!/bin/sh

set -eu

exec /usr/local/bin/modemdeck \
    --listen "${MODEMDECK_HTTP_ADDRESS}" \
    --database "${MODEMDECK_DATABASE_PATH}" \
    --legacy-database "${MODEMDECK_LEGACY_DATABASE_PATH}" \
    --agent-socket "${MODEMDECK_AGENT_SOCKET}" \
    "$@"
