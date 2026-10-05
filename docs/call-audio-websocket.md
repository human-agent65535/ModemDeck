# Call audio over WebSocket (version 1)

Real calls use `GET /api/v1/calls/{id}/media/ws`. iOS audio tests use
`GET /api/v1/mobile/call-tests/{id}/media/ws`. Both enter the same media runtime,
call lease, single media owner, and shared PCM hub. Hardware access remains
behind the Agent Unix socket. Existing WebRTC endpoints remain during rollout.

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
rejects backward/duplicate sequence and inconsistent timestamp advances. Queues
are bounded to at most 100 ms and stale frames are discarded; silence fills
capture gaps. A pause in microphone packets alone does not end the connection.
The server compares uplink sample timestamps with arrival time, so audio held in
TCP buffers does not become fresh when received. A changed latency is accepted
only after three packets resume normal 20 ms arrival cadence; queued bursts are
discarded during recovery.

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
Clients may ignore
unknown JSON fields. Server snapshots retain final counters after disconnection.

Reconnect through a fresh upgrade/start and fresh codec/queues. Release the old
owner through the existing media DELETE endpoint before replacing it, or wait
for socket closure; never replay buffered audio. Retry transient closure with
bounded backoff while the authoritative call is active and the lease remains
held. Keep the original retry deadline across early `ready` messages; clear it
only after useful duplex audio has stayed healthy for at least one second.
Reconnecting does not create a new call or recording.
