# Contributing to novel-mcp

感谢参与 novel-mcp。这个仓库是公开仓库，所有提交、CI 日志、Issue 和 Pull Request 都应按“长期公开”处理。

## 开始之前

新 clone 第一次开发先执行：

```bash
git config core.hooksPath .githooks
```

仓库要求提交身份使用 GitHub noreply 邮箱。检查：

```bash
git config user.email
git config core.hooksPath
```

开发所需 Go 版本见 `.go-version`。Windows 没有合适 Go 时可以运行：

```bat
scripts\bootstrap-go.cmd
```

## 推荐阅读顺序

1. [`AGENTS.md`](AGENTS.md) — 项目不变量、改动导航、隐私规则；人类贡献者也建议阅读。
2. [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — 运行时架构、状态机、持久化与并发边界。
3. [`SECURITY.md`](SECURITY.md) — 公网暴露、凭据和路径安全模型。
4. 对应目录下的 `AGENTS.md` — `internal/server`、`internal/store`、`internal/tools` 有局部约束。

## 本地验证

日常改动优先使用统一检查入口：

```bash
python scripts/check.py
```

它会执行当前树隐私审计、Go 测试、`go vet` 和 `git diff --check`。

涉及并发、锁、HTTP/MCP 生命周期、持久化恢复或准备 Release 时再执行：

```bash
python scripts/check.py --race --history
```

正式 Release 还有额外的真实进程/官方 SDK 黑盒验证，见 [`docs/RELEASING.md`](docs/RELEASING.md)。

## 改动边界

- 服务器不调用任何模型。不要把 LLM provider、模型密钥或后台创作引擎加回服务端。
- 远端客户端不能传入宿主文件路径。项目只能通过受限 project ID 寻址。
- MCP 写工具依赖 whole-project `revision` 做乐观并发控制；不要绕开 `expected_revision`。
- `next_step` 是确定性路由入口。客户端应执行返回的 action plan，而不是在服务端复制一套智能代理。
- `commit_chapter` 是可恢复 Saga。修改提交/恢复逻辑时必须保持冻结载荷和幂等重放语义。
- 开发期项目格式只支持当前版本；不要默默兼容旧格式，除非项目明确决定引入迁移层。

更完整说明见 [`AGENTS.md`](AGENTS.md) 和 [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)。

## Pull Request

PR 尽量保持单一目的。描述中说明：

- 改了什么以及为什么；
- 触及哪些架构不变量；
- 跑过哪些测试；
- 是否改变 MCP tool/resource/prompt/schema、磁盘格式、安全边界或 Release 行为。

仓库的 PR 模板会提醒隐私与验证要求。

### Dependabot 自动维护

Dependabot 每周检查 Go modules 与 GitHub Actions。为了减少通知，patch 与 minor 更新分别按生态分组；
major 更新仍保持独立 PR，便于定位兼容性问题。

自动合并策略刻意保守：只有 **Dependabot 创建、版本变化可明确解析为纯 patch、并且完整
`safety` CI 全部通过，而且 PR head 仍与这次 `safety` 实际检查的 commit 一致** 的 PR 才会自动 squash merge。若 `main` 已前进，自动化会先刷新
Dependabot 分支，等待新一轮 CI，而不是用旧测试结果直接合并。

以下情况保留人工处理：minor/major、预发布或无法可靠解析的版本变化，以及任何 CI 失败。
自动化只在这些情况下添加 `manual-review` 并请求仓库 owner review；普通 patch 更新不再主动打扰维护者。
因此维护者不需要逐个处理普通 patch 更新，但仍应人工查看可能改变兼容性的升级。

## Issue 与安全问题

普通 Bug/Feature 可以使用 Issue 模板，但必须先脱敏。不要粘贴真实 MCP URL、route、Bearer、`credentials.json`、私有小说正文、本机绝对路径或内部主机名。

认证绕过、路径越界、凭据泄露等安全问题请使用 GitHub Private vulnerability reporting / Security Advisory。详见 [`SECURITY.md`](SECURITY.md)。
