# Changelog

本文件记录 novel-mcp 的公开发行版本。公开仓库从 `v0.6.0` 开始使用干净 Git 历史；
旧 Private archive 的 tag、Release 与提交历史不导入本仓库。

## [Unreleased]

### Added

- local/public 默认允许任意浏览器 Origin（`allow_origins: ["*"]`），避免为网页 MCP Host 逐个维护域名；Host、秘密 route 与 Bearer 仍保持独立准入控制。
- 自动生成的 `config.json` 将数据目录写成相对路径 `"data": "."`，加载时相对配置文件解析，避免便携目录移动后残留安装机绝对路径。
- 正式 Release 压缩包与 Windows 开发构建加入 `portable.flag`：解压后的程序默认把配置、凭据、日志和小说项目放在同目录 `data/`，整个文件夹可直接移动或备份；单独二进制仍回退到 `~/.novel-mcp`。
- 新增 `novel-mcp doctor [--deep] [--json]` 脱敏诊断入口，可检查配置、凭据格式、服务状态和项目完整性，输出不包含 route、Bearer、项目 ID、正文或本机绝对路径。
- 新增 `<data>/logs/novel-mcp.log` 持久脱敏调用日志，记录 HTTP/MCP 调用元数据、参数形状、错误码、恢复提示和耗时，不记录 route、Bearer、Authorization、正文或完整请求体。
- 新增长篇 lifecycle scenario，覆盖多弧、多卷、弧/卷收尾、收官卷、重启恢复、完本后返工和再次完结。
- GitHub CodeQL 静态安全分析。
- 根 README 重构为简短英文入口，并新增对等的 `README.zh-CN.md`；详细写作工作流和连接/公网排障拆为中英文专题文档，portable Release 同时携带两份 README。

### Fixed

- Windows TUI 剪贴板改为直接调用 Win32 Unicode Clipboard API，不再通过 `powershell.exe` / `Set-Clipboard`，避免 Windows PowerShell 5.1 参数解析导致 `C/U/B/P` 复制失败。
- Windows Go bootstrap 改用 .NET `ZipFile` 解压，避免 Windows PowerShell 5.1 在非交互式
  Agent/CI 宿主中由 `Expand-Archive` / `Write-Progress` 触发异常。
- 返工/打磨已完成章节时，`next_step` 不再要求一个必然被跳过的 `plan_chapter`，改为直接进入返工正文→回读→检查→提交协议。
- 完本后的 `next_step` 明确指向 `reopen_book`，不再用含糊的“改设定再重开”提示。
- 完整 Tailscale smoke 成功后自动删除临时项目；Tailscale 启动/控制面异常时给出更直接的代理/TUN 排查提示。
- 首次项目规划改成两阶段 action plan：先落带 `scale` 的 premise 确定 short/mid/long，再由下一次 `next_step` 精确派发 `outline`/`layered_outline`、角色、世界规则等缺项，避免严格按 actions 执行的 Host 漏设定。
- MCP instructions、长篇提示词和运行时错误统一使用真实工具调用名，例如 `save_foundation(type=append_volume)`，不再把 `append_volume` / `complete_book` 误写成独立工具。
- 连接文档补充 Streamable HTTP / 旧 HTTP+SSE 的 `405` 识别说明，避免把客户端错误使用旧 SSE 传输误判成 route、Bearer 或服务端故障。
- `next_step.actions` 现在预填已知 `project` 和首个写动作的 `expected_revision`，并给出明确的 `expected_revision_source`；连接 instructions 要求客户端保留 `action.arguments` 再补 `required_inputs`，减少通用 Agent 丢失乐观锁/固定参数导致的无效重试。
- `commit_chapter.state_changes` 的 strict schema 明确说明首次状态或未知旧值仍需传 `old_value: null`，无原因时传 `reason: null`，避免模型误把 nullable 理解成“字段可省略”。
- 对下一步完全确定的可恢复错误增加最小 `error.recovery` 提示：revision 冲突引导重新读取路由/状态，`edit_chapter` 精确匹配失败引导先回读草稿；其余错误仍保持原有 `code/message`，不自动猜测恢复动作。
- 加固 local/public 启动与 Tailscale Funnel：产品模式固定回环监听、启动前预占本机端口、已有实例同时核验 route/Bearer；修正自定义 HTTPS 端口、双 DoH 传播窗口、重定向与跨平台恢复提示等边界行为。
- 配置、凭据与 store 原子写入统一到同目录临时文件 + sync + rename；严格拒绝未知/尾随 JSON，并修复便携配置移动、IPv6 Host/URL 和 Origin/public URL 端口校验等问题。
- MCP/诊断路径不再把项目绝对路径或自由错误文本持久化；核心工具日志保留 project 归属，写工具在“已落盘但结果编码失败”时返回最新 revision 与确定性恢复提示，避免盲重放。
- Release/公开导出脚本增加破坏性输出目录保护，Release 校验独立断言五个平台包齐全；CI 统一使用 `.go-version`，Dependabot 自动合并只接受与触发 `safety` 的 head SHA 一致的 PR。

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
