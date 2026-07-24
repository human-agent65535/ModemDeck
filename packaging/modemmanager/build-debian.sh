#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
output_dir=${1:-"$repo_root/dist/modemmanager"}
debian_version=${MODEMMANAGER_DEBIAN_VERSION:-1.24.0-1+deb13u1}
custom_version=${MODEMMANAGER_CUSTOM_VERSION:-"${debian_version}+modemdeck1"}

mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)

docker run --rm \
  -e DEBIAN_FRONTEND=noninteractive \
  -e DEBFULLNAME=ModemDeck \
  -e DEBEMAIL=build@modemdeck.invalid \
  -e MM_DEBIAN_VERSION="$debian_version" \
  -e MM_CUSTOM_VERSION="$custom_version" \
  -e OUTPUT_UID="$(id -u)" \
  -e OUTPUT_GID="$(id -g)" \
  -v "$output_dir:/out" \
  debian:13-slim \
  sh -euxc '
    sed -i "s/^Types: deb$/Types: deb deb-src/" /etc/apt/sources.list.d/debian.sources
    apt-get update
    apt-get install -y --no-install-recommends ca-certificates devscripts dpkg-dev
    apt-get build-dep -y modemmanager

    mkdir /build
    cd /build
    apt-get download "modemmanager=$MM_DEBIAN_VERSION"
    apt-get source "modemmanager=$MM_DEBIAN_VERSION"
    source_dir=$(find /build -mindepth 1 -maxdepth 1 -type d -name "modemmanager-*" -print -quit)
    test -n "$source_dir"
    cd "$source_dir"

    sed -i "/^export DEB_BUILD_MAINT_OPTIONS/i configure_flags += -Dat_command_via_dbus=true" debian/rules
    sed -i "s/-Dpolkit=permissive/-Dpolkit=strict/" debian/rules
    grep -q -- "-Dat_command_via_dbus=true" debian/rules
    grep -q -- "-Dpolkit=strict" debian/rules

    dch --newversion "$MM_CUSTOM_VERSION" \
      "Enable the production AT D-Bus interface for the ModemDeck host agent."
    DEB_BUILD_OPTIONS="nocheck nodoc" dpkg-buildpackage -B -uc -us

    architecture=$(dpkg --print-architecture)
    stock="/build/modemmanager_${MM_DEBIAN_VERSION}_${architecture}.deb"
    custom="/build/modemmanager_${MM_CUSTOM_VERSION}_${architecture}.deb"
    test -f "$stock"
    test -f "$custom"
    cp "$stock" "$custom" /out/
    cd /out
    sha256sum ./*.deb > SHA256SUMS
    chown "$OUTPUT_UID:$OUTPUT_GID" ./*.deb SHA256SUMS
  '
