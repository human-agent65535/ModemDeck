# Shared NetEq and Opus sources

This directory vendors the upstream source closure used by the three production
audio adapters. It contains no prebuilt native binaries. The deterministic
`webrtc-audio-source.tar.xz` expands to `src/`; CMake verifies its SHA-256 before
extraction. `upstream.json` records exact upstream revisions and every file hash.
This keeps ordinary builds independent of network availability while preserving
an auditable source distribution. Upstream algorithms are not patched.

| Component | Official revision |
| --- | --- |
| WebRTC | `df21b0a5548fd4631d39fb723d16ead30144c4b2` |
| Chromium third_party snapshot | `8a83e63525b495aa7f35648a3b4b676279deb864` |
| Abseil | `7f008af1930f59d869a816795ac5d445e3628e4d` |
| Opus | `55513e81d8f606bd75d0ff773d2144e5f2a732f5` |

WebRTC files come from https://webrtc.googlesource.com/src/ at the recorded
revision. Abseil and Opus come from the matching Chromium third_party snapshot,
whose DEPS pins the revisions above. Nine Opus build-description files omitted
by Chromium's source subset (`*_headers.mk`, `*_sources.mk`, `Makefile.am`)
come from https://github.com/xiph/opus at that same Opus revision. The registered
field-trials header is generated with the pinned upstream `field_trials.py`.

`internal/audiocore/native/sources.cmake` lists the compiled WebRTC units; CMake
adds the upstream stdlib task-queue factory and implementation. No mock DSP,
decoder, platform factory or transport is substituted. Build settings select
portable C Opus with floating-point API, disable architecture intrinsics and
optional DNN support, and preserve standard Opus concealment. No browser
PeerConnection, ICE or TURN library is linked.

The checked-in archive is a source-distribution convenience. For audit, extract
it with `tar -xJf webrtc-audio-source.tar.xz`, then compare every `src/<path>` with
`upstream.json`. For an upgrade, fetch an explicitly selected upstream revision,
resolve its DEPS, regenerate the source list and field-trials header, rebuild
all native/Wasm targets and rerun conformance and adapter tests. Do not replace
only one platform's dependency or silently repack modified upstream files.

`LICENSES.txt` preserves WebRTC's BSD license/IP grant/authors, Abseil's Apache
license, Opus's BSD notices and the two bundled DSP dependency licenses. It is
packaged with the API/Web images and iOS application. Source files retain their
upstream license headers inside the archive.
