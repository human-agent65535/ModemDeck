#!/usr/bin/env bash
set -Eeuo pipefail

readonly tests_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly hardware_dir="$(cd -- "${tests_dir}/.." && pwd)"
readonly trixie_image="debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad"
readonly go_image="golang:1.26.10-trixie@sha256:909771fef53e3dcb0950255f0c9a8b4728c3b841e61509c7b29b098de7ee02d4"

if [[ "${MODEMDECK_HARDWARE_TEST_CONTAINER:-}" != "1" ]]; then
  docker run --rm \
    -e MODEMDECK_HARDWARE_TEST_CONTAINER=1 \
    -v "${hardware_dir}:/hardware:ro" \
    "${trixie_image}" \
    /hardware/tests/run.sh

  docker run --rm \
    -e GOTOOLCHAIN=local \
    --mount type=volume,source=modemdeck-hardware-go-mod,target=/go/pkg/mod \
    --mount type=volume,source=modemdeck-hardware-go-build,target=/root/.cache/go-build \
    -v "${hardware_dir}/ownership:/workspace:ro" \
    -w /workspace \
    "${go_image}" \
    go test -mod=readonly ./...
  exit
fi

"${tests_dir}/static-test.sh"
"${tests_dir}/supervisor-test.sh"
