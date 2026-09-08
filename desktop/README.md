[English section](#orca-desktop-303-english) | **简体中文**

# O.R.C.A. Desktop 3.0.3

这是 O.R.C.A. Go 内核的 Wails 桌面壳。React + TypeScript 前端通过 Wails typed bindings 直接调用 `desktop/app.go`，Go 侧把 `control.Controller`、Provider、工具、MCP、Skill、会话、产物、本地 AI 和权限事件绑定到 WebView；没有额外的 HTTP hop。

**3.0.3 所有平台暂时禁用电脑操控。** Windows、macOS、Linux 均不注册电脑操控工具，不截取屏幕，不执行原生鼠标、键盘或窗口控制；代码和配置保留，待后续验收后恢复。默认 Vision 识图与普通附件仍支持。测试范围与结果见验证报告。

English follows the Chinese section.

## 目录与边界

| 路径 | 用途 |
| --- | --- |
| `desktop/app.go` | Wails 方法、会话标签、配置、附件和事件桥接 |
| `desktop/main.go` | 窗口、平台 WebView、菜单、拖放和嵌入前端 |
| `desktop/updater*.go` | 更新检查、签名校验和平台动作 |
| `desktop/computer_use*.go` | 保留的 Computer Use 会话、观察、授权和动作代码；本版本所有平台禁用 |
| `desktop/local_ai_app.go` | Windows 本地运行时、模型目录和下载状态 |
| `desktop/frontend/src/` | Modern/Classic UI、Composer、会话、设置、工具和右侧工作区 |
| `desktop/build/` | Wails、Linux、Windows 安装和发布所需源文件 |

`desktop/` 是独立 Go module，通过 `replace` 引用父目录内核。它与 CLI 的构建隔离，因为桌面端使用 CGO 和各平台 WebView；前端独立运行时的 mock 只用于布局开发，不证明原生能力。

## 本地开发

先安装 Go、Node.js、npm 和 Wails CLI v2：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
cd desktop
npm --prefix frontend install
wails dev
```

只调前端时：

```powershell
cd desktop\frontend
npm install
npm run dev
```

浏览器 mock 可以展示消息流、Markdown、工具卡片和部分布局，但没有 Wails bindings、原生文件拖放、窗口框架、GPU/WebView、Local AI 或 Computer Use。不要用它作为桌面验收。

## 构建

```powershell
cd desktop\frontend
npm install
npm run build

cd ..
go test .
wails build
```

从仓库根目录执行内核测试：

```powershell
go test ./...
```

Windows 安装器还需要 NSIS。Windows 使用 Edge WebView2 Runtime；macOS 使用系统 WebKit；Linux 需要 GTK 和 WebKitGTK 开发/运行库。发行版使用 WebKitGTK 4.1 时，按本机 Wails/发行版配置使用 `-tags webkit2_41`：

```sh
wails build -tags webkit2_41
wails dev -tags webkit2_41
```

`frontend/dist` 由构建生成。没有先执行前端构建时，Go embed 可能只有占位目录，窗口会白屏或缺少资源；这不是 Provider 或模型错误。

## 桌面产品行为

- Modern 是默认工作壳；Classic 保留 V2.1.3 蓝白布局和原生窗口框架。样式选择持久化，Windows 壳切换通常在重启后完全生效。
- Assistant、Coding 和固定 Orca 三种工作入口共享内核，但分别使用不同的提示词、工具和记忆边界。
- Provider、主模型、planner、subagent 和自动化角色分别解析完整 `provider/model` 引用。只有未配置的视觉/控制角色才默认候选官方 `deepseek/deepseek-v4-flash-vision-exp`；显式角色保留，文本 subagent 不跟随该默认值。保留的 Computer Use 控制角色不运行，普通 Vision 识图仍支持，发布包不含 API key。
- Computer Use 在 Windows、macOS、Linux 均暂时禁用。已有授权、配置、Full access、恢复会话及派发任务都不能重新启用它；代码和配置保留供后续验收恢复。
- Windows 本地 AI 是可选的 O.R.C.A 管理 `llama.cpp` 运行时和模型下载；macOS/Linux 仍不可用或未完成验证。它不接管 LM Studio。
- 图片和附件属于当前回合上下文；结构化 DOCX/XLSX/PPTX/PDF 产物使用 sidecar、重新解析和真实渲染器限制，详情见 [`../docs/ARTIFACT_RUNTIME.md`](../docs/ARTIFACT_RUNTIME.md)。
- 可用示例：手动附图并要求“识别表格并生成 CSV”，或引用项目文件要求审查；无需启动电脑操控。

## 更新与分发

3.0.3 stable updater 的更新方式如下：

1. 首先检查 [`https://orca.aichat.diy/updates/stable/latest.json`](https://orca.aichat.diy/updates/stable/latest.json)，失败时回退 GitHub 的签名 manifest 和签名 payload。
2. Windows 已安装版本只在用户明确操作后下载；下载完成后提示退出应用，再运行安装器。不得后台自动下载或自动安装。
3. macOS/Linux 展示版本与完整性信息并打开下载页/包；不承诺跨平台原位自更新。
4. 旧版本用户需手动安装一次 3.0.3；下载与校验文件以 [Release 页资产](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.3)为准。

签名校验必须先于落盘或替换安装。私钥只存在发布环境，不进入仓库、manifest 示例或文档。验证范围、历史失败和待验收项记录在 [3.0.3 验证报告](../docs/audits/2026-09-08-v3.0.3-validation.md)；已有单测和候选包校验不等于安装或发布验收完成。

## 平台排查

- Windows 白屏：确认 WebView2 Runtime、显卡驱动和 WebView2 GPU 设置；先用 Classic/Modern 对照，再收集日志。不要把浏览器 mock 的成功当成原生成功。
- Linux 白屏或闪烁：确认 GTK/WebKitGTK 版本和发行版包；必要时按平台文档使用 `WEBKIT_DISABLE_COMPOSITING_MODE=1` 做诊断。不同 GPU/发行版必须单独验证。
- macOS 首次打开：检查 DMG、签名/公证和系统 Gatekeeper 提示。不要把清除隔离属性当成正式签名替代方案。
- 拖放/剪贴板异常：确认 Wails 运行窗口、路径是否位于工作区、附件是否可读；浏览器 mock 没有同等的原生路径语义。
- Computer Use 不可用：3.0.3 所有平台暂时禁用，授权、旧配置或更换控制模型均不能开启；普通 Vision 识图和附件处理仍可用。
- 本地模型失败：查看运行时状态、模型 SHA-256、显存/内存/磁盘和 `127.0.0.1` 回环服务；保留下载与加载错误。
- 更新检查失败：分别检查主 manifest、GitHub 回退、系统代理、签名和版本字段。Windows 按“下载、退出、安装”的显式流程处理。

## 发布前检查

- 先构建前端，再运行 `go test .` 和需要的根模块测试；测试结果只能代表运行过的范围。
- 在目标 OS 上验证窗口、WebView、拖放、安装/升级、设置迁移、Provider 请求、附件、产物、权限和本地 AI；没有实机证据就标为未验证。
- 本版本新增门槛：全平台不注册电脑操控工具，直接调用、旧配置/授权、会话恢复、自动化和派发都无法绕过禁用，截图与原生输入调用次数为零；普通附件 Vision 回归仍通过。原生控制失败及真机 DPI/人工接管移至后续恢复验收，不能将这些自动测试算作真机通过。
- 生成并核对 manifest、payload、签名和 SHA-256，并与 Release 页文件逐项对应。
- 保留旧用户目录和会话备份；不要在构建或文档任务中修改服务器、网站、密钥、版本配置或无关目录。

---

# O.R.C.A. Desktop 3.0.3 (English)

This is the Wails desktop shell around the O.R.C.A. Go kernel. The React + TypeScript frontend calls `desktop/app.go` through typed Wails bindings. The Go side binds `control.Controller`, providers, tools, MCP, Skills, sessions, artifacts, local AI, and permission events to the WebView without an extra HTTP hop.

**Computer Use is temporarily disabled on all platforms in 3.0.3.** Windows, macOS, and Linux register no computer-control tools, capture no screens, and perform no native mouse, keyboard, or window-control actions. Code and configuration remain for restoration after later validation. Default Vision image analysis and ordinary attachments remain supported. See the validation report for test scope and results.

## Layout and Boundary

| Path | Purpose |
| --- | --- |
| `desktop/app.go` | Wails methods, session tabs, config, attachments, and event bridge |
| `desktop/main.go` | Window, platform WebView, menu, file drops, and embedded frontend |
| `desktop/updater*.go` | Update checks, signature verification, and platform actions |
| `desktop/computer_use*.go` | Retained Computer Use session, observation, consent, and action code; disabled on all platforms in this version |
| `desktop/local_ai_app.go` | Windows local runtime, model directory, and download state |
| `desktop/frontend/src/` | Modern/Classic UI, Composer, sessions, settings, tools, and workspace dock |
| `desktop/build/` | Wails, Linux, Windows installer, and release source files |

`desktop/` is a nested Go module that uses `replace` to import the kernel from the parent directory. It is separate from the CLI build because the desktop target uses CGO and the native WebView on each OS. The standalone frontend mock is for layout work only and does not prove native behavior.

## Local Development

Install Go, Node.js, npm, and Wails CLI v2:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
cd desktop
npm --prefix frontend install
wails dev
```

For frontend-only work:

```powershell
cd desktop\frontend
npm install
npm run dev
```

The browser mock can show message streaming, Markdown, tool cards, and parts of the layout, but it has no Wails bindings, native file-drop semantics, window frame, GPU/WebView, Local AI, or Computer Use. Do not use it as desktop acceptance.

## Build

```powershell
cd desktop\frontend
npm install
npm run build

cd ..
go test .
wails build
```

Run kernel tests from the repository root:

```powershell
go test ./...
```

The Windows installer also needs NSIS. Windows uses Edge WebView2 Runtime; macOS uses system WebKit; Linux needs GTK and WebKitGTK development/runtime libraries. On distributions using WebKitGTK 4.1, use the local Wails/distribution setting and, where required, `-tags webkit2_41`:

```sh
wails build -tags webkit2_41
wails dev -tags webkit2_41
```

`frontend/dist` is generated by the build. Without a frontend build first, Go embed may contain only the placeholder directory and the window may be blank or missing resources; that is not a provider or model failure.

## Desktop Product Behavior

- Modern is the default work shell; Classic retains the V2.1.3 blue-and-white layout and native frame. The style choice persists, and the Windows shell normally switches fully after restart.
- Assistant, Coding, and the fixed Orca entry share the kernel but use distinct prompt, tool, and memory boundaries.
- Providers, main model, planner, subagent, and automation roles resolve independent fully qualified `provider/model` references. Only an unconfigured vision/control role defaults to the official `deepseek/deepseek-v4-flash-vision-exp`; explicit roles are preserved and text subagents do not inherit it. The retained Computer Use role does not run. Ordinary Vision image analysis remains supported, and no API key ships in the package.
- Computer Use is temporarily disabled on Windows, macOS, and Linux. Existing consent, configuration, Full access, restored sessions, and dispatched tasks cannot re-enable it. Code and configuration remain for restoration after later validation.
- Windows optionally manages a pinned `llama.cpp` runtime and model downloads; macOS/Linux remain unavailable or not fully validated for this feature. LM Studio is not controlled.
- Images and attachments are current-turn context. Structured DOCX/XLSX/PPTX/PDF artifacts use sidecars, reparse checks, and real renderer limits; see [`../docs/ARTIFACT_RUNTIME.md`](../docs/ARTIFACT_RUNTIME.md).
- Example: attach an image manually and ask, "Extract its table as CSV," or reference a project file for review. These tasks do not require Computer Use.

## Updates and Distribution

The 3.0.3 stable updater works as follows:

1. Check [`https://orca.aichat.diy/updates/stable/latest.json`](https://orca.aichat.diy/updates/stable/latest.json) first, then fall back to GitHub's signed manifest and signed payload.
2. An installed Windows build downloads only after an explicit user action; after the download it asks the user to exit and then runs the installer. No background download or installation is allowed.
3. macOS/Linux show the version and integrity information and open the download page/package; in-place cross-platform self-update is not promised.
4. Users on older versions need one manual 3.0.3 installation. Use the [Release page assets](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.3) for downloads and verification files.

Signature verification must happen before writing or replacing an installation. Private signing keys stay in the release environment and never enter the repository, example manifest, or documentation. The [3.0.3 validation report](../docs/audits/2026-09-08-v3.0.3-validation.md) records scope, historical failures, and pending checks. Unit tests and candidate-package verification do not establish installation or release acceptance.

## Platform Troubleshooting

- Windows blank window: check WebView2 Runtime, GPU drivers, and WebView2 GPU settings. Compare Classic and Modern, then collect logs. Browser-mock success is not native success.
- Linux blank or flickering window: check GTK/WebKitGTK versions and distribution packages. Where needed, use `WEBKIT_DISABLE_COMPOSITING_MODE=1` for diagnosis. Each GPU/distribution needs separate validation.
- macOS first launch: check the DMG, signing/notarization state, and Gatekeeper prompt. Clearing quarantine is not a substitute for formal signing.
- Drop/clipboard issue: confirm a Wails window, workspace path boundaries, and readable attachments. The browser mock does not have the same native path semantics.
- Computer Use unavailable: it is temporarily disabled on all platforms in 3.0.3. Consent, old configuration, and changing the control model cannot enable it; ordinary Vision and attachment processing remain supported.
- Local-model failure: inspect runtime state, model SHA-256, VRAM/RAM/disk, and the `127.0.0.1` loopback service. Keep download and load errors.
- Update check failure: inspect the primary manifest, GitHub fallback, system proxy, signature, and version fields separately. On Windows follow the explicit download, exit, install flow.

## Release Checklist

- Build the frontend first, then run `go test .` and the necessary root-module tests; a result covers only the executed scope.
- On target OSes, validate window/WebView, drops, install/upgrade, migration, provider requests, attachments, artifacts, permissions, and local AI. Without native evidence, mark the item unvalidated.
- New release requirement: no platform registers computer-control tools; direct calls, old settings/consent, restored sessions, automation, and dispatch cannot bypass disablement. Screen-capture and native-input calls must remain at zero, while ordinary attachment Vision regression checks pass. Native control failures and real-device DPI/human takeover are deferred to restoration acceptance; automated tests do not count as real-device acceptance.
- Generate and compare the manifest, payload, signatures, and SHA-256 values against the files on the Release page.
- Preserve old user roots and session backups. Do not modify servers, websites, keys, version configuration, or unrelated directories during a documentation/build task.
