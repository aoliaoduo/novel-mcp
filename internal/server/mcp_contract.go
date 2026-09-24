package server

import "github.com/modelcontextprotocol/go-sdk/mcp"

// MCP 输出契约集中放在适配层：上游工具只负责小说业务，不反向依赖 MCP。
// 对上游原语至少稳定声明 project/revision/result 外层；工程工具再把本层掌握的
// result 结构收紧。这样客户端可以依赖 structuredContent，又不会在这里复制
// 一份上游业务结果模型。

func anySchema() map[string]any { return map[string]any{} }

func arraySchema(item any) map[string]any {
	return map[string]any{"type": "array", "items": item}
}

func revisionSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}
}

func projectInfoSchema() map[string]any {
	return object(map[string]any{
		"id":         map[string]any{"type": "string"},
		"brief":      map[string]any{"type": "string"},
		"style":      map[string]any{"type": "string"},
		"created_at": map[string]any{"type": "string"},
	}, "id", "brief", "style", "created_at")
}

func projectEnvelopeSchema(result map[string]any) map[string]any {
	return object(map[string]any{
		"project":  projectProp(),
		"revision": revisionSchema(),
		"result":   result,
	}, "project", "revision", "result")
}

func routeActionSchema() map[string]any {
	return object(map[string]any{
		"id":                map[string]any{"type": "string"},
		"tool":              map[string]any{"type": "string"},
		"arguments":         map[string]any{"type": "object"},
		"required_inputs":   arraySchema(map[string]any{"type": "string"}),
		"requires_revision": map[string]any{"type": "boolean"},
		"revision_source":   map[string]any{"type": "string"},
		"depends_on":        arraySchema(map[string]any{"type": "string"}),
		"purpose":           map[string]any{"type": "string"},
		"mode":              map[string]any{"type": "string", "enum": []string{"required", "choice"}},
		"choice_group":      map[string]any{"type": "string"},
		"resource_uri":      map[string]any{"type": "string"},
	}, "id", "tool", "arguments", "required_inputs", "requires_revision", "revision_source", "depends_on", "purpose", "mode", "choice_group")
}

func nextStepResultSchema() map[string]any {
	return object(map[string]any{
		"done":                 map[string]any{"type": "boolean"},
		"need":                 map[string]any{"type": "string"},
		"agent":                map[string]any{"type": "string"},
		"chapter":              map[string]any{"type": "integer", "minimum": 1, "maximum": 10000},
		"task":                 map[string]any{"type": "string"},
		"reason":               map[string]any{"type": "string"},
		"summary":              map[string]any{"type": "string"},
		"flow":                 map[string]any{"type": "string"},
		"brief":                map[string]any{"type": "string"},
		"style":                map[string]any{"type": "string"},
		"foundation_missing":   arraySchema(map[string]any{"type": "string"}),
		"planning_tier":        map[string]any{"type": "string"},
		"context_resource_uri": map[string]any{"type": "string"},
		"actions":              arraySchema(routeActionSchema()),
	}, "done", "actions")
}

func outputSchemaFor(name string) map[string]any {
	switch name {
	case "list_projects":
		return object(map[string]any{"projects": arraySchema(projectInfoSchema())}, "projects")
	case "create_project":
		return object(map[string]any{
			"project": projectInfoSchema(), "revision": revisionSchema(), "next_tool": map[string]any{"type": "string"},
		}, "project", "revision", "next_tool")
	case "novel_guide":
		return object(map[string]any{
			"role": map[string]any{"type": "string"}, "guide": map[string]any{"type": "string"},
			"styles": arraySchema(map[string]any{"type": "string"}),
		}, "role", "guide", "styles")
	case "project_status":
		return projectEnvelopeSchema(object(map[string]any{
			"info": projectInfoSchema(), "progress": anySchema(),
			"foundation_missing": arraySchema(map[string]any{"type": "string"}),
			"pending_commit":     anySchema(), "warnings": arraySchema(map[string]any{"type": "string"}),
			"resource_uri": map[string]any{"type": "string"},
		}, "info", "progress", "foundation_missing", "pending_commit", "warnings", "resource_uri"))
	case "verify_project":
		issue := object(map[string]any{
			"severity": map[string]any{"type": "string", "enum": []string{"error", "warning"}},
			"code":     map[string]any{"type": "string"},
			"artifact": map[string]any{"type": "string"},
			"chapter":  map[string]any{"type": "integer"},
			"message":  map[string]any{"type": "string"},
		}, "severity", "code", "artifact", "message")
		return projectEnvelopeSchema(object(map[string]any{
			"ok":               map[string]any{"type": "boolean"},
			"checked_chapters": map[string]any{"type": "integer"}, "errors": map[string]any{"type": "integer"},
			"warnings": map[string]any{"type": "integer"}, "issues": arraySchema(issue),
		}, "ok", "checked_chapters", "errors", "warnings", "issues"))
	case "export_book":
		return projectEnvelopeSchema(object(map[string]any{
			"format": map[string]any{"type": "string", "const": "txt"}, "title": map[string]any{"type": "string"},
			"from_chapter": map[string]any{"type": "integer"}, "to_chapter": map[string]any{"type": "integer"},
			"chapters": arraySchema(map[string]any{"type": "integer"}), "skipped": arraySchema(map[string]any{"type": "integer"}),
			"content": map[string]any{"type": "string"},
		}, "format", "title", "from_chapter", "to_chapter", "chapters", "skipped", "content"))
	case "next_step":
		return projectEnvelopeSchema(nextStepResultSchema())
	case "delete_project":
		// 删除成功后项目自身已不存在，返回的是数据根 revision。若根目录里另有
		// 损坏/不安全项目导致根指纹无法计算，Delete 会按既有语义返回空 revision，
		// 不能让 MCP 输出校验把“已删除成功”翻成协议失败。
		return object(map[string]any{
			"project":  projectProp(),
			"revision": map[string]any{"type": "string", "pattern": "^(?:[a-f0-9]{64})?$"},
			"result":   object(map[string]any{"deleted": map[string]any{"type": "boolean"}}, "deleted"),
		}, "project", "revision", "result")
	default:
		return projectEnvelopeSchema(coreToolResultSchema(name))
	}
}

func ptrBool(v bool) *bool { return &v }

func toolTitle(name string) string {
	titles := map[string]string{
		"list_projects": "列出小说项目", "create_project": "创建小说项目", "delete_project": "删除小说项目",
		"novel_guide": "读取创作协议", "project_status": "读取项目状态", "verify_project": "核验项目完整性", "export_book": "导出已提交正文", "next_step": "获取下一步路由",
		"novel_context": "读取小说上下文", "save_book": "保存作品信息", "save_foundation": "保存基础设定", "audit_foundation": "审查基础设定",
		"plan_chapter": "规划章节", "draft_chapter": "写入章节草稿", "edit_chapter": "编辑章节草稿", "read_chapter": "读取章节",
		"check_consistency": "加载一致性对照", "commit_chapter": "提交章节终稿", "revise_outline": "修订后续大纲",
		"resolve_outline_feedback": "确认大纲反馈", "expand_next_arc": "展开下一故事弧", "save_review": "保存审阅结果",
		"save_arc_summary": "保存故事弧摘要", "save_volume_summary": "保存卷摘要", "reopen_book": "重开完本返工",
	}
	if title := titles[name]; title != "" {
		return title
	}
	return name
}

// annotationsFor 只声明客户端可安全依赖的粗粒度副作用提示；参数相关的幂等性
// （例如 draft_chapter write/append）不在静态 annotation 里猜。
func annotationsFor(name string, readOnly bool) *mcp.ToolAnnotations {
	closed := false
	destructive := !readOnly
	idempotent := readOnly
	switch name {
	case "create_project":
		destructive, idempotent = false, false
	case "delete_project":
		destructive, idempotent = true, false
	}
	return &mcp.ToolAnnotations{
		Title: toolTitle(name), ReadOnlyHint: readOnly, DestructiveHint: ptrBool(destructive),
		IdempotentHint: idempotent, OpenWorldHint: &closed,
	}
}
