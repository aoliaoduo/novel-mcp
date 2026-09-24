# Changelog

本文件记录 novel-mcp 的公开发行版本。公开仓库从 `v0.6.0` 开始使用干净 Git 历史；
旧 Private archive 的 tag、Release 与提交历史不导入本仓库。

## [Unreleased]

### Added

- 新增 `novel-mcp doctor [--deep] [--json]` 脱敏诊断入口，可检查配置、凭据格式、服务状态和项目完整性，输出不包含 route、Bearer、项目 ID、正文或本机绝对路径。
- 新增长篇 lifecycle scenario，覆盖多弧、多卷、弧/卷收尾、收官卷、重启恢复、完本后返工和再次完结。
- GitHub CodeQL 静态安全分析。
- 根 README 重构为简短英文入口，并新增对等的 `README.zh-CN.md`；详细写作工作流和连接/公网排障拆为中英文专题文档，portable Release 同时携带两份 README。

### Fixed

- Windows Go bootstrap 改用 .NET `ZipFile` 解压，避免 Windows PowerShell 5.1 在非交互式
  Agent/CI 宿主中由 `Expand-Archive` / `Write-Progress` 触发异常。
- 返工/打磨已完成章节时，`next_step` 不再要求一个必然被跳过的 `plan_chapter`，改为直接进入返工正文→回读→检查→提交协议。
- 完本后的 `next_step` 明确指向 `reopen_book`，不再用含糊的“改设定再重开”提示。
- 完整 Tailscale smoke 成功后自动删除临时项目；Tailscale 启动/控制面异常时给出更直接的代理/TUN 排查提示。
- 首次项目规划改成两阶段 action plan：先落带 `scale` 的 premise 确定 short/mid/long，再由下一次 `next_step` 精确派发 `outline`/`layered_outline`、角色、世界规则等缺项，避免严格按 actions 执行的 Host 漏设定。
- MCP instructions、长篇提示词和运行时错误统一使用真实工具调用名，例如 `save_foundation(type=append_volume)`，不再把 `append_volume` / `complete_book` 误写成独立工具。

## [0.6.0] - 2026-09-24

首个公开发行版。

### Added

- 基于 MCP Streamable HTTP 的长篇小说写作服务，服务端不调用模型；AI 客户端负责创作与语义判断。
- 25 个 MCP tools，以及 Prompts、Resources、Resource Templates 与 completion surface。
- `next_step` 确定性工作流路由、whole-project revision 乐观并发控制，以及可恢复的 `commit_chapter` Saga。
- 项目隔离、原子持久化、checkpoint、JSONL 崩溃恢复和只读项目完整性验证。
- Windows 本机模式与 Tailscale Serve/Funnel 公网模式，以及交互式 TUI。
- Windows amd64、Linux amd64/arm64、macOS amd64/arm64 的可复现 Release 构建与校验清单。

### Security

- MCP route 与 Bearer 双凭据、严格 Host/Origin/path 检查、请求体和并发限制。
- GitHub Secret Scanning、Push Protection、最小 Actions 权限和受保护的 `main`。
- 当前树与完整 Git 历史隐私审计、本地 pre-commit/pre-push hooks。
- 公开仓库使用全新干净历史，旧私有历史与公开仓库永久隔离。

### Developer Experience

- `AGENTS.md`、模块级 Agent 指令、`docs/ARCHITECTURE.md` 与 `CONTRIBUTING.md`。
- `python scripts/check.py` 统一执行隐私审计、测试、vet、可选 race/history 检查。
- Dependabot 分组更新；完整 CI 通过的纯 patch 更新自动合并，其余更新进入 `manual-review`。

[0.6.0]: https://github.com/aoliaoduo/novel-mcp/releases/tag/v0.6.0
