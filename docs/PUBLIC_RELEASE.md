# 公开仓库前检查

本仓库当前仍按**私有仓库**维护。未来公开时，不要直接把现有仓库可见性从 Private 切成 Public。

原因不是当前代码树，而是 Git 历史本身也属于公开内容：删除文件、改掉示例、轮换令牌，都不会自动从旧 commit 中删除已经提交过的信息。

## 当前原则

公开前必须同时满足：

1. 当前可发布树（已跟踪 + 未被 ignore 的未跟踪文件）通过：

   ```bash
   python scripts/public-audit.py
   ```

2. 准备公开的**完整历史**也通过：

   ```bash
   python scripts/public-audit.py --history
   ```

   也可以在 GitHub Actions 手工运行 `safety` workflow，并勾选 `full_history_audit`；该 job
   会以 `fetch-depth: 0` 获取完整历史。`public` 事件也会自动执行同一历史审计，但那只是误切
   Public 后的报警线，**不能**代替公开前的本地/手工检查。

3. 所有曾进入 Git 历史的真实凭据都已轮换/撤销。
4. 提交作者使用可公开身份；希望隐藏真实邮箱时使用 GitHub noreply 地址。
5. Release 资产重新检查，不包含 `credentials.json`、`config.json`、数据目录、日志、私钥或本机专用配置。
6. GitHub 安全设置已启用，且默认 workflow token 只给最小权限。

审计脚本只输出类别与位置，不输出匹配到的秘密值。

## 当前私有历史的已知阻断

当前可发布树可以通过公开审计，但现有私有 Git 历史**尚不适合直接公开**。历史审计会故意失败，因为旧 commit 中存在以下类别：

- 已轮换的真实 MCP 访问凭据；
- 个人作者邮箱元数据；
- 旧的本机绝对路径；
- 旧的具体隧道/网络主机名。

这些信息即使已经从当前文件删除，仍能从历史 commit 取回。

因此，在历史审计变成 PASS 之前，**不要切换当前仓库可见性**。

## 推荐的公开方式：干净 public mirror

如果不要求把私有开发历史全部公开，推荐创建一个新的 public 仓库，从已经审计通过的当前树开始新的 Git 历史。

这样做的优点：

- 不需要把私有历史里的个人信息逐条清洗；
- 不需要 force-push 当前私有仓库；
- 不会改写已经存在的私有 release/tag；
- 最容易证明“公开仓库从第一天起没有携带旧秘密”。

公开镜像的初始提交前，应再次运行：

```bash
python scripts/public-audit.py
go test -count=1 ./...
go vet ./...
```

然后在**新仓库**里创建初始 commit/tag/release。不要复制 `.git/`。

本仓库提供一个 fail-closed 的导出脚本，避免手工复制时把 `.git/`、`.local/`、运行数据或凭据一起带出去：

```bash
python scripts/export-public-tree.py --out ../novel-mcp-public
```

它会先运行当前树 `public-audit`，然后只复制“已跟踪 + 未被 ignore 的未跟踪文件”，拒绝符号链接，
最后在目标目录初始化一个**没有 commit、没有 remote**的新 Git 仓库。脚本故意不自动创建第一次提交：
先在新目录配置准备公开的 Git 身份（邮箱建议使用 GitHub Settings → Emails 页面显示的 noreply 地址），
再次检查 `git status` 后再 `git add . && git commit`。

如果目标是保留当前公开名称，推荐流程是：先把现有私有仓库改名为仅内部使用的名称，保持 Private；
再用上述导出目录创建新的公开仓库并使用原公开名称。不要把旧仓库的 `.git`、tags 或 Releases 迁入新仓库。

## 如果必须保留现有历史

只有确实需要公开完整开发历史时才选择历史重写。

需要同时处理：

- 真实 Bearer/route 等秘密字符串；
- 个人提交邮箱；
- 具体本机路径；
- 私有主机名或内部 URL；
- 含上述信息的 tags；
- GitHub Release 所指向的旧 tag/commit。

可使用 `git filter-repo` 等工具做一次离线副本演练。重写后必须：

1. `python scripts/public-audit.py --history` 返回 PASS；
2. 完整测试通过；
3. 检查所有 tag；
4. 重新创建受影响 release；
5. force-push 之前确认没有其他协作者依赖旧 commit ID；
6. 将旧 clone、bundle、备份视作仍含历史敏感信息。

`.mailmap` 只能改变显示方式，**不能删除 commit object 中原始邮箱**，不能替代历史重写。

## 凭据事件规则

任何真实访问凭据只要进入过 Git 历史，就按“已泄露”处理，而不是按“文件后来删掉了”处理：

1. 先轮换/撤销；
2. 确认当前运行实例不会继续接受旧凭据；
3. 再决定清洗历史或使用干净 public mirror；
4. 不在 issue、PR、聊天截图、CI log 中粘贴新凭据。

MCP 凭据轮换：

```bash
novel-mcp token rotate --i-understand-this-invalidates-current-clients
```

轮换会使现有客户端配置失效，需要重新分发新的 URL/Bearer。

## GitHub 公开时的仓库设置

公开当天至少检查：

- Secret scanning / push protection；
- Private vulnerability reporting / Security Advisories；
- Dependabot alerts；
- 默认 Actions workflow permissions 使用 read-only；
- 对 `main` 启用 branch protection/ruleset，并要求 CI 通过；
- Release 只从已验证 tag 构建；
- Issues/PR 模板提醒不要粘贴 URL、Bearer、credentials、小说私有内容或本机路径。

仓库内 CI 使用固定 commit SHA 引用 GitHub Actions，并显式声明 `permissions: contents: read`。
当前仓库仍为 Private 时，`.github/workflows/safety.yml` 的 jobs 会主动跳过；仓库切换为
Public 的 `public` 事件会立即启用当前树与完整历史审计，之后每次 push / pull request 都自动运行
当前树审计与 Go 验证。私有阶段仍以本地 `python scripts/public-audit.py` 与 Go 测试结果为准；
需要全历史复核时手工触发 `full_history_audit`。

## 每次发布前

```bash
python scripts/public-audit.py
go test -count=1 ./...
go vet ./...
git diff --check
```

准备真正公开现有历史时，额外要求：

```bash
python scripts/public-audit.py --history
```

只有这个命令也 PASS，当前仓库才具备“直接改 Public”的条件。
