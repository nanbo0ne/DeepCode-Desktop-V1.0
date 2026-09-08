# Windows Packaging Chain Review

Date: 2026-09-07
Scope: read-only inspection of the local Wails/NSIS chain and existing
artifacts. No `wails build`, installer execution, extraction to a destination,
installation, user-directory change, or final EXE generation was performed.

## Toolchain observed

- Wails: `v2.12.0` at `C:\Users\16260\go\bin\wails.exe`.
- NSIS: `v3.12` at `C:\Program Files (x86)\NSIS\makensis.exe`; it is not on
  the current PATH, but the explicit path works.
- 7-Zip: `24.08` at `C:\Program Files\7-Zip\7z.exe`; it is not on the
  current PATH, but the repository scripts recognize the Git-Bash equivalent
  `/c/Program Files/7-Zip/7z.exe`.
- Current shell Node: `C:\Program Files\nodejs\node.exe`, `v18.20.0`.
  No Node 22 executable was found in the inspected local/runtime roots. The
  Codex runtime Node is `v24.19.0`; it is not the release Node. The CI workflow
  explicitly installs Node `22`.
- The checked-in installer payload has root `node.exe` `v18.20.0` and
  `codegraph\node.exe` `v24.16.0`.

## Expected Wails/NSIS flow

`desktop/wails.json` currently declares output `Orca` and product version
`3.0.2`. `scripts/desktop-build.sh` runs from `desktop/`, prepares the Windows
payload, then invokes:

```powershell
Set-Location 'D:\AI-Reasonix\.tmp\v2.1.3-worktree\desktop'
& 'C:\Users\16260\go\bin\wails.exe' build -clean -platform windows/amd64 -ldflags '-X main.version=v3.0.2 -X main.channel=stable' -nsis -webview2 embed
```

The command above is the exact Windows shape used by the script; it was not
run during this review. Expected Wails outputs are under
`desktop/build/bin/`, including `Orca.exe` and an NSIS installer. The script
then copies a stable installer to `dist/`, verifies it when 7-Zip is found,
and creates the portable ZIP from the selected `build/bin/*.exe`.

The checked-in NSIS template writes the installer to
`desktop/build/bin/O.R.C.A-for-Windows-windows-${ARCH}-installer.exe`, embeds
the Wails app binary, adds root `node.exe`, and recursively embeds the complete
`codegraph` directory. It also embeds the WebView2 bootstrapper.

## Runtime and payload findings

The current payload is present and internally complete:

- `desktop/build/windows/installer-go/payload/node.exe`: 69,632,664 bytes,
  `v18.20.0`.
- `payload/codegraph`: 686 files, about 159,868,668 bytes.
- `payload/codegraph/node.exe`: 92,279,112 bytes, `v24.16.0`.
- `payload/codegraph/bin/codegraph.cmd` points to
  `%~dp0..\node.exe` and launches `lib\dist\bin\codegraph.js`.
- `payload/codegraph/lib/package.json` is CodeGraph `0.9.7`, matching the
  pinned `CODEGRAPH_VERSION` and the checked-in CodeGraph checksum table.

`internal/codegraph.Resolve()` searches the explicit config path, versioned
user cache, PATH, and finally the `codegraph` directory beside the executable.
The beside-executable fallback therefore requires this relocatable layout:

```text
Orca.exe
codegraph/
  node.exe
  bin/codegraph.cmd
  lib/package.json
  lib/dist/**
  lib/node_modules/**
```

The NSIS installer contains these entries, confirmed by archive listing. The
root `node.exe` is also present in the installer, but the CodeGraph launcher
uses `codegraph\node.exe`; no desktop source reference to the root Node binary
was found.

## Portable ZIP contract

The current `scripts/desktop-build.sh` intentionally stages only `Orca.exe`
before `Compress-Archive`. Existing portable ZIPs likewise contain exactly
`Orca.exe`.

That is sufficient only for a lightweight portable download whose CodeGraph
feature may use a pre-existing user cache or PATH installation. It is **not** a
fully offline portable package. For an offline-capable portable package, stage
`Orca.exe` plus the complete `codegraph/` tree shown above. Root `node.exe` is
not required for the CodeGraph launcher unless another feature later adopts it.
Do not silently add the installer-only WebView2 bootstrapper to the portable
ZIP; it is an installer dependency, not an app runtime file.

## Existing `dist` evidence

The normal root Windows artifacts are dated 2026-08-25:

- `dist/O.R.C.A-for-Windows-windows-amd64-installer.exe`: 81,529,222 bytes,
  SHA256 `27BE92506216BC3215BA3F3A0C1EA1FF601C2BA9231F8BFC7FDCD052EDC03F6B`.
- `dist/O.R.C.A-for-Windows-windows-amd64.zip`: 11,352,007 bytes,
  SHA256 `444B96F5CA4A18FD4933527A70F94EF169C7C92D4625A3AE5C5C4D780DCB7017`.
- The `DeepSeek-Orca` aliases have the same bytes and hashes.

The normal root installer and ZIP, plus historical `desktop-v3.0.0`,
`desktop-v3.0.0-local`, and `desktop-v3.0.1` Windows artifacts, passed local
7-Zip `t` checks. The normal installer listings include `Orca.exe`, root
`node.exe`, `codegraph\node.exe`, the CodeGraph launcher, and its package
metadata. The normal portable ZIP listing contains only `Orca.exe`.

`dist/desktop-v3.0.1/github-release-final` is not valid release evidence:
its Windows installer copies are only about 594 KB/606 KB instead of about
81.5 MB, its Windows ZIP is about 651 KB instead of about 11.35 MB, and 7-Zip
reported `Unexpected end of archive` or `Data Error` for every checked file in
that directory. Its files also do not match `SHA256SUMS-GITHUB.txt`.

The normal root installer Authenticode status is `NotSigned`. This is expected
for a local unsigned artifact only if it is not published; the release workflow
requires SignPath signing before publication when configured.

## Offline CRC/SHA verification

There is no standalone local CRC script. The available checks are:

- `scripts/desktop-build.sh`: optional `7z t` for the NSIS installer; it warns
  and continues when 7-Zip is absent.
- `.github/workflows/release-desktop.yml`: release verification requires 7-Zip
  and tests both the installer and portable ZIP with `7z t`.
- `Get-FileHash` or `certutil -hashfile` verifies SHA-256, not archive CRC.

Read-only commands for the parent build/review:

```powershell
$seven = 'C:\Program Files\7-Zip\7z.exe'
& $seven t 'D:\AI-Reasonix\.tmp\v2.1.3-worktree\dist\O.R.C.A-for-Windows-windows-amd64-installer.exe'
& $seven t 'D:\AI-Reasonix\.tmp\v2.1.3-worktree\dist\O.R.C.A-for-Windows-windows-amd64.zip'
& $seven l -ba 'D:\AI-Reasonix\.tmp\v2.1.3-worktree\dist\O.R.C.A-for-Windows-windows-amd64-installer.exe' 'Orca.exe' 'node.exe' 'codegraph\node.exe' 'codegraph\bin\codegraph.cmd'
Get-FileHash -Algorithm SHA256 'D:\AI-Reasonix\.tmp\v2.1.3-worktree\dist\O.R.C.A-for-Windows-windows-amd64-installer.exe'
Get-FileHash -Algorithm SHA256 'D:\AI-Reasonix\.tmp\v2.1.3-worktree\dist\O.R.C.A-for-Windows-windows-amd64.zip'
Get-AuthenticodeSignature 'D:\AI-Reasonix\.tmp\v2.1.3-worktree\dist\O.R.C.A-for-Windows-windows-amd64-installer.exe'
```

## Potential issues for the parent build

1. The current local Node is `18.20.0`, not the CI-required Node 22. The
   Windows payload was also copied from Node 18. The parent should use the
   intended Node 22 executable explicitly and verify the staged version before
   building.
2. `scripts/desktop-build.sh` chooses the first `node.exe`/`node` on PATH and
   only copies it when the payload file is absent. A stale payload can therefore
   survive a Node or CodeGraph version change.
3. The Windows CodeGraph download path uses `curl` plus `Expand-Archive` but
   does not compare the downloaded archive with the embedded CodeGraph SHA-256
   table before staging. This weakens offline bundle provenance.
4. `desktop/wails.json` hardcodes the frontend npm cache to
   `D:\AI-Reasonix\.npm-cache`, which is machine-specific and may not exist on
   another Windows runner.
5. `wails_tools.nsh` currently carries fallback product version `3.0.1` while
   `wails.json` and `info.json` are `3.0.2`. Wails should regenerate it during
   the build; a manual `makensis project.nsi` without regeneration can produce
   stale version metadata.
6. The optional local 7-Zip check in `desktop-build.sh` is fail-open, while the
   release workflow is fail-closed. For a local release candidate, run the
   explicit `7z t` commands above and require exit code 0.
7. Existing `github-release-final` files are truncated/corrupt and should not
   be used as release inputs. No cleanup was performed in this read-only review.
