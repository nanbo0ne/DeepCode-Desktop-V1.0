[简体中文](README.md) | **English**

# O.R.C.A. for Windows

**O.R.C.A.** stands for **Open Reasoning & Computing Agent**. It is an open-source agent workspace for real work, bringing multi-model conversations, software engineering, research and documents, computer control, local models, memory, and automation into one inspectable, pausable, recoverable application.

O.R.C.A. is not tied to one model or one conversation style. Ordinary sessions can use **Assistant mode** or **Coding mode**, while the fixed **Orca** conversation coordinates cross-session work, remote channels, and computer-control tasks.

## Downloads

| Platform | Package | Notes |
| --- | --- | --- |
| Windows x64 | [Installer](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-for-Windows-windows-amd64-installer.exe) · [Portable ZIP](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-for-Windows-windows-amd64.zip) | Full local-AI and Computer Use support |
| macOS | [Universal DMG](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-macos-universal.dmg) | Intel and Apple Silicon |
| Linux x64 | [DEB](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-linux-amd64.deb) | Debian and Ubuntu |

[View every O.R.C.A. v3.0.2 asset, checksum, and release note](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.2)

[Validation record and remaining coverage limits](docs/audits/2026-09-07-real-usage-release.md)

The Windows installer supports in-place upgrades, Start Menu and optional desktop shortcuts, and an optional post-install launch. Uninstalling keeps user data and local models by default. The app reports available updates but never downloads or installs one without user action.

## Product Surfaces

| Surface | Best for | Main capabilities |
| --- | --- | --- |
| Assistant mode | Questions, research, writing, information organization, office files, and everyday tasks | Web and local tools, personal memory, artifact creation, planning, and automation |
| Coding mode | Repository development, debugging, refactoring, testing, and review | Shell, Git, LSP, CodeGraph, checkpoints, rollback, and engineering verification |
| Orca | Cross-session coordination, remote channels, desktop tasks, and a persistent control entry | Session dispatch, QQ/Weixin bridges, Computer Use, shared automation model, and personal profile |
| CLI | Terminal chat, one-shot tasks, and scripted workflows | The same provider, tool, MCP, skill, memory, and session core as the desktop app |

The selected mode is stored per session. Switching preserves visible history and rebuilds the prompt, tool, and memory boundary for the target profile. An active turn finishes or stops before the switch is applied.

## Feature Map

### Models and Providers

- Built-in presets for OpenAI, Anthropic, OpenRouter, DeepSeek, DashScope, Zhipu, Kimi, MiniMax, Volcano Ark, Baidu Qianfan, Tencent Hunyuan, StepFun, Xiaomi MiMo, SiliconFlow, and isolated subscription-plan endpoints.
- Custom OpenAI-compatible and Anthropic-compatible providers.
- Separate provider IDs, base URLs, credential slots, and fully qualified model references prevent same-name models from crossing endpoints.
- First launch offers a fast DeepSeek-key path, with alternatives for other providers, local AI, or skipping setup entirely.
- Independent choices for the default, automation, planner, subagent, and Computer Use models.
- Context capacity is resolved per model. Cost and balance disappear when reliable metadata is unavailable instead of showing misleading zero values.
- Official DeepSeek requests freeze a peak/off-peak price snapshot when the request starts; persisted historical cost is never repriced later.

### Workspaces, Sessions, and Context

- Project workspaces bind to real directories; independent workspaces cover tasks that do not need a repository.
- Multiple tabs, pinning, renaming, forks, history, recycle bin, export, and session resume.
- Checkpoints record conversation and workspace state; rollback and rewind restore context or related file changes.
- Long sessions support compaction, older-session retrieval, and stable Turn/Item identities.
- Images, files, and workspace references are current-turn attachments; binary data is not embedded in the chat JSONL.
- Final answers remain separate from execution details. Successful turns can fold, while failed, cancelled, and interrupted turns retain diagnostic context.

### Tools and Engineering Execution

- Shell, file operations, search, Git, tests, builds, package managers, and common host actions.
- Coding mode adds LSP, CodeGraph, code review, security checks, and post-write verification.
- Plan, Todo, and Goal workflows make long-running work and completion conditions visible.
- Subagents can take research, analysis, vision, and engineering subtasks, then return results to the current session.
- Readiness checks distinguish real blockers from optional extra verification, avoiding mechanical retries after successful work.
- Background work, model loading, downloads, approvals, and Computer Use have distinct states rather than pretending everything is model reasoning.

### MCP, Skills, Bots, and Automation

- The MCP manager shows servers, authorization, connection state, errors, retries, and exposed tools.
- Skills are discovered from built-in, global, project, or custom roots and loaded on demand by command or model.
- The slash menu combines built-in commands, skills, and MCP prompts.
- Orca can selectively list, read, dispatch, wait for, or stop ordinary session tasks without recursively dispatching itself.
- QQ and Weixin can connect to the same Orca conversation, current context segment, and serialized task queue.
- Bot, automation, and personal-profile data use explicit configuration and do not create hidden ordinary sessions.

### Multimodal Input and Work Artifacts

- Paste, drop, or reference PNG, JPEG, WebP, GIF, and common document formats.
- Vision capability is tracked per model. Auto mode prefers a confirmed vision-capable current model or vision subagent.
- DeepSeek Vision Exp can handle screenshots, OCR, charts, and visual-agent tasks; text-only models are not mislabeled as image-capable.
- Built-in artifact tools create, structurally edit, and validate DOCX, XLSX, PPTX, and PDF files. Visual previews require a real renderer; missing dependencies produce an explicit error, not a placeholder pretending to be a document preview.
- Generated artifacts carry a structured sidecar for reliable follow-up edits. The app states its limits when a complex third-party file cannot be preserved safely.

### Windows Local AI

- Optionally install a pinned `llama.cpp` runtime managed by O.R.C.A.; LM Studio is not controlled or modified.
- Detect every GPU rather than assuming `GPU 0`, including NVIDIA, AMD, Intel integrated/discrete combinations, and CPU fallback.
- Select CUDA, Vulkan, or CPU packages from dedicated-memory budget, currently free VRAM, system memory, and disk capacity.
- Model downloads support queues, pause/resume, HTTP range continuation, mirrors, speed/ETA, and SHA-256 verification.
- Systems with about 16 GB of VRAM are offered Qwen3.8-27B IQ3_XXS first, with context, batch, and GPU layers adjusted to preserve headroom.
- Local models can serve as the main, subagent, or Computer Use model. One model stays resident at a time and may unload after an idle timeout.
- The runtime binds only to a random `127.0.0.1` port and uses an ephemeral authorization token. Removing the runtime does not delete model files.

### Windows Computer Use

- Assistant, Coding, and Orca can all initiate computer tasks. A control subagent handles short action loops; ambiguous work returns to the main model.
- Observations can combine screenshots, UI Automation elements, and window state. Every action is followed by a fresh observation and success check.
- Click, double-click, right-click, drag, scroll, key chords, Unicode input, and window operations are supported.
- After one-time full-access consent, policy-approved low/medium-risk actions may continue. High risk, explicit Ask/Deny rules, and host boundaries still require intervention.
- A full-screen blue edge, pointer halo, and click feedback indicate active control. `Esc` force-stops the session and releases held keys.
- O.R.C.A. does not cross the UAC secure desktop, handle lock screens, fill password fields, solve CAPTCHAs, or bypass high-integrity boundaries.
- Screenshots are memory-only by default and are excluded from provider history. Action telemetry excludes images and sensitive text.

### Permissions, Risk, and Privacy

- Ask, automatic review, and full-access strategies coexist with host deny rules and workspace write boundaries.
- Automatic review may use an independent model request for risk classification. It receives no history, tools, images, or secret fields. High-risk actions go to manual approval; classifier errors retain the existing automatic-approval fallback and emit warning telemetry.
- API keys stay in local credential configuration. Every provider preset has its own key slot, and credentials are not written into chat messages.
- Messages and relevant attachments are sent to the active model and any vision model or subagent used for the task. Risk review receives only a redacted operation summary. Roles can use different providers; review their privacy and billing terms before enabling them.
- Sessions, configuration, logs, cache, local models, and download tasks use separate storage so they can be backed up or removed independently.

### Interface and Accessibility

- **Modern** is the default focused workspace, with compact menus, a single-row composer, process timeline, and responsive layout.
- **Classic** restores the V2.1.3 blue-and-white layout, native window, and control placement while retaining the V3 backend.
- Style choice is persisted and changes the window shell after restart without rebuilding or losing sessions.
- Internal scaling stays at 100% and follows Windows Per-Monitor DPI. Keyboard focus, reduced motion, and narrow layouts are supported.

## Platform Support

| Capability | Windows | macOS | Linux |
| --- | :---: | :---: | :---: |
| Cloud models, sessions, tools, MCP, skills, memory | Yes | Yes | Yes |
| Coding, Assistant, and Orca | Yes | Yes | Yes |
| Managed local `llama.cpp` | Yes | Not yet | Not yet |
| Computer Use | Yes | Not yet | Not yet |
| Modern / Classic window shell | Yes | Native platform window | Native platform window |

## Configuration and Migration

V3 project configuration uses `orca.toml`, the `.orca/` project directory, `ORCA.md` instructions, and `ORCA_*` environment variables. On Windows, global configuration is `%APPDATA%\orca\config.toml`; credentials, sessions, and migration records also live under `%APPDATA%\orca\`. Managed local runtimes, models, and download state use `%LOCALAPPDATA%\O.R.C.A\`. Back up both locations and each project's `.orca/` directory.

When upgrading from V2, O.R.C.A. reads legacy configuration, sessions, attachments, providers, credentials, memory, skills, MCP, cost, and telemetry, then writes only to the new directory after an atomic migration. The old directory remains as a backup. Git-tracked legacy project instruction files are not renamed automatically. The current configuration schema is V11.

## Build From Source

You need Go, Node.js 22+, npm, and Wails CLI v2. Building the Windows installer also requires NSIS.

```powershell
git clone https://github.com/nanbo0ne/O.R.C.A-for-Windows.git
cd O.R.C.A-for-Windows\desktop\frontend
npm install
npm run test:all
npm run build

cd ..\..
go test ./...
cd desktop
go test .
wails build
```

See [README.CLI.md](README.CLI.md) for CLI commands, [README.DESKTOP.md](README.DESKTOP.md) for desktop packaging, [DESKTOP_CHANGELOG.md](DESKTOP_CHANGELOG.md) for release history, and [docs/ARTIFACT_RUNTIME.md](docs/ARTIFACT_RUNTIME.md) for the artifact runtime's scope.

## License

O.R.C.A. is released under the [MIT License](LICENSE). `llama.cpp`, Wails, and other third-party components retain their respective notices and licenses.
