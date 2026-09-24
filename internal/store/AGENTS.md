# internal/store Agent Guide

本目录是磁盘事实与恢复层。任何“看起来只是文件 IO”的改动，都可能影响 crash recovery、幂等和项目兼容性。

## 不变量

- 关键文件写入保持 temp → write → sync/close → rename 的原子模式。
- JSON 或明确指定的 final 文本是权威数据；Markdown sidecar 可以 best-effort，但不能反过来成为隐式真相源。
- JSONL 追加日志以完整换行记录作为 commit marker；读路径必须容忍尾部半截记录。
- append/replay 路径要有幂等语义，恢复时不能重复产生逻辑记录。
- store 不做创造性/语义判断，只保存和读取结构化事实。
- 不要在这里偷偷引入跨项目路径访问。
- 项目格式升级必须显式改变 format version，并同步 server 当前格式测试/文档。

## 修改数据结构时

先回答：

1. 新字段是否有零值兼容语义？
2. 崩溃发生在每一个写入阶段时，重启会读到什么？
3. 同一操作执行两次是否产生重复记录？
4. 旧开发格式是明确拒绝，还是项目决定提供迁移？不要半兼容。
5. chapter record / summaries / signals / progress 的派生关系是否仍可验证？

## 高风险区域

- append log 与 dedup；
- pending commit / checkpoints；
- chapter records；
- progress 与 summaries；
- foundation fingerprint；
- run meta / world / relationship / foreshadow 等派生事实。

## 测试

至少运行：

```bash
go test -count=1 ./internal/store
```

涉及并发、恢复、append 或格式：

```bash
go test -race -count=1 ./internal/store
python scripts/check.py --race --history
```
