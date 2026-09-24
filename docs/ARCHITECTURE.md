# Architecture

本文给维护者和编码 Agent 一个稳定的项目心智模型。它描述“哪些边界不能破”，而不是逐文件复述实现。

## 目标

novel-mcp 是一个 **MCP over HTTP 的小说工件服务**。网页/云端 AI 负责创作推理、语义判断和文本生成；服务器负责事实、校验、持久化、确定性路由、并发控制与断点恢复。

```text
AI / MCP Host
    │
    │ Streamable HTTP MCP
    ▼
cmd/novel-mcp
    │
    ├── internal/server  ── MCP/HTTP/项目隔离/资源与提示词
    │        │
    │        ├── internal/flow   ── 确定性 next_step 路由
    │        ├── internal/tools  ── 小说业务工具
    │        └── internal/store  ── 原子持久化与恢复数据
    │
    ├── internal/public ── Tailscale Serve/Funnel 接入
    └── internal/tui    ── 本机启动与运行状态 UI
```

## 核心边界

### 1. 服务端不调用模型

生产路径没有模型 provider、模型 API key、后台 Worker 或“自动续写”引擎。任何需要创造性判断的内容都由 MCP 客户端提供。

如果一个需求看起来需要“服务器自己想一下”，优先把它表达成：

- 服务器返回结构化事实/context；
- 客户端 AI 产出语义结果；
- 服务器验证并持久化结果。

### 2. `next_step` 是确定性调度器

`internal/flow` 从磁盘事实加载 State，再由纯路由逻辑决定下一步。`next_step` 返回 machine-readable actions：tool、arguments、required inputs、依赖、revision 来源、choice group 等。

不要在这里引入模型判断，也不要把客户端 action plan 重新实现成第二套后台 agent。

### 3. 写入使用 whole-project revision

每次项目调用返回 whole-project `revision`（项目树内容指纹），不是递增版本号。写工具要求 `expected_revision`；不匹配时返回 `REVISION_CONFLICT` 并拒绝写入。

这用于防止并发客户端和超时重试造成静默覆盖或重复 append。

### 4. `commit_chapter` 是可恢复 Saga

章节提交不是单次文件写入。第一次执行会冻结结构化提交载荷和 draft snapshot，并逐阶段推进 pending commit。崩溃后恢复必须重放冻结事实，而不是依赖聊天上下文重建正文。

修改 commit/recovery 代码时重点验证：

- 阶段推进可重复；
- 同一 pending commit 重放不会重复副作用；
- frozen payload 不被新的调用参数偷偷替换；
- checkpoint / progress / signals / summaries 最终收敛到一致状态。

### 5. 项目文件系统是能力边界

远端 API 只接受 project ID，不接受宿主 `path` / `dir` / `file`。项目 root 在服务启动时固定；项目遍历拒绝符号链接、非普通文件和超配额目录。

不要为了便利新增“读某路径”“导入任意文件”“执行 shell”这类 MCP 工具。

## MCP Surface

当前只支持 MCP 协议版本 `2026-07-28`。

Surface 包含：

- Tools：项目工程工具 + 小说业务原语；
- Prompts：overview / architect / writer / editor；
- Resources：项目列表、状态、context、章节 draft/final；
- Completion：为 resource template 参数提供纯读补全。

契约实现主要在 `internal/server/mcp*.go`。改变 tool input/output、resource URI、prompt 参数或协议行为时，需要同步契约测试和 README/文档。

## HTTP 与安全边界

`internal/server/http.go` 的公网面刻意很小：

- `GET /healthz`；
- 精确 `/mcp/<route>`；
- Host 白名单；
- Origin 白名单；
- route + Bearer 常量时间比较；
- MCP POST/OPTIONS；
- 4 MiB request body；
- 全局并发上限。

route 与 Bearer 是两个独立 256-bit 随机值。错误不能回显真实凭据或宿主绝对路径。

更完整的威胁模型见 [`../SECURITY.md`](../SECURITY.md)。

## 并发模型

每个 project 有自己的锁：

- 确认并发安全的纯读可以使用共享读锁；
- 写操作与不安全读使用独占锁；
- `revision` 检查与项目安全遍历共同防止跨调用竞态。

涉及锁粒度的改动建议跑 race tests，并考虑 benchmark/大项目测试。

## 持久化模型

`internal/store` 是事实层。关键约束：

- 文件写入使用 temp → write → chmod/sync/close → rename；
- JSON/特定 final 文本是权威数据；Markdown sidecar 可以是 best-effort；
- JSONL 追加日志用换行作为 commit marker，尾部半截记录可恢复；
- append 操作需要支持幂等 replay；
- foundation audit 使用 fingerprint 绑定“AI 看过的设定”和“服务器当前事实”。

磁盘格式的当前版本定义在 store/domain 相关代码中。开发阶段不自动迁移旧格式。

## 路由与状态机

项目阶段大致为：

```text
init → premise → outline → writing → complete
```

写作状态还区分 normal writing / rewriting / polishing。路由会优先处理恢复与返工，再处理聚合刷新、反馈、长篇 arc/volume 边界，最后才是正常续写。

如果增加新的状态或路由条件，优先保持 `flow.Route(State)` 纯函数，并补 exhaustive/property/fuzz 测试。

## 目录导航

| 目录 | 主要职责 | 改动时先看 |
| --- | --- | --- |
| `cmd/novel-mcp` | CLI、启动、配置、TUI 入口 | `main.go` |
| `internal/server` | MCP/HTTP/项目边界/资源/契约 | 目录内 `AGENTS.md` |
| `internal/tools` | 业务工具、写作流程、commit | 目录内 `AGENTS.md` |
| `internal/store` | 原子 IO、日志、持久化状态 | 目录内 `AGENTS.md` |
| `internal/flow` | 纯路由与状态加载 | `router.go`, `state.go` |
| `internal/domain` | 状态与数据模型 | transitions / commit / story |
| `internal/public` | Tailscale Serve/Funnel | `public.go` |
| `internal/tui` | 本机交互 UI | `tui.go` |
| `assets` | 内嵌 prompts/references/voice | `assets/README.md` |
| `scripts` | build/release/audit/维护工具 | `scripts/README.md` |

## 测试策略

最常用：

```bash
python scripts/check.py
```

高风险改动：

```bash
python scripts/check.py --race --history
```

Release 还会跑真实进程的官方 Go MCP client 与 TypeScript SDK v2 E2E。外网/Tailscale 真实环境 smoke 不属于普通单测。

## 设计变化检查表

改代码前先判断是否触及以下契约：

- MCP tool/resource/prompt/schema；
- 项目磁盘格式；
- revision 或锁语义；
- pending commit 恢复；
- HTTP 鉴权/Host/Origin；
- 路由优先级；
- release artifact；
- 隐私审计。

如果触及，除了局部测试，还要更新对应文档和端到端契约测试。
