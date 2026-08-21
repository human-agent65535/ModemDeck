#!/bin/sh

set -eu

# Direct installs and simulator builds do not produce distributable archives.
if [ "${ACTION:-}" != "install" ] || [ "${PLATFORM_NAME:-}" != "iphoneos" ]; then
    exit 0
fi

embedded_binary="${TARGET_BUILD_DIR}/${FRAMEWORKS_FOLDER_PATH}/WebRTC.framework/WebRTC"
derived_data_root="${BUILD_ROOT%%/Build/*}"
package_binary="${derived_data_root}/SourcePackages/artifacts/webrtc/WebRTC/WebRTC.xcframework/ios-arm64/WebRTC.framework/WebRTC"

if [ -f "${embedded_binary}" ]; then
    framework_binary="${embedded_binary}"
elif [ -f "${package_binary}" ]; then
    framework_binary="${package_binary}"
else
    echo "error: unable to locate the WebRTC device framework binary" >&2
    exit 1
fi

dsym_path="${DWARF_DSYM_FOLDER_PATH}/WebRTC.framework.dSYM"
dwarf_path="${dsym_path}/Contents/Resources/DWARF/WebRTC"

if [ -e "${dsym_path}" ]; then
    /bin/rm -rf -- "${dsym_path}"
fi

/usr/bin/xcrun dsymutil "${framework_binary}" -o "${dsym_path}"

if [ ! -s "${dwarf_path}" ]; then
    echo "error: dsymutil did not create a WebRTC DWARF file" >&2
    exit 1
fi

binary_uuids="$(/usr/bin/xcrun dwarfdump --uuid "${framework_binary}" | /usr/bin/awk '{ print toupper($2) }' | /usr/bin/sort)"
dsym_uuids="$(/usr/bin/xcrun dwarfdump --uuid "${dwarf_path}" | /usr/bin/awk '{ print toupper($2) }' | /usr/bin/sort)"

if [ -z "${binary_uuids}" ] || [ "${binary_uuids}" != "${dsym_uuids}" ]; then
    echo "error: WebRTC framework and dSYM UUIDs do not match" >&2
    exit 1
fi

echo "Generated WebRTC.framework.dSYM for UUID(s): ${dsym_uuids}"
