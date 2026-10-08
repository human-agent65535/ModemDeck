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
Native capture/render at a different rate must resample at the transport boundary.
The server adapts hardware 8/16 kHz and 10/20 ms PCM internally.

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
their distinct 20 ms media positions. Silence fills missing source positions.
A pause in microphone packets alone does not end the connection.

Native, browser and server playback use a fixed 40 ms prebuffer. Each source timestamp
maps to its own playback position; arrival time is not a shared expiry deadline
for an entire batch. The bounded playback capacity is seven frames: five frames
of supported capture batching plus two frames of prebuffer, or 140 ms of queued
audio. This is a capacity limit, not an added 140 ms delay. Native capture and
transport queues retain their separate 100 ms freshness limits. Device output
latency is not counted as audio still waiting to be rendered.
The server maps playback positions to the nearest hardware frame boundary;
40 ms is the buffering target, not an exact wall-clock or end-to-end guarantee.

All three receivers use the same C source in `internal/audiocore/audio_core.c`.
iOS compiles it into the native App, Go calls it through the small CGO adapter,
and Web loads the generated `audioCore.wasm` only when connecting a call. There
is no separate Swift, TypeScript or Go implementation of clock recovery.
The adapters own CallKit/AVAudioEngine, Web Audio, or the hardware PCM cadence;
platform device interfaces and PCM formats remain outside the portable core.

The shared core compares source progress with monotonic arrival
time, so audio held in TCP buffers does not become fresh merely on receipt.
The source-age and late-playback budgets remain 100 ms. A sustained change in
arrival latency is accepted only after three independent windows of at least
100 ms of source audio show matching real-time progress (within 20 ms per
window). Packet freshness and this estimator are independent: a fresh tail of
a partially stale batch cannot reset an incomplete observation window. A full
stable fresh window ends a transient episode without changing the source anchor.
This permits regular five-frame batches, while fast stale TCP bursts
cannot establish a new clock. A new clock generation invalidates old queued
audio. A true playback underrun starts a new 40 ms playback epoch without
resetting the source-age check or accumulating unbounded delay. The server uses
a fixed logical hardware grid for slot selection and the actual monotonic wake
time for expiry; timer wake delay cannot introduce an extra frame each batch.
All device epochs project the core's unwrapped source sample positions onto
the device render cursor. A missed hardware slot or Web Audio render interval
skips the missed samples rather than delaying all subsequent audio. A partial
10 ms hardware frame is accounted for at most once per original Opus packet.
Capture batches carry their own source sample ranges and each frame's sample-end
time, so discarded future batches cannot move the position of an earlier batch.
Encoding failures preserve the omitted source positions as sequence gaps.

The 40 ms target is a low-latency starting point, not a universal network
guarantee. Twilio documents fixed 20/40/60 ms conference buffers; WebRTC NetEq
starts with an 80 ms target and then adapts to arrivals. Our target, queue
capacity, source freshness and hardware latency are deliberately independent.
See [Twilio's buffer controls](https://www.twilio.com/en-us/blog/products/launches/improve-call-experience-new-twilio-conference-jitter-buffer-controls)
and [NetEq's delay manager](https://webrtc.googlesource.com/src.git/+/refs/heads/main/modules/audio_coding/neteq/delay_manager.cc).

The server sends WebSocket ping control frames every five seconds; clients must
respond with pong (native WebSocket APIs do so automatically). No pong/read
activity for 30 seconds, a stalled writer, malformed audio, or loss of ownership
closes the transport. Audio writes have a 200 ms deadline. Normal shutdown
sends close code 1000; failures send 1011. JSON `stats` messages are emitted every five seconds and at
close when writable: `{"type":"stats","final":false,"audio":{...}}`; `audio`
contains `received_packets`, `received_bytes`, `sent_packets`, `input_dbfs`,
`input_peak_dbfs`, and `output_dbfs`, plus `transport`, `state`, optional
`dropped_packets` (valid incoming packets discarded as stale or overflowing), and
`failure_code`. Counters accumulate across reconnects within the same call;
`state` stays `connecting` until the first valid uplink packet, even after `ready`;
state becomes `disconnected` and failure remains available after a failed session. Levels use -96 dBFS for silence. An error is
`{"type":"error","code":"invalid_audio","message":"..."}`. Bounded codes include
`transport_timeout`, `transport_closed`, `invalid_audio`, `endpoint_failed`,
`codec_failed`, `backpressure`, `cancelled`, and fallback `media_failed`.
WSS `dropped_packets` is the sum of six mutually exclusive counters:
`dropped_source_early_packets`, `dropped_source_late_packets`,
`dropped_queue_overflow_packets`, `dropped_reanchor_packets`,
`dropped_playout_packets`, and `dropped_rebuffer_packets`.
`dropped_playout_packets` includes expired source deadlines and packets whose
hardware source slots or remaining fragments were missed. `clock_reanchors`,
`playout_underruns`, `playout_silence_frames`, and `playout_missed_ticks`
distinguish source-clock recovery, missing media and scheduler delay. Client
diagnostics upload local capture/send/receive/playback counts and the server
counts in separate bounded events, using the existing opt-in setting. These
counters contain no PCM, Opus payload, phone number or recording content.
Clients may ignore unknown JSON fields. Server snapshots retain final counters after disconnection.

Reconnect through a fresh upgrade/start and fresh codec/queues. Release the old
owner through the existing media DELETE endpoint before replacing it, or wait
for socket closure; never replay buffered audio. Retry transient closure with
bounded backoff while the authoritative call is active and the lease remains
held. Keep the original retry deadline across early `ready` messages; clear it
only after useful duplex audio has stayed healthy for at least one second.
Reconnecting does not create a new call or recording.

The portable source is built reproducibly with `make audio-core`, using the
Docker SDK image pinned by digest in `scripts/build-audio-core.mjs`. Commit the
small Wasm file and its source/artifact fingerprint together. `npm test` and
`npm run build` in `web/` reject stale artifacts. Xcode and CGO compile the
canonical C file directly; do not copy it into either client. The shared clock
vectors in `internal/callmedia/testdata/socket_clock_vectors.json` run through
each production adapter, including actual Wasm in the Web tests. The suite
covers latency steps, partial stale batches, transient catch-up, fast TCP
backlog, source gaps and uint32 wrap; device rendering and hardware cadence
also have adapter-specific tests.
