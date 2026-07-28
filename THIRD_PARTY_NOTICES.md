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

## Runtime and hardware image

The application and hardware images include Go, ModemManager, Alpine Linux,
Debian, and their transitive system libraries and utilities. Their individual
licenses and copyright notices are provided by the corresponding upstream
projects and by the package metadata installed in each image.

This file does not replace any third-party license or notice. When redistributing
a ModemDeck image, retain this file together with the license and notice files
provided by its packaged dependencies.
