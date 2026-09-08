# O.R.C.A v3.0.1 文案与过度工程化短审计

审计基线：工作树 D:\AI-Reasonix\.tmp\v2.1.3-worktree，HEAD 414225de。范围是 desktop/frontend/src 的用户可见中文，以及 internal 的 prompt/instruction 拼装路径。未修改产品代码、用户配置或密钥，未调用真实付费 API；本轮没有新增运行测试。没有发现位于该工作树或其祖先目录的适用 AGENTS.md；D:\AI-Reasonix\.tmp\superpowers\AGENTS.md 不在路径祖先，未套用。

结论标签：

- 静态确认：源码中已确认字符串和实际引用点；不等同于每种路由、语言或运行时状态都已视觉验证。
- 已运行复现：来自已落盘安全日志或主审实测，本轮只读取证据，不重复调用 API。
- 待验证：需要在真实 UI/CLI 中按触发条件复跑的显示效果、布局和最终回答长度。

## 一、前端实际可见文案

### 1. 首启连接说明

- 结论：静态确认；触发条件是首启且 OnboardingOverlay 处于 deepseek 路由。
- 位置：desktop/frontend/src/components/OnboardingOverlay.tsx:75；同一组件在 :70-75 使用原生硬编码文案，不走 locale key。
- 现文案：输入 DeepSeek API Key 即可开始。密钥仅保存到本机凭据配置；也可以跳过，稍后连接其他供应商或本地模型。
- 问题：API Key、凭据配置、供应商都是实现或行业术语；“保存到本机凭据配置”不如“只保存在本机”直白；连接方式、隐私承诺和跳过路径挤在一段中。另有 desktop/frontend/src/locales/zh.ts:1237 的相似 onboarding.tagline，形成硬编码文案与 locale 文案并存，后续容易不一致。
- 建议：输入 API 密钥即可开始。密钥只保存在本机。也可以跳过，稍后再连接其他服务或本地模型。
- 影响：首启用户还未建立术语和信任时，需要先理解存储实现，而不是先完成连接。

### 2. Composer 输入框占位符

- 结论：静态确认；Composer.tsx:2178 在普通输入框中直接渲染该 placeholder。
- 位置：desktop/frontend/src/locales/zh.ts:314；引用 desktop/frontend/src/components/Composer.tsx:2178。
- 现文案：给 O.R.C.A. 发消息…  ( / 命令 · @ 文件 · ! 终端 )
- 问题：一个占位符同时承担输入提示、三个快捷语法和工具入口说明；/、@、! 的含义没有上下文，括号前有多余双空格，中文标点和半角符号混排。
- 建议：给 O.R.C.A. 发消息…。把 /、@、! 放到输入框旁的帮助入口或按键提示中，并分别说明用途。
- 影响：首次输入的主路径被低优先级的高级语法挤占，且用户容易把 ! 终端误认为普通文本。

### 3. 设置中的权限规则格式

- 结论：静态确认；SettingsPanel.tsx:3477 将该字符串作为权限规则区域 description 显示。
- 位置：desktop/frontend/src/locales/zh.ts:978；引用 desktop/frontend/src/components/SettingsPanel.tsx:3477。
- 现文案：规则格式：ToolName 或 ToolName(glob)。优先级：deny > ask > allow。
- 问题：ToolName、glob、deny、ask、allow 是未解释的代码和英文策略术语；比较符号只给出优先级，没有告诉用户怎么写一个可用规则。
- 建议：可按工具或命令设置规则；拒绝优先，其次询问，最后自动允许。需要匹配一组名称时，可使用通配符。
- 影响：设置页把配置语法当成说明，普通用户无法从文案判断输入格式和风险。

### 4. 设置中的视觉能力检测

- 结论：静态确认；SettingsPanel.tsx:1951 将该 hint 绑定到视觉设置字段。
- 位置：desktop/frontend/src/locales/zh.ts:697；引用 desktop/frontend/src/components/SettingsPanel.tsx:1951。
- 现文案：自动模式只向确认支持视觉的模型发送图片；检测会产生一次很小的模型请求。
- 问题：“支持视觉”是内部能力叫法；“确认支持”没有说明确认来源；“很小的模型请求”既不说明会发送什么，也不说明用户是否需要承担一次调用。
- 建议：自动模式只会把图片发送给已确认支持图片的模型；检测会发送一次测试请求。
- 影响：用户难以判断自动模式的边界和检测动作，隐私与调用成本提示也不够明确。

### 5. 会话 token 状态提示

- 结论：静态确认；StatusBar.tsx:178 将该字符串作为 token 状态的 Tooltip 显示。
- 位置：desktop/frontend/src/locales/zh.ts:413；引用 desktop/frontend/src/components/StatusBar.tsx:178。
- 现文案：本会话累计消耗的模型 tokens，不等于当前上下文窗口占用。
- 问题：tokens、上下文窗口是两个未解释概念；“消耗”可能被理解为费用；后半句用否定定义，用户仍不知道这个数字的用途。
- 建议：本会话已使用的 token 数；不代表当前上下文占用。
- 影响：状态栏的辅助信息增加认知负担，却没有帮助用户做出下一步判断。

### 6. 过程时间文案

- 结论：静态确认；Transcript.tsx:367 在存在 elapsedMs 时显示该字符串。
- 位置：desktop/frontend/src/locales/zh.ts:729；引用 desktop/frontend/src/components/Transcript.tsx:367。
- 现文案：已处理 {elapsed}
- 问题：这里的占位值是耗时，已处理却像“处理了多少内容”；与同一区域 :730 的“已处理”重复语义，不能一眼读出这是本轮用时。
- 建议：本轮用时 {elapsed}。
- 影响：用户看到过程收尾状态时无法立即区分耗时、完成状态和处理数量。

### 7. 设置刷新模型失败

- 结论：静态确认；SettingsPanel.tsx:2346 将原始错误字符串插入警告文本。
- 位置：desktop/frontend/src/locales/zh.ts:1101；引用 desktop/frontend/src/components/SettingsPanel.tsx:2346。
- 现文案：刷新 {provider} 失败：{err}
- 问题：“刷新”没有说明刷新的是模型列表；原始 err 可能是面向开发者的网络或协议错误；主提示没有下一步动作，且 provider 名称和错误细节挤在一行。
- 建议：无法获取 {provider} 的模型列表。请检查连接设置后重试。详细错误放入可展开的诊断区域。
- 影响：错误发生后用户不知道该检查什么；开发信息直接进入主操作路径，增加噪声。

## 二、内部 prompt/instruction 与最终回答形态

### 8. 全 profile 共用 completion contract

- 结论：静态确认；internal/promptprofile/enhanced.go:16-22 定义了“首个工具批次前先说明、阶段完成时更新、最后单独给出结果/验证/阻塞”的 contract；:168-179 将它拼入 Assistant 和 Orca prompt，:160-165 也拼入 coding prompt；internal/boot/boot.go:294-298 是实际组装入口。
- 触发条件：只要该 profile 运行并发生有意义的工具批次或工具收尾；无工具的普通问答不会仅凭这几行必然产生阶段播报。
- 实际证据：静态确认该规则覆盖 everyday questions 的 Assistant prompt（enhanced.go:32），但没有在本轮单独运行 Assistant profile。
- 问题：把“进度播报、最终结果、相关验证、阻塞说明”作为统一完成协议，边界是工具批次而不是任务复杂度；一次查资料的普通问题也可能被拆成过程话术加验证收尾。
- 建议：按任务复杂度分层：无工具或单次查询只返回答案；多工具任务才播报关键进展；写入、发布或高风险操作才要求验证和交付摘要。
- 状态：静态确认；普通问答是否因此明显变长，待验证。

### 9. Step thinking 的固定长流程

- 结论：静态确认；internal/promptprofile/enhanced.go:234-241 在 stepThinking=true 时要求 explore、brainstorm 2-3 approaches、design/spec、implementation plan、focused tasks、task review、final review；internal/control/input.go:137-139 将此 reminder 注入用户消息前。
- 触发条件：用户或运行时开启 step thinking；不是所有普通问答的默认路径。
- 问题：固定阶段数和 2-3 个方案对简单咨询、定义解释或单步检索都是仪式性成本；虽然 :241 要求不要暴露内部 ceremony，但“是否有帮助”留给模型判断，不能作为稳定的长度控制。
- 建议：把阶段模板限定为多文件修改、调试、发布或长任务；普通问答只允许直接回答，最多给一个简短思路，不生成 brainstorm、任务评审和最终评审段落。
- 状态：静态确认；需分别验证 step thinking 开关下的最终可见输出。

### 10. CLI 默认 profile 的单个已运行样本

- 结论：已运行复现，证据为 .tmp/audit-2026-09-07/cli-copy-probe.json:1-8；该文件记录 code=0、ms=31965，并在 stdout 中记录 host_system_info、host_command ls -la 失败、host_command dir /a、ask，之后输出长目录模板和“零风险上手”话术。仅代表 CLI 默认 profile 的单个样本，不归因某模型。
- 静态路径解释：默认分支在 internal/boot/boot.go:292-298 走 CodingSystemPrompt；其默认基线来自 internal/config/config.go:1059-1071，并追加 TaskTrackingPolicy（:1073-1078）和工具路由策略（:1080-1091）。基线要求用工具验证事实并在完成后说明如何验证，但 TaskTrackingPolicy 明确写出 simple answers、quick checks 不应为仪式创建 todo（:1076-1078）。
- 问题：样本把“怎么开始整理读书笔记”的普通咨询先变成环境检查、失败命令纠正、ask，再给五种方案、目录模板和三条启动语，耗时约 32 秒；结果可用，但对咨询意图显得工具化、过度结构化。现有 prompt 能解释“优先用工具核实工作区”和完成说明的倾向，却不能仅凭静态文本证明长目录模板是某一条硬性格式规则。
- 建议：为默认 coding profile 增加任务意图门槛：普通咨询先直接给 2-4 句起步建议；只有用户要求查看工作区、创建笔记库或给出定制结构时才调用环境工具和 ask；模板、目录树、交付计划改为按需展开。
- 状态：已运行复现；因只有单个 CLI 样本，跨任务稳定性和其他 profile 行为待验证。

## 补充：本轮保留的后端 CLI 复现

主审已运行复现并提供 .tmp/audit-2026-09-07/cli-explicit-dir.json：使用 orca run --dir nested 时，read_file 相对路径读到父仓库标记 ORCA_READ_37，而预期项目根 nested 的标记为 ORCA_CHILD_84。该结论属于后端工作区边界问题，本短审计不重复运行、不读取 key；状态保留为已运行复现。

## 建议验证命令与局限

建议在无真实密钥的隔离 fixture 中验证：

- 静态引用核对：rg -n "onboarding|composer.placeholder|settings.ruleForm|settings.visionEnabledHint|sessionTokensTitle|process.timeline.elapsed|fetchModelsFailedForProvider" desktop/frontend/src
- prompt 组装核对：go test ./internal/promptprofile ./internal/boot -run "Prompt|Routing"；仅用于确认拼装路径，不代表最终回答长度正确。
- CLI 文案复现应使用 mock provider 和无凭据 fixture，记录工具调用数、首个可见文本延迟、最终输出字符数；不要使用真实付费 API。

局限：本轮没有做浏览器视觉布局检查，没有运行 Assistant/Step thinking 的对照矩阵，也没有把单个 CLI 样本推广成模型或所有任务的普遍结论；现有测试通过只能证明测试断言成立，不能证明文案清晰或回答复杂度合适。
