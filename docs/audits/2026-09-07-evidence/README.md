# Audit Evidence / 2026-09-07

这是 v3.0.1 审计的证据快照，不是永久回归测试套件。结论以 [主报告](../2026-09-07-v3.0.1-audit.md) 为准。

- Go 测试有意断言当前缺陷存在。修复产品之后这些断言应失败；不要为保持测试绿色而恢复缺陷。
- 风险分类失败仍允许执行的测试记录的是历史明确选择的策略，主报告将它单列为设计风险，而不是实现偏离计划。
- Windows 配置目录测试仅确认实际路径，供卸载清单对照。
- 所有 API/CLI 材料只含合成内容，没有测试密钥。不要向本目录加入真实凭据。
- `catalog-sources.md` 和 `copy-ux.md` 是子代理专项证据；严重度和最终范围由主报告复核。
- 原始更完整运行材料仍保留在仓库 `.tmp/audit-2026-09-07/`，没有复制大型二进制、原生用户配置或 CLI 凭据环境。

## 重跑 Go 复现

在 Windows、具备已有 Go 依赖的环境中运行：

```powershell
powershell.exe -NoProfile -File docs/audits/2026-09-07-evidence/replay-go.ps1
```

脚本隔离用户目录，使用本地替换模块，不连接任何模型 API，不注入电脑输入。嵌套 `go.mod` 防止这些“证明缺陷存在”的测试被根模块 `go test ./...` 当成正常套件。

## 重跑浏览器

前端已有 npm 依赖；需提供 Playwright 包和 Edge。先在 `desktop/frontend` 启动独立本地 Vite：

```text
npm run dev -- --host 127.0.0.1 --port 41873 --strictPort
```

然后在本目录用含 Playwright 的 Node 运行：

```text
node browser-audit.cjs
node browser-functional.cjs
```

本次使用的 Node 包目录为 Codex bundled runtime，脚本中的 Edge 路径和端口是本机环境值。重跑会覆盖同目录的 JSON/PNG；保留本证据快照时应先在独立目录运行副本。浏览器脚本使用 mock Wails 数据，不测试真实供应商、原生窗口控制或 Windows 系统 DPI。

## 现有包摘要

本地 `dist/desktop-v3.0.1/` 的重新计算结果与既有清单一致：

```text
27be92506216bc3215ba3f3a0c1ea1ff601c2ba9231f8bfc7fdcd052edc03f6b  O.R.C.A-for-Windows-windows-amd64-installer.exe
444b96f5ca4a18fd4933527a70f94ef169c7c92d4625a3ae5c5c4d780dcb7017  O.R.C.A-for-Windows-windows-amd64.zip
```

仅证明现有文件完整性，没有运行安装/卸载，也没有重新发布或声称与当前源码完全可重建。
