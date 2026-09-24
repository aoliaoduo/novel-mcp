package server

// MCP 上游原语的输出契约只约束稳定字段，同时允许工具自身继续增加可选事实字段。
// 这样 Host 能对 structuredContent 做类型验证，又不会因上游新增非破坏性字段而失效。

func openObject(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type": "object", "properties": props, "required": required, "additionalProperties": true,
	}
}

func nullableArraySchema(item any) map[string]any {
	return map[string]any{"anyOf": []any{
		arraySchema(item),
		map[string]any{"type": "null"},
	}}
}

func coreToolResultSchema(name string) map[string]any {
	stringArray := nullableArraySchema(map[string]any{"type": "string"})
	intArray := nullableArraySchema(map[string]any{"type": "integer"})
	violations := nullableArraySchema(openObject(nil))

	switch name {
	case "novel_context":
		return openObject(map[string]any{
			"_loading_summary":  map[string]any{"type": "string"},
			"_warnings":         stringArray,
			"working_memory":    openObject(nil),
			"episodic_memory":   openObject(nil),
			"planning_memory":   openObject(nil),
			"foundation_memory": openObject(nil),
			"reference_pack":    openObject(nil),
			"selected_memory":   openObject(nil),
		}, "_loading_summary")

	case "read_chapter":
		// read_chapter 有单章、范围、角色对话三种模式，因此不强制模式特有字段。
		return openObject(map[string]any{
			"chapter":    map[string]any{"type": "integer"},
			"from":       map[string]any{"type": "integer"},
			"to":         map[string]any{"type": "integer"},
			"source":     map[string]any{"type": "string", "enum": []string{"final", "draft"}},
			"exists":     map[string]any{"type": "boolean"},
			"content":    map[string]any{"type": "string"},
			"word_count": map[string]any{"type": "integer"},
			"chapters":   openObject(nil),
			"character":  map[string]any{"type": "string"},
			"samples":    stringArray,
			"hint":       map[string]any{"type": "string"},
			"status":     map[string]any{"type": "string"},
			"_warnings":  stringArray,
		})

	case "save_book":
		return openObject(map[string]any{
			"saved":            map[string]any{"type": "boolean"},
			"foundation_ready": map[string]any{"type": "boolean"},
			"remaining":        stringArray,
		}, "saved", "foundation_ready", "remaining")

	case "save_foundation":
		return openObject(map[string]any{
			"saved":             map[string]any{"type": "boolean"},
			"type":              map[string]any{"type": "string"},
			"scale":             map[string]any{"type": "string"},
			"foundation_ready":  map[string]any{"type": "boolean"},
			"remaining":         stringArray,
			"chapters":          map[string]any{"type": "integer"},
			"volumes":           map[string]any{"type": "integer"},
			"dynamic_planning":  map[string]any{"type": "boolean"},
			"outlined_chapters": map[string]any{"type": "integer"},
			"count":             map[string]any{"type": "integer"},
			"volume":            map[string]any{"type": "integer"},
			"final_volume":      map[string]any{"type": "boolean"},
			"finale_released":   map[string]any{"type": "boolean"},
			"arcs":              map[string]any{"type": "integer"},
			"book_complete":     map[string]any{"type": "boolean"},
			"phase":             map[string]any{"type": "string"},
			"ending_direction":  map[string]any{"type": "string"},
			"last_updated":      map[string]any{"type": "integer"},
		}, "saved", "type", "scale", "foundation_ready", "remaining")

	case "audit_foundation":
		return openObject(map[string]any{
			"foundation_ready": map[string]any{"type": "boolean"},
			"issues":           nullableArraySchema(openObject(nil)),
			"next_action":      map[string]any{"type": "string"},
			"phase":            map[string]any{"type": "string"},
		}, "foundation_ready", "issues")

	case "plan_chapter":
		return openObject(map[string]any{
			"chapter":   map[string]any{"type": "integer"},
			"planned":   map[string]any{"type": "boolean"},
			"skipped":   map[string]any{"type": "boolean"},
			"completed": map[string]any{"type": "boolean"},
			"reason":    map[string]any{"type": "string"},
			"next_step": map[string]any{"type": "string"},
		}, "chapter")

	case "draft_chapter":
		return openObject(map[string]any{
			"chapter":    map[string]any{"type": "integer"},
			"written":    map[string]any{"type": "boolean"},
			"mode":       map[string]any{"type": "string"},
			"word_count": map[string]any{"type": "integer"},
			"skipped":    map[string]any{"type": "boolean"},
			"completed":  map[string]any{"type": "boolean"},
			"reason":     map[string]any{"type": "string"},
			"next_step":  map[string]any{"type": "string"},
		}, "chapter")

	case "edit_chapter":
		// agentcore EditTool 的 passthrough 结果会随实现增加诊断字段，这里只锁住
		// novel-mcp 自己追加的字段。
		return openObject(map[string]any{
			"chapter":   map[string]any{"type": "integer"},
			"next_step": map[string]any{"type": "string"},
		})

	case "check_consistency":
		return openObject(map[string]any{
			"chapter":   map[string]any{"type": "integer"},
			"status":    map[string]any{"type": "string"},
			"_warnings": stringArray,
		}, "chapter")

	case "commit_chapter":
		// 正向提交与 rewrite/polish drain 共用一个宽联合契约；chapter 是唯一
		// 两条路径都稳定存在的字段。
		return openObject(map[string]any{
			"chapter":          map[string]any{"type": "integer"},
			"committed":        map[string]any{"type": "boolean"},
			"rewritten":        map[string]any{"type": "boolean"},
			"mode":             map[string]any{"type": "string"},
			"word_count":       map[string]any{"type": "integer"},
			"next_chapter":     map[string]any{"type": "integer"},
			"review_required":  map[string]any{"type": "boolean"},
			"review_reason":    map[string]any{"type": "string"},
			"hook_type":        map[string]any{"type": "string"},
			"dominant_strand":  map[string]any{"type": "string"},
			"arc_end":          map[string]any{"type": "boolean"},
			"volume_end":       map[string]any{"type": "boolean"},
			"volume":           map[string]any{"type": "integer"},
			"arc":              map[string]any{"type": "integer"},
			"needs_expansion":  map[string]any{"type": "boolean"},
			"needs_new_volume": map[string]any{"type": "boolean"},
			"next_volume":      map[string]any{"type": "integer"},
			"next_arc":         map[string]any{"type": "integer"},
			"book_complete":    map[string]any{"type": "boolean"},
			"flow":             map[string]any{"type": "string"},
			"remaining_queue":  intArray,
			"queue_drained":    map[string]any{"type": "boolean"},
			"rule_violations":  violations,
		}, "chapter")

	case "revise_outline":
		return openObject(map[string]any{
			"revised":           map[string]any{"type": "boolean"},
			"from_chapter":      map[string]any{"type": "integer"},
			"replacement":       map[string]any{"type": "integer"},
			"reason":            map[string]any{"type": "string"},
			"dynamic_planning":  map[string]any{"type": "boolean"},
			"outlined_chapters": map[string]any{"type": "integer"},
			"total_chapters":    map[string]any{"type": "integer"},
		}, "revised", "from_chapter", "replacement", "reason")

	case "resolve_outline_feedback":
		return openObject(map[string]any{
			"resolved":        map[string]any{"type": "integer"},
			"outline_changed": map[string]any{"type": "boolean"},
			"reason":          map[string]any{"type": "string"},
		}, "resolved", "outline_changed", "reason")

	case "expand_next_arc":
		return openObject(map[string]any{
			"saved":    map[string]any{"type": "boolean"},
			"type":     map[string]any{"type": "string"},
			"volume":   map[string]any{"type": "integer"},
			"arc":      map[string]any{"type": "integer"},
			"title":    map[string]any{"type": "string"},
			"goal":     map[string]any{"type": "string"},
			"chapters": map[string]any{"type": "integer"},
		}, "saved", "type", "volume", "arc", "title", "goal", "chapters")

	case "save_review":
		return openObject(map[string]any{
			"saved":             map[string]any{"type": "boolean"},
			"chapter":           map[string]any{"type": "integer"},
			"scope":             map[string]any{"type": "string"},
			"verdict":           map[string]any{"type": "string"},
			"affected_chapters": intArray,
			"issues":            map[string]any{"type": "integer"},
			"next_flow":         map[string]any{"type": "string"},
			"next_chapter":      map[string]any{"type": "integer"},
		}, "saved", "chapter", "scope", "verdict", "affected_chapters", "issues", "next_flow", "next_chapter")

	case "save_arc_summary":
		return openObject(map[string]any{
			"saved":             map[string]any{"type": "boolean"},
			"type":              map[string]any{"type": "string"},
			"volume":            map[string]any{"type": "integer"},
			"arc":               map[string]any{"type": "integer"},
			"snapshots":         map[string]any{"type": "integer"},
			"style_rules_saved": map[string]any{"type": "boolean"},
		}, "saved", "type", "volume", "arc", "snapshots", "style_rules_saved")

	case "save_volume_summary":
		return openObject(map[string]any{
			"saved":         map[string]any{"type": "boolean"},
			"type":          map[string]any{"type": "string"},
			"volume":        map[string]any{"type": "integer"},
			"book_complete": map[string]any{"type": "boolean"},
		}, "saved", "type", "volume")

	case "reopen_book":
		return openObject(map[string]any{
			"reopened":         intArray,
			"reason":           map[string]any{"type": "string"},
			"phase":            map[string]any{"type": "string"},
			"flow":             map[string]any{"type": "string"},
			"pending_rewrites": intArray,
		}, "reopened", "reason", "phase", "flow", "pending_rewrites")
	}
	return anySchema()
}
