# Desktop Download Diagnosis

Date: 2026-09-08 (Asia/Shanghai)

## Scope and Method

This was a read-only investigation from `macdeMac-mini` (Darwin 21.6.0), reached through the existing `aichat-home` SSH alias with these overrides:

`-o BatchMode=yes -o ConnectTimeout=12 -o ProxyCommand=none -o HostName=192.168.31.239 -o HostKeyAlias=ssh.aichat.diy`

The measured object was the published 3.0.3 macOS universal DMG:

`O.R.C.A-macos-universal.dmg`

The live stable manifest advertised version `v3.0.3`, size `18,561,854` bytes, and SHA-256 `a8a988d5fc590f3d33b6e96db8a4efc2385e73a9acee3445ff15a791a55c0cec` for this asset. Each transfer used `Accept-Encoding: identity`, one sequential `Range: bytes=0-4194303` request, `/dev/null` output, and a maximum duration of 20 seconds. The public transfer budget used was about 4.29 MB: 4,194,304 bytes from the public Mac source and 99,085 bytes from GitHub. Local transfers were not counted as public egress.

## Measurements

| Path | HTTP result | Range / length | First byte | Total / received | Effective speed | Cache and type | Outcome |
|---|---:|---|---:|---:|---:|---|---|
| Mac localhost, `http://127.0.0.1:8089/releases/...dmg` | 206 | `0-4194303/18561854`, 4,194,304 | 0.001175 s | 0.003322 s / 4,194,304 | 1,262,583,985 B/s | `Accept-Ranges`; `application/x-apple-diskimage`; ETag `"dl9jg75geix9b1uf2"`; immutable cache header | Complete bounded range |
| LAN, `http://192.168.31.239:8089/releases/...dmg` | 000 | No response | 0.000000 s | 0.001440 s / 0 | 0 B/s | No HTTP headers | Direct LAN access failed because the service is loopback-bound |
| LAN, `http://192.168.31.239:8088/releases/...dmg` with `Host: orca.aichat.diy` | 206 | `0-11623/11624`, 11,624 | 0.002334 s | 0.002450 s / 11,624 | 4,744,489 B/s | `Accept-Ranges`; `text/html; charset=utf-8`; ETag `"dl89k12xhhdu8yw"` | Wrong content: aichat.diy homepage HTML, not the DMG |
| Public Mac, `https://orca.aichat.diy/releases/...dmg` | 206 | `0-4194303/18561854`, 4,194,304 | 2.018935 s | 7.261390 s / 4,194,304 | 577,617 B/s | `cf-cache-status: HIT`; `age: 18659`; `public, max-age=31536000, immutable`; DMG type | Complete bounded range |
| GitHub asset, canonical 3.0.3 release URL | 302 then 206 | Final `0-4194303/18561854`, 4,194,304 declared | 3.314328 s | 20.428012 s / 99,085 | 4,850 B/s | Final response had `Accept-Ranges`, `age: 54`, `x-cache: HIT, HIT`, `application/octet-stream` | Incomplete; curl timed out with code 28 |

The GitHub result includes one redirect to a signed release-asset URL. That redirect target is intentionally not reproduced here.

## Manifest and Signature Headers

The public Mac manifest endpoint returned HTTP 200 with `application/json`, 7,729 bytes, `cache-control: no-store`, and `cf-cache-status: DYNAMIC`. Its detached manifest signature endpoint returned HTTP 200 and 268 bytes with `no-store`. The DMG detached signature endpoint returned HTTP 200 and 284 bytes with `public, max-age=31536000, immutable`. These observations confirm endpoint availability and advertised metadata; this report does not claim an independent cryptographic verification of the live files.

## Readable Route Evidence

- Caddy was running as root with `/usr/local/aichat/bin/caddy run --config /usr/local/aichat/etc/caddy/Caddyfile --adapter caddyfile`.
- Cloudflared was running as root with `/usr/local/aichat/bin/cloudflared tunnel --config /usr/local/aichat/etc/cloudflared/config.yml run`.
- The Caddy admin endpoint `http://127.0.0.1:2019/config/` was readable and returned HTTP 200. Server `srv0` listened on `127.0.0.1:8089`, used root `/usr/local/aichat/srv/site/orca`, and had `/releases/*` and `/updates/*` branches using `file_server`.
- The Caddyfile was mode `600` and the Cloudflared config was not readable by the SSH user. `sudo -n` was attempted only for route-only inspection and was refused because a password is required. No password or credential document was read.
- Direct TLS probes to localhost/LAN port 443 failed. The public HTTPS path is therefore not the same as a directly reachable LAN TLS listener in this test.

## Diagnosis

1. The actual Mac-local origin on `127.0.0.1:8089` serves the correct DMG bytes and honors Range requests. It is not LAN-reachable because Caddy binds only to loopback.
2. Port `8088` is reachable on LAN but is a different site route. It returns the aichat.diy homepage for the asset path, so it must not be used as an updater source.
3. The public Mac source serves the correct asset and Range semantics through a Cloudflare cache hit, but this sample had about a 2.0 second first-byte delay and about 0.58 MB/s sustained transfer for the bounded range.
4. GitHub honored the requested range in headers but delivered only 99,085 bytes during the approximately 20 second window, about 4.85 KB/s. This is a severe slow/incomplete fallback sample, not proof that every GitHub request is permanently slow.
5. The observations support keeping source selection and sequential fallback in the updater. They do not support unbounded parallel races or automatic source switching. The existing low-throughput suggestion threshold of 128 KiB/s sustained for 30 seconds is a client-side policy; no client UI behavior was verified here.

## Verification Boundary

No server files, Caddy/Cloudflared configuration, releases, deployment state, tags, secrets, or user configuration were changed. No claim is made that the desktop client UI displays or acts on these measurements; this was a network/origin investigation only.
