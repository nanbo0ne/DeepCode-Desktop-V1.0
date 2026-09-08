[English](#english) | **简体中文**

# O.R.C.A. Desktop v3.0.3

## 简体中文

3.0.3 保留 Orca、助手、编程三种模式，以及工作区、会话、文件、产物、工程工具、MCP、Skill、记忆、自动化和可选本地 AI。完整功能与使用方法见 [中文 README](../../README.md)；下载、平台包及校验文件以 [Release 页资产](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.3)为准。

**Vision 默认值与电脑操控：** 未配置的视觉/控制角色默认候选为官方 `deepseek/deepseek-v4-flash-vision-exp`；显式角色配置保留，文本 subagent 默认值不变，包内不提供 API key。普通附件与默认 Vision 识图仍支持，例如手动附图并要求提取表格。**Windows、macOS、Linux 所有平台暂时禁用电脑操控**：不注册电脑操控工具、不截图、不执行原生鼠标键盘或窗口控制。控制代码和配置保留，待后续验收恢复；旧授权、Full access 和控制模型配置不能在本版本中启用它。

**签名更新与旧版迁移：** 稳定通道以 [官方 manifest](https://orca.aichat.diy/updates/stable/latest.json) 为主源，GitHub 的签名 manifest 和对应签名 payload 为回退；校验签名、版本与 SHA-256。Windows 安装版需明确下载、确认退出后安装，不会自动下载或安装；其他平台打开下载页或包。旧版本用户需从 Release 页**手动安装一次 3.0.3**，升级前备份配置和会话，不要通过修改配置版本强制升级。

**系统签名限制：** Windows 包没有 Authenticode 发布者签名，macOS 包未公证，系统可能提示来源或信誉风险。更新文件的 Minisign 签名不能替代系统发布者签名或公证。核对来源与校验文件，并按系统提示处理。测试范围、已知问题与延期项见 [验证记录](../audits/2026-09-08-v3.0.3-validation.md)。

## English

3.0.3 retains Orca, Assistant, and Coding modes, along with workspaces, sessions, files, artifacts, engineering tools, MCP, Skills, memory, automation, and optional local AI. See the [English README](../../README.en.md) for features and usage, and the [Release page assets](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.3) for downloads, platform packages, and verification files.

**Vision default and Computer Use:** Unconfigured vision/control roles default to the official `deepseek/deepseek-v4-flash-vision-exp`. Explicit role choices and text-subagent defaults are preserved; no API key is bundled. Ordinary attachments and default Vision image analysis remain supported, such as extracting a table from an image you attach. **Computer Use is temporarily disabled on Windows, macOS, and Linux:** no computer-control tool registration, screen capture, or native mouse, keyboard, or window actions. Its code and configuration remain for restoration after later validation. Old consent, Full access, and control-model settings cannot enable it in this version.

**Signed updates and migration:** The stable channel uses the [official manifest](https://orca.aichat.diy/updates/stable/latest.json) first, with GitHub's signed manifest and matching signed payload as fallback. Signatures, versions, and SHA-256 are checked. Installed Windows builds require an explicit download and confirmation to exit and install; downloads and installation are not automatic. Other platforms open the download page/package. Users on older versions need **one manual installation of 3.0.3** from the Release page. Back up configuration and sessions first; do not force an upgrade by editing the configuration version.

**OS signing limits:** Windows packages do not have an Authenticode publisher signature, and macOS packages are not notarized, so the OS may show source or reputation warnings. Minisign signatures on update files do not replace an OS publisher signature or notarization. Verify the source and integrity files and follow system prompts. See the [validation record](../audits/2026-09-08-v3.0.3-validation.md) for test scope, known issues, and deferred work.
