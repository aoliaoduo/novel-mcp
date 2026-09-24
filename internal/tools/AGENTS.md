# internal/tools Agent Guide

本目录是小说业务工具层。它保存客户端 AI 的结构化结果，但不替客户端做创造性判断。

## 不变量

- 工具必须基于磁盘事实和显式输入工作，不读取聊天历史。
- 写工具必须由 server 层的 `expected_revision` / 锁语义保护；不要设计可被盲目重放的隐藏副作用。
- `check_consistency` 只加载事实材料，不宣称语义“通过”。真正的语义判断来自客户端 AI。
- `plan_chapter` / draft / edit / commit 的阶段约束不能被快捷路径绕过。
- completed chapter 返工必须遵守 rewrite queue / reopen 语义。
- foundation audit 必须绑定客户端实际读取过的 fingerprint。
- 长篇 arc/volume/compass 的容量估算是内部规划事实，不要误暴露成固定总章节承诺。

## `commit_chapter` 特别规则

这是本目录最高风险路径之一：

- 第一次提交冻结 structured payload 与 draft snapshot；
- pending commit 按阶段推进；
- 恢复重放冻结载荷，不接受新的正文替换它；
- 子步骤需要幂等，崩溃后重复执行应最终收敛；
- progress、chapter record、summary、world/signals、checkpoint 最终要一致；
- 在成功清理 pending 之前，不要允许路由跳到下一章。

修改 `commit_*` 时优先补 crash/replay/idempotency 测试。

## 改业务规则时

检查是否同时影响：

- `internal/domain` 数据模型；
- `internal/flow` 路由条件；
- `internal/store` 持久化；
- `internal/server` 对外 schema/action plan；
- prompt/reference 文本；
- verify_project 的诊断逻辑。

不要只改一个层让其它层依赖旧假设。

## 测试

至少运行：

```bash
go test -count=1 ./internal/tools
```

涉及 commit/recovery/foundation/返工/长篇边界时：

```bash
go test -race -count=1 ./internal/tools ./internal/flow ./internal/store ./internal/server
python scripts/check.py --race --history
```
