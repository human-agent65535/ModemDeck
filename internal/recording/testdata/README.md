# Pinned Ogg compatibility reference

`ogg_upstream_v4.2.18.json` was generated using the unchanged
`github.com/pion/webrtc/v4@v4.2.18/pkg/media/oggwriter` with its default mono
headers and non-seekable output, exactly as the previous recording writer.
The final page EOS flag was set by the existing recording finalization rule.
Random serial bytes and CRC bytes are zeroed for comparison; the test separately
verifies real page CRCs and stream continuity. Four vectors contain production
libopus 440 Hz audio at 8/16 kHz and 10/20 ms; the fifth covers 254/255/256,
510 and 1275 byte packet lacing. No user audio or transport credentials appear.
The source and MIT license are recorded in ../ogg.go and ../LICENSE.pion-ogg.
