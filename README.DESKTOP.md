# O.R.C.A. Desktop

O.R.C.A. Desktop is the graphical workspace for **Open Reasoning & Computing Agent**. It provides Assistant and Coding conversations, the fixed Orca control entry, multi-provider models, tools, files, projects, sessions, memory, MCP, Skills, artifacts, automation, and inspectable execution history.

The complete product overview, platform matrix, privacy model, migration notes, and source-build instructions are maintained in [README.md](README.md) and [README.en.md](README.en.md).

## O.R.C.A. v3.0.2

- [Windows installer](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-for-Windows-windows-amd64-installer.exe)
- [Windows portable ZIP](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-for-Windows-windows-amd64.zip)
- [macOS universal DMG](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-macos-universal.dmg)
- [Linux amd64 DEB](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-linux-amd64.deb)
- [Release page and checksums](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.2)

Windows is the full-capability platform for V3, including the managed local `llama.cpp` runtime and Computer Use. macOS and Linux continue to provide cloud-model conversations and the shared agent workspace, while those two Windows-native capabilities are shown as unavailable.

## Local Build

```powershell
cd desktop\frontend
npm install
npm run test:all
npm run build

cd ..
go test .
wails build
```

For a Windows NSIS package, install NSIS and run the repository packaging script from a Git Bash environment:

```bash
scripts/desktop-build.sh windows/amd64 v3.0.2 stable
```

Release packaging is defined by [`.github/workflows/release-desktop.yml`](.github/workflows/release-desktop.yml). A stable `desktop-v*` tag builds Windows, macOS, and Linux artifacts, verifies package integrity, publishes the GitHub Release, and updates the configured release mirror.
