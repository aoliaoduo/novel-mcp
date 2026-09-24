# 用户写作工作流

**简体中文** · [English](WORKFLOW.md)

本文说明 MCP 客户端应该怎样驱动 `novel-mcp`。这里只讲协议行为，不讲文学风格。

## 心智模型

`novel-mcp` 是持久化工件与状态服务。客户端 AI 负责语义判断与正文生成；服务器负责事实、校验、持久化、路由、并发保护和恢复。

主循环只有一条：

```text
next_step → 执行返回的 actions → next_step → ... → done=true
```

客户端不要自己再实现第二套工作流引擎。当前 plan 做完就重新问 `next_step`。

## 打开或创建项目

通常先：

```text
list_projects
```

然后选择已有项目，或用安全 project ID 与明确 brief 调 `create_project`。

`create_project` 是唯一不依赖“已有项目 revision”的创建动作。项目一旦存在，后续写操作都应使用该项目最近返回的 `expected_revision`。

## 首次规划刻意拆成两轮

新项目还没有规划 tier 时，`next_step` 只返回足够确定 tier 的动作：

1. 根据 brief 调 `novel_guide(role=architect|architect_long)`。
2. 缺少作品信息时先 `save_book`。
3. 保存一个 premise 种子，并带 `scale=short|mid|long`。
4. 立即再次调用 `next_step`。

第二轮已经能读到落盘的 tier，因此服务器可以逐项返回准确设定动作：short/mid 使用 `outline`，long 使用 `layered_outline`，客户端不用在第一轮自己猜。

基础设定齐全后，路由会要求 `novel_context` 读取真实落盘工件和 foundation fingerprint，再由客户端做跨文件语义审查并调用 `audit_foundation`。审查通过后才进入写作阶段。

## 理解 `next_step.result.actions`

每个 action 都是机器可读指令，主要字段：

- `id`：当前 plan 内稳定编号，如 `a1`。
- `tool`：要调用的 MCP 工具。
- `arguments`：服务器已经根据事实确定的参数。
- `required_inputs`：仍需 AI 创作或判断后补齐的字段。
- `depends_on`：必须先完成的 action ID。
- `requires_revision`：是否需要 `expected_revision`。
- `revision_source`：revision 应来自当前 `plan`、某个前序 action（如 `a3`），或 `none`。
- `expected_revision_source`：需要绑定的明确结果字段，例如 `next_step.revision` 或 `a3.revision`。
- `mode`：`required` 或 `choice`。
- `choice_group`：同一非空组内互斥，只能选一个，不能全部顺序执行。
- `resource_uri`：该工件也能通过 MCP Resource 读取时给出。

自然语言 `task` 用来解释意图；真正调度工具时优先服从 `actions`。

## revision 规则

revision 是整本项目内容指纹，不是递增版本号。

写 action：

- `arguments` 会直接带上已知的 `project`；
- `revision_source=plan`：`arguments.expected_revision` 已由本次 `next_step` 外层 revision 预填；
- 指向 `a3` 等 action：把 `a3.revision` 绑定到 `expected_revision`（同一来源也会写在 `expected_revision_source`）；
- 纯读 action 为 `none`。

遇到 `REVISION_CONFLICT` 必须先重读事实，再判断是否重试。不要盲目重放 append；重复执行 `draft_chapter(mode=append)` 会重复正文，因此服务器会在写入前拦陈旧 revision。

失败的写调用也可能在后续 checkpoint 出错前已经改过磁盘，所以失败响应仍可能带**新的** revision。始终以实际响应返回的 revision 为准。

## 正常写新章

Writer 标准路线：

```text
novel_context(chapter)
→ plan_chapter
→ draft_chapter(mode=write)
→ read_chapter(source=draft)
→ check_consistency
→ 必要时做语义修订
→ commit_chapter
→ next_step
```

`check_consistency` 只是加载对照资料，**不等于**证明情节没有矛盾。客户端 AI 必须结合实际回读正文判断。

检查之后如果正文又改了，提交前要重新回读并重新检查。

只有 `commit_chapter` 完成后才形成持久化终稿事实；草稿不能当成已经提交的章节。

## 返工已完成章节

完本或已提交章节不要直接覆盖。完本书要修改旧章时先调用 `reopen_book(chapters, reason)`。

返工路线：

```text
novel_context(chapter)
→ draft_chapter(mode=write)
→ read_chapter(source=draft)
→ check_consistency
→ commit_chapter
→ next_step
```

已完成章节刻意**不再**调用 `plan_chapter`。返工队列排空后，原本已完结的书会自动回到 complete。

## 长篇卷弧

分层长篇的结构边界由路由统一决定：

- 弧末 → `save_review(scope=arc)`；
- 然后 `save_arc_summary`；
- 卷末 → `save_volume_summary`；
- 下一弧只有骨架 → `expand_next_arc`；
- 追加新卷或收官卷 → `save_foundation(type=append_volume, ...)`；
- 故事满足完结条件 → `save_foundation(type=complete_book, content={}, reason=...)`。

**不存在名为 `append_volume` 的 MCP 工具。** 它是 `save_foundation` 的一种 type。

后续未发生章节需要改计划时用 `revise_outline`。Writer feedback 也可能形成互斥 choice：要么修订大纲，要么确认现有计划仍适用并消费反馈。

## 崩溃、断线或换会话后的恢复

不要靠聊天记忆重建状态。

1. 调 `next_step`。
2. 如果存在 pending commit，路由会优先返回，并附冻结参数；按这些参数重放 `commit_chapter` 收尾。
3. 想看全貌时调 `project_status`。
4. 继续新的 `next_step` plan。

磁盘事实才是权威源，没有单独的“恢复会话”操作。

## 只读诊断

常用读工具：

- `project_status`：阶段、进度、设定缺项、pending、告警。
- `verify_project`：更完整的项目一致性核验，只诊断不修复。
- `export_book`：只导出已提交正文。
- `novel_context`：规划/写作上下文和事实。
- `read_chapter`：读 draft/final。
- `check_consistency`：语义审查所需对照资料。

CLI 层还可运行 `novel-mcp doctor --deep`。它不会输出小说正文、项目 ID、连接凭据、公网 hostname 或本机绝对路径。

## 常见错误

| code | 含义 | 客户端怎么做 |
| --- | --- | --- |
| `REVISION_CONFLICT` | 上次读取后磁盘变了 | 重读，再决定；不要盲重放 |
| `CONFLICT` | 语义/状态冲突，如 foundation audit 过期 | 按 message 刷新事实 |
| `PRECONDITION_FAILED` | 阶段或前置条件不满足 | 先完成路由要求 |
| `INVALID_REQUEST` | 参数错误 | 修正请求 |
| `STORE_ERROR` | 存储读写异常 | 本机检查磁盘/权限 |
| `PROJECT_NOT_FOUND` | 项目不存在 | 重新选择或创建 |
| `PROJECT_DAMAGED` | 项目格式缺失/损坏 | 开发期项目人工检查或重建 |
| `PROJECT_UNSAFE` | 符号链接/异常文件/超限 | 本机修复项目目录 |

当前项目格式仍处于开发阶段，只支持当前格式版本，还没有旧格式迁移层。

## Prompts 与 Resources

支持 MCP Prompts 的 Host 可以直接使用内置 overview / architect / writer / editor prompts；只支持 Tools 的 Host 用 `novel_guide` 读取同一份内嵌协议。

支持 Resources 的 Host 可以通过 `novel://...` 读取项目状态、context 与章节 draft/final；只支持 Tools 的 Host 完全可以忽略 resource URI。
