# Bounded Live Updater Probe

Generated: `2026-09-08T08:24:07.8891469Z`

Measurement: `bytes received from network` excludes the first 1 MiB, which was a synthetic zero-filled prefix. The 2 MiB cancellation bound is file size, not network bytes.

Verification: not_run: Range/transport probe only; synthetic resume contents were not verified; no digest/signature or installer verification

## mac

- URL: `https://orca.aichat.diy/releases/desktop-v3.0.3/O.R.C.A-macos-universal.dmg`
- Expected bytes: `18561854`
- Seeded prefix: first 1 MiB are synthetic zero bytes, not network data
- Bytes received from network this run: `1055815`
- Bytes on disk (synthetic prefix + network bytes): `2104391`
- Response: `206`
- First byte: `1023 ms`
- Duration: `6042 ms`
- Speed: `174810 B/s`
- Cancel: `2MiB file size (1MiB synthetic prefix)`
- Resume transport: range-request-accepted
- Resume contents verified: `false`
- Error: `context canceled`
- Full verification: `false`

## github

- URL: `https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.3/O.R.C.A-macos-universal.dmg`
- Expected bytes: `18561854`
- Seeded prefix: first 1 MiB are synthetic zero bytes, not network data
- Bytes received from network this run: `1053504`
- Bytes on disk (synthetic prefix + network bytes): `2102080`
- Response: `206`
- First byte: `1929 ms`
- Duration: `2261 ms`
- Speed: `466381 B/s`
- Cancel: `2MiB file size (1MiB synthetic prefix)`
- Resume transport: range-request-accepted
- Resume contents verified: `false`
- Error: `context canceled`
- Full verification: `false`
