# O.R.C.A. Desktop 3.0.4

O.R.C.A. 桌面端 3.0.4 修复了流式回复、视觉附件委派、会话费用和多处界面问题，保留助手、编程与 ORCA Agent 的完整工作区。托管本地 AI 与 Computer Use 在所有平台继续暂时不可用。验收范围见[验证报告](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/main/docs/audits/2026-09-08-v3.0.4-release-validation.md)。

## 本版本亮点

- 保留 ORCA Agent、助手、编程，以及供应商、模型和角色配置、工作区与会话、附件、产物、MCP、Skill、机器人、自动化、记忆、权限、Modern 与 Classic 界面。
- 首次配置聚焦 DeepSeek；文本默认 Flash，未配置的视觉任务使用默认 DeepSeek Vision 候选。明确角色、自定义供应商、普通 Vision 附件和外部兼容本地服务保留。
- 流式阶段回复和工具组按时间顺序显示；Compact/Detailed 仅控制供应商 reasoning；完成摘要显示耗时、token 和可确认的官方 DeepSeek 费用，最终回答保持独立。
- 工作区生成图片在视觉委派前经过读取/发送权限、路径、所有权、类型和大小限制检查，并冻结父任务所属快照。
- 下载器显示来源、速度、ETA 和低速提示；用户取消后可切换同一签名负载的来源并续传。3.0.4 保留历史发布兼容性，不生成旧品牌重复资产。
- 托管本地 AI 在 Windows、macOS 和 Linux 暂时禁用并隐藏，代码、配置和模型文件保留；Computer Use 同样在所有平台禁用，不注册电脑操控工具、不截图、不执行原生鼠标键盘或窗口输入。现有授权、Full access、机器人和自动化不能重新启用它。

完整功能介绍见 [中文 README](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.4/README.md) 和 [English README](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.4/README.en.md)。

## DeepSeek 官方人民币价格

以下为 2026-09-08 核对的官方表，单位为 CNY/百万 token，来源为[官方价格页](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)：

| 模型与时段 | 缓存命中输入 | 未命中输入 | 输出 |
| --- | ---: | ---: | ---: |
| Flash / Vision 闲时 | ¥0.05 | ¥1.5 | ¥4.5 |
| Flash / Vision 峰时 | ¥0.10 | ¥3 | ¥9 |
| Pro 闲时 | ¥0.15 | ¥4.5 | ¥13.5 |
| Pro 峰时 | ¥0.30 | ¥9 | ¥27 |

峰时固定为 UTC+8 周一至周五 09:00-12:00、14:00-18:00，其余时间含周末为闲时。每次请求开始时冻结计价依据；已存历史金额不重算、不改标签。3.0.3 历史金额继续以 USD 保存，不追溯转换为人民币；混合 USD/CNY 的会话不显示为一个权威总额，各回合保留自己的币种。

## 安装与历史数据

Windows 安装器可原位升级并保留配置、会话与模型文件。安装包和更新清单附带 Minisign 签名及 SHA-256；Windows 仍未做发布者签名，macOS 仍未公证，系统可能提示来源或信誉警告。更新文件签名不代替系统发布者签名。

3.0.3 的发布文件、签名、标签、审计报告和历史金额保持不变。旧金额仍按 USD 显示，不回溯转换为人民币。

## English

O.R.C.A. Desktop 3.0.4 fixes streaming replies, visual attachment delegation, session costs, and several layout issues across Assistant, Coding, and ORCA Agent. Managed local AI and Computer Use remain temporarily unavailable on every platform. Test coverage and limitations are recorded in the [validation report](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/main/docs/audits/2026-09-08-v3.0.4-release-validation.md). Desktop downloads and checksums are listed on the [`desktop-v3.0.4` Release page](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.4).

The Windows installer upgrades in place while preserving configuration, sessions, and model files. Packages and the update manifest include Minisign signatures and SHA-256 checksums. Windows packages still lack an Authenticode publisher signature and macOS packages are not notarized; update signatures do not replace OS publisher signing.

The scope retains the full Orca Agent, Assistant, and Coding workspace, provider/model/role isolation, sessions, workspaces, attachments, artifacts, MCP, Skills, bots, automation, memory, permissions, Modern and Classic shells, ordered streaming/tool groups, generated-image delegation with permission-checked snapshots, and resumable signed downloads. Managed local AI remains temporarily disabled and hidden on Windows, macOS, and Linux, while Computer Use remains disabled on every platform. Code and configuration are retained, ordinary Vision attachments remain supported, and external compatible local services remain available when configured.

DeepSeek official pricing checked on 2026-09-08 is CNY per million tokens: Flash/Vision off-peak ¥0.05 / ¥1.5 / ¥4.5 and peak ¥0.10 / ¥3 / ¥9; Pro off-peak ¥0.15 / ¥4.5 / ¥13.5 and peak ¥0.30 / ¥9 / ¥27, in cache-hit input / cache-miss input / output order. Peak time is fixed at UTC+8 Monday-Friday 09:00-12:00 and 14:00-18:00; other times are off-peak. Each request freezes its pricing basis at start. Stored 3.0.3 amounts remain USD and are not back-converted or relabeled. A session containing USD and CNY turns is not shown as one authoritative total; each turn keeps its own currency. See the [official pricing page](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/).
