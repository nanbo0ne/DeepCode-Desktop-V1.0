# O.R.C.A v3.0.1 LocalAI Catalog Source核查

核查对象：`D:\AI-Reasonix\.tmp\v2.1.3-worktree`，`HEAD 414225de74b1f1f772bab22ad7e25b422137c037`
核查日期：2026-09-07（Asia/Shanghai）
范围：只读核对 `internal/localai/catalog.go` 的 llama.cpp `b10453` runtime 资产和首推 `Qwen3.8-27B-IQ3_XXS` 模型及 `mmproj`。没有下载模型/runtime 大文件，没有启动安装器或安装流程，没有读取/写入密钥。

## 结论

1. **已静态确认且远程元数据匹配**：`b10453` 的 Windows CUDA 12.4 主包、CUDA runtime DLL 包、CPU x64 包、Vulkan x64 包的包名、字节数和 SHA-256 均与 GitHub 官方 release 页面/API 及 `HEAD` 响应匹配。
2. **已静态确认且远程元数据匹配**：`mmproj-F16.gguf` 的大小和 SHA-256 在 Hugging Face 与 ModelScope 两个源一致，并与源码匹配；`hf-mirror.com` 的 `HEAD` 也返回相同大小。
3. **实证问题（高）**：首推 `Qwen3.8-27B-UD-IQ3_XXS.gguf` 的源码固定大小/SHA 已过时或指向另一版本：源码为 `11,913,559,104` 字节和 `0a6129dc...d6be`，而 Hugging Face、ModelScope 当前同名文件均为 `10,934,860,704` 字节和 `c0b7c303...f3eee`。本轮没有下载大文件，因此“实际下载后失败”是由代码路径与已观测元数据推出的静态结果，不冒充下载复现。

## 源码固定值

位置：`internal/localai/catalog.go:4,41-45,67-85`。

| 对象 | 源码固定值 |
|---|---|
| Runtime | `b10453` |
| CUDA 主包 | `llama-b10453-bin-win-cuda-12.4-x64.zip`; `250,790,655`; `84b863f70a8b4c2873e93385d0b208f24776ecd1b946a2cb6d5cda863d143c3d` |
| CUDA DLL 包 | `cudart-llama-bin-win-cuda-12.4-x64.zip`; `391,443,627`; `8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6` |
| CPU | `llama-b10453-bin-win-cpu-x64.zip`; `18,464,078`; `70c07211d0027305f0be09cd755d79641ebb0bb646590ff3d498c66b22df29b0` |
| Vulkan | `llama-b10453-bin-win-vulkan-x64.zip`; `34,807,257`; `123001c3e3918f29420f622431b06dfc5e09ef4d6aff366860d3fd5b9f3418d8` |
| 首推模型 | `Qwen3.8-27B-UD-IQ3_XXS.gguf`; `11,913,559,104`; `0a6129dcbbbe72f423dc67e0e3bbfbbdf3e923981a3637687ebb96a46c59d6be` |
| 首推 mmproj | `mmproj-F16.gguf`; `927,607,488`; `cbb841a9ee0636b2ec172f5bb8df2ea8dfeb01e90fe7c6126581d662a0b4e43e` |

## GitHub llama.cpp b10453

### 实际只读响应

- `GET https://api.github.com/repos/ggml-org/llama.cpp/releases/tags/b10453`：`curl.exe` 返回 `HTTP/1.1 200 OK`；响应字段为 `tag_name=b10453`、release id `371325099`、`published_at=2026-08-16T12:54:19Z`、`target_commitish=3cb7ffb1a1f612d5e4a46244ae5a3c77ad934a70`。同一响应包含 Windows CPU、CUDA 12.4/CUDA DLL、Vulkan 资产。
- 同一官方 release 的 `expanded_assets/b10453` 页面列出了所需四个资产的 digest；对每个 download URL 发 `HEAD` 均返回 `200`，`Content-Type=application/octet-stream`。`Content-Length` 分别为 `250790655`、`391443627`、`18464078`、`34807257`，与源码字节数逐项相同；页面 digest 与源码 SHA 逐项相同。
- GitHub release 页面仍存在 `b10453`，并列出 Windows x64 CPU、CUDA 12（CUDA 12.4 DLLs）和 Vulkan 条目。官方 release 列表当前已显示更晚的 `b10516` 为 Latest；这说明当前固定版本存在维护滞后风险，但不等于 `b10453` 的固定元数据错误，也没有在本轮提出升级版本结论。

### 逐项比对

| 资产 | HEAD 字节数 | 官方 digest | 与源码 |
|---|---:|---|---|
| `llama-b10453-bin-win-cuda-12.4-x64.zip` | `250,790,655` | `84b863f70a8b4c2873e93385d0b208f24776ecd1b946a2cb6d5cda863d143c3d` | 匹配 |
| `cudart-llama-bin-win-cuda-12.4-x64.zip` | `391,443,627` | `8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6` | 匹配 |
| `llama-b10453-bin-win-cpu-x64.zip` | `18,464,078` | `70c07211d0027305f0be09cd755d79641ebb0bb646590ff3d498c66b22df29b0` | 匹配 |
| `llama-b10453-bin-win-vulkan-x64.zip` | `34,807,257` | `123001c3e3918f29420f622431b06dfc5e09ef4d6aff366860d3fd5b9f3418d8` | 匹配 |

## 首推 Qwen3.8-27B 与 mmproj

### 实际只读响应

- Hugging Face model API `GET https://huggingface.co/api/models/unsloth/Qwen3.8-27B-GGUF`：`200`；repo 为 public、`gated=false`、`disabled=false`，`lastModified=2026-08-20T12:04:25Z`。
- Hugging Face tree API `GET https://huggingface.co/api/models/unsloth/Qwen3.8-27B-GGUF/tree/main`：`200`。两个目标文件均为 LFS file；`Qwen3.8-27B-UD-IQ3_XXS.gguf` 的 `size=10934860704`、LFS `oid=c0b7c3038681ed2e3040456c1dd45f9858b6c2290bed172c70388a94874f3eee`；`mmproj-F16.gguf` 的 `size=927607488`、LFS `oid=cbb841a9ee0636b2ec172f5bb8df2ea8dfeb01e90fe7c6126581d662a0b4e43e`。
- Hugging Face 两个 `resolve/main/...` URL 的 `HEAD` 均为 `200`，`Content-Length` 分别为 `10934860704`、`927607488`。响应的 ETag 是 Xet hash，不当作 SHA-256；SHA 采用 tree API 的 LFS oid 和文件页显示的 SHA-256。
- ModelScope repo files API `GET https://modelscope.cn/api/v1/models/unsloth/Qwen3.8-27B-GGUF/repo/files?Revision=master&Recursive=true`：`200`。两个目标文件均 `IsLFS=true`；同名文件的 `Sha256`/`Size` 分别为 `c0b7c303...f3eee`/`10934860704` 和 `cbb841a9...e43e`/`927607488`。ModelScope 返回的文件 revision 分别为 `cda69804e9a0bf6546a3adefb63a771c37e50a5d`、`276faa3e9be1b3b57954c1eec3b5e993802a880f`。
- `hf-mirror.com` 两个源码 source URL 发 `HEAD` 均返回 `200`，`Content-Length` 与 Hugging Face 相同；该镜像响应没有提供可独立使用的 SHA header，故只作为可达性/大小交叉检查，不作为权威 digest 来源。

### 比对结果

| 文件 | 源码 `catalog.go` | HF tree/API + HEAD | ModelScope API | 结论 |
|---|---|---|---|---|
| `Qwen3.8-27B-UD-IQ3_XXS.gguf` | `11,913,559,104`; `0a6129dc...d6be` | `10,934,860,704`; `c0b7c303...f3eee` | `10,934,860,704`; `c0b7c303...f3eee` | **不匹配** |
| `mmproj-F16.gguf` | `927,607,488`; `cbb841a9...e43e` | `927,607,488`; `cbb841a9...e43e` | `927,607,488`; `cbb841a9...e43e` | 匹配 |

## 实证问题：首推 27B 模型 pin 与当前源不一致

- **状态**：静态确认，基于两个独立模型源的实时只读 metadata；没有下载或哈希大文件。
- **位置**：`internal/localai/catalog.go:41-45`；源列表构造 `internal/localai/catalog.go:79-85`。
- **触发条件**：用户从模型库选择首推 `qwen3.8-27b-iq3-xxs`，任务按 catalog 对 `Qwen3.8-27B-UD-IQ3_XXS.gguf` 发起下载并按固定大小/SHA 校验。
- **实际**：当前 HF 和 ModelScope 同名文件均返回 `10,934,860,704` 字节、SHA-256 `c0b7c3038681ed2e3040456c1dd45f9858b6c2290bed172c70388a94874f3eee`；源码仍期待 `11,913,559,104` 字节、`0a6129dcbbbe72f423dc67e0e3bbfbbdf3e923981a3637687ebb96a46c59d6be`。
- **预期**：catalog 的文件名、来源、大小和 SHA 应共同指向同一可下载版本，两个可用镜像应能完成同一校验。
- **影响**：`internal/localai/manager.go:431-434` 在响应读完后要求实际写入字节数等于 catalog 大小；按当前源下载时会静态落入 `downloaded 10934860704 bytes, expected 11913559104` 的错误分支，因而不会进入正常安装。即使只修正大小，`internal/localai/manager.go:624-641` 的本地 SHA 校验仍会拒绝当前文件。模型安装/首推模型可用性因此受阻；本轮没有把该推断描述成已下载复现。
- **建议**：维护者应先确认要 pin 的具体 upstream revision/file，再同步文件名、大小、SHA 和三个 source URL；发布前用同一 revision 的 HF/ModelScope metadata fixture 检查 catalog 一致性。若确实要保留旧文件，应把旧 revision 固定到不可漂移的来源，而不是继续使用 `main`/`master`。
- **验证命令**：只读元数据核查可复用：`curl.exe -I -L https://huggingface.co/unsloth/Qwen3.8-27B-GGUF/resolve/main/Qwen3.8-27B-UD-IQ3_XXS.gguf`；`curl.exe -I -L https://huggingface.co/unsloth/Qwen3.8-27B-GGUF/resolve/main/mmproj-F16.gguf`；以及上述 HF tree、ModelScope repo files API。修订 catalog 后，应用层应另用本地 HTTP fixture 覆盖 `downloadFrom` 的大小/SHA 成功和失败分支，不需要下载真实大文件。

## 局限

- 本轮没有下载任何模型/runtime 大文件，所以没有计算本地文件 SHA，也没有运行安装任务；下载后文件是否发生 CDN 内容替换未验证。
- Hugging Face 的 `HEAD` ETag 是 Xet hash，不是文件 SHA-256；ModelScope 的 `Sha256` 与 HF LFS oid 一致，作为交叉验证，但没有把 ETag 当作 SHA。
- `b10453` 的 release API 初次 .NET 请求遇到 SSL/403 rate-limit 响应；随后无缓存的 `curl.exe` 请求成功返回 200，官方 release 页面和资产 `HEAD` 也成功。报告采用成功的官方响应，不把 403 解释为资源不存在。
- `main`/`master` 是可变分支；本报告记录了核查时的响应和 revision，不保证未来同一 URL 的元数据不变。模型固定值要获得可复现发布保证，应改用明确 revision/不可变快照并在发布流水线中验证。
