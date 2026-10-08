# Shared audio timing core

`audio_core.c` owns the source clock, bounded recovery estimator, freshness
decisions and common timing budgets for iOS, Web and Go. It allocates no memory
and has no device, codec, network or wall-clock dependencies. Inputs/outputs use
monotonic seconds; decisions use integer microseconds. One state belongs to one
receive stream and is reset on reconnect, never merely on a playback underrun.

The native target imports `ModemDeckAudioCore` via `module.modulemap` and compiles
this C source. Go CGO compiles it in this package, so its build cache tracks the
actual C and header files. The browser compiles it with
`scripts/audio-core/wasm.c`; each Wasm instance has its own state. There is no
separate timing implementation for builds without CGO: those builds report
WSS audio unsupported, just as they cannot supply the production Opus codec.

Run `make audio-core` after changing the C source, header or Wasm build script.
The Docker compiler is pinned by digest. `web` tests/builds verify the source
and artifact hashes, and the production Wasm has no runtime imports. See
`docs/call-audio-websocket.md` for the wire protocol and audio lifecycle.
