# Release 流程

novel-mcp 的正式分发由 Git tag 驱动。Release workflow 与本地使用同一个
`scripts/build-release.py`，避免 CI 和本机构建成两套实现。

## 版本约束

1. 唯一版本源：`internal/server/version.go`。
2. 开发分支使用 `x.y.z-dev`；正式发布前改成不带 `-dev` 的版本。
3. tag 必须严格等于 `v<Version>`，例如源码是 `0.6.0` 时只能推 `v0.6.0`。
4. 正式构建拒绝 dirty tree 和 `-dev` 版本。
5. 正式构建必须使用 `.go-version` 指定的精确 Go 版本；bootstrap 和 GitHub Actions 共用这一个 pin。

## 本地发布候选检查

在提交版本号之后、打 tag 之前：

```bash
python scripts/check.py --race --history

python scripts/build-release.py --out dist/release
python scripts/verify-release.py dist/release
```

开发期想测试打包器本身，可以显式使用：

```bash
python scripts/build-release.py --out .local/release-test --allow-dev --allow-dirty
python scripts/verify-release.py .local/release-test
```

## 正式发布

完整回归通过后创建 annotated tag 并推送：

```bash
git tag -a v0.6.0 -m "novel-mcp v0.6.0"
git push origin main
git push origin v0.6.0
```

`.github/workflows/release.yml` 会在 tag push 后重新执行安全审计、Go cold/vet/race、
官方 Go MCP SDK 真进程 E2E、官方 TypeScript MCP SDK v2 黑盒测试，然后生成并验证：

- Windows amd64 portable ZIP；
- Linux amd64 / arm64 tar.gz；
- macOS amd64 / arm64 tar.gz；
- `SHA256SUMS.txt`；
- `RELEASE-MANIFEST.json`（版本、commit、Go 版本、每个 artifact 的大小和 SHA-256）。

所有 gate 通过后才由 GitHub CLI 创建 Release。工作流不依赖第三方 release action。

## 可复现性

release 构建固定使用：

- `CGO_ENABLED=0`；
- `-trimpath`；
- `-buildvcs=false`；
- 空 Go build ID；
- Git commit 时间作为压缩包时间戳；
- 固定文件顺序、uid/gid 和权限。

同一 source commit + 同一 Go toolchain 构建两次，应得到相同的 artifact SHA-256。
