# internal/server Agent Guide

本目录是 novel-mcp 的 MCP/HTTP/项目安全边界。这里的改动默认视为高风险。

## 主要职责

- MCP server、tools、prompts、resources、completion；
- HTTP 鉴权与 Host/Origin 门禁；
- 项目隔离、revision、锁与错误封装；
- `next_step` 对外 action plan；
- 只读 verify/status/export 等工程能力。

## 不变量

- 当前只支持 MCP `2026-07-28`；不要静默加未测试 legacy shim。
- HTTP 公网面保持最小：`/healthz` + 精确 `/mcp/<route>`。
- route 与 Bearer 独立随机，比较使用常量时间；错误不得回显秘密。
- 非回环暴露必须有预期 public URL，默认要求 Bearer。
- 浏览器 Origin 未明确允许时拒绝；不要“反射任意 Origin”。
- 远端参数不能变成宿主路径。
- 项目安全遍历必须拒绝 symlink / 非普通文件 / 超配额目录。
- pure read 不应偷偷改变 revision、checkpoint 或项目状态。
- 对外错误不要泄露宿主绝对路径。

## MCP 契约改动

改 tool/resource/prompt 时同步检查：

- input/output schema；
- structuredContent；
- annotations（read-only/destructive/idempotent/open-world）；
- resource URI/template/completion；
- client-visible 文档；
- contract/E2E tests。

不要只让 Go 编译通过就认为契约兼容。

## revision 与锁

- 写操作必须在正确锁内验证 `expected_revision`；
- 并发安全 pure read 才能使用共享锁；
- revision 计算和项目安全遍历是安全边界的一部分，不要绕开；
- 不要引入“冲突后自动重试写入”。

## 测试

最少：

```bash
go test -count=1 ./internal/server
```

涉及 HTTP/MCP/锁/生命周期时：

```bash
go test -race -count=1 ./internal/server
python scripts/check.py --race --history
```

协议 wire 行为改动还应运行对应 real-process/official SDK E2E。
