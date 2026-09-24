# 上游归属与边界

本项目的小说业务核心改编自 https://github.com/voocel/ainovel-cli 。
来源提交：3390e34e0a7756403977ad14f456a174c10b307f；许可证 Apache-2.0，完整文本见 LICENSE。
原目录只读，未导入其 .git、远端、运行数据或个人配置。

复用 internal 下 tools/store 及其当前 MCP 所需的依赖闭包、assets 和相应测试。
导入后会继续裁掉只服务于上游 Host/CLI 的代码；修改文件有明确标记。
上游原文件 SHA-256 见 upstream-manifest.json，便于后续审阅和同步。

新增 cmd/novel-mcp、internal/server 与项目管理适配层不来自上游。
不启动上游模型提供商、Worker/Arbiter/Engine、TUI、shell 或通用文件工具。
网页 AI 承担语义创作与评审；原工具负责保存、前置条件、事实和 checkpoint。

Open Bridge 仅作为 HTTP MCP、安全边界及运维设计参考，当前未复制其源代码。

## 已移除的上游文件（2026-09-23）

以下文件随依赖闭包拷入，但 novel-mcp 的生产路径从不调用；其中
Analyze/Service/Execute 要求一个 live 模型，与"服务器不调用任何模型"
的定位矛盾，故删除。upstream-manifest.json 对应条目同步移除；
将来如需取回，按上文来源提交重新拷入即可：

- internal/revision/analyze.go、service.go、scan.go：
  修订分析服务与外部正文扫描；保留 projector.go
 （NewProjector/ValidateRecords，commit_chapter 在用）。
- internal/llmcontract/execute.go、internal/llmretry/（整包）：
  Execute 调用链与重试内核；llmcontract 仅保留 Nullable
  与 ValidateStrictReady（schema 小工具，无模型调用）。
- internal/llmcontract/validate.go：ValidateJSON 仅被 Execute 与测试引用。
- 测试同步裁剪：revision_test.go 仅留 Projector 用例，
  contract_test.go 仅留 Nullable 用例。

本轮继续移除从上游依赖闭包带入、但 MCP 从不生产或消费的 Host 能力：

- runtime/session/usage/advance 状态与存储，以及 reviewing/steering/advance
  这组只有 Host 会写入的运行态；MCP flow 只保留 writing/rewriting/polishing。
- RunMeta 中 StartPrompt/PlanStart/PendingSteer/AdvanceHold 等 Host 字段；
  当前只保留来源、style/model 与 PlanningTier。
- user_rules 的 HOME/cwd 扫描、快照归一化与持久化链；当前 MCP 没有规则写入工具，
  仅保留服务端确定性的机械规则基线。
- SimulationProfile 的 domain/store/context 注入链；当前 MCP 没有仿写画像生成入口。
- Arbiter、import、simulation、revision 一次性 prompts，以及未接线的参考资料。
- assets/styles/*.md：其内容没有进入任何 MCP Prompt/context；可用 style 现在直接由
  references/genres/<style>/ 目录定义，避免维护两套不一致的题材配置。

这些删除改变开发期项目磁盘契约，因此项目格式提升为 v5；项目仍只接受当前格式，
不做旧开发数据迁移。

## 增补复用（v0.3.0）

- internal/exp/txt.go、txt_test.go：取自上游 internal/host/exp/（TXT 排版与
  单测原文，仅模块路径不同），供 export_book 只读渲染；上游原文件 SHA 见
  upstream-manifest.json。internal/exp/export.go 是本仓库的公开包装，不来自上游。
- internal/server/reopen.go：本仓库新增，把上游 ReopenBook 包成 MCP 写工具。
