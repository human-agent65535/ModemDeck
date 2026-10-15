# ModemDeck third-party notices

ModemDeck includes or is distributed with third-party software and data. This
notice is a practical index to the principal components shipped by the project;
the applicable license text and source package metadata remain authoritative.

ModemDeck itself is licensed under the PolyForm Noncommercial License 1.0.0.
See [LICENSE](LICENSE).

## Go API WebSocket transport

The Go API uses `github.com/gorilla/websocket` v1.5.3, distributed under the
BSD 2-Clause License. The license below is reproduced from the exact module's
`LICENSE` file ([upstream source](https://github.com/gorilla/websocket/blob/v1.5.3/LICENSE)).

```text
Copyright (c) 2013 The Gorilla WebSocket Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

  Redistributions of source code must retain the above copyright notice, this
  list of conditions and the following disclaimer.

  Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## Web application

- Vue.js, Vue Router, and Vue I18n — MIT License
- Lucide Vue Next — ISC License
- Feather Icons portions included by Lucide — MIT License

The installed package versions and dependency tree are recorded in
`web/package-lock.json`.

## Audio assets

The ringtone and notification assets under `web/src/assets/` are derived from
the Android Open Source Project and are used under the Apache License 2.0. See
[NOTICE.md](NOTICE.md) for source paths and attribution details.

## Operator data

The MCC/MNC operator table used by the host agent is distributed under the MIT
License. Its license text is preserved at
`agent/internal/operator/LICENSE.mcc-mnc-table`.

## QDC507 interoperability references

The QDC507 USB/ADB provisioning design was informed by the public
[DJOneHubNative](https://github.com/cr-zhichen/DJOneHubNative) implementation
at revision `7889978250218423bb29854dc412ab5636c73e50`. ModemDeck preserves
the modem's complete VID/PID and unrelated USB function bits, applies the
legacy authorization response only in memory, verifies both sides of the
controlled restart, and uses its own Linux/ModemManager integration.
DJOneHubNative is distributed under the PolyForm Noncommercial License 1.0.0.

Legacy `QADBKEY` interoperability and test vectors were also informed by the
GPL-3.0-licensed
[`carp4/qadbkey-unlock`](https://github.com/carp4/qadbkey-unlock/tree/cab52a0a7429c8d8b8f31da8894c8c93155c0fc5)
reference. ModemDeck does not bundle or execute that script; its Go
implementation derives the documented legacy MD5-crypt response locally and
does not persist the challenge or response.

## Optional QDC507 voice runtime

The QDC507 module voice runtime is not stored in this repository or baked into
the hardware image. A private deployment may provide the reviewed files from
the MIT-licensed [MaVo](https://github.com/moluncn/mavo) project at exact
revision `0443dfdaf8aec086fd76ba2ee9152fd908114524`, directory
[`Resources/ModuleVoice`](https://github.com/moluncn/mavo/tree/0443dfdaf8aec086fd76ba2ee9152fd908114524/Resources/ModuleVoice).
The Agent accepts only the compiled-in file sizes and SHA-256 hashes.

The `mavo-pcm-bridge.armv7` helper is part of the MIT-licensed MaVo project.
The two optional kernel objects expose `MODULE_LICENSE("GPL v2")` metadata,
and the runtime directory includes `COPYING-GPL-2.0` and
`MODULE-REPORT.md`. Deployments that provide these files must retain the
corresponding license and notice material.

## Runtime and hardware image

The application and hardware images include Go, ModemManager, Alpine Linux,
Debian, and their transitive system libraries and utilities. Their individual
licenses and copyright notices are provided by the corresponding upstream
projects and by the package metadata installed in each image.

This file does not replace any third-party license or notice. When redistributing
a ModemDeck image, retain this file together with the license and notice files
provided by its packaged dependencies.

## Ogg recording container

`internal/recording/ogg.go` adapts the single-track Ogg page framing and Opus
granule counting from `github.com/pion/webrtc/v4` v4.2.18,
[`pkg/media/oggwriter/oggwriter.go`](https://github.com/pion/webrtc/blob/v4.2.18/pkg/media/oggwriter/oggwriter.go).
It provides file framing only; ModemDeck does not ship the Pion realtime
transport stack. The upstream MIT license is reproduced below and preserved in
`internal/recording/LICENSE.pion-ogg`.

```text
MIT License

Copyright (c) 2026 The Pion community <https://pion.ly>

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

## Shared call audio core

Native iOS, Web and the Go API use the same pinned WebRTC NetEq, Abseil and Opus
source distribution. NetEq supplies buffering, concealment and time stretching;
only WSS is used for call transport. Exact revisions and source hashes are in
`third_party/audio/upstream.json`. The complete license texts, patent grant and
DSP dependency notices are preserved in `third_party/audio/LICENSES.txt` and
packaged as `audio-LICENSES.txt` in distributed images and `AudioCoreNotices.txt`
in the iOS app. See `third_party/audio/README.md` for build provenance.
