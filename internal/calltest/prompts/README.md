# Call test prompts

These synthetic prompts contain no user recordings. They are embedded in the
server and played through the same Opus/PCM endpoint as the test tone and echo.
Format: signed 16-bit little-endian PCM, 16 kHz, mono, padded to 20 ms frames.

Generated locally with [Kokoro 82M v1.1-zh](https://huggingface.co/hexgrad/Kokoro-82M-v1.1-zh)
(Apache-2.0), Chinese voice `zf_001` and English voice `af_maple`, at speed 0.95.
The model and voice revisions, hashes, exact text, duration, and output levels
are recorded in `manifest.json`. No model, voice embedding, user audio or TTS
runtime is included in the server; only these four synthetic PCM prompts ship.

To regenerate in an isolated Python 3.12 environment:

```sh
python -m pip install 'kokoro==0.9.4' 'misaki[zh]==0.9.4' \
  'transformers==4.49.0' 'tokenizers==0.21.4' soundfile scipy
python -m spacy download en_core_web_sm
python scripts/generate-call-test-prompts.py --work-dir dist/call-test-prompts
```

Review the generated WAV previews before replacing the PCM and manifest here.
The script verifies the model SHA-256, resamples 24 to 16 kHz, normalizes active
speech to -20 dBFS with a -3 dBFS peak ceiling, and adds 300 ms leading/200 ms
trailing silence before padding to 640-byte frames. Hardware and library
versions can produce small floating-point differences; the manifest hashes the
actual shipped bytes. These clips were independently transcribed to confirm
their text, and checked for frame alignment and clipping. Physical receiver and
speaker intelligibility still requires device testing.
