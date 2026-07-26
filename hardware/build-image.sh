#!/usr/bin/env bash
set -Eeuo pipefail

readonly script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd -- "${script_dir}/.." && pwd)"

platforms="${MODEMDECK_HARDWARE_PLATFORMS:-linux/amd64,linux/arm64}"
tag="${MODEMDECK_HARDWARE_TAG:-modemdeck-hardware:dev}"
output="${MODEMDECK_HARDWARE_OUTPUT:-oci}"
version="${VERSION:-dev}"
build_date="${BUILD_DATE:-unknown}"
vcs_ref="${VCS_REF:-unknown}"

usage() {
  cat <<'EOF'
Usage: hardware/build-image.sh [OPTIONS]

Options:
  --platform PLATFORMS  BuildKit platform list (default: amd64 and arm64)
  --tag IMAGE           Image tag (default: modemdeck-hardware:dev)
  --output oci|load|push
  --help

The default OCI output is hardware/dist/modemdeck-hardware.oci.tar. Docker
cannot --load a multi-platform image, so --output load accepts one platform.
EOF
}

while (( $# > 0 )); do
  case "$1" in
    --platform)
      [[ $# -ge 2 ]] || { printf '%s\n' "--platform requires a value" >&2; exit 2; }
      platforms="$2"
      shift 2
      ;;
    --tag)
      [[ $# -ge 2 ]] || { printf '%s\n' "--tag requires a value" >&2; exit 2; }
      tag="$2"
      shift 2
      ;;
    --output)
      [[ $# -ge 2 ]] || { printf '%s\n' "--output requires a value" >&2; exit 2; }
      output="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 2
      ;;
  esac
done

output_args=()
case "${output}" in
  oci)
    mkdir -p "${script_dir}/dist"
    output_args=(
      "--output=type=oci,dest=${script_dir}/dist/modemdeck-hardware.oci.tar"
    )
    ;;
  load)
    [[ "${platforms}" != *,* ]] \
      || { printf '%s\n' "--output load accepts exactly one platform" >&2; exit 2; }
    output_args=("--load")
    ;;
  push)
    output_args=("--push")
    ;;
  *)
    printf 'invalid output %q; expected oci, load, or push\n' "${output}" >&2
    exit 2
    ;;
esac

docker buildx build \
  --file "${script_dir}/Dockerfile" \
  --platform "${platforms}" \
  --tag "${tag}" \
  --build-arg "VERSION=${version}" \
  --build-arg "BUILD_DATE=${build_date}" \
  --build-arg "VCS_REF=${vcs_ref}" \
  "${output_args[@]}" \
  "${repo_root}"

if [[ "${output}" == "oci" ]]; then
  "${script_dir}/tests/verify-oci-platforms.sh" \
    "${script_dir}/dist/modemdeck-hardware.oci.tar"
fi
