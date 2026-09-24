# AGENTS.md

本文件是编码 Agent 在 novel-mcp 仓库中的主入口。目标是让任何 AI 在不依赖聊天历史的情况下，快速建立正确心智模型并安全修改代码。

## 先读什么

开始复杂改动前，按顺序读取：

1. `AGENTS.md`（本文件）；
2. `docs/ARCHITECTURE.md`；
3. 与任务相关目录下的局部 `AGENTS.md`；
4. `SECURITY.md`（涉及 HTTP、凭据、路径、外网时必须读）；
5. `CONTRIBUTING.md`（提交、测试和 PR 约定）。

不要先通读整个仓库再猜架构；优先沿下面的“改动导航”定位。

## 一句话心智模型

**AI 客户端负责创作和语义判断；novel-mcp 服务器负责事实、校验、持久化、确定性路由、并发控制与恢复。服务器本身不调用模型。**

```text
AI / MCP Host → Streamable HTTP → internal/server
                                ├→ internal/flow
                                ├→ internal/tools
                                └→ internal/store
```

## 不可破坏的不变量

### 1. 服务端不调用模型

不要加入 LLM provider、模型 API key、后台 Worker、自动续写引擎或“服务器替 AI 做语义判断”的路径。

需要创造性判断时，应让服务器返回结构化 context，由客户端 AI 产出结果，再由服务器验证/保存。

### 2. `next_step` 必须保持确定性

`internal/flow` 只根据磁盘事实和状态做路由。不要在 flow 中引入随机性、网络调用或模型调用。

客户端主循环应是：`next_step → 执行 actions → next_step`。

### 3. 不接受宿主任意路径

远端只能通过安全 project ID 寻址。不要增加 MCP 参数让客户端传宿主 `path`、`dir`、`file`，也不要新增 shell/exec/任意文件工具。

### 4. 写工具必须尊重 revision

项目 `revision` 是 whole-project 内容指纹，不是递增版本号。写操作必须使用最新 `expected_revision`；冲突时拒绝写入。

不要通过“自动重试写入”掩盖 `REVISION_CONFLICT`，尤其是 append 类操作。

### 5. `commit_chapter` 恢复语义不能被破坏

章节提交是可恢复 Saga。第一次执行会冻结 structured payload 和 draft snapshot；崩溃恢复重放冻结事实。

不要让恢复路径重新使用新的聊天内容或新的调用参数替换冻结载荷。

### 6. 持久化必须可恢复

关键写入使用原子 temp + sync + rename；JSONL 追加要能处理尾部半截记录并支持幂等 replay。

不要用简单覆盖替换已有恢复机制。

### 7. MCP 契约是公开 API

当前只支持 MCP `2026-07-28`。修改 tool/resource/prompt/schema/URI 时必须同步契约测试和文档。

不要为了“兼容一下”静默保留未测试的旧协议 shim。

### 8. 公开仓库隐私是硬约束

所有 commit、tag、Release、Issue、PR、CI log 都按永久公开处理。不得提交真实凭据、个人邮箱、本机路径、机器 hostname、私有网络地址或用户小说数据。

## 改动导航

| 需求 | 优先查看 |
| --- | --- |
| CLI / 启动参数 / 双击行为 | `cmd/novel-mcp` |
| MCP tool/schema/prompt/resource | `internal/server` |
| HTTP/Auth/Host/Origin | `internal/server/http.go`, `SECURITY.md` |
| 项目创建/删除/隔离/revision | `internal/server/projects.go` |
| `next_step` 路由 | `internal/flow` |
| 写作业务工具 | `internal/tools` |
| commit/recovery | `internal/tools/commit_*`, `internal/domain/commit.go` |
| 磁盘格式/原子 IO/JSONL | `internal/store` |
| 阶段/状态模型 | `internal/domain` |
| Tailscale Serve/Funnel | `internal/public` |
| TUI | `internal/tui` |
| prompts/references | `assets` |
| release/build/audit | `scripts`, `.github/workflows` |

高风险目录有自己的 `AGENTS.md`，局部规则优先于本文件的一般建议。

## 推荐工作方式

1. 先确认任务触及哪些不变量和公开契约。
2. 阅读最小必要文件，不要无目的大范围重写。
3. 优先补/改测试，再改实现，或至少同步完成。
4. 保持错误码、revision、恢复语义和安全边界显式。
5. 完成后运行与风险匹配的验证。

## 验证矩阵

普通代码/文档改动：

```bash
python scripts/check.py
```

涉及并发、锁、HTTP/MCP 生命周期、store、commit/recovery：

```bash
python scripts/check.py --race --history
```

涉及真实 MCP wire contract 时，除普通测试外重点跑 `internal/server` 对应 E2E/contract 测试。

涉及 Release 时按 `docs/RELEASING.md` 执行完整流程。

涉及 Tailscale/Funnel 的真实环境问题时，不要把同机 tailnet 测试等同于公网验证。

## 隐私与 Git 安全规则

### 禁止提交

- API key、token、Bearer、MCP route、真实可访问 MCP URL、cookie、私钥、Tailscale auth key、OAuth/登录凭据；
- `credentials.json`、`config.json`、`.env*`、日志、运行时数据、用户小说项目数据、备份；
- 私人邮箱、本机用户名、真实 home 绝对路径、机器 hostname、未明确公开的 tailnet/Funnel/内网主机名或 URL；
- 旧 Private archive 的 `.git/`、branch、tag、Release 或其他未经审计的 Git 对象。

示例统一使用 `<route>`、`<bearer>`、`<user>`、`<host>`、`example.com`、`127.0.0.1` 等明显占位值。

### 每个 clone 的本地保护

新 clone 第一次开发必须：

```bash
git config core.hooksPath .githooks
```

本仓库提交身份必须使用 GitHub noreply 邮箱。不要使用 `--no-verify` 绕过 hooks，除非用户明确要求且已经人工完成等价安全审计。

`.githooks/pre-commit` 会运行当前树审计并检查 author/committer 邮箱；`.githooks/pre-push` 会扫描完整可达历史。

### 发现泄露

如果真实凭据进入过任何 commit：

1. 停止继续发布；
2. 不回显秘密值；
3. 先轮换/撤销凭据；
4. 再处理历史或重新建立干净历史；
5. 不把“后来删掉了”视为安全。

## 不要做的事情

- 不要把业务逻辑复制到 `cmd`、TUI 或第二套服务实现中；
- 不要让 `check_consistency` 冒充语义正确性证明；它只加载事实材料；
- 不要把 internal capacity/估算值伪装成用户承诺的固定总章节数；
- 不要盲重试可能已经产生副作用的写调用；
- 不要扩大 HTTP surface 加管理后台、shell 或通用文件 API；
- 不要弱化 `.gitignore`、`public-audit.py`、hooks 或 CI 以“让测试先过”。

## 完成任务前检查

- 是否保持服务器无模型调用？
- 是否保持路径隔离和鉴权边界？
- 写路径是否正确处理 revision？
- crash/retry 是否仍安全？
- MCP/schema/磁盘格式变化是否有测试和文档？
- `python scripts/check.py` 是否通过？
- 高风险改动是否跑了 `--race --history`？
- `git status` / diff 是否只包含预期内容？
- 是否没有把隐私或真实凭据写入 Git 历史？
