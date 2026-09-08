**English** | [简体中文](README.md)

# O.R.C.A. 3.0.3

**O.R.C.A.** (**Open Reasoning & Computing Agent**) is an open-source workspace for real work. It brings multi-model conversations, Assistant, Coding, research, files and images, engineering tools, memory, automation, and optional local AI into one pausable, inspectable, recoverable application.

> **Computer Use is temporarily disabled on all platforms in 3.0.3.** Windows, macOS, and Linux register no computer-control tools, take no screen captures, and send no native mouse or keyboard input through this feature. Its code and configuration remain for restoration after validation in a later release. Ordinary attachments and default Vision image analysis remain supported. See the [3.0.3 validation record](docs/audits/2026-09-08-v3.0.3-validation.md) for details.

## Downloads

Use the [Release page assets](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.3) as the source for downloads, integrity information, and available platform packages.

| Platform | Package | Notes |
| --- | --- | --- |
| Windows x64 | [Installer](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.3/O.R.C.A-for-Windows-windows-amd64-installer.exe) · [Portable ZIP](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.3/O.R.C.A-for-Windows-windows-amd64.zip) | Cloud features and optional local AI; Computer Use temporarily disabled |
| macOS 12+ Universal | [DMG](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.3/O.R.C.A-macos-universal.dmg) | Intel and Apple Silicon; local AI is platform-limited, Computer Use temporarily disabled |
| Linux x64 | [DEB](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.3/O.R.C.A-linux-amd64.deb) | Debian / Ubuntu; requires a matching WebKitGTK runtime; Computer Use temporarily disabled |

- [Open the 3.0.3 release page, checksums, and final notes](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.3)
- Check the version, platform, and filename when downloading.
- Never commit an API key to documentation; official packages ship with no provider key.

## Three Work Modes

| Mode | Best for | Default boundary |
| --- | --- | --- |
| **Orca** | Cross-session coordination, bot channels, and a persistent entry point | Owns dispatch, waiting, and automation; it is not a replacement for ordinary text subagents |
| **Assistant** | Questions, research, writing, information organization, office files, and everyday work | Uses web/local tools, Assistant memory, plans, automation, and artifact tools |
| **Coding** | Repository development, debugging, refactoring, testing, and review | Uses Shell, files, Git, LSP, CodeGraph, checkpoints, and engineering verification |

Modes are stored per session. Switching preserves visible history and rebuilds the prompt, tool, and memory boundary for the target mode after the active turn finishes or stops. Orca is the fixed top-level control entry; ordinary sessions still choose Assistant or Coding.

## Features

### Providers, Models, and Roles

- Built-in provider presets cover OpenAI, Anthropic, OpenRouter, DeepSeek, DashScope, Zhipu, Kimi, MiniMax, Volcano Ark, Baidu Qianfan, Tencent Hunyuan, StepFun, Xiaomi MiMo, SiliconFlow, and others. Custom OpenAI-compatible and Anthropic-compatible services are supported. Available presets depend on the build and configuration.
- Provider IDs, base URLs, credential slots, and fully qualified `provider/model` references are isolated. Same-name models do not silently cross-resolve to another endpoint.
- The main conversation, planner, subagent, and Orca/automation roles can be selected independently. Computer Use role configuration is retained but does not run in this version. A role is not a guarantee of model capability: context, pricing, tool calling, and vision still depend on the actual model and its checks.
- **3.0.3 vision default:** for an *unconfigured* vision or control role, the default candidate is the official `deepseek/deepseek-v4-flash-vision-exp`. Ordinary image attachments still use vision routing; the retained control-role default does not enable Computer Use. Text-subagent defaults and explicit role choices remain unchanged. Your own credentials and an available endpoint are required; no key is bundled.
- Text subagents are not silently redirected to the vision model. Explicit role configuration wins; missing credentials or failed capability checks should produce a visible reason and a configuration path, not a false success.
- When pricing, balance, or context metadata is unreliable, the UI hides it or marks it unknown instead of displaying a misleading zero. Provider data retention, billing, and training policies remain provider-specific.

### Workspaces, Sessions, and Context

- Project workspaces bind to real directories; independent workspaces cover tasks that do not need a repository. Each session has boundaries for history, attachments, workspace references, and tool cwd.
- Multiple tabs, pinning, renaming, branch/fork operations, history, recycle bin, export, and resume are supported. Checkpoints, rollback, and rewind restore related state only when its scope is known; they cannot undo side effects in external systems.
- Long sessions can compact context, preserve compaction archives, and search older local sessions. Compaction is not a complete backup; write important facts to project files or explicit memory.
- Turns, items, tool results, and final answers are stored separately. Successful turns may fold; failed, cancelled, interrupted, and denied turns retain diagnostics.
- Background work, model loading, downloads, and approvals have separate states. Starting a background subagent does not mean the current work is complete; dependent work must wait and inspect its result.

### Images, Files, and Artifacts

- Paste, drop, or reference PNG, JPEG, WebP, GIF, and common document, spreadsheet, presentation, and text formats. In-workspace files can be referenced in place; out-of-workspace files are copied into the current workspace attachment area.
- Images are current-turn input by default and are not written into session JSONL, titles, memory, or compaction text. They are still sent to the enabled vision provider. Size, count, format, and model capability limit availability.
- For example, attach an image you choose and ask, "Extract the table in this image as CSV," or attach a document for a summary. Disabling Computer Use does not affect ordinary attachment processing; the app does not capture the screen for these tasks.
- `artifact_create`, `artifact_edit`, `artifact_preview`, and `artifact_validate` cover DOCX, XLSX, PPTX, and PDF. Artifacts carry a structured sidecar for follow-up edits and integrity checks.
- Real previews require a local renderer. Missing dependencies produce an explicit error rather than a placeholder image. Structural validation is not a guarantee of layout, fonts, formulas, pagination, or every page. Complex third-party Office files may be refused for editing; the original is not overwritten.

### Tools, Engineering, and Subagents

- Tools include file operations, search, Shell, Git, tests, builds, package managers, and browser/network capabilities as enabled by the tool library and provider configuration. Tool groups can be disabled.
- Coding mode emphasizes LSP, CodeGraph, code review, security checks, Plan, Todo, Goal, checkpoints, and verification after writes. Tool output is evidence, not an automatic test-pass claim.
- Subagents run focused research, analysis, vision, or engineering work in separate sessions and return a final result to the parent. Saved subagent transcripts can be waited on, continued, or forked when their tool scope, model, effort, workspace, and parent session match.
- Subagent/Skill meta-tools do not recurse without bound. Keep references and failure reasons, and never turn “started” into “completed.”

### MCP, Skills, Bots, and Automation

- MCP supports stdio and Streamable HTTP. The manager shows servers, authorization, connection state, failures, retries, and exposed tools. Keep secrets in environment variables or local credentials, not project files.
- Skills are discovered as `SKILL.md` or named Markdown playbooks from built-in, global, project, or custom roots. The slash menu exposes commands, Skills, and MCP prompts. Disabling a Skill hides it from prompts and invocation without deleting its files.
- Orca can list, read, dispatch, wait for, inspect, or stop ordinary session tasks; it does not recursively dispatch itself. QQ, Weixin, Feishu, and other bot channels depend on their account, configuration, and network.
- Bot, automation, and personal-profile data use explicit configuration and isolated workspaces. Continuous monitoring or scheduled work keeps approval, failure, and notification boundaries; a chat does not silently create an ordinary sidebar session.

### Local AI

- Windows can optionally install an O.R.C.A.-managed pinned `llama.cpp` runtime and local models. It does not modify or take over LM Studio.
- The manager inspects GPUs, VRAM, memory, and disk and may choose CUDA, Vulkan, or CPU fallback. A recommendation is not a guarantee; quantization, context, and drivers still change results.
- Model downloads support queues, pause, resume/range continuation, mirrors, speed/ETA, and SHA-256 verification. A completed download does not prove that the model runs reliably.
- Local models can be assigned to the main role, planner, subagent, or Orca. Existing Computer Use role references are retained but do not run. Resident resources are limited, and deleting a model still referenced by a role is refused. The runtime binds to loopback and uses an ephemeral authorization token.
- Local-AI management is unavailable or not fully validated on macOS/Linux in this release; cloud providers remain available there.

### Computer Use: Temporarily Disabled

3.0.3 temporarily disables Computer Use on **Windows, macOS, and Linux**. It registers no computer-control tools, captures no screens, and performs no native mouse, keyboard, or window-control actions. Orca, automation, bots, and subtasks cannot start this feature.

Its code and configuration remain for restoration after validation in a later version. Existing consent, Full access, and control-model settings cannot enable it in this release. Default Vision, images you explicitly provide, and ordinary file attachments remain supported. Ask the assistant to analyze attachments or organize files; desktop operation is unavailable.

Earlier native test records remain part of the acceptance requirements for restoring the feature. See the validation record for details.

### Permissions and Privacy

- Ask, automatic review, and Full access are distinct strategies. Host deny rules, workspace write boundaries, network proxy, and tool-library switches take precedence over model requests.
- Automatic review may use a separate model request for risk classification, but that request should not carry full history, tool output, images, or secrets. Classification failure should fall back to human handling with an auditable summary.
- API keys stay in local credentials/environment variables. They are not written into chat messages, titles, memory, release packages, or this repository. Check each provider's retention, training, region, and billing policies before connecting it.
- Sessions, configuration, credentials, attachments, cache, logs, memory, local models, and download tasks are stored separately for backup and cleanup. Sharing, bots, and MCP servers may send data to additional third-party endpoints.

### Modern and Classic

- **Modern** is the default lightweight interface with compact menus, a timeline, a single-row Composer, model/effort controls, and responsive layout.
- **Classic** retains the V2.1.3 blue-and-white layout, native window decoration, and control arrangement while using the V3 session, provider, tool, and permission services.
- The style choice is persisted. Windows Modern owns its title bar; Classic uses the native frame and normally needs a restart to switch the shell fully. The window follows system DPI; it cannot repair every WebView or driver issue.

## Platform Limits

| Capability | Windows | macOS | Linux |
| --- | :---: | :---: | :---: |
| Cloud providers, sessions, Assistant/Coding/Orca, files, MCP, Skills, memory | Available | Available | Available |
| Windows `llama.cpp` management | Target platform | Unavailable/not validated | Unavailable/not validated |
| Computer Use | Temporarily disabled | Temporarily disabled | Temporarily disabled |
| Modern / Classic | Both shells | Native platform window | Native platform window |

Windows requires WebView2; macOS uses system WebKit; Linux requires GTK/WebKitGTK runtime libraries. Distribution and GPU-driver differences can cause blank windows, flicker, or font problems. Platform packages and native installation remain subject to the validation record.

## Install and Update

### Install

1. On Windows, choose the x64 installer from the Release page. Back up user data before upgrading; extract the portable ZIP to a user-writable directory. Configure a provider on first run or skip setup; no API key is bundled.
2. On macOS, open the Universal DMG and drag the app to Applications. If signing or notarization status requires Gatekeeper confirmation, follow the system prompt; do not download an untrusted bypass script.
3. On Linux, install the DEB after preparing matching WebKitGTK/GTK dependencies. Use the system package manager or a source build when the distribution lacks the required library.
4. Compare the package with the signature, SHA-256, and manifest from the Release page.

### Updating to 3.0.3

- The stable channel first checks [`https://orca.aichat.diy/updates/stable/latest.json`](https://orca.aichat.diy/updates/stable/latest.json). If it fails, it falls back to GitHub's signed manifest and matching signed payload. Manifest, payload, version, and SHA-256 must agree; a failed signature must never be installed.
- An installed Windows build offers **explicit download -> user confirms exit -> run the installer**. It never downloads or installs in the background. The app should save sessions and drafts before exit and leave the existing installation in place if the operation fails.
- macOS/Linux show the available version and integrity details and open the matching download page/package. Cross-platform in-place updating is not presented as complete. The macOS check uses the URL above as its primary source, with GitHub as fallback.
- Users on older versions must manually download and run the installer/package from the Release page for a one-time 3.0.3 bootstrap. Do not edit the config version or replace credential files by hand to force an upgrade.

## Configuration and Migration

- Current project configuration is `orca.toml`; project state is `.orca/`; project instructions are `ORCA.md`; environment variables use the `ORCA_` prefix. Legacy V2 paths and environment variables remain readable for migration; new content uses the names above.
- User configuration is normally `os.UserConfigDir()/orca/config.toml`; the same root holds `credentials`, `sessions`, `archive`, `cache`, and memory data. On Windows this resolves through the system `AppData`, commonly `%APPDATA%\\orca\\`. Local models and runtimes use a separate local data root; use Settings for the actual path.
- Configuration merges in priority order: explicit flags/parameters, project `orca.toml`, user configuration, and built-in defaults. Project `.mcp.json` can also provide MCP. Workspaces resolve their own config, `.env`, MCP, and sessions.
- Before migration, close the app and back up the `orca` and legacy V2 user roots, plus project config, `.orca/`, attachments, and instruction files. Migration should preserve sessions, attachments, provider references, credentials references, memory, Skills, MCP, bots, and telemetry; keep the old root as rollback material.
- Migration cannot undo external side effects or infer one provider's key for another. Explicit role/model settings take precedence over the new vision default. Computer Use code and configuration remain; migration does not re-enable the feature. On failure, fall back to compatibility reads and the backup; do not delete the source files.

## Build From Source

You need Go, Node.js, npm, and Wails CLI v2. Linux also needs GTK/WebKitGTK; building the Windows installer needs NSIS. The desktop module has its own dependencies and frontend build:

```powershell
cd desktop\frontend
npm install
npm run build

cd ..\..
go test ./...
cd desktop
go test .
wails build
```

Run `wails dev` in `desktop` for development. Running `npm run dev` alone uses a browser mock and cannot prove Wails bindings, native file drops, window framing, local AI, or Computer Use. A release build must inject its official version and regenerate/check frontend artifacts and signatures before upload.

## Troubleshooting

| Symptom | Check first |
| --- | --- |
| No model or request failure | Provider access, complete `provider/model`, local credentials, and proxy reachability; for vision, check the model capability result |
| Image rejected | Format/size/count, vision mode, current role, and model capability; text subagents do not gain vision automatically |
| Artifact generated but preview fails | Renderer dependency or unverified layout; structural validation is not visual acceptance; check Poppler and the target Office reader |
| Tool blocked | Ask/Auto/Full access, deny rules, workspace path, sandbox, and tool-library switches; do not disable security boundaries to resolve an unknown error |
| Local model fails to load | GPU driver, VRAM/RAM/disk, model integrity, runtime state, and loopback port; preserve failure logs before retrying a download |
| Computer Use is missing or requests are refused | It is temporarily disabled on all platforms in 3.0.3; consent or model settings cannot enable it. Ordinary Vision image analysis and file attachments remain supported |
| Blank window or frame issue | WebView2/WebKitGTK/GTK versions, GPU driver, and Modern/Classic selection; restart and collect logs before classifying it as a native defect |
| Updater does nothing | Primary manifest, GitHub fallback, signature/version fields, and system proxy; on Windows manually download and exit to install rather than expecting a background install |

See the [3.0.3 validation record](docs/audits/2026-09-08-v3.0.3-validation.md), [desktop/README.md](desktop/README.md), and [artifact runtime boundary](docs/ARTIFACT_RUNTIME.md) for focused details.

## License

O.R.C.A. is released under the [MIT License](LICENSE). Wails, `llama.cpp`, WebKit/GTK, provider SDKs, and other third-party components retain their own licenses and notices.
