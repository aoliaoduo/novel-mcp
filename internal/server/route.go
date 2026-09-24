// Package server 里的路由出口：把 flow.Route 暴露给 MCP 客户端。
// MCP 模式没有内置 Engine，“问路由→执行→问路由”就是客户端的主循环。
package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"novel-mcp/internal/domain"
	"novel-mcp/internal/flow"
	"novel-mcp/internal/store"
)

// action 是 next_step 的机器可执行层。arguments 只放服务端已经确定的值；
// 仍需要模型创作/判断的字段列在 required_inputs，客户端不必再从 task 文本猜
// “下一步应该调用哪个工具”，但也不会把占位符当成真实业务参数。
func action(tool string, args map[string]any, required []string, purpose string) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"tool": tool, "arguments": args, "required_inputs": required,
		"requires_revision": toolRequiresRevision(tool),
		"purpose":           purpose, "mode": string(flow.RouteActionRequired), "choice_group": "",
	}
}

func toolRequiresRevision(tool string) bool {
	if readTools[tool] {
		return false
	}
	switch tool {
	case "list_projects", "create_project", "novel_guide", "project_status", "verify_project", "export_book", "next_step":
		return false
	default:
		return true
	}
}

func actionNeedsProject(tool string) bool {
	switch tool {
	case "list_projects", "create_project", "novel_guide":
		return false
	default:
		return true
	}
}

// decoratePlan 只补执行元数据，不执行任何动作。revision_source="plan" 表示使用
// 当前 next_step 外层 envelope 的 revision；否则值是某个先前 action id，表示使用该
// action 返回的 revision。这样 Host 不需要从自然语言推断乐观锁链。
func decoratePlan(project string, out map[string]any) map[string]any {
	actions, ok := out["actions"].([]map[string]any)
	if !ok {
		return out
	}
	lastRequired := ""
	lastRevision := ""
	for i, item := range actions {
		id := fmt.Sprintf("a%d", i+1)
		item["id"] = id
		tool, _ := item["tool"].(string)
		args, _ := item["arguments"].(map[string]any)
		if args == nil {
			args = map[string]any{}
			item["arguments"] = args
		}
		if actionNeedsProject(tool) {
			args["project"] = project
		}
		deps := []string{}
		if lastRequired != "" {
			deps = append(deps, lastRequired)
		}
		item["depends_on"] = deps
		requires, _ := item["requires_revision"].(bool)
		if !requires {
			item["revision_source"] = "none"
			item["expected_revision_source"] = "none"
		} else if lastRevision == "" {
			item["revision_source"] = "plan"
			item["expected_revision_source"] = "next_step.revision"
		} else {
			item["revision_source"] = lastRevision
			item["expected_revision_source"] = lastRevision + ".revision"
		}
		if uri := actionResourceURI(project, tool, args); uri != "" {
			item["resource_uri"] = uri
		}
		if mode, _ := item["mode"].(string); mode == string(flow.RouteActionRequired) {
			lastRequired = id
			// 纯读 action 返回 revision 但不会改变它。乐观锁只追踪最近一次
			// 可能改盘的 required action，避免 Agent 在 read/check 结果间搬运同一指纹。
			if requires {
				lastRevision = id
			}
		}
	}
	out["actions"] = actions
	if chapter, ok := out["chapter"].(int); ok && chapter > 0 {
		out["context_resource_uri"] = chapterContextResourceURI(project, chapter)
	} else {
		out["context_resource_uri"] = projectContextResourceURI(project)
	}
	return out
}

// bindPlanRevision 把当前 next_step 已知的 revision 直接填进首批写 action。
// 后续写 action 的 revision 要等前序写入完成后才知道，因此只通过
// expected_revision_source=<action>.revision 描述绑定关系，绝不预填陈旧值。
func bindPlanRevision(out map[string]any, revision string) {
	if revision == "" {
		return
	}
	actions, ok := out["actions"].([]map[string]any)
	if !ok {
		return
	}
	for _, item := range actions {
		if item["revision_source"] != "plan" {
			continue
		}
		args, _ := item["arguments"].(map[string]any)
		if args != nil {
			args["expected_revision"] = revision
		}
	}
}

func routeActions(actions []flow.RouteAction) []map[string]any {
	out := make([]map[string]any, 0, len(actions))
	for _, a := range actions {
		item := action(a.Tool, a.Arguments, a.RequiredInputs, a.Purpose)
		item["mode"] = string(a.Mode)
		item["choice_group"] = a.ChoiceGroup
		out = append(out, item)
	}
	return out
}

// NextStep 对项目跑一轮确定性路由，返回客户端下一步 exact 干什么。
// 纯读：只 LoadState + Route，不写盘，因此不需要 expected_revision。
// 返回的 map 进 Call 的 {project, revision, result} 信封，revision 供后续写工具链式使用。
func NextStep(st *store.Store, info ProjectInfo) (map[string]any, error) {
	// 提交中断优先：pending_commit 不收尾，任何新派单都可能写出第二份正文。
	pending, err := st.Signals.LoadPendingCommit()
	if err != nil {
		return nil, err
	}
	if pending != nil {
		args := map[string]any{"chapter": pending.Chapter}
		required := []string{
			"title", "summary", "characters", "key_events", "timeline_events", "foreshadow_updates",
			"relationship_changes", "state_changes", "cast_intros", "hook_type", "dominant_strand", "feedback",
		}
		if len(pending.Payload) > 0 {
			if err := json.Unmarshal(pending.Payload, &args); err == nil {
				required = []string{}
			}
		}
		return decoratePlan(info.ID, map[string]any{
			"done":    false,
			"need":    "resume_commit",
			"agent":   "writer",
			"chapter": pending.Chapter,
			"task":    fmt.Sprintf("第 %d 章上次提交未收尾（stage=%s）：用同一参数重放 commit_chapter，上游会用落盘的冻结载荷继续，不要用聊天记忆重建正文；收尾后再调 next_step", pending.Chapter, pending.Stage),
			"reason":  "上次提交中断，先收尾再领新活",
			"actions": []map[string]any{action("commit_chapter", args, required, "重放冻结提交，完成中断的 Commit Saga")},
		}), nil
	}
	s, err := flow.LoadState(st)
	if err != nil {
		return nil, err
	}
	if inst := flow.Route(s); inst != nil {
		task := inst.Task
		if inst.Agent == "writer" {
			if s.Progress != nil && len(s.Progress.PendingRewrites) > 0 && s.Progress.PendingRewrites[0] == inst.Chapter {
				task += rewriteProtocol(inst.Chapter)
			} else {
				task += writerProtocol(inst.Chapter)
			}
		}
		out := map[string]any{
			"done": false, "agent": inst.Agent, "task": task, "reason": inst.Reason,
		}
		if inst.Chapter > 0 {
			out["chapter"] = inst.Chapter
		}
		out["actions"] = routeActions(inst.Actions)
		return decoratePlan(info.ID, out), nil
	}
	return decoratePlan(info.ID, routeNil(s, info)), nil
}

// writerProtocol 是写一章的固定工具顺序（与 instructions 的主循环同源，
// 内联进 task 免得客户端翻文档）。chapter<=0 时不点名章节号。
func writerProtocol(chapter int) string {
	target := ""
	if chapter > 0 {
		target = fmt.Sprintf("(chapter=%d)", chapter)
	}
	return "；写作协议（固定顺序）：novel_context" + target +
		" → plan_chapter → draft_chapter → read_chapter(source=draft) 回读" +
		" → check_consistency → 按回读正文自行语义修订 → commit_chapter；" +
		"正文一改就重新回读和检查，收尾后再调 next_step。"
}

// rewriteProtocol 与普通写章协议分开。已完成章节的 plan_chapter 会被工具层跳过，
// 返工时要求它只会制造无效调用。
func rewriteProtocol(chapter int) string {
	target := ""
	if chapter > 0 {
		target = fmt.Sprintf("(chapter=%d)", chapter)
	}
	return "；返工协议（固定顺序）：novel_context" + target +
		" → draft_chapter(mode=write) 写入返工后的完整草稿" +
		" → read_chapter(source=draft) 回读 → check_consistency" +
		" → 按回读正文自行语义修订 → commit_chapter；" +
		"不要重新 plan_chapter，正文一改就重新回读和检查，收尾后再调 next_step。"
}

// routeNil 处理 Route 返回 nil 的四种情形：完本、开局未定型、
// Flow 让位人工、其它（next<=0 等）。一律给出明确的 need + 下一步。
func routeNil(s flow.State, info ProjectInfo) map[string]any {
	p := s.Progress
	if p == nil {
		return planStart(s, info, "项目刚创建，还没有任何进度")
	}
	if p.Phase == domain.PhaseComplete {
		n := len(p.CompletedChapters)
		return map[string]any{
			"done":        true,
			"need":        "complete",
			"summary":     fmt.Sprintf("全书已完结（共 %d 章），正常写作流程到此结束。若要修改已完成章节，显式调用 reopen_book(chapters, reason)，再按 next_step 的返工路由处理；不要直接覆盖终稿。", n),
			"reopen_hint": "reopen_book 只用于返工已完成章节；它会把指定章节加入返工队列，返工排空后自动重新完结。",
			"actions":     []map[string]any{},
		}
	}
	if p.Phase != domain.PhaseWriting {
		return planStart(s, info, "规划阶段尚未产出可用设定")
	}
	return map[string]any{
		"done": false, "need": "decide",
		"task":    "路由无派单：读 project_status 看清进度与告警后自行决定下一步（常见于大纲耗尽且未触发续接分支，或数据异常），然后调 next_step 继续。",
		"reason":  "无确定性派单，转人工判断",
		"actions": []map[string]any{action("project_status", map[string]any{}, nil, "读取完整事实后决定下一步")},
	}
}

// planStart 给开局/规划期拼“选型+落设定”的确定步骤。brief/style/缺项都带上，
// 客户端照做即可，不用猜。
func planStart(s flow.State, info ProjectInfo, reason string) map[string]any {
	foundationMissing := s.FoundationMissing
	if foundationMissing == nil {
		foundationMissing = []string{}
	}
	missing := "book、premise、outline、characters、world_rules"
	if len(foundationMissing) > 0 {
		missing = strings.Join(foundationMissing, "、")
	}
	tier := string(s.PlanningTier)
	who := "短篇用 architect_short（单卷单冲突高密度），长篇连载用 architect_long（分层卷弧）"
	guideRole := ""
	if tier != "" {
		who = "规划级别已定为 " + tier + "（short 走 architect_short，其余走 architect_long），沿用它补齐"
		if tier == string(domain.PlanningTierShort) {
			guideRole = "architect"
		} else {
			guideRole = "architect_long"
		}
	}
	guideArgs := map[string]any{}
	guideRequired := []string{"role：根据 brief 在 architect / architect_long 中选择"}
	if guideRole != "" {
		guideArgs["role"] = guideRole
		guideRequired = []string{}
	}

	// 首次规划时 tier 仍为空。此时不能在同一个机器计划里提前猜 short/long
	// 后续大纲究竟是 outline 还是 layered_outline。先只完成“选型 + 落一个带
	// scale 的种子设定”，再 next_step；下一轮拿到已落盘 tier 后，Router 会把
	// 每个剩余缺项展开成一个确定的 action。
	if tier == "" {
		actions := []map[string]any{
			action("novel_guide", guideArgs, guideRequired, "根据 brief 选择短篇或长篇规划协议"),
		}
		if containsString(foundationMissing, "book") {
			actions = append(actions, action("save_book", map[string]any{}, []string{"title", "synopsis"}, "落盘正式书名与读者简介"))
		}
		if containsString(foundationMissing, "premise") {
			actions = append(actions, action("save_foundation", map[string]any{"type": "premise"},
				[]string{"content", "scale：short / mid / long"}, "保存故事前提并确定规划级别；完成后重新调用 next_step"))
		} else {
			actions = append(actions, action("save_foundation", map[string]any{},
				[]string{"type", "content", "scale：short / mid / long"}, "给一个尚缺的基础设定写入 scale，确定规划级别；完成后重新调用 next_step"))
		}
		return map[string]any{
			"done": false, "need": "plan_start", "agent": "architect",
			"brief": info.Brief, "style": info.Style,
			"foundation_missing": foundationMissing, "planning_tier": tier,
			"task":    "首次规划只做两件事：先根据 brief 选择 novel_guide(role=architect 或 architect_long)；再调用 save_book（若缺）落书名，并用 save_foundation(type=premise, scale=short|mid|long) 保存种子设定。完成后立刻重新调用 next_step，让服务器根据已落盘 short/mid/long 生成准确的后续设定 actions；不要在当前 plan 里自行猜 outline / layered_outline。项目 brief：" + info.Brief,
			"reason":  reason,
			"actions": actions,
		}
	}

	actions := []map[string]any{
		action("novel_guide", guideArgs, guideRequired, "读取与篇幅匹配的规划协议"),
	}
	for _, a := range flow.PlanningRepairActions(foundationMissing, s.PlanningTier) {
		item := action(a.Tool, a.Arguments, a.RequiredInputs, a.Purpose)
		item["mode"] = string(a.Mode)
		item["choice_group"] = a.ChoiceGroup
		actions = append(actions, item)
	}
	return map[string]any{
		"done": false, "need": "plan_start", "agent": "architect",
		"brief": info.Brief, "style": info.Style,
		"foundation_missing": foundationMissing, "planning_tier": tier,
		"task":    "开局：" + who + "。步骤：读取匹配篇幅的 novel_guide → 按 actions 逐项补齐缺项（" + missing + "）；基础设定齐全后 next_step 会要求 novel_context 取 fingerprint 并 audit_foundation。每完成当前 actions 都重新调 next_step，不要自行跳过路由。项目 brief：" + info.Brief,
		"reason":  reason,
		"actions": actions,
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
