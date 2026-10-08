# Call audio over WebSocket (version 1)

Real calls use `GET /api/v1/calls/{id}/media/ws`. iOS audio tests use
`GET /api/v1/mobile/call-tests/{id}/media/ws`. Both enter the same media runtime,
call lease, single media owner, and shared PCM hub. Hardware access remains
behind the Agent Unix socket. WSS is the only call-media transport; SDP/ICE and TURN endpoints are removed.

Use WSS on the existing HTTPS ingress (443). Authenticate the upgrade with the
existing iOS Bearer header or same-origin browser session cookies. Credentials
must never be put in a URL. Browser upgrades require an Origin matching the
request host; native authenticated upgrades may omit Origin. A call lease must
already be held by this authenticated session/device. Renew it through the
existing HTTP lease endpoint.

After upgrade the client sends one JSON text message within five seconds:

```json
{"type":"start","version":1,"codec":"opus","sample_rate":16000,"channels":1,"frame_ms":20,"owner_token":"client-generated UUID"}
```

The owner token is the same identity used by `DELETE .../media`. The server
responds with `{"type":"ready","version":1,"codec":"opus","sample_rate":16000,"channels":1,"frame_ms":20}`
before sending or accepting audio. Unsupported formats terminate the session.
The shared PCM source starts only after the first valid 20 ms Opus uplink frame: after native
audio activation send silence frames even when muted. The first uplink must
arrive within the 15-second media preparation window. This prevents the test
guide from playing before CallKit has activated audio.
The negotiated `sample_rate` is the 16 kHz wire timestamp clock and nominal PCM
format, not a requirement to resample before Opus encoding. Opus supports input
at 8/16/48 kHz; each 20 ms packet retains the same wire timestamp advance. The server
encodes hardware 8/16 kHz PCM directly. NetEq always decodes internally at 48 kHz and
uses the official resampler where the output device requires 8/16 kHz PCM.
The server adapts 10/20 ms hardware periods without a second playout queue.

Each binary WebSocket message contains exactly one mono Opus packet encoding
20 ms (320 samples at 16 kHz). The 12-byte header is:

| Offset | Bytes | Meaning |
| --- | --- | --- |
| 0 | 2 | ASCII `MD` |
| 2 | 1 | Version `1` |
| 3 | 1 | Flags `0` |
| 4 | 4 | Unsigned sequence, big endian |
| 8 | 4 | Unsigned timestamp in 16 kHz sample ticks, big endian |
| 12 | 1–1275 | Opus payload |

Each direction has independent sequence/timestamp counters. Start at zero;
sequence increases by one and timestamp by 320 per audio frame, wrapping uint32.
After capture interruption, counters may skip over omitted frames. The server
rejects backward/duplicate sequence and inconsistent timestamp advances. Native
capture can deliver five frames together every 100 ms; receivers must retain
their distinct 20 ms media positions. NetEq conceals and recovers from missing source positions.
A pause in microphone packets alone does not end the connection.

All three receivers use the same pinned upstream WebRTC NetEq and Opus build.
The implementation and C ABI are in `internal/audiocore/native/`; the verified
upstream source archive, per-file hashes, revisions and licenses are in
`third_party/audio/`. No PeerConnection, ICE, TURN, SDP negotiation or WebRTC
transport is introduced. WSS authentication, leases and the hardware boundary
remain unchanged.

NetEq owns packet buffering, decoding, adaptive delay, packet-loss concealment
and time stretching. Its internal Opus/RTP clock is 48 kHz. A wire timestamp is
multiplied by three modulo uint32 and the low 16 sequence bits form its local
RTP header; wire validation retains the original 32-bit counters. Internal
48 kHz processing avoids rescaling a wrapping RTP timestamp through a second
clock. The official WebRTC resampler provides the required 8/16 kHz PCM output.

The initial minimum delay is 40 ms and the adaptive target ceiling is 200 ms.
These configure NetEq's buffering target, **not an end-to-end latency limit**.
Capture batching, network stalls and hardware buffering add delay. A packet
100 ms late is not automatically discarded. NetEq can preserve useful speech
by increasing delay and later accelerating audio; it decides when concealment
or a packet discard is necessary. Concealed samples are replacement audio,
not proof that the original words were preserved. A long TCP stall can still
cause both concealment and temporarily high latency.

The actual consumer owns playout. iOS uses the activated VPIO render callback;
Web uses AudioWorklet render demand; the server PCM hub uses one 10 ms consumption
clock. Each consumes NetEq's 10 ms output. A 20 ms hardware endpoint receives
two adjacent halves assembled by that same clock, with no extra playback FIFO. A platform callback with a different
block size retains at most one 10 ms remainder. There is no independent
40 ms prefill queue, fixed source slot, 100 ms expiry gate or second socket
playback timer. The server recorder observes the PCM actually handed to the
hardware, including concealment.

A preallocated single-producer/single-consumer ingress queue transports packets
from a network callback to the render owner; it does not schedule playout. It
can hold 200 packets (four seconds of 20 ms packets), matching NetEq's packet
storage limit. The sender can hold 100 pending packets (two seconds). These are
resource limits, not preferred latency or instructions to silently drop speech.
An exhausted transport queue reports backpressure and follows the existing
bounded reconnection lifecycle. A normal 100 ms microphone batch retains all
five packets. Encoding failures preserve omitted positions as sequence gaps.

Arrival and render timestamps use one monotonic epoch. Delayed delivery of a
network callback may carry an arrival time earlier than the most recent render;
that is accepted and the original arrival is supplied to NetEq. The environment
clock itself never goes backwards. Source counters wrap without resetting audio.
Recreating a transport creates fresh codec and buffer state; a device callback
must never consume a replacement connection's receiver through a stale owner.

The server sends WebSocket ping control frames every five seconds; clients must
respond with pong (native WebSocket APIs do so automatically). No pong/read
activity for 30 seconds, a stalled writer, malformed audio, or loss of ownership
closes the transport. Audio writes have a two-second deadline. Normal shutdown
sends close code 1000; failures send 1011. JSON `stats` messages are emitted every five seconds and at
close when writable: `{"type":"stats","final":false,"audio":{...}}`; `audio`
contains `received_packets`, `received_bytes`, `sent_packets`, `input_dbfs`,
`input_peak_dbfs`, and `output_dbfs`, plus `transport`, `state`, optional
`dropped_packets` (packets discarded by NetEq), and
`failure_code`. Counters accumulate across reconnects within the same call;
`state` stays `connecting` until the first valid uplink packet, even after `ready`;
state becomes `disconnected` and failure remains available after a failed session. Levels use -96 dBFS for silence. An error is
`{"type":"error","code":"invalid_audio","message":"..."}`. Bounded codes include
`transport_timeout`, `transport_closed`, `invalid_audio`, `endpoint_failed`,
`codec_failed`, `backpressure`, `cancelled`, and fallback `media_failed`.
NetEq diagnostics distinguish concealment, acceleration/deceleration, discarded
packets, target/current buffer delay, ingress occupancy and render failures.
`concealed_samples`, `inserted_samples` and `removed_samples` use the fixed
48 kHz internal clock. Useful output counters use their explicitly reported
output rate. Buffer delay is not microphone-to-ear latency. Existing numeric
fields from older iOS builds remain accepted so a queued old event cannot reject
an otherwise valid diagnostic batch. Diagnostics are still opt-in and contain
no PCM, encoded payloads, phone numbers or recording content.
Clients may ignore unknown JSON fields. Server snapshots retain final counters after disconnection.

Reconnect through a fresh upgrade/start and fresh codec/queues. Release the old
owner through the existing media DELETE endpoint before replacing it, or wait
for socket closure; never replay buffered audio. Retry transient closure with
bounded backoff while the authoritative call is active and the lease remains
held. Keep the original retry deadline across early `ready` messages; clear it
only after useful duplex audio has stayed healthy for at least one second.
Reconnecting does not create a new call or recording.

Build Web artifacts with `make audio-core`. The Emscripten image is pinned by
digest. Commit `audioCore.mjs`, `audioCore.wasm` and `audioCore.build.json`
together; Web test/build entry points verify source and artifact fingerprints.
The module uses fixed, unshared 32 MiB memory and no pthread/SharedArrayBuffer
requirement. It loads only when call audio is prepared, before activation.

The Docker `go-toolchain` stage builds the same sources and installs one bundled
static archive. `node scripts/build-native-audio-core.mjs --ios` builds the iOS
device/simulator XCFramework; `--host` builds the Swift behavior-test library.
Native adapters and standalone recording codecs link the same bundled Opus.
They must not add a second system, Swift Package or JavaScript Opus provider.

Verification covers actual encoded audio, 20/100 ms batches, isolated and
repeated jitter, latency steps, TCP stalls, sequence/timestamp wrap, clock drift,
8/16 kHz hardware output, variable render blocks, reconnect and ownership.
Offline correctness and a successful app build do not establish physical-device
microphone quality, cellular performance, acoustic echo cancellation or power use.
