# ModemDeck third-party notices

ModemDeck includes or is distributed with third-party software and data. This
notice is a practical index to the principal components shipped by the project;
the applicable license text and source package metadata remain authoritative.

ModemDeck itself is licensed under the PolyForm Noncommercial License 1.0.0.
See [LICENSE](LICENSE).

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
