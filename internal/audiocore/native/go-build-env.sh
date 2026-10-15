#!/bin/sh
set -eu
# Go does not hash external static archives in its action cache. Include the
# exact bundled core's source fingerprint in cgo compiler flags for every run.
md_audio_build_id=$(cat /usr/local/share/modemdeck-audio/source.sha256)
case "$md_audio_build_id" in
  *[!0-9a-f]*|'') echo "Invalid audio source fingerprint" >&2; exit 1 ;;
esac
[ "${#md_audio_build_id}" -eq 64 ] || exit 1
export CGO_CPPFLAGS="${CGO_CPPFLAGS:+$CGO_CPPFLAGS }-DMD_AUDIO_CORE_BUILD_${md_audio_build_id}=1"
exec "$@"
