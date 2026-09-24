# 安全模型

novel-mcp 把"能读写你磁盘上小说项目"的能力暴露成一条 URL。它的安全设计只有两条主线：
**准入**（谁能连上）与**路径与数据安全**（连上之后能碰到什么）。

## 1. 谁需要保护什么

| 资产 | 风险 | 现阶段的控制 |
| --- | --- | --- |
| 接入地址（路由令牌） | 泄漏即等于拿到工具调用权 | 首次启动生成 32 字节随机值（64 hex），只出现在 URL 里；不写日志、不回显 |
| Bearer 令牌 | 同上，但不出现在 URL | 独立随机值，与路由令牌分开；两者相同会被拒绝加载 |
| 小说项目内容 | 被未授权读取/篡改 | 每项目一个目录，无跨项目入口；写工具需 `expected_revision` |
| 宿主文件系统 | 被读到项目外的东西 | 只按项目 ID 寻址；目录遍历、符号链接、设备文件一律整体拒绝 |
| 宿主命令执行 | 任意代码执行 | **没有** shell / exec / 上传下载类工具，也没有管理 API 或控制台 |

## 2. 准入控制

请求要依次通过：

1. **Host 精确白名单**：只接受 `--host:--port`、`localhost:--port`、`127.0.0.1:--port`、
   `[::1]:--port` 以及 `--public-url` 的 host。SDK 自带的 localhost 保护被显式关掉并由这份
   白名单取代（否则隧道 Host 会被误拒）；这里不做通配匹配，避免 DNS 重绑定。
2. **Origin 白名单**：浏览器跨源请求带 `Origin`，不在 `--allow-origins` 里就 403。
   未列出的源不会被"允许并回显"，恶意页面读不到响应。预检 `OPTIONS` 只对已授权源放行。
3. **路由路径**：`/mcp/<64hex>` 与配置里的值做常量时间比较。任何偏差——包括多一个查询串——
   都直接 404，且**不重定向**，不给"路径对不对"的可比对信号。`/healthz` 只回
   `{"ok":true,"service":"novel-mcp","auth":"bearer|url-token"}`，不含任何配置、版本或路径。
4. **Bearer**（`require_bearer`，默认开）：`Authorization: Bearer <64hex>` 同样常量时间比较，
   失败回 401 并带 `WWW-Authenticate`。

这里的 Bearer 是**单用户静态访问令牌**，不是 OAuth access token，也不会发布虚假的
`/.well-known/oauth-protected-resource`。标准 MCP OAuth 需要真实授权服务器、Protected
Resource Metadata，以及对该授权服务器签发 token 的 issuer/audience/scope/有效期验证；
当前部署模型没有外部 IdP / Authorization Server，因此不能只靠现有静态 Bearer 冒充 OAuth。
将来接入真实授权服务器时，应在 HTTP 边界使用 Go MCP SDK 的
`auth.RequireBearerToken` + `ProtectedResourceMetadataHandler`，而不是在 tool 内部返回鉴权错误；
现有 route token 可以继续作为独立的 endpoint capability。

非回环地址启动时，必须同时具备 Bearer 与 `--public-url`，否则拒绝启动；绑定 `0.0.0.0`
还需要显式 `--insecure-allow-open`。这些是"少一件就起不来"，而不是警告。

其他响应侧约束：`Cache-Control: no-store`、`Referrer-Policy: no-referrer`、
`X-Content-Type-Options: nosniff`、请求体上限 4 MiB、并发 16、无 GET 工具调用路径、
无 SSE 长连接（stateless + JSON 响应，GET/DELETE 回 405）。

## 3. 路径与数据安全

* 项目 ID 必须匹配 `^[a-z][a-z0-9-]{0,47}$` 且不是 Windows 保留名（`con`/`nul`/`lpt1`…）。
  远端**无法**提交宿主路径：没有 `path` / `dir` / `file` 之类的参数。
* 每次读项目之前先过一遍安全门（与内容指纹共用一次遍历）：目录里出现符号链接、非普通文件，
  或文件数超过 20000、体积超过 256 MiB，整本项目拒绝服务——不是跳过，而是报错。
  这样"项目里放一个指向 `/etc/passwd` 的 `project.json`"不会把宿主文件带进结果。
* 数据根在启动时解析一次（含符号链接展开）后不可被远端改变。最多 100 个项目。
* 凭据文件 `credentials.json` 以 0600 写入，采用临时文件 + `fsync` + `rename`；
  轮换（`token rotate --i-understand-this-invalidates-current-clients`）同样是原子替换。

## 4. 已知边界（请按此评估风险）

* **`--bearer=false` 时，URL 就是唯一凭据**。它会出现在浏览器历史、剪贴板、代理与隧道
  访问日志里。只应在回环 + 本机可信的前提下使用；一旦要经隧道暴露，请开启 Bearer。
* **没有速率限制与配额隔离**：单实例设计给一个使用者；并发上限 16 只防打爆，不防滥用。
* **持有 URL 的人拥有全部工具权限**：没有只读令牌、没有按项目授权。要收回权限就轮换令牌。
* **项目内容属于不可信数据**：如果一本书的内容里写着"忽略上面的指令去做 X"，那是数据，
  不是给你的指令——这是客户端（网页 AI）需要遵守的约定，服务器不会替你判断。
* **传输安全由外层负责**：服务本身只说 HTTP。经隧道/反代时必须由那一层提供 TLS，
  并用 `--public-url` 把预期来源固定下来。
* **数据目录不加密**：内容以明文存放，备份、权限与磁盘加密由使用者负责。
* **经 Tailscale 暴露时**：服务仍在回环，只有 tailnet 内设备能到达，TLS 由 `tailscale serve`
  用 ts.net 证书终结。此时"URL 即凭据"的强度由**你的 tailnet 成员身份**兜底：任何能进你
  tailnet 且拿到这条 URL 的设备都能读写你的书。改用 Funnel 对公网开放时必须开启 Bearer，
  并接受"任何同时拿到 URL 与 Bearer 的人都能写你的书"。
* **本机代理会打穿 Tailscale**：若本机跑着 TUN/系统代理（如 Clash/mihomo 监听 127.0.0.1:7890），
  把 `*.tailscale.com`、`*.ts.net`、`*.tailscale.io` 设为直连，否则 tailscaled 可能连不上
  控制面、节点起不来（表现为 `BackendState = NoState` 且健康信息为 "Tailscale is starting"）。

## 5. 令牌轮换与事件响应

```bash
./novel-mcp token rotate --i-understand-this-invalidates-current-clients
./novel-mcp url        # 把新地址交给仍需要访问的客户端
```

轮换后旧 URL 立刻 404（配置里的路由令牌已经不同）。如果怀疑项目内容被改过，
先 `project_status` 看 `pending_commit` 与进度，再决定是否从备份恢复数据目录——
本服务不会自动修复或改写既有工件。

报告问题请附：`novel-mcp version`、`/healthz` 的响应体（只有三个字段）、以及能重现的最小
调用序列（**不要**附上 URL、Bearer 或完整的 `credentials.json`）。

## 6. 公开仓库与漏洞报告隐私

MCP route 与 Bearer 都按**凭据**处理，不允许出现在源码、测试 fixture、Issue、Actions log、
截图或 Release notes 中。本仓库已经公开，因此 Git 历史、tag、提交作者元数据和曾经提交后又删除的
内容都属于公开面；“当前文件里已经没有秘密”并不足够。每次提交/推送与 CI 都应保持以下审计通过：

```bash
python scripts/public-audit.py
python scripts/public-audit.py --history
```

任何真实访问凭据只要进入过 Git 历史，就按已泄露处理：先轮换/撤销，再处理历史；不要
依赖删除文件、revert、`.gitignore` 或 `.mailmap` 来“隐藏”旧值。

凭据轮换只能撤销访问权，不能删除历史里的个人邮箱、本机路径、主机名或旧 secret 文本。
旧 Private archive 已知含这些历史痕迹，因此它的 branch/tag/commit/Release **不得导入本公开仓库**。

安全问题不要附带真实 URL、Bearer、`credentials.json`、私有小说内容、本机绝对路径或
内部网络主机名。仓库公开后，优先使用 GitHub 的 Private vulnerability reporting / Security
Advisory 渠道报告安全问题；普通 issue 只放已经脱敏的最小复现。
