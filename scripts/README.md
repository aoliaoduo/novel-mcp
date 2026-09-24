# 开发与维护脚本

这里只放仓库维护辅助脚本；日常启动 novel-mcp 不需要进入这个目录。

| 文件 | 用途 |
| --- | --- |
| `build-dev.cmd` | Windows 开发构建，唯一输出为 `dist\\novel-mcp.exe` |
| `build-release.py` | 用 `.go-version` 固定工具链交叉构建全部正式分发资产 |
| `verify-release.py` | 校验 release manifest、SHA256 和 portable 包内容 |
| `bootstrap-go.cmd` / `.ps1` | 本机没有合适 Go 时，把固定版本工具链放到 `.toolchain/` |
| `tailscale-repair.cmd` / `.ps1` | Tailscale 卡在未登录/NoState 时的 Windows 修复工具 |
| `mcp-ts-smoke.mjs` | 官方 TypeScript MCP SDK 黑盒验证 |
| `public-audit.py` | 当前树/完整历史的隐私与 secret 审计 |
| `check.py` | 统一本地验证入口：隐私审计 + Go test/vet + diff check；可选 race/history |
| `export-public-tree.py` | 将已通过审计的当前可发布树导出为**无旧历史**的新 Git 仓库；用于从私有开发仓安全创建 public mirror |

约定：

- `.toolchain/`：本地 Go 工具链缓存，不提交。
- `.go-version`：官方开发/Release 的精确 Go 工具链版本；`go.mod` 仍只表示最低要求。
- `.local/`：测试依赖与临时开发缓存，不放给用户双击的程序。
- `dist/`：本机当前可运行构建；需要测试最新代码时双击 `dist\\novel-mcp.exe`。
- 正式历史版本从 GitHub Releases 获取，不在仓库目录长期保存副本。
- 若旧 Git 历史含凭据、个人邮箱、本机路径或私有主机名，不要直接改仓库 Visibility；先运行
  `python scripts/export-public-tree.py --out ../novel-mcp-public` 生成无历史的公开镜像工作区。
