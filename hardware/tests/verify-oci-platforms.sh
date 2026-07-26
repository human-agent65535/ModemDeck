#!/usr/bin/env bash
set -Eeuo pipefail

readonly tests_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly hardware_dir="$(cd -- "${tests_dir}/.." && pwd)"
readonly archive="${1:-${hardware_dir}/dist/modemdeck-hardware.oci.tar}"

command -v jq >/dev/null
command -v tar >/dev/null
[[ -f "${archive}" ]] || {
  printf 'OCI archive not found: %s\n' "${archive}" >&2
  exit 1
}

work_dir="$(mktemp -d)"
trap 'find "${work_dir}" -depth -delete' EXIT
tar -xf "${archive}" -C "${work_dir}"

resolve_blob() {
  local digest="$1"

  [[ "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]] || {
    printf 'unsupported OCI digest: %s\n' "${digest}" >&2
    return 1
  }
  printf '%s/blobs/sha256/%s\n' "${work_dir}" "${digest#sha256:}"
}

walk_index() {
  local index_path="$1"
  local descriptor
  local media_type
  local digest
  local os
  local architecture
  local blob_path

  [[ -f "${index_path}" ]] || {
    printf 'missing OCI descriptor blob: %s\n' "${index_path}" >&2
    return 1
  }

  while IFS= read -r descriptor; do
    media_type="$(jq -r '.mediaType // ""' <<<"${descriptor}")"
    digest="$(jq -r '.digest // ""' <<<"${descriptor}")"
    os="$(jq -r '.platform.os // ""' <<<"${descriptor}")"
    architecture="$(jq -r '.platform.architecture // ""' <<<"${descriptor}")"
    blob_path="$(resolve_blob "${digest}")"

    case "${media_type}" in
      application/vnd.oci.image.index.v1+json|\
      application/vnd.docker.distribution.manifest.list.v2+json)
        walk_index "${blob_path}"
        ;;
      application/vnd.oci.image.manifest.v1+json|\
      application/vnd.docker.distribution.manifest.v2+json)
        if [[ "${os}" != "unknown" && "${architecture}" != "unknown" ]]; then
          [[ -f "${blob_path}" ]] || {
            printf 'missing OCI manifest blob: %s\n' "${blob_path}" >&2
            return 1
          }
          printf '%s/%s\n' "${os}" "${architecture}"
        fi
        ;;
      *)
        printf 'unsupported OCI media type: %s\n' "${media_type}" >&2
        return 1
        ;;
    esac
  done < <(jq -c '.manifests[]' "${index_path}")
}

platforms="$(walk_index "${work_dir}/index.json" | sort)"
expected=$'linux/amd64\nlinux/arm64'

[[ "${platforms}" == "${expected}" ]] || {
  printf 'unexpected OCI platforms\nexpected:\n%s\nactual:\n%s\n' \
    "${expected}" "${platforms}" >&2
  exit 1
}

printf 'verify-oci-platforms: ok (%s)\n' \
  "$(tr '\n' ' ' <<<"${platforms}" | sed 's/[[:space:]]*$//')"
