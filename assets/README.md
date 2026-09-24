# 内嵌创作资产

`assets` 只保存当前 MCP 运行时实际会暴露或注入的静态资源。所有资源编译进二进制；服务端不会从 HOME、cwd 或项目外目录加载覆盖文件。

| 目录/文件 | 当前用途 |
| --- | --- |
| `prompts/architect-short.md` | `novel_guide(role=architect)` / `novel_architect_short` |
| `prompts/architect-long.md` | `novel_guide(role=architect_long)` / `novel_architect_long` |
| `prompts/writer.md` | `novel_guide(role=writer)` / `novel_writer`；`{{VOICE}}` 由 `voice.md` 原位替换 |
| `prompts/editor.md` | `novel_guide(role=editor)` / `novel_editor` |
| `references/` | 由 `novel_context` 按角色和章节裁剪注入 `reference_pack` |
| `references/genres/<style>/` | style 专属 `style-references.md` / `arc-templates.md`；目录名同时定义可用 style |
| `voice.md` | Writer 的内嵌写作标准 |

机械禁词/疲劳词基线不在资产目录维护，见 `internal/rules/defaults.go`。`working_memory.user_rules.structured` 只是这份确定性基线的上下文投影，不代表额外的 HOME/cwd 用户规则来源。

上游 Host 曾使用的 Arbiter、import、simulation、revision 一次性 Prompt 已从本项目移除；如果未来某个能力真正通过 MCP 暴露，再随该能力一起添加对应资产和测试。
