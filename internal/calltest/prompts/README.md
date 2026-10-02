# Call test prompts

These synthetic prompts contain no user recordings. They are embedded in the
server and played through the same Opus/PCM endpoint as the test tone and echo.
Format: signed 16-bit little-endian PCM, 16 kHz, mono, padded to 20 ms frames.

Generated with eSpeak NG 1.51 (Debian bookworm), then converted with FFmpeg:

```sh
espeak-ng -v cmn -s 165 -w zh-guide.wav '提示音后，请说几句话。'
espeak-ng -v cmn -s 165 -w zh-playback.wav '现在回放你的声音。'
espeak-ng -v en-us -s 160 -w en-guide.wav 'After the beep, say a few words.'
espeak-ng -v en-us -s 160 -w en-playback.wav 'Here is your recording.'
ffmpeg -i INPUT.wav -ac 1 -ar 16000 -af volume=0.65 -f s16le OUTPUT.pcm
```

Pad the PCM with zeros to a multiple of 640 bytes. The eSpeak executable and
voice data are development tools, not runtime dependencies or bundled assets.
