# novel-mcp

[![safety](https://github.com/aoliaoduo/novel-mcp/actions/workflows/safety.yml/badge.svg)](https://github.com/aoliaoduo/novel-mcp/actions/workflows/safety.yml)
[![codeql](https://github.com/aoliaoduo/novel-mcp/actions/workflows/codeql.yml/badge.svg)](https://github.com/aoliaoduo/novel-mcp/actions/workflows/codeql.yml)

[English](README.md) · **简体中文**

一个面向 AI 小说创作的、可恢复的 MCP 工件服务。

AI 客户端负责创作推理和正文生成；`novel-mcp` 负责持久化事实：大纲、章节、摘要、校验、乐观并发、确定性路由与崩溃恢复。

```text
AI / MCP Host  ── Streamable HTTP MCP ──▶  novel-mcp  ──▶  项目文件
   思考 + 写作                                  校验 + 持久化
```

服务端**不会**调用模型，不保存模型 API Key，不在后台自动续写，不提供 shell，也不接受任意宿主文件路径。

## 为什么用它

- **长篇连续性**：章节、卷弧、摘要、世界状态、Story Compass 都落盘，不只存在聊天上下文里。
- **下一步可确定**：`next_step` 返回机器可读 action plan，客户端不需要猜下一件工具该调什么。
- **重试更安全**：整本项目的 `revision` 会拒绝陈旧写入，避免超时重放导致正文重复追加。
- **崩溃可恢复**：章节提交中断后从冻结载荷继续，不靠会话记忆重建事实。
- **公网或本机均可**：可只监听回环，也可通过 Tailscale Funnel 暴露回环服务。

## 快速开始

1. 从 **GitHub Releases** 下载 Windows ZIP，解压后双击其中的 `novel-mcp.exe`。
2. 首次启动选择 **网页 / 云端 AI 客户端** 或 **仅本机 AI 客户端**。
3. TUI 就绪后按 **`C`**，复制完整 MCP 配置。
4. 粘贴到 MCP 客户端。连接后先调 `list_projects`，选定项目后调 `next_step` 开工。

网页/云端模式默认使用 Tailscale Funnel + Bearer；本机模式只监听 `127.0.0.1`。

官方 Release 是便携包：请把解压后的目录整体保留。`portable.flag` 存在时，配置、凭据、日志和小说项目都会写到 `novel-mcp` 程序旁的 `data/`；整个文件夹可以直接移动或备份。自动生成的 `config.json` 使用相对数据路径 `"data": "."`，不会把安装位置写死。单独复制一个没有 `portable.flag` 的二进制时，才回退到用户目录下的 `~/.novel-mcp`。

local/public 默认 `allow_origins: ["*"]`，因此不需要为 ChatGPT、Arena 或其他网页 MCP Host 逐个维护域名；公网访问仍必须持有秘密 route 与 Bearer。

启动或连接异常时先运行：

```bash
novel-mcp doctor
novel-mcp doctor --deep
```

`doctor` 只读且主动脱敏，不输出 route、Bearer、公网 hostname、项目 ID、小说正文或本机绝对路径，结果可以较安全地贴给 AI 或 Issue 排查。

实际运行时还会在 `<data>/logs/novel-mcp.log` 追加脱敏 JSONL 调用日志；portable 包中就是 `data/logs/novel-mcp.log`。日志便于事后定位网页 AI 的真实调用顺序、错误码和耗时，不保存正文、route、Bearer 或完整请求体。

## 常用命令

| 命令 | 作用 |
| --- | --- |
| `novel-mcp local` | 本机回环模式，Bearer + TUI |
| `novel-mcp public` | Tailscale Funnel/Serve 预检并启动 MCP |
| `novel-mcp url` | 打印当前 MCP URL |
| `novel-mcp prompt` | 打印连接片段与初始提示词 |
| `novel-mcp doctor [--deep] [--json]` | 脱敏诊断 |
| `novel-mcp token rotate --i-understand-this-invalidates-current-clients` | 轮换 route + Bearer |
| `novel-mcp version` | 打印版本 |

## AI 写作主循环

控制流程刻意保持很小：

```text
next_step → 执行返回的 actions → next_step → ... → done=true
```

`next_step.result.actions` 会给出工具名、服务端已知参数、模型仍需补齐的输入、依赖、revision 来源，以及互斥 choice group。

新项目分两轮规划：第一轮只选择篇幅级别，并保存一个带 `scale=short|mid|long` 的 premise 种子；随后立刻再次调用 `next_step`，由服务器根据已落盘 tier 返回准确的 `outline` 或 `layered_outline` actions。客户端不应在第一轮自行猜完后续设定形态。

正常写新章：

```text
novel_context → plan_chapter → draft_chapter → read_chapter
→ check_consistency → 必要时语义修订 → commit_chapter
```

返工已完成章节时不会重新 `plan_chapter`，而是从当前终稿/context 开始。

详细 action plan、revision、恢复、卷弧路由和错误处理见 [写作工作流](docs/WORKFLOW.zh-CN.md)。

## 安全模型

公网端点由随机 route capability 与默认开启的独立 Bearer 双重保护；服务保持精确 Host 门禁，Origin 默认允许任意网页，并刻意保持很小的 HTTP 暴露面。

项目只能通过受限 project ID 访问，远端不能提交任意宿主路径；没有 shell/exec，也没有模型凭据接口。

需要暴露到回环以外前先读 [SECURITY.md](SECURITY.md)。启动、公网接入和 Tailscale 排障见 [连接与公网指南](docs/CONNECTIVITY.zh-CN.md)。

## 文档

| 主题 | 中文 | English |
| --- | --- | --- |
| 写作工作流 / MCP 行为 | [WORKFLOW.zh-CN.md](docs/WORKFLOW.zh-CN.md) | [WORKFLOW.md](docs/WORKFLOW.md) |
| 启动 / 公网接入 / 排障 | [CONNECTIVITY.zh-CN.md](docs/CONNECTIVITY.zh-CN.md) | [CONNECTIVITY.md](docs/CONNECTIVITY.md) |
| 架构与不变量 | [ARCHITECTURE.md](docs/ARCHITECTURE.md) | 同一文档 |
| 安全模型 | [SECURITY.md](SECURITY.md) | 同一文档 |
| 参与开发 | [CONTRIBUTING.md](CONTRIBUTING.md) | 同一文档 |
| 编码 Agent 规则 | [AGENTS.md](AGENTS.md) | 同一文档 |
| 正式发布 | [docs/RELEASING.md](docs/RELEASING.md) | 同一文档 |
| 版本变化 | [CHANGELOG.md](CHANGELOG.md) | 同一文档 |

## 开发

新 clone 先启用仓库隐私 hooks：

```bash
git config core.hooksPath .githooks
```

使用仓库声明的 Go 版本。Windows 没有全局 Go 时：

```bat
scripts\bootstrap-go.cmd
scripts\build-dev.cmd
```

统一检查入口：

```bash
python scripts/check.py
python scripts/check.py --race --history   # 高风险 / 发布前改动
```

更完整的开发规则见 [CONTRIBUTING.md](CONTRIBUTING.md) 和 [AGENTS.md](AGENTS.md)。

## 上游

小说工件层源自 Apache-2.0 的 `ainovel-cli`。复用边界与来源说明见 [UPSTREAM.md](UPSTREAM.md)。
