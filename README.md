**简体中文** | [English](README.en.md)

# O.R.C.A. for Windows

**O.R.C.A.**（**Open Reasoning & Computing Agent**）是一套面向真实工作的开源 AI Agent。它把多模型对话、代码工程、资料处理、桌面操作、本地模型、记忆与自动化放进同一个可检查、可暂停、可恢复的工作区。

O.R.C.A. 不绑定单一模型，也不把所有任务塞进同一种对话：用户可以在普通会话中选择 **助手模式** 或 **编程模式**，并通过固定的 **Orca** 主对话处理跨会话、远程渠道和电脑控制任务。

## 下载

| 平台 | 安装包 | 说明 |
| --- | --- | --- |
| Windows x64 | [安装器](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-for-Windows-windows-amd64-installer.exe) · [便携版](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-for-Windows-windows-amd64.zip) | 完整支持本地 AI 与 Computer Use |
| macOS | [Universal DMG](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-macos-universal.dmg) | 支持 Intel 与 Apple Silicon |
| Linux x64 | [DEB](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/O.R.C.A-linux-amd64.deb) | Debian / Ubuntu |

[查看 O.R.C.A. v3.0.2 的全部文件、校验信息和更新说明](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.2)

[本版验证记录与尚未覆盖的场景](docs/audits/2026-09-07-real-usage-release.md)

Windows 安装器支持原位升级、开始菜单/桌面快捷方式和可选的安装后启动。卸载默认保留用户数据与本地模型。应用只提示可用更新，不会未经确认自动下载安装。

## 产品组成

| 入口 | 适合的工作 | 主要能力 |
| --- | --- | --- |
| 助手模式 | 问答、调研、写作、资料整理、办公文件和日常任务 | 联网与本地工具、个人记忆、产物创建、计划与自动化 |
| 编程模式 | 仓库开发、调试、重构、测试和代码审查 | Shell、Git、LSP、CodeGraph、检查点、回滚和工程验证 |
| Orca | 跨会话协调、远程渠道、桌面任务和长期入口 | 会话派发、QQ/微信接入、Computer Use、共享自动化模型与个人画像 |
| CLI | 终端交互、一次性任务和脚本化工作流 | 与桌面版共享 Provider、工具、MCP、Skill、记忆和会话核心 |

模式按会话保存。切换模式时，O.R.C.A. 会保留可见历史并重新建立对应的提示词、工具和记忆边界；正在执行的回合会先完成或停止，不会被静默替换。

## 功能全览

### 模型与供应商

- 内置 OpenAI、Anthropic、OpenRouter、DeepSeek、阿里云百炼、智谱、Kimi、MiniMax、火山方舟、百度千帆、腾讯混元、StepFun、Xiaomi MiMo、SiliconFlow，以及独立套餐端点。
- 支持自定义 OpenAI-compatible 与 Anthropic-compatible 服务。
- Provider ID、Base URL、密钥槽和完整模型引用相互隔离；同名模型不会跨供应商串换。
- 首次启动提供 DeepSeek Key 快速接入，也可选择其他供应商、本地模型或直接跳过。
- 默认模型、自动化模型、Planner、Subagent 和 Computer Use 模型可分别指定。
- 上下文容量按模型解析；价格或余额没有可靠数据时直接隐藏，不显示误导性的零值。
- DeepSeek 官方模型按请求开始时的峰谷价格快照计费；历史费用不会因之后的价格变化被重算。

### 项目、会话与上下文

- 项目工作区绑定真实目录，独立工作区用于不依赖仓库的任务。
- 支持多标签、固定、重命名、分支、历史、回收站、导出和会话恢复。
- 检查点可记录对话与工作区状态；回滚和 rewind 用于恢复上下文或相关文件变更。
- 长会话支持自动压缩、较早内容检索和稳定的 Turn/Item 生命周期。
- 图片、文件和工作区引用以附件形式进入当前回合，不把二进制内容写进聊天 JSONL。
- 最终回答与执行过程分离；成功回合可折叠，失败、取消和中断保留完整诊断信息。

### 工具与工程执行

- Shell、文件读写、搜索、Git、测试、构建、包管理和常用主机操作。
- 编程模式可使用 LSP、CodeGraph、代码审查、安全检查和写入后的验证流程。
- Plan、Todo 与 Goal 为长任务提供可见计划、进度和明确完成条件。
- Subagent 可分担检索、分析、视觉和工程子任务，结果回到当前会话统一收口。
- readiness 检查区分真正失败和建议性验证，成功任务不会因缺少重复检查而机械重试。
- 后台任务、模型加载、下载、审批和 Computer Use 都有独立状态，不会混成“正在思考”。

### MCP、Skill、机器人与自动化

- MCP 管理页显示 Server、授权、连接状态、失败原因、重试和暴露的工具。
- Skill 可从内置、全局、项目或自定义目录发现，并通过命令或模型按需加载。
- 斜杠菜单统一呈现内置命令、Skill 和 MCP Prompt。
- Orca 可按需列出、读取、派发、等待或停止普通会话任务，并避免递归派发自身。
- QQ 与微信渠道可以接入同一个 Orca 主对话，共享当前上下文段和串行任务队列。
- 机器人、自动化和个人画像使用独立配置，不会偷偷创建普通侧栏会话。

### 多模态与办公产物

- 支持粘贴、拖放或引用 PNG、JPEG、WebP、GIF 和常用文档。
- 每个模型记录独立的视觉能力；自动模式优先使用已确认支持图片的当前模型或视觉 Subagent。
- DeepSeek Vision Exp 可承担截图、OCR、图表和视觉 Agent 任务；纯文本模型不会被错误标记为可识图。
- 内置产物工具可创建、结构化编辑和重新验证 DOCX、XLSX、PPTX 与 PDF；视觉预览需要可用的真实渲染器，缺少依赖时明确提示，不生成占位预览冒充文档。
- 生成的产物带结构化 sidecar，便于后续可靠修改；无法保证保真度的第三方复杂文件会明确提示限制。

### Windows 本地 AI

- 可选安装由 O.R.C.A. 管理的固定版本 `llama.cpp`，不接管 LM Studio。
- 检测所有 GPU，而不是只读取 `GPU 0`；兼容 NVIDIA、AMD、Intel 核显/独显组合和 CPU 回退。
- 根据专用显存预算、当前空闲显存、内存和磁盘选择 CUDA、Vulkan 或 CPU 运行包。
- 模型库支持下载队列、暂停、恢复、断点续传、镜像切换、速度/剩余时间和 SHA-256 校验。
- 16GB 级显存优先推荐 Qwen3.8-27B IQ3_XXS，并自动调整上下文、batch 与 GPU layers 以保留显存余量。
- 本地模型可作为主模型、Subagent 或 Computer Use 模型；同一时间只保留一个常驻模型，空闲后可自动卸载。
- 运行器仅监听 `127.0.0.1` 随机端口并使用临时授权令牌。卸载运行器不会删除模型文件。

### Windows Computer Use

- 助手模式、编程模式和 Orca 都可以发起电脑操作；控制 Subagent 负责短循环，复杂歧义回到主模型判断。
- 每轮观察可组合屏幕截图、UI Automation 元素和窗口信息；每个动作后重新观察并检查成功条件。
- 支持点击、双击、右击、拖拽、滚动、组合键、Unicode 输入和窗口操作。
- 一次性完全授权后，普通低/中风险动作可按策略继续；高风险、显式 Ask/Deny 和安全边界仍需要人工处理。
- 全屏蓝色边缘、指针光晕和点击反馈提示控制状态；`Esc` 随时强制停止并释放按键。
- 不跨越 UAC 安全桌面，不处理锁屏、密码字段或 CAPTCHA，也不对高完整性进程绕过权限。
- 截图默认不落盘、不写入 Provider 历史；动作 telemetry 不记录图片或敏感文本。

### 权限、风险与隐私

- 提供 Ask、自动审批和完全访问等权限策略，并保留宿主 deny 规则与工作区写入边界。
- 自动审批可使用独立模型请求判断风险；分类请求不带历史、工具、图片和秘密字段。高风险进入人工审批，分类异常沿用既有自动审批回退并留下警告 telemetry。
- API Key 使用本地凭据配置，每个供应商预设拥有独立密钥槽，不写入会话消息。
- 消息与任务所需附件会发送给当前模型，以及实际启用的视觉模型或 Subagent；风险复核只发送脱敏操作摘要。不同角色可以配置不同 Provider，使用前请确认各自的隐私与计费规则。
- 会话、配置、日志、缓存、本地模型和下载任务分别存放，便于备份和清理。

### 界面与可访问性

- **Modern** 是默认的轻量工作界面，提供紧凑菜单、单行 Composer、过程时间线和响应式布局。
- **Classic** 恢复 V2.1.3 的蓝白布局、原生窗口和控件分布，同时继续使用 V3 后端能力。
- 样式在设置中保存，重启后切换窗口壳；不会重建或丢失会话。
- 应用内部固定 100% 缩放，并跟随 Windows Per-Monitor DPI；支持键盘焦点、减少动画和窄窗口布局。

## 平台支持

| 能力 | Windows | macOS | Linux |
| --- | :---: | :---: | :---: |
| 云端模型、会话、工具、MCP、Skill、记忆 | ✓ | ✓ | ✓ |
| 编程模式、助手模式与 Orca | ✓ | ✓ | ✓ |
| 本地 `llama.cpp` 管理 | ✓ | 暂不可用 | 暂不可用 |
| Computer Use | ✓ | 暂不可用 | 暂不可用 |
| Modern / Classic 窗口壳 | ✓ | 使用平台原生窗口 | 使用平台原生窗口 |

## 配置与迁移

V3 项目配置使用 `orca.toml`、项目目录 `.orca/`、项目说明 `ORCA.md` 和 `ORCA_*` 环境变量。Windows 全局配置为 `%APPDATA%\orca\config.toml`，凭据、会话和迁移记录等也保存在 `%APPDATA%\orca\`；本地运行器、模型和下载状态使用 `%LOCALAPPDATA%\O.R.C.A\`。备份时应同时保留这两个目录和项目内的 `.orca/`。

从 V2 升级时会读取旧配置、会话、附件、Provider、密钥、记忆、Skill、MCP、费用和 telemetry，并在原子迁移成功后只写新目录。旧目录保留为备份；项目中已由 Git 跟踪的旧说明文件不会被擅自改名。配置版本为 V11。

## 从源码构建

需要 Go、Node.js 22+、npm 和 Wails CLI v2。Windows 安装器还需要 NSIS。

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

CLI 构建和命令说明见 [README.CLI.md](README.CLI.md)，桌面打包说明见 [README.DESKTOP.md](README.DESKTOP.md)，更新记录见 [DESKTOP_CHANGELOG.md](DESKTOP_CHANGELOG.md)。办公产物边界见 [docs/ARTIFACT_RUNTIME.md](docs/ARTIFACT_RUNTIME.md)。

## 许可

O.R.C.A. 使用 [MIT License](LICENSE)。`llama.cpp`、Wails 及其他第三方组件保留各自许可证与声明。
