# AGENTS.md

## 隐私与公开仓库安全规则

本仓库按“所有已提交内容最终都可能永久公开”处理。AI/Agent 在修改、提交、打 tag、创建 Release 或推送前，必须优先保护凭据、个人隐私和本机环境信息。

### 禁止提交的内容

- 任何真实凭据或访问材料：API key、token、Bearer、MCP route、完整可访问 MCP URL、cookie、私钥、Tailscale auth key、OAuth/登录凭据等。
- `credentials.json`、`config.json`、`.env*`、私钥文件、运行时数据目录、日志、用户小说项目数据、备份文件。
- 个人或机器可识别信息：私人邮箱、本机用户名、真实 home 绝对路径、机器专用 hostname、未明确打算公开的 tailnet/Funnel/内网主机名或 URL。
- 从私有历史复制来的 `.git/`、旧 commit、旧 tag、旧 Release 元数据，除非已经完成专门的历史安全审计。

示例和文档必须使用明显的占位值，例如 `<route>`、`<bearer>`、`<user>`、`<host>`、`example.com`、`127.0.0.1`。不要为了“让示例可直接运行”而放入真实值。

### 提交前强制检查

在任何 `git commit` 或 `git push` 前：

1. 运行 `python scripts/public-audit.py`，必须 PASS。
2. 检查 `git status`、`git diff` 和 `git diff --cached`，确认没有意外加入本机文件、运行数据或秘密值。
3. 确认提交身份适合公开；本仓库必须使用 GitHub noreply 地址，不要使用个人邮箱。
4. 不得通过删除/弱化 `.gitignore`、`scripts/public-audit.py` 或 CI 安全检查来绕过审计。

本仓库提供版本化 Git hooks。每个新 clone 首次开发前必须确认：

```bash
git config core.hooksPath .githooks
```

`.githooks/pre-commit` 会在 commit 前检查当前可发布树和提交邮箱；
`.githooks/pre-push` 会在 push 前扫描完整可达 Git 历史。不要使用 `--no-verify`
绕过这些 hooks，除非用户明确要求且已经人工完成等价安全审计。

在准备公开历史、迁移仓库、创建 tag/Release 或执行可能让旧历史可见的操作前，还必须运行：

```bash
python scripts/public-audit.py --history
```

必须 PASS 后才能继续。

### 发现泄露时

如果真实凭据曾进入任何 commit，即使之后已删除，也按“已经泄露”处理：

- 立即停止继续发布；
- 不在聊天、日志或报告里回显秘密值；需要核对时优先使用布尔比较、哈希或脱敏结果；
- 先轮换/撤销相关凭据；
- 再清理/重写历史，或改用经过审计的干净公开历史；
- 不能用“当前文件已经删掉”作为历史安全的依据。

### Git 历史边界

- 不要把含私有历史的仓库 remote 改指向公开仓后直接 push。
- 不要向公开仓 force-push 未经完整历史审计的旧分支、tag 或其他 ref。
- 公开仓应只接收已经通过当前树审计和必要历史审计的内容。
- 对任何不确定是否属于隐私/秘密的信息，默认不提交，并先向用户确认。

这些规则优先于为了省事而执行的批量 `git add .`、历史迁移、Release 或自动发布操作。
