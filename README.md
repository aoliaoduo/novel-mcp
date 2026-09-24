# novel-mcp

[![safety](https://github.com/aoliaoduo/novel-mcp/actions/workflows/safety.yml/badge.svg)](https://github.com/aoliaoduo/novel-mcp/actions/workflows/safety.yml)

供**网页 MCP 客户端**（网页版 AI、浏览器里的 Agent）调用的小说创作服务。服务器只做一件事：
把小说工件、校验与断点恢复暴露成 MCP 工具；**创作推理由网页 AI 完成，这里不调用任何模型**。

它适合把“长篇小说写作”拆成可恢复、可验证、可并发保护的 MCP 工作流：AI 负责想和写，
novel-mcp 负责记住事实、守住状态、拒绝冲突，并在进程或会话中断后从磁盘继续。

核心逻辑来自 [ainovel-cli](UPSTREAM.md)（Apache-2.0）的工件层：原子写入、checkpoint、
PendingCommit Saga、阶段守卫原样复用，没有第二套业务实现，也没有后台写作引擎。

```
网页 AI ──MCP over HTTP──▶ novel-mcp ──▶ projects/<id>/{book,outline,chapters,meta,...}
   （推理、语义判断）           （事实、校验、持久化）
```

### 项目入口

| 你想做什么 | 从这里开始 |
| --- | --- |
| 直接使用 | [快速开始](#1-快速开始) |
| 理解系统设计 | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| 参与开发 | [CONTRIBUTING.md](CONTRIBUTING.md) |
| 让编码 AI/Agent 修改项目 | [AGENTS.md](AGENTS.md) |
| 查看版本变化 | [CHANGELOG.md](CHANGELOG.md) |
| 查看安全模型 | [SECURITY.md](SECURITY.md) |
| 了解上游来源 | [UPSTREAM.md](UPSTREAM.md) |
| 发布版本 | [docs/RELEASING.md](docs/RELEASING.md) |

编码 Agent 不需要先通读整份 README：先读 `AGENTS.md`，再按任务进入对应目录；
`internal/server`、`internal/store`、`internal/tools` 还有局部 `AGENTS.md`。

## 1. 快速开始

### 普通用户：直接下载 Release

如果仓库的 **GitHub Releases** 已有正式版本，不需要安装 Go，也不需要下载源码，直接下载与你系统匹配的资产：

- Windows x64：优先下载 `novel-mcp-vX.Y.Z-windows-amd64.exe`，下载后直接双击；
- Windows portable：下载 `novel-mcp-vX.Y.Z-windows-amd64.zip`，解压后双击 `novel-mcp.exe`；
- Linux：下载对应 `linux-amd64` / `linux-arm64` 的 `.tar.gz`；
- macOS：下载对应 `darwin-amd64` / `darwin-arm64` 的 `.tar.gz`。

每个正式 Release 都附 `SHA256SUMS.txt` 与 `RELEASE-MANIFEST.json`。portable 包内还有
`START_HERE.txt`、README 和 LICENSE。当前 Windows/macOS 二进制**尚未代码签名**，系统可能提示未知发布者；
请只从本仓库 Release 下载，并在需要时先核对 SHA-256。

如果 Releases 页面暂时为空，表示当前公开历史还没有发布正式版本；不要从旧 Private archive
搬运历史 Release，开发者可按下节从源码构建。

Windows 第一次双击后只需要选一次“网页/云端 AI 客户端”或“仅本机 AI 客户端”；服务就绪后按
**`C`** 复制完整 MCP 配置，粘进 AI 客户端即可。

### 开发者：从源码构建

新 clone 第一次开发前先启用仓库自带的隐私保护 hooks：

```bash
git config core.hooksPath .githooks
```

这些 hooks 会在 commit 前检查当前可发布树和 GitHub noreply 提交邮箱，并在 push 前扫描完整
Git 历史。不要用个人邮箱提交到这个公开仓库，也不要用 `--no-verify` 绕过隐私检查。

先准备 Go 工具链（二选一；`go.mod` 里的 `go` 行是最低版本要求）：

- 本机有 Go：`go version` 够新就直接用，下面命令里的 `go` 原样敲。
- 本机没有：双击跑一次 `scripts\bootstrap-go.cmd`（免安装、免管理员；
  把 pinned 工具链下到 `.toolchain/`，该目录不进仓库）。`build-dev.cmd` 会自动优先使用它。

Windows 开发构建统一用：

```bat
scripts\build-dev.cmd
```

唯一输出是 `dist\novel-mcp.exe`。测试最新代码时就双击这个文件；不要从 `.local/`、根目录或
历史 release 目录找 EXE。第一次只选一次“网页/云端客户端”或“仅本机”，以后会记住模式和配置。

终端等价用法：

```bat
dist\novel-mcp.exe local                   # 一键本机模式：127.0.0.1 + Bearer + TUI
dist\novel-mcp.exe public                  # 一键公网模式：Tailscale Funnel + Bearer + TUI
dist\novel-mcp.exe url                     # 打印上次启动模式对应的 MCP URL
dist\novel-mcp.exe prompt                  # 打印完整 MCP 配置片段与初始提示词
```

首次启动会在数据目录生成随机凭据（`credentials.json`，0600）。TUI 就绪后最省事的用法是
按 **`C`**：直接把完整 Streamable HTTP MCP 配置复制到剪贴板，粘进 AI 客户端即可。
`U` 只复制 URL、`B` 复制 Bearer、`P` 复制初始提示词、`O` 打开健康检查；连接 route 与
Bearer **默认遮罩**，按 `V` 才临时显示，避免截图时顺手泄露凭据。

### 仓库目录约定

| 路径 | 用途 |
| --- | --- |
| `cmd/` | 可执行程序入口；`novel-mcp` 是主程序，`tailnet-smoke` 是公网环境验证工具 |
| `internal/` | MCP、工具、存储、路由、TUI 等生产代码 |
| `assets/` | 编译进二进制的 Prompts / References / Voice |
| `scripts/` | 构建、工具链、Tailscale 修复和发布安全审计；见 `scripts/README.md` |
| `docs/` | 专项文档：公开仓库检查与正式发布流程 |
| `dist/` | **唯一的本机可运行开发构建出口**，不提交 Git |
| `.local/` | 测试依赖/临时缓存，不放用户要双击的程序，不提交 Git |
| `.toolchain/` | 可选的本地 Go 工具链，不提交 Git |

正式历史版本从 GitHub Releases 获取，不在仓库目录长期保存 release 二进制副本。
维护者的 tag/release 流程见 [`docs/RELEASING.md`](docs/RELEASING.md)。

想要"只贴一条 URL、不带任何 header"的形态（很多网页客户端还不会发自定义头）：

```bash
./novel-mcp serve --bearer=false           # 仅限回环：这条 URL 本身就是凭据
```

### 命令

| 命令 | 说明 |
| --- | --- |
| `novel-mcp serve` | 启动服务。首次运行生成凭据，之后每次启动读取同一份（URL 不变） |
| `novel-mcp local` | 一键本机：只监听 `127.0.0.1`，不要求 Tailscale，进入同一套实时 TUI |
| `novel-mcp public` | 一键公网：Tailscale 检查→Funnel/Serve→DNS→MCP 服务；启动过程和错误都在 TUI 内展示 |
| `novel-mcp url` | 自动读取上次本机/公网模式并打印接入 URL；Bearer 关闭时提醒“URL 即凭据” |
| `novel-mcp token rotate --i-understand-this-invalidates-current-clients` | 轮换路由与 Bearer，旧 URL 立刻失效 |
| `novel-mcp version` | 版本号 |
| `novel-mcp prompt` | 打印连接 MCP 的初始提示词（配置+URL+Bearer+验证步骤） |

### 参数与配置文件

`serve` 的 flag：`--host` `--port` `--data` `--public-url` `--allow-origin`（可重复）
`--bearer=<true|false>` `--insecure-allow-open` `--config <file>`；同名 JSON 配置键见下：

```json
{
  "host": "127.0.0.1",
  "port": 8765,
  "data": "~/.novel-mcp",
  "public_url": "https://xxxx.ngrok-free.dev",
  "allow_origins": ["https://arena.ai"],
  "require_bearer": true,
  "startup_mode": "public"
}
```

放置规则：

* 数据目录下有 `credentials.json`、`config.json`（一键启动模式写入）与 `projects/<项目 ID>/`。备份就是拷这个目录（凭据也在里面，妥善保管）。
* 想从公网（隧道/反代）访问：必须同时给出 `--public-url`（HTTPS 源）与开启 Bearer；
  `--host 0.0.0.0` 还需要显式 `--insecure-allow-open`。少一样就拒绝启动——这是刻意的。

## 2. 工具

共 **25** 个（16 个上游原语 + 9 个工程工具）。`novel_context`、`read_chapter` 与
`check_consistency` 是纯读；其余上游写调用会落盘 checkpoint / 进度，因此要求
`expected_revision`。

MCP 契约层为全部工具同时声明 `inputSchema` 与 `outputSchema`，成功结果通过
`structuredContent` 返回；工具还声明 title、read-only / destructive / idempotent / closed-world
annotations，供 Host 更准确地展示和调度。

开发阶段只支持当前 MCP 协议 **`2026-07-28`**。服务端不维护旧协议握手、旧参数 schema 或
其他向后兼容 shim；客户端也应使用支持当前协议的 SDK/Host。

MCP 原生 surface 不只 tools：当前同时暴露 **5 个 Prompts** 与 **1 个固定 Resource + 4 个
Resource Templates**，支持对应 surface 的 Host 可以直接使用协议原生入口。

Prompts：`novel_overview` `novel_architect_short` `novel_architect_long` `novel_writer`
`novel_editor`。它们与 `novel_guide` 共用同一份内嵌协议文本，不存在两套提示词。

Resources：

* `novel://projects`：项目列表；
* `novel://project/{project}/status`：项目状态；
* `novel://project/{project}/context` 与 `/context/{chapter}`：全局/章节上下文；
* `novel://project/{project}/chapter/{chapter}/{source}`：章节 `final` / `draft` 原文。

Resources 只接受项目 ID 和受限参数，不接受宿主路径；底层仍走与 tools 相同的项目安全门和并发锁，
且 Resource 读取不会写 checkpoint 或改变项目 revision。

支持 MCP `completion/complete` 的 Host 还可以直接补全 Resource Template 参数：`project` 从当前
项目列表补全，`chapter` 只暴露真实已展开/已发生章节，`source` 补全 `draft` / `final`。补全过程
纯读，不会把长篇未展开弧的内部章节容量估算暴露给客户端。

工程工具（本仓库新增）：

| 工具 | 作用 | 是否需要 revision |
| --- | --- | --- |
| `list_projects` | 列出项目（只回 ID、需求、文风、创建时间） | 否 |
| `create_project` | 新建项目；同名一律拒绝，不覆盖 | 否 |
| `delete_project` | 删除整本项目；不可逆，须带最新 revision | 要 |
| `reopen_book` | 把完本书重开为返工态；队列排空后自动重新完结，须带最新 revision | 要 |
| `next_step` | 问路由：下一步 exact 干什么；主循环就是反复调它 | 否 |
| `project_status` | 阶段、进度、缺失设定、未完成提交、一致性告警 | 否 |
| `verify_project` | 全量只读核验终稿、接纳记录、摘要、派生世界状态、字数投影、事实链与 pending commit；只诊断不修复 | 否 |
| `novel_guide` | 按需取回上游创作协议与模板（overview/architect/architect_long/writer/editor） | 否 |
| `export_book` | 导出已提交正文为 TXT 版式（《书名》→卷分隔→“第 N 章 标题”），每次最多 50 章、上限 2 MiB，未完成跳过进 skipped | 否 |

上游原语（`ainovel-cli` 原样，仅包住寻址、并发与错误信封）：

`novel_context` `save_book` `save_foundation` `audit_foundation` `plan_chapter`
`draft_chapter` `edit_chapter` `read_chapter` `check_consistency` `commit_chapter`
`revise_outline` `resolve_outline_feedback` `expand_next_arc` `save_review`
`save_arc_summary` `save_volume_summary`

其中三点容易被误读，这里说清楚：

* `check_consistency` **只**加载对照资料，不写 checkpoint，也不证明情节没有矛盾；
  语义判断是网页 AI 的职责。
* `read_chapter` 与 `check_consistency` 都是纯读，不接受也不要求 `expected_revision`。
* `commit_chapter` 的 `feedback`（偏离/建议）会进 `writer_feedback` 池：普通反馈不打断续写，留到下一次结构操作统一吸收；确认后续计划仍适用时调 `resolve_outline_feedback`（带 reason）消费，需要改大纲时调 `revise_outline`。

## 3. 写作协议（给网页 AI 的约定）

主循环：调 `next_step` → 按返回的 agent/task 执行 → 调 `next_step`，直到 `done=true`。
路由返回的 task 会点名 exact 调哪些工具、按什么顺序；下面是 task 点名时用的固定顺序：

`next_step.result.actions` 同时提供机器可读的调用计划：每项包含稳定的 `id`、`tool`、服务端已确定的
`arguments`、仍需模型补齐的 `required_inputs`、`depends_on`、`requires_revision`、
`revision_source`、`purpose`、`mode` 与 `choice_group`。写动作的 `revision_source=plan` 表示使用
本次 `next_step` 外层的 revision；值为 `aN` 时使用对应前序 action 返回的 revision；纯读为
`none`。`mode=choice` 时，同一 `choice_group` 内的动作互斥，只能选择其中一个。

若某个动作读写的工件同时有 MCP Resource，action 还会带 `resource_uri`；`next_step` 自身会给
`context_resource_uri`。`novel_context`、单章 `read_chapter`、`draft_chapter`、`edit_chapter`、
`commit_chapter` 和 `project_status` 的结果也会回对应 `resource_uri`。只支持 Tools 的 Host 可以
完全忽略这些 URI；支持 Resources 的 Host 不必再自行拼 `novel://...`。

开书：

```
list_projects → create_project → novel_guide → save_book
  → save_foundation(type=premise/outline/characters/world_rules…)
  → novel_context（取 foundation fingerprint）→ audit_foundation（ready=true 才进写作）
```

每章：

```
novel_context(chapter) → plan_chapter → draft_chapter → read_chapter(source=draft)
  → check_consistency → （你自己做语义修订，需要就 edit_chapter）→ commit_chapter
```

正文一改就要重新回读与检查；`commit_chapter` 之后才有终稿。

### revision：乐观锁，不是版本号

每次调用都返回整本项目的 `revision`（全树内容 sha256）。写工具必须带上你最近看到的
`expected_revision`；不一致就回 `REVISION_CONFLICT` 且**不落盘**。这不是仪式：
网页客户端最容易犯的错是超时后盲目重放，`draft_chapter(mode="append")` 重放一次，
正文就多一份。

拿不准时的正确动作：先 `project_status` 或 `novel_context` 读最新 `revision`，
再决定是重放还是改参数。

### 错误码

成功与失败的信封形状一致：`{"project":…, "revision":…, "result":…}` 或
`{"project":…, "revision":…, "error":{"code":…,"message":…}}`，失败结果的 `revision`
是最新值（失败也可能已经落盘，比如写入后 checkpoint 才失败）。

| code | 含义 | 该怎么做 |
| --- | --- | --- |
| `REVISION_CONFLICT` | 磁盘在你上次读之后变了 | 重读，再决定是否重放 |
| `CONFLICT` | 上游判定的冲突（如审计 fingerprint 过期、存在别的未完成提交） | 按 message 提示重读或先恢复 |
| `PRECONDITION_FAILED` | 阶段/前置条件不满足（如没审计就写章） | 补齐顺序，别重试同一调用 |
| `INVALID_REQUEST` | 参数问题 | 改参数 |
| `STORE_ERROR` | 读写磁盘失败 | 检查数据目录 |
| `NOVEL_TOOL_FAILED` | 上游工具的其他执行失败 | 读 message |
| `PROJECT_NOT_FOUND` / `PROJECT_DAMAGED` / `PROJECT_UNSAFE` | 项目不存在 / 未初始化或损坏 / 含符号链接或超配额 | 换项目或人工检查 |
| `PROJECT_EXISTS` / `PROJECT_LIMIT` | 重名创建 / 项目数达上限 | 换 ID 或清理 |

当前仍处于开发阶段，项目数据格式只支持当前版本：`meta/format.json` 缺失或版本不匹配都按
`PROJECT_DAMAGED` 处理，不提供旧格式兼容或迁移。开发期数据如果失效，直接删除项目后重建。

### 恢复：读事实，不读会话

服务重启、网页会话断掉、进程被杀之后：

1. `next_step`：路由优先指出 `pending_commit`（若有，按同一章节重放 `commit_chapter`——
   上游会用落盘的冻结载荷收尾，不要用聊天记忆重建正文）。
2. 要看全貌再调 `project_status`（`phase`、`foundation_missing`、`warnings`）。
3. 按路由返回的 agent/task 执行，调 `next_step` 继续。

没有"恢复会话"这个动作：恢复的永远是磁盘上的事实。

## 4. 边界与限制

* 不给 shell、不给任意文件读写、不给模型密钥接口；项目内容一律当作待处理数据。
* 只按项目 ID 寻址（`^[a-z][a-z0-9-]{0,47}$`，且拒 Windows 保留名），不接受宿主路径。
* 单本上限 256 MiB / 20000 个文件；最多 100 个项目；请求体上限 4 MiB；并发 16。
* 服务器不调用模型：断线后不会有任何"后台续写"。

### 公开仓库隐私安全

本仓库已经公开，因此当前工作树、完整 Git 历史、提交邮箱、tags 和 Releases 都按永久公开面处理。
开发时 `pre-commit` 会运行 `python scripts/public-audit.py`，`pre-push` 会运行
`python scripts/public-audit.py --history`；GitHub Actions 也会在每次 push / PR 复查当前树与完整历史。
旧 Private archive 的 branch、tag、Release 或其他 ref 不得导入本仓库。完整规则见
[`AGENTS.md`](AGENTS.md) 与 [`docs/PUBLIC_RELEASE.md`](docs/PUBLIC_RELEASE.md)。

## 5. 启动与公网接入

### 双击启动（日常推荐）

Windows 下直接双击 `dist\novel-mcp.exe`。**第一次启动**会先出现两项选择：

- **网页 / 云端 AI 客户端**：推荐，走 Tailscale Funnel，得到公网 HTTPS MCP 地址；
- **仅本机 AI 客户端**：不使用 Tailscale，只监听 `127.0.0.1`。

选择会写进 `~/.novel-mcp/config.json`，以后双击直接复用上次模式、端口和相关配置。
需要切换时可直接运行 `novel-mcp local` 或 `novel-mcp public`，新选择会成为后续双击默认值。

启动不是一屏滚动日志：交互终端中会显示进度页。公网模式依次显示
`Tailscale → 连接配置 → Funnel/Serve → 公网 DNS → MCP 服务`；本机模式显示
`连接配置 → MCP 服务`。某一步失败时停在对应错误页并保留具体修复提示。

运行页分为 **连接 / 事件 / 项目** 三页：

- 连接页：显示就绪/客户端连接状态；`C` 复制完整 MCP 配置，`U/B/P` 分别复制 URL、Bearer、初始提示词，`O` 打开健康检查，`V` 显示/隐藏凭据；
- 事件页：实时请求记录；按 `E` 在全部事件与仅错误之间切换；
- 项目页：显示项目阶段、当前章节进度和待返工数量；项目状态每 5 秒轻量刷新。

凭据默认遮罩只是防止误截图；按 `C/U/B` 复制到剪贴板后，剪贴板内容本身仍包含真实凭据，使用后注意不要粘贴到公开位置。

### 公网模式：Tailscale Funnel

novel-mcp 本体只监听回环（`127.0.0.1:8765`，不开公网端口、不加防火墙规则）；
`tailscale funnel` 用真实的 `*.ts.net` 证书终结 TLS，把
`https://<节点>.<tailnet>.ts.net/` 代理进来，同时发布到公网（仅 443）。
整条链内置在 exe 里，双击即跑。

前置：本机 Tailscale 已登录（`tailscale status` 的 `BackendState` 为 `Running`）。
若没登录或卡在 "Tailscale is starting"，先跑一次 `scripts\tailscale-repair.cmd`
（会请求 UAC：先起托盘进程，必要时重启服务并打开登录链接）。

终端里显式运行 `novel-mcp public` 与双击后选择“网页 / 云端 AI 客户端”相同：
检查 tailnet → 写 `~/.novel-mcp/config.json`（把 ts.net 源写进 Host 白名单）
→ Funnel 挂载（已挂好就不动）→ DoH 实测公网 A 记录 → 启动本机 MCP 服务 → 进入运行 TUI。
`--no-tui` 则保留适合脚本/管道的纯文本输出。

```
https://<主机名>.<你的tailnet>.ts.net/mcp/<64位路由>
```

最推荐按 `C` 复制完整客户端配置；也可手工使用上面的 URL 与 Bearer。停止：在 TUI 按 `q` / `Ctrl+C`，或关窗口（Funnel 挂载保留，下次直接复用）；
也可以 `Get-Process novel-mcp | Stop-Process`。撤销对外暴露：`tailscale funnel --https=443 off`。

`novel-mcp public` 常用 flag：

| flag | 作用 |
| --- | --- |
| `--tailnet-only` | 只做 `tailscale serve`（仅 tailnet 内可连），不发布公网 |
| `--allow-public-url-token-only` | 与 `--bearer=false` 联用：明确同意发布无 Bearer 的公网端点 |
| `--allow-origin https://…`（可重复） | 跨源白名单；默认 arena.ai 四个源 |
| `--smoke` | 结尾加跑轻量公网冒烟（只读：错路由 404 / healthz / MCP 握手 / 工具清单） |
| `--dry-run` | 只读侦察：不写配置、不挂载、不起服务 |
| `--no-tui` | 不进 TUI，只打印静态连接卡（管道/重定向下自动如此） |
| `--port` / `--serve-port` / `--data` | 服务端口 / funnel 的 https 端口 / 数据目录 |

三个刻意的设计：

* **幂等**：Funnel 已正确挂载时不再跑 `funnel --bg`（重跑会重启 Tailscale 那套异步的公网
  DNS 发布，之前就是栽在这上面），只报告不动它。
* **单实现**：整条链收进 `internal/public`，exe 自己就是启动器，不再有第二套脚本
  （原 `scripts/tailscale/*.ps1` 已合并进来并移除，见 git 历史）。
* **默认不带自测**：日常启动不跑 smoke；`--smoke` 也只是只读轻量版，不会自造
  `smoke-*` 项目。完整写作闭环自测用 `cmd/tailnet-smoke/main.go`（见 §6）。

### 客户端里填什么

| 项 | 值 |
| --- | --- |
| URL | `https://<主机名>.<你的tailnet>.ts.net/mcp/<64位路由>` |
| 自定义 header | `Authorization: Bearer <token>`（默认必需；token 由启动窗口打印） |
| 健康检查 | `https://<主机名>.<你的tailnet>.ts.net/healthz` → `{"ok":true,"auth":"bearer"}` |

注意：

* 这是**公网地址**，靠 64 位路由 + Bearer 两把钥匙。URL 和 token 一起泄露就等于把写作
  工位交出去，别贴进公开频道。funnel 下关掉 Bearer 必须显式加 `--allow-public-url-token-only`，
  否则直接拒绝启动。
* 客户端得能发自定义 header。若它只能填 URL，就退回 tailnet-only（`--tailnet-only`）；
  确实要在公网用又发不了 header，才考虑 token-only（等于承认 URL 本身就是凭据）。
* 浏览器页面里发起的跨源请求会被 Origin 白名单拦下，用 `--allow-origin` 把该页面的源加进来。
* 443 上 serve 与 funnel 不能共存：**最后配置的那个赢**。跑过 funnel 之后再跑 `--tailnet-only`，
  端口就变回仅 tailnet 可用。

几个本机踩过的坑：

* tailscaled 可能一直停在 `NoState` + "Tailscale is starting"：本机实测控制面是通的
  （TLS 用的是真 Let's Encrypt 证书、HTTPS GET 返回 200），卡住的是状态机——**托盘进程
  `tailscale-ipn.exe` 没在跑**。启动时会自动拉它（免 UAC）；拉不起来再跑 `scripts\tailscale-repair.cmd`
  （先起托盘进程，必要时重启服务，最后才走登录）。
* `tailscale funnel` 打印 "Funnel on" 只代表本地 serve 配置写好了，名字是否真的出现在公网 DNS
  里是另一回事。funnel node attribute 由默认策略文件授予（`autogroup:member`），本机不需要
  改 ACL；仅凭 CLI 输出无法区分"配置已写"和"已发布"，所以启动时会用
  1.1.1.1 / dns.google 的 DoH 实测公网 A 记录。
* 即便一切就绪，**公网 DNS 记录最长要 10 分钟才出现**（官方排错文档明说）。看到 NXDOMAIN
  先等 10 分钟再判断；重跑 `funnel --bg` 会让这个等待重新开始——反复 `off`/`on` 是最常见的
  自坑方式。
* `*.ts.net` 的公网 A/AAAA 记录**只在该节点此刻确实挂着 Funnel 时存在**：`funnel off`（或
  别的工具在同一端口接管）之后 Tailscale 会把记录撤掉。所以这个域名 NXDOMAIN 本身不代表
  出故障——它只说明现在没有 Funnel 挂在它下面，重新挂载后要等它重新发布。
* 等满 10 分钟、权威 NS 直查仍是 NXDOMAIN（`dig @ns1.dnsimple.com <名字> A`），那基本不是
  DNS 传播，而是这套 Funnel 配置没有随节点重连登记到中继侧：**在桌面客户端里退出账号再登录
  一次**即可重新走一遍 ingress 注册。本机实测——重新登录后数秒内公网 A/AAAA 记录就位
  （`185.40.234.x` / `2a00:dd80:20::`），全程不需要动 ACL、证书或 serve 配置。
  启动时的 DoH 探测失败也会按这个顺序提示。
* 本机开着 Clash Party / mihomo（监听 `127.0.0.1:7890`，系统代理已启用）。若登录反复失败，
  给 `*.tailscale.com`、`*.ts.net`、`*.tailscale.io` 加 DIRECT 规则，或临时关掉 DNS 劫持。
* 若你的 MCP 客户端是**服务端**发起调用（厂商云端的 Agent 去连你的 URL），它不在你的 tailnet
  里：确认跑的是默认 funnel 模式（`--tailnet-only` 下它连不上），或用 ngrok 之类隧道，
  并开启 Bearer。

## 6. 测试

日常开发优先跑统一入口：

```bash
python scripts/check.py
python scripts/check.py --race --history   # 高风险/发布前改动
```

需要只跑某一层时再直接使用 Go 测试命令：

```bash
go test ./...                    # 单元 + 进程内端到端（11 个上游包 + internal/server）
NOVEL_MCP_E2E=1 go test ./internal/server -run TestEndToEnd -v
                                 # 起真二进制、真 HTTP、官方 Go SDK 客户端，
                                 # 走完 开书→设定→审计→写章→校验→提交→导出，
                                 # 再杀进程重启验证恢复；并验证 Bearer/Host/Origin 门禁

npm install --prefix .local/mcp-ts-sdk --save-exact @modelcontextprotocol/client@2.0.0 @modelcontextprotocol/core@2.0.0
NOVEL_MCP_TS_E2E=1 go test ./internal/server -run TestTypeScriptSDKCurrentProtocol -v
                                 # 可选：起真二进制，用官方 TypeScript SDK v2 + 当前协议做第二实现黑盒验证；
                                 # 覆盖 tools/prompts/resources/completion；依赖只装进 gitignore 的 .local

python scripts/public-audit.py   # 只跑当前可发布树隐私审计
```

`internal/server` 测试 tools 的 output schema、Prompts 与 Resources 契约。`scripts/mcp-ts-smoke.mjs` 也可直接
对任意正在运行的 novel-mcp URL 做 TypeScript SDK smoke。

公网侧必须单独验：本机的 `https://<名字>.ts.net/...` 请求会走 tailnet 内部路径，**同机自测
通过不等于公网可达**。保留一套官方 Go SDK 的完整公网闭环工具：
`cmd/tailnet-smoke/main.go`（`go run ./cmd/tailnet-smoke -url <完整 URL>`）。它可以从 tailnet 外的
机器发起 MCP 握手，走完开书→审计→写作→提交→导出，不再维护第二套 Python 公网 loop。

这属于环境验证（依赖真实公网/tailnet，不进 `go test`）。
它会创建名为 `smoke-<时间戳>` 的项目并留在数据目录里；可用 `delete_project` 带最新
revision 删除，或停服后手动清理 `~/.novel-mcp/projects/smoke-*`。

安全模型与威胁边界见 [SECURITY.md](SECURITY.md)；与上游的关系见 [UPSTREAM.md](UPSTREAM.md)。
公开仓库的 Git 隐私门禁、旧 Private archive 边界与历史审计见
[docs/PUBLIC_RELEASE.md](docs/PUBLIC_RELEASE.md)。
