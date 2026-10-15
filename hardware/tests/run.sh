#!/usr/bin/env bash
set -Eeuo pipefail

readonly tests_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly hardware_dir="$(cd -- "${tests_dir}/.." && pwd)"
readonly trixie_image="debian:trixie-slim@sha256:020c0d20b9880058cbe785a9db107156c3c75c2ac944a6aa7ab59f2add76a7bd"
readonly go_image="golang:1.26.9-trixie@sha256:f06b7122f888680048afb247c4e5f49c0069eb6c4c1ccae0f3f4834527a8d22c"

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
