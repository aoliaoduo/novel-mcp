# 启动、连接与排障

**简体中文** · [English](CONNECTIVITY.md)

本文只讲本机进程、MCP URL、Tailscale 公网模式和常见连接失败。

## 两种启动模式

### 仅本机 AI 客户端

```bash
novel-mcp local
```

服务只监听 `127.0.0.1`，Bearer 保持开启，不使用 Tailscale。

适合 MCP 客户端和 `novel-mcp` 在同一台机器上的场景。

### 网页 / 云端 AI 客户端

```bash
novel-mcp public
```

MCP 服务本体仍只监听回环。`novel-mcp` 会先检查 Tailscale，配置 Serve/Funnel；使用 Funnel 时还会独立验证公网 DNS，然后才启动 MCP 服务。

适合云端 MCP 客户端需要通过 HTTPS 访问本机的场景。

Windows 直接双击无参数 `.exe` 时，首次会让你选择这两种模式之一，并记住选择。

## TUI 快捷键

运行页的连接页面：

- `C`：复制完整 MCP 客户端配置；
- `U`：只复制 MCP URL；
- `B`：复制 Bearer；
- `P`：复制连接初始提示词；
- `O`：打开健康检查；
- `V`：临时显示/隐藏凭据；
- `q` / `Ctrl+C`：停止进程。

界面遮罩凭据只是为了减少误截图。使用 `C`、`U`、`B` 后，剪贴板里仍然是真实凭据。

## 端点形态

公网模式通常得到：

```text
https://<节点>.<tailnet>.ts.net/mcp/<64位十六进制route>
```

并单独携带请求头：

```text
Authorization: Bearer <64位十六进制token>
```

健康检查：

```text
https://<节点>.<tailnet>.ts.net/healthz
```

真实 URL 与 Bearer 不要一起贴到公开 Issue、截图、日志或聊天群。

## 常用诊断命令

```bash
novel-mcp url
novel-mcp prompt
novel-mcp doctor
novel-mcp doctor --deep
novel-mcp doctor --json
novel-mcp public --dry-run
```

`doctor` 只做本机诊断，不主动访问公网 hostname。Tailscale / 公网链问题更适合 `public --dry-run`，因为它会执行只读公网预检。

## `public` 常用参数

| 参数 | 作用 |
| --- | --- |
| `--tailnet-only` | 只用 Tailscale Serve，仅 tailnet 内可达，不开放公网 |
| `--allow-origin https://...` | 增加浏览器 Origin 白名单，可重复 |
| `--smoke` | 启动后跑轻量只读公网 smoke |
| `--dry-run` | 只读预检，不写配置、不挂 Funnel、不启动 MCP |
| `--no-tui` | 使用纯文本输出 |
| `--port` | 本机 MCP 监听端口 |
| `--serve-port` | Tailscale HTTPS 端口 |
| `--data` | 数据目录 |

公网 Funnel 在 Bearer 关闭时默认会被拒绝，除非显式给 token-only 同意参数。那种模式下 URL 本身就是凭据，更容易出现在历史、代理日志和截图中。

## 客户端连不上时

### 1. 先跑 `doctor`

```bash
novel-mcp doctor --deep
```

它检查配置解析/安全组合、数据目录、凭据格式、服务是否存在，以及可选的逐项目完整性；输出已为支持场景脱敏。

### 2. 看是不是服务已经在跑

再次启动时，如果端口上的 `/healthz` 确实是 `novel-mcp`，程序会优先显示可复用连接卡，而不是只报一个模糊的“端口占用”。

### 3. 检查 Tailscale 状态

公网模式要求 Tailscale 最终处于 `BackendState=Running`。

`NeedsLogin` 就在 Tailscale 桌面客户端完成登录；设备等待审批时在 tailnet 管理后台批准。

如果长期卡在 `NoState`、`starting` 或控制面不可达，同时机器上有 TUN/系统代理，优先让这些域名直连：

```text
*.tailscale.com
*.tailscale.io
*.ts.net
```

然后重试。Windows 上如果桌面服务/进程本身异常，再用 `scripts\tailscale-repair.cmd` 作为修复入口。

### 4. Funnel 显示已开启，但公网 DNS 没有记录

本地 `Funnel on` 只能证明本地 Serve/Funnel 配置已经写入，不能证明公网 DNS 已经发布。

不要反复 `off/on`：那会重新触发异步发布，反而延长等待。`novel-mcp public` 会单独检查公网 DNS，不会把本地状态文字当成公网可达证明。

如果在合理传播时间后仍无记录，先重新检查登录/节点健康，再考虑应用配置。

### 5. 浏览器 Origin 被拒绝

网页 MCP 客户端会带 `Origin`。用重复的 `--allow-origin` 或配置文件加入**精确** origin；服务不会做通配 Origin 回显。

### 6. 云端 Agent 连不上 `--tailnet-only`

厂商云端 Agent 通常不在你的 tailnet 里。`--tailnet-only` 只适合本来就在该 tailnet 的客户端；需要任意云端访问时应使用公网 Funnel + Bearer。

## Smoke 测试

`novel-mcp public --smoke` 只做轻量只读公网检查：错 route 404、health、MCP 握手和工具列表。

要从真实公网环境走完整写入闭环，可在真正能访问公网端点的机器上运行：

```bash
go run ./cmd/tailnet-smoke -url <完整MCP地址> -bearer <token>
```

工具会创建临时 `smoke-*` 项目，完成写入/提交/导出闭环后自动删除。若中途失败，项目会保留用于排查。

## 凭据轮换

怀疑 route 或 Bearer 泄露时：

```bash
novel-mcp token rotate --i-understand-this-invalidates-current-clients
novel-mcp url
```

轮换会同时替换 route 与 Bearer，并立即让旧客户端失效。

完整威胁模型见 [../SECURITY.md](../SECURITY.md)。
