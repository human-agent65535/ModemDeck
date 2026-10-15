# Shared call audio core

`native/` exposes a small C ABI over the pinned original WebRTC NetEq and Opus.
iOS, Web and Go compile this same source with the same codec configuration.
The upstream source archive, revisions, hashes and licenses live in
`third_party/audio/`. There is no second Swift, Go or TypeScript implementation
of jitter buffering, concealment or adaptive playout.

A receiver has one render consumer and one network producer. Network enqueue
copies into preallocated SPSC ingress storage; the consumer owns NetEq. Only
that consumer calls pull/render or resets a device remainder. Stop both users
before destroying the receiver. The Go adapter additionally guards lifecycle
with a read/write lock; it does not serialize network enqueue against render.
The C header documents time domains, sample units, capacities and error codes.

All NetEq processing uses 48 kHz internally. RTP timestamp conversion is wrapping
16 kHz wire to 48 kHz multiplication, not a custom 16 kHz decoder. Official WebRTC
resampling supplies the device rate. NetEq owns delay and time stretching;
platforms own only authentication, transport, device activation and actual
render demand. See `docs/call-audio-websocket.md` for the complete contract.

Build Web artifacts with `make audio-core`, the Go toolchain with
`make root-toolchain`, and native libraries with
`node scripts/build-native-audio-core.mjs --ios` or `--host`.
Web checks reject stale source/artifact fingerprints. iOS build scripts rebuild
the static XCFramework when sources change. Go production builds use the
Docker-built static library and require CGO; no fallback audio algorithm is
provided when CGO is disabled.
