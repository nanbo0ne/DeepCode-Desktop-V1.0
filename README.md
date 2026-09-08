[English](README.en.md) | **简体中文**

# O.R.C.A. 3.0.4

**O.R.C.A.**（**Open Reasoning & Computing Agent**）是面向真实工作的开源 AI 工作区：把模型对话、助手、编程、研究、文件与图片、工程工具、记忆和自动化放在同一个可暂停、可检查、可恢复的应用里。

> **3.0.4 面向真实工作的 AI 工作区。** 本版带来更清晰的助手、编程、Orca Agent、Vision 附件、产物和多供应商工作流。托管本地 AI 与电脑操控在所有平台继续暂时禁用，相关实现、配置和模型文件保留；普通图片识别、自定义供应商和外部兼容本地服务不受影响。DPI 与其余测试范围见[验证报告](docs/audits/2026-09-08-v3.0.4-release-validation.md)。

## 下载

3.0.4 桌面版下载、校验信息和平台包见 [Release 页资产](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.4)。本版本不生成旧品牌兼容副本，安装升级兼容逻辑仍保留。

| 平台 | 包 | 说明 |
| --- | --- | --- |
| Windows x64 | [安装器](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.4/O.R.C.A-for-Windows-windows-amd64-installer.exe) · [便携版](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.4/O.R.C.A-for-Windows-windows-amd64.zip) | 云端功能；托管本地 AI 与电脑操控暂时禁用 |
| macOS 12+ Universal | [DMG](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.4/O.R.C.A-macos-universal.dmg) | Intel 与 Apple Silicon；托管本地 AI 与电脑操控暂时禁用 |
| Linux x64 | [DEB](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.4/O.R.C.A-linux-amd64.deb) | Debian / Ubuntu；需要匹配的 WebKitGTK 运行库，托管本地 AI 与电脑操控暂时禁用 |

- [查看 3.0.4 发布页、校验文件与正式说明](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.4)
- 下载时核对版本、平台与文件名。
- 不要在文档提交 API key；正式包不内置任何供应商密钥。

## 三种工作模式

| 模式 | 适合 | 默认工作边界 |
| --- | --- | --- |
| **ORCA Agent** | 跨会话协调、机器人渠道、长期入口 | 处理会话派发、等待和自动化；不是普通文本子代理的替代品 |
| **助手** | 问答、调研、写作、资料整理、办公文件和日常任务 | 使用联网/本地工具、助手记忆、计划、自动化和产物工具 |
| **编程** | 仓库开发、调试、重构、测试、审查 | 使用 Shell、文件、Git、LSP、CodeGraph、检查点和工程验证 |

模式按会话保存。切换模式会保留可见历史，再重建对应的提示词、工具和记忆边界；正在执行的回合会先完成或停止。Orca 是固定的顶层控制入口，普通会话仍分别选择助手或编程。

## 功能

### Provider、模型与角色

- 首次启动只需输入并验证 DeepSeek Key，也可以跳过。新安装默认使用 Flash；未单独设置的普通子代理继承主模型。新增供应商入口保留 DeepSeek 和自定义 OpenAI-compatible、Anthropic-compatible 接入；已有供应商、独立密钥和明确选择的角色不删除、不强制替换。
- Provider ID、Base URL、凭据槽和完整的 `provider/model` 引用相互隔离；同名模型不会自动跨端点串换。
- 可以分别设置主对话、planner、subagent 和 Orca/自动化角色；Computer Use 控制角色配置保留，但本版本不运行。角色不是“模型能力保证”：上下文窗口、价格、工具调用和视觉能力仍按实际模型与检测结果判断。
- **视觉角色：** 优先使用明确设置的视觉模型，其次是已确认支持图片的当前模型，再采用官方 `deepseek/deepseek-v4-flash-vision-exp` 默认候选。Flash、Pro 保持文本模型标记。发送图片前仍检查目标供应商与权限，不静默跨供应商上传；显式关闭视觉会保持关闭。普通子代理不会因此全部切换成 Vision。
- 文本 subagent 不会因为视觉默认值被静默改成视觉模型。显式角色配置优先；没有可用凭据或能力检测失败时，应用应显示原因并要求重新配置，而不是伪造成功。
- 价格、余额和上下文信息没有可靠来源时会隐藏或标为未知，不显示误导性的零值。供应商计费和数据保留规则由供应商决定。

DeepSeek 价格已于 2026-09-08 按[官方价格页](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)核对，以下均为每百万 token 的人民币价格（CNY/百万 token）：

| 模型与时段 | 缓存命中输入 | 未命中输入 | 输出 |
| --- | ---: | ---: | ---: |
| Flash / Vision 闲时 | ¥0.05 | ¥1.5 | ¥4.5 |
| Flash / Vision 峰时 | ¥0.10 | ¥3 | ¥9 |
| Pro 闲时 | ¥0.15 | ¥4.5 | ¥13.5 |
| Pro 峰时 | ¥0.30 | ¥9 | ¥27 |

峰时固定为 UTC+8 周一至周五 09:00–12:00、14:00–18:00，其余时段含周末为闲时。每次请求开始时冻结计价依据，历史金额不重算；3.0.3 已存金额继续保留 USD，不追溯转换为人民币。混合 USD/CNY 的会话不显示为一个权威总额，各回合保留自己的币种。

### 工作区、会话与上下文

- 项目工作区绑定真实目录；独立工作区用于不依赖仓库的任务。每个会话有自己的历史、附件、工作区引用和工具 cwd 边界。
- 支持多标签、固定、重命名、分支/派生、历史、回收站、导出和恢复。检查点、rollback 和 rewind 只在能确认范围时恢复关联状态；不会承诺恢复外部系统副作用。
- 长会话可压缩上下文、保存压缩归档并检索较早本地会话。压缩不是完整备份，重要事实应写入项目文件或明确记忆。
- Turn、Item、工具结果和最终回答分开保存。成功回合可折叠；失败、取消、中断和审批拒绝保留诊断信息。
- 后台任务、模型加载、下载和审批都有独立状态。启动后台 subagent 不等于当前工作已经完成，依赖结果时必须等待并核对。

### 图片、文件与产物

- 可粘贴、拖放或引用 PNG、JPEG、WebP、GIF 和常见文档、表格、演示及文本文件；项目内文件可引用而不必复制，项目外附件会进入当前工作区的附件目录。
- 图片默认只作为当前回合输入，不写入会话 JSONL、标题、记忆或压缩文本；发送前仍会交给当前启用的视觉 Provider。尺寸、数量、格式和模型能力会限制可用性。
- 主 Agent 在当前工作区生成的图片，也可委派视觉子代理读取。文件读取及图片发送权限通过后，应用校验路径、格式和限额并保存不可变快照；仅在工作区内并不等于自动获准上传。其他会话文件、目录逃逸和链接替换会被拒绝。
- 例如，附上自己选择的图片并提问“提取这张图里的表格，整理成 CSV”，或附上文档要求摘要。禁用电脑操控不影响这类普通附件处理；应用不会为此自行截取屏幕。
- `artifact_create`、`artifact_edit`、`artifact_preview` 和 `artifact_validate` 面向 DOCX、XLSX、PPTX、PDF。产物带结构化 sidecar，便于后续编辑和完整性检查。
- 真实预览依赖本机渲染器；缺依赖时明确报错，不用占位图冒充视觉验收。结构检查通过不等于版面、字体、公式、分页或所有页面都正确。复杂第三方 Office 文件可能拒绝修改，原文件不会被覆盖。

### 工具、工程与 subagent

- 工具包括文件读写、搜索、Shell、Git、测试、构建、包管理、浏览/联网能力（按工具库和 Provider 配置启用）。权限策略可以关闭工具组。
- 编程模式额外强调 LSP、CodeGraph、代码审查、安全检查、计划、Todo、Goal、checkpoint 和写入后的验证。工具输出是证据，不是自动通过的测试结论。
- Subagent 在独立会话中处理聚焦的检索、分析、视觉或工程任务，并把最终结果带回父会话；可等待、继续或派生已保存的 subagent transcript。工具范围、模型、effort、工作区和父会话必须匹配。
- Subagent/Skill 元工具不会无限递归。长任务应保留引用和失败原因，不要把“已启动”写成“已完成”。

### MCP、Skill、机器人与自动化

- MCP 支持 stdio 和 Streamable HTTP；管理界面显示 Server、授权、连接、失败、重试和暴露工具。密钥应通过环境变量或本地凭据引用，不提交到项目文件。
- Skill 以 `SKILL.md` 或命名 Markdown playbook 发现，可来自内置、全局、项目或自定义目录；斜杠菜单提供命令、Skill 和 MCP Prompt。禁用 Skill 会从提示词和调用入口隐藏，但不会删除文件。
- Orca 可列出、读取、派发、等待、查询状态或停止普通会话任务；它不递归派发自己。QQ、微信/Weixin、飞书等机器人渠道的可用性取决于对应配置、账号和网络。
- Bot、自动化和个人画像使用显式配置与隔离工作区。持续监控或周期运行应保留审批、失败和通知边界，不会因为一次聊天自动创建隐藏普通会话。

### 本地 AI

3.0.4 在所有平台暂时禁用 O.R.C.A. 管理的 `llama.cpp` 和模型下载，隐藏页面、向导路线及启动开关。后端同时拒绝安装、下载、恢复下载和自动启动，不是仅隐藏按钮。停止、取消和清理接口保留，已有模型与配置不自动删除。

这不禁止手动配置外部 OpenAI-compatible 本地服务；应用不会接管 LM Studio。托管运行时的硬件检测、下载与加载实现保留，待后续独立验收后恢复。

### Computer Use：本版本暂时禁用

3.0.4 继续在 **Windows、macOS 和 Linux 所有平台**暂时禁用电脑操控：不注册电脑操控工具，不截取屏幕，不执行原生鼠标、键盘或窗口控制。ORCA Agent、自动化、机器人和子任务都不能启动这项功能。

代码和配置仍然保留，待后续版本完成验收后恢复。已有授权、Full access 或修改控制模型配置均不能在本版本中启用它。默认 Vision、用户主动提供的图片及普通文件附件仍可使用；可以让助手分析附件或整理文件，但不能让它代操作桌面。

既有原生测试记录保留为恢复功能前的验收依据，详情见验证报告。

### 权限与隐私

- Ask、自动审批和 Full access 是不同策略；宿主 deny 规则、工作区写入边界、网络代理和工具库开关优先于模型请求。
- 自动审批可以用独立模型做风险分类，不携带完整历史、工具输出、图片或秘密。分类异常沿用当前审批策略的回退行为并记录告警，不绕过宿主 deny 或显式 ask 规则。
- API key 保存在本地凭据文件/环境变量中，不写入聊天消息、标题、记忆、发布包或本仓库。连接 Provider 前确认其数据训练、保留、地区和计费规则。
- 会话、配置、凭据、附件、缓存、日志、记忆、本地模型和下载任务分开存放，便于备份和清理。共享、机器人和 MCP 服务可能把数据发送到额外的第三方端点。

### Modern 与 Classic

- 运行中的工具组和阶段性回复按实际发生顺序显示，收到的文本片段及时呈现。`简略 / 详细` 名称保留：简略默认隐藏供应商 reasoning，详细显示它；两者都显示进度和工具，不改变模型思考强度。
- 完成后过程折叠成轻量边框标题，显示状态、耗时、本轮 token，以及可确认的 DeepSeek 官方费用。最终答案位于框外。统计按请求去重，包含关联子代理与风险复核；未知、未结算或缺少用量的费用隐藏，reasoning 不重复计入输出 token。
- 回合定位短线贴近聊天区左缘。Todo 是 Composer 上方的独立小浮层，两侧透明且不拦截正文操作。固定主对话显示名改为 ORCA Agent，历史与会话 ID 不变。
- **Modern** 是默认的轻量界面：紧凑菜单、时间线、单行 Composer、模型/effort 控件和响应式布局。
- **Classic** 保留 V2.1.3 风格的蓝白布局、原生窗口装饰和控件安排，同时使用 V3 的会话、Provider、工具和权限服务。
- 样式选择会持久化；Windows Modern 使用自有标题栏，Classic 使用原生窗口框架，通常需要重启才完全切换。窗口按系统 DPI 工作，不等于应用能修复所有 WebView/驱动问题。

## 平台限制

| 能力 | Windows | macOS | Linux |
| --- | :---: | :---: | :---: |
| 云端 Provider、会话、助手/编程/Orca、文件、MCP、Skill、记忆 | 可用 | 可用 | 可用 |
| 托管 `llama.cpp` / 本地模型下载 | 暂时禁用 | 暂时禁用 | 暂时禁用 |
| Computer Use | 暂时禁用 | 暂时禁用 | 暂时禁用 |
| Modern / Classic | 两种窗口壳 | 平台原生窗口 | 平台原生窗口 |

Windows 需要 WebView2；macOS 使用系统 WebKit；Linux 需要 GTK/WebKitGTK 运行库，发行版和 GPU 驱动差异可能造成白屏、闪烁或字体问题。平台包和原生安装仍以验证记录为准。

## 安装与更新

### 安装

1. Windows 从 Release 页选择 x64 安装器；升级前备份用户数据，便携版解压到用户有写权限的目录。首次运行时自行配置 Provider 或选择跳过，包不含 API key。
2. macOS 打开 Universal DMG 并拖到 Applications。未公证或签名状态变化时，Gatekeeper 可能需要用户按系统提示确认；不要下载来历不明的绕过脚本。
3. Linux 安装 DEB，并先准备对应 WebKitGTK/GTK 依赖。发行版没有匹配库时，优先使用系统包管理器或源码构建。
4. 使用 Release 页中的签名、SHA-256 和 manifest 校验文件核对包。

### 应用内更新

- 稳定通道首先检查 [`https://orca.aichat.diy/updates/stable/latest.json`](https://orca.aichat.diy/updates/stable/latest.json)，失败时回退 GitHub 的签名 manifest 与对应签名 payload。manifest、payload、版本和 SHA-256 必须一致；签名失败不得安装。
- Windows 已安装版本只提供“明确下载 → 用户确认退出 → 运行安装器”的流程；不会在后台自动下载或自动安装。关闭前保存会话和草稿，失败时保留原安装。
- 3.0.4 下载界面显示来源、实测速度与预计剩余时间；持续低速时提示切源。先取消，再选择另一来源并重试，可继续有效断点。备用文件必须匹配签名清单中的版本、大小与摘要，切源不会放宽校验。速度取决于当前网络和服务器，不保证 GitHub 更快。
- macOS/Linux 显示可用版本和校验信息，打开对应下载页/包；不把跨平台自更新当成已完成能力。macOS 的主检查地址如上，GitHub 是回退来源。
- 旧版本用户需要从 Release 页手动下载并运行安装器/包，完成一次 3.0.3 bootstrap；不要修改配置版本或手工替换凭据文件来“强制升级”。

## 配置与迁移

- 当前项目配置：`orca.toml`；项目状态目录：`.orca/`；项目说明：`ORCA.md`；环境变量前缀：`ORCA_`。V2 的旧路径和环境变量仍可兼容读取；新内容使用以上名称。
- 用户配置通常位于 `os.UserConfigDir()/orca/config.toml`；同目录保存 `credentials`、`sessions`、`archive`、`cache` 和记忆数据。Windows 的实际根目录由系统 `AppData` 解析，常见位置是 `%APPDATA%\\orca\\`。本地模型/运行时使用独立的本地数据目录，具体路径以设置页为准。
- 配置按优先级合并：命令行/显式参数、项目 `orca.toml`、用户配置、内置默认值；项目 `.mcp.json` 也可提供 MCP。不同工作区分别解析配置、`.env`、MCP 和会话。
- 迁移前关闭应用并备份 `orca` 与 V2 用户目录，以及项目中的配置、`.orca/`、附件和说明文件。迁移应保留会话、附件、Provider、凭据引用、记忆、Skill、MCP、机器人和 telemetry；旧目录保留为回滚材料。
- 迁移不能恢复外部副作用，也不能把一个 Provider 的 key 推断给另一个 Provider。显式 role/model 设置优先于新的视觉默认值；Computer Use 代码和配置保留，迁移不会重新启用它。失败时回到旧数据和兼容读取，不要删除源文件。

## 从源码构建

需要 Go、Node.js、npm 和 Wails CLI v2；Linux 还需要 GTK/WebKitGTK，Windows 构建安装器需要 NSIS。桌面模块使用自己的依赖和前端构建：

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

开发时可在 `desktop` 执行 `wails dev`。前端单独运行 `npm run dev` 时使用浏览器 mock，不能证明 Wails 原生绑定、文件拖放、窗口框架、本地 AI 或 Computer Use 可用。发布构建需要使用正式版本注入参数，并在上传前重新生成和校验前端产物与签名。

## 故障排查

| 现象 | 先检查 |
| --- | --- |
| 没有模型/请求失败 | Provider 是否启用、完整 `provider/model` 是否存在、凭据是否在本地、代理是否可达；视觉失败再检查该模型的能力检测 |
| 图片被拒绝 | 格式/大小/数量、视觉模式、当前角色和模型能力；文本 subagent 不会自动获得视觉能力 |
| 产物能生成但预览失败 | 这是渲染器缺失或版面未验收，不要把结构校验当视觉通过；检查 Poppler/目标 Office 阅读器 |
| 工具被阻止 | Ask/Auto/Full access、deny 规则、工作区路径、sandbox 和工具库开关；不要用关闭安全边界解决未知错误 |
| 找不到本地 AI | 3.0.4 暂时禁用托管运行时及模型下载，旧配置不能重新开启；文件保留，外部自定义服务仍可使用 |
| 找不到 Computer Use 或请求被拒绝 | 本版本继续暂时禁用电脑操控，授权或模型设置无法开启；普通 Vision 识图和文件附件仍可用 |
| 白屏或窗口异常 | WebView2/WebKitGTK/GTK 版本、GPU 驱动、Modern/Classic 选择；先重启并收集日志，再判断是否为原生问题 |
| 更新器无响应 | 先访问主 manifest，再检查 GitHub 回退、签名/版本字段和系统代理；Windows 手动下载并退出安装，不要期待后台安装 |

3.0.4 的验证范围与结果见 [验证报告](docs/audits/2026-09-08-v3.0.4-release-validation.md)，下载速度证据见 [测速记录](docs/audits/2026-09-08-download-diagnosis.md)。桌面开发、打包和平台细节见 [desktop/README.md](desktop/README.md)；办公产物边界见 [docs/ARTIFACT_RUNTIME.md](docs/ARTIFACT_RUNTIME.md)。

## 许可

O.R.C.A. 使用 [MIT License](LICENSE)。Wails、`llama.cpp`、WebKit/GTK、Provider SDK 和其他第三方组件保留各自许可证与声明。
