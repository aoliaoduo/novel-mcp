// Modified for novel-mcp: local module import paths; see UPSTREAM.md.
// Package flow 实现垂类路由：Host 根据事实决定下一个调哪个子代理做什么。
//
// 设计原则：
//   - Route 是纯函数：输入 State，输出 *Instruction。无 IO、无 Store 调用，可单测。
//   - State 由 LoadState（非纯）从 Store 构造，一次性把路由需要的事实读齐。
//   - 返回 nil 是合法的：表示当前没有可由确定性事实推出的 Worker 指令；
//     Engine 再按终态、启动补裁或等待用户干预处理。
//
// Router 覆盖的是"查表型"决策（每章下一步、弧末后处理、队列驱动），
// 不覆盖"语义理解型"决策（选规划师、处理用户 Steer、输出总结）。
package flow

import (
	"fmt"
	"strings"

	"novel-mcp/internal/domain"
	storepkg "novel-mcp/internal/store"
)

// plannerForTier 从已落盘的规划级别推导规划师身份:short 归短篇规划师,
// mid/long 归长篇规划师(与启动 Arbiter 的选型口径一致)。
func plannerForTier(tier domain.PlanningTier) string {
	if tier == domain.PlanningTierShort {
		return "architect_short"
	}
	return "architect_long"
}

// Instruction 指示 Engine 下一步直接运行的 Worker 与任务。
type Instruction struct {
	Agent   string        // architect_long / architect_short / writer / editor
	Task    string        // 给子代理的任务描述
	Reason  string        // 路由理由（用于事件、日志与失败裁定）
	Chapter int           // writer 任务涉及的章节号（续写/重写/打磨）；0 表示不涉及（editor/architect 任务）
	Actions []RouteAction // 机器可读的合法 MCP 调用；Task 只负责解释语义，不再承担工具发现
}

type RouteActionMode string

const (
	RouteActionRequired RouteActionMode = "required"
	RouteActionChoice   RouteActionMode = "choice"
)

// RouteAction 是 Router 输出的工具级契约。Arguments 只包含由磁盘事实已经确定的
// 参数；RequiredInputs 列出仍需客户端/模型生成的业务参数。choice_group 相同的
// choice 动作互斥，客户端只能选择其中一个，不能按顺序全部执行。
type RouteAction struct {
	Tool           string
	Arguments      map[string]any
	RequiredInputs []string
	Purpose        string
	Mode           RouteActionMode
	ChoiceGroup    string
}

func requiredAction(tool string, args map[string]any, required []string, purpose string) RouteAction {
	if args == nil {
		args = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return RouteAction{Tool: tool, Arguments: args, RequiredInputs: required, Purpose: purpose, Mode: RouteActionRequired}
}

func choiceAction(group, tool string, args map[string]any, required []string, purpose string) RouteAction {
	a := requiredAction(tool, args, required, purpose)
	a.Mode = RouteActionChoice
	a.ChoiceGroup = group
	return a
}

func writerActions(chapter int) []RouteAction {
	return []RouteAction{
		requiredAction("novel_context", map[string]any{"chapter": chapter}, nil, "读取本章写作上下文与当前事实"),
		requiredAction("plan_chapter", map[string]any{"chapter": chapter}, []string{"title", "goal", "conflict", "hook"}, "生成并保存本章写作构思"),
		requiredAction("draft_chapter", map[string]any{"chapter": chapter, "mode": "write"}, []string{"content"}, "写入完整章节草稿；续写时客户端可显式改为 mode=append"),
		requiredAction("read_chapter", map[string]any{"chapter": chapter, "source": "draft"}, nil, "回读实际落盘草稿"),
		requiredAction("check_consistency", map[string]any{"chapter": chapter}, nil, "加载规则、伏笔、关系和摘要对照资料"),
		requiredAction("commit_chapter", map[string]any{"chapter": chapter}, []string{
			"title", "summary", "characters", "key_events", "timeline_events", "foreshadow_updates",
			"relationship_changes", "state_changes", "cast_intros", "hook_type", "dominant_strand", "feedback",
		}, "根据最终回读正文提交结构化章节事实和终稿"),
	}
}

func reviewActions(chapter int, scope string) []RouteAction {
	return []RouteAction{
		requiredAction("novel_context", map[string]any{"chapter": chapter}, nil, "读取评审所需上下文与已发生事实"),
		requiredAction("save_review", map[string]any{"chapter": chapter, "scope": scope}, []string{
			"dimensions", "issues", "contract_status", "contract_misses", "contract_notes", "verdict", "summary",
		}, "保存基于证据的评审结论并更新流程状态"),
	}
}

func planningRepairActions(missing []string, tier domain.PlanningTier) []RouteAction {
	actions := make([]RouteAction, 0, len(missing)+2)
	for _, item := range missing {
		switch item {
		case "book":
			actions = append(actions, requiredAction("save_book", nil, []string{"title", "synopsis"}, "补齐作品书名与简介"))
		case "compass":
			actions = append(actions, requiredAction("save_foundation", map[string]any{"type": "update_compass"},
				[]string{"content"}, "补齐长篇 Story Compass；content 使用指南针结构"))
		case "foundation_audit":
			actions = append(actions,
				requiredAction("novel_context", nil, nil, "读取全部基础设定与 foundation fingerprint"),
				requiredAction("audit_foundation", nil, []string{"fingerprint", "ready", "summary", "issues"}, "提交跨文件语义一致性审查"),
			)
		case "outline":
			outlineType := "outline"
			if tier == domain.PlanningTierLong {
				outlineType = "layered_outline"
			}
			args := map[string]any{"type": outlineType}
			if tier != "" {
				args["scale"] = string(tier)
			}
			actions = append(actions, requiredAction("save_foundation", args, []string{"content"}, "补齐与规划级别匹配的大纲"))
		default:
			args := map[string]any{"type": item}
			if tier != "" {
				args["scale"] = string(tier)
			}
			actions = append(actions, requiredAction("save_foundation", args, []string{"content"}, "补齐缺失的基础设定"))
		}
	}
	return actions
}

type AggregateKind string

const (
	AggregateArcReview     AggregateKind = "arc_review"
	AggregateArcSummary    AggregateKind = "arc_summary"
	AggregateVolumeSummary AggregateKind = "volume_summary"
	AggregateGlobalReview  AggregateKind = "global_review"
)

type AggregateRefresh struct {
	Kind         AggregateKind
	Volume       int
	Arc          int
	StartChapter int
	EndChapter   int
}

// State 是 Route 的输入：所有事实必须在此显式声明，禁止 Route 内部读 Store。
type State struct {
	Progress *domain.Progress

	// 已完成章节中的最大章节号；为 0 表示尚未开始写作。
	LastCompleted int

	// 上一章的弧边界信息；IsArcEnd=false 时其他字段无意义。
	// 当 LastCompleted=0 或非 Layered 模式时应为 nil。
	ArcBoundary *storepkg.ArcBoundary

	// 弧末后处理的三个事实：评审 / 弧摘要 / 卷摘要是否已完成。
	HasArcReview     bool
	HasArcSummary    bool
	HasVolumeSummary bool

	// 基础设定缺项（规划阶段的补齐信号）。
	FoundationMissing []string

	// 已落盘的规划级别（save_foundation 落 scale 时写入 RunMeta）。
	// 空 = 首次规划尚未产出任何设定，规划师身份不可判定。
	PlanningTier domain.PlanningTier

	// 非分层书：最近完成章是否已有 scope=global 的全局审阅
	//（仅在 ShouldReview 触发点有意义；分层书恒 false）。
	HasGlobalReview bool

	// 必须在续写前由 Architect 处理的外部修订影响。普通 Writer 反馈留到下一次
	// 自然结构操作统一吸收，不为每章额外派发规划师。
	ImmediateFeedbackCount int

	// 外部修订后最早一个需要由 Editor 重新生成的弧/卷工件。
	AggregateRefresh *AggregateRefresh
}

// Route 根据事实返回下一步确定性指令；返回 nil 由 Engine 按调用上下文处理。
//
// 决策优先级（互斥，自上而下匹配第一个）：
//  1. Phase=Complete        → nil（Host 确定性输出总结）
//  2. 规划期设定缺项且规划师可判定 → 同一规划师补齐；否则 nil（Engine 启动补裁）
//  3. PendingRewrites 非空  → writer 按队列重写/打磨
//  4. 外部修订导致聚合工件失效 → editor 重建
//  5. 外部修订影响后续规划     → architect 处理
//  6. 分层书到达弧末          → 评审、摘要、扩弧或续卷
//  7. 非分层全局审阅到期       → editor(global review)
//  8. 非分层大纲已耗尽        → architect(决定完结或续接大纲)
//  9. 其它                   → writer(写 next_chapter)
func Route(s State) *Instruction {
	p := s.Progress
	if p == nil {
		return nil
	}

	// 1. 终态：Host 根据 store 事实生成确定性总结
	if p.Phase == domain.PhaseComplete {
		return nil
	}

	// 2. 规划期补齐：查表型决策——缺什么在 store，规划师身份从已落盘的 scale 推导
	//    （short → architect_short，其余 → architect_long）。tier 为空说明首次规划
	//    尚未落盘任何设定（选型是语义判断），由 Engine 的 planStartFallback 补裁。
	if p.Phase != domain.PhaseWriting {
		if len(s.FoundationMissing) > 0 && s.PlanningTier != "" {
			task := fmt.Sprintf("补齐基础设定与作品信息缺项：%s；book 使用 save_book，其余基础设定使用 save_foundation 落盘", strings.Join(s.FoundationMissing, "、"))
			for _, item := range s.FoundationMissing {
				if item == "compass" {
					task += "；compass 对应 save_foundation(type=update_compass)"
				}
				if item == "outline" && s.PlanningTier == domain.PlanningTierLong {
					task += "；long 的 outline 对应 save_foundation(type=layered_outline)"
				}
			}
			if len(s.FoundationMissing) == 1 && s.FoundationMissing[0] == "foundation_audit" {
				task = "基础设定已齐全：重新调用 novel_context 读取全部已落盘工件与 foundation_status.fingerprint，审查跨文件语义一致性后调用 audit_foundation；有问题先修正并重新审查"
			}
			return &Instruction{
				Agent:   plannerForTier(s.PlanningTier),
				Task:    task,
				Reason:  "基础设定缺项未齐，照缺项续派同一规划师",
				Actions: planningRepairActions(s.FoundationMissing, s.PlanningTier),
			}
		}
		return nil
	}

	// 3. 重写/打磨队列优先（事实已在工具层落盘，Router 只照单派发）
	if len(p.PendingRewrites) > 0 {
		ch := p.PendingRewrites[0]
		verb := "重写"
		if p.Flow == domain.FlowPolishing {
			verb = "打磨"
		}
		return &Instruction{
			Agent:   "writer",
			Task:    fmt.Sprintf("%s第 %d 章", verb, ch),
			Reason:  fmt.Sprintf("PendingRewrites 队列剩余 %d 章", len(p.PendingRewrites)),
			Chapter: ch,
			Actions: writerActions(ch),
		}
	}

	if refresh := s.AggregateRefresh; refresh != nil {
		switch refresh.Kind {
		case AggregateArcReview:
			return &Instruction{
				Agent: "editor",
				Task: fmt.Sprintf(
					"审阅第 %d 卷第 %d 弧（第 %d-%d 章）：调用 novel_context(chapter=%d)，save_review 使用 scope=arc、chapter=%d",
					refresh.Volume, refresh.Arc, refresh.StartChapter, refresh.EndChapter, refresh.EndChapter, refresh.EndChapter,
				),
				Reason:  "弧级审阅缺失",
				Actions: reviewActions(refresh.EndChapter, "arc"),
			}
		case AggregateArcSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf("生成第 %d 卷第 %d 弧摘要、角色快照与写作规则（save_arc_summary）", refresh.Volume, refresh.Arc),
				Reason: "弧级摘要缺失",
				Actions: []RouteAction{requiredAction("save_arc_summary", map[string]any{"volume": refresh.Volume, "arc": refresh.Arc},
					[]string{"title", "summary", "key_events", "character_snapshots", "style_rules"}, "保存弧级摘要、角色快照与写作规则")},
			}
		case AggregateVolumeSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf("生成第 %d 卷卷摘要（save_volume_summary）", refresh.Volume),
				Reason: "卷摘要缺失",
				Actions: []RouteAction{requiredAction("save_volume_summary", map[string]any{"volume": refresh.Volume},
					[]string{"title", "summary", "key_events"}, "保存卷级摘要")},
			}
		case AggregateGlobalReview:
			return &Instruction{
				Agent:   "editor",
				Task:    fmt.Sprintf("审阅前 %d 章：调用 novel_context(chapter=%d)，save_review 使用 scope=global、chapter=%d", refresh.EndChapter, refresh.EndChapter, refresh.EndChapter),
				Reason:  "全局审阅缺失",
				Actions: reviewActions(refresh.EndChapter, "global"),
			}
		}
	}

	if s.ImmediateFeedbackCount > 0 {
		return &Instruction{
			Agent:  plannerForTier(s.PlanningTier),
			Task:   "仅处理 novel_context 中的外部修订 writer_feedback：核对已发生剧情与后续计划，需要调整时调用 revise_outline 或相应结构工具，无需调整时调用 resolve_outline_feedback；不得处理 foundation_status 或其它规划，落盘后用一句话结束",
			Reason: fmt.Sprintf("有 %d 条外部修订影响尚未传播到后续规划", s.ImmediateFeedbackCount),
			Actions: []RouteAction{
				requiredAction("novel_context", nil, nil, "读取外部修订反馈与当前规划事实"),
				choiceAction("feedback_resolution", "revise_outline", nil, []string{"from_chapter", "replacement", "reason"}, "后续计划确需调整时选择此动作"),
				choiceAction("feedback_resolution", "resolve_outline_feedback", nil, []string{"reason"}, "现有计划仍适用时选择此动作"),
			},
		}
	}

	// 6. 分层模式的弧末后处理
	if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.IsArcEnd {
		b := s.ArcBoundary
		switch {
		case !s.HasArcReview:
			return &Instruction{
				Agent: "editor",
				Task: fmt.Sprintf(
					"对第 %d 卷第 %d 弧（第 %d-%d 章）做弧级评审：调用 novel_context(chapter=%d)，save_review 使用 scope=arc、chapter=%d；issues[].chapters 只能落在该区间",
					b.Volume, b.Arc, b.StartChapter, b.EndChapter, b.EndChapter, b.EndChapter,
				),
				Reason:  "弧末评审未完成",
				Actions: reviewActions(b.EndChapter, "arc"),
			}
		case !s.HasArcSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf("生成第 %d 卷第 %d 弧摘要、角色快照与写作规则（save_arc_summary）", b.Volume, b.Arc),
				Reason: "弧摘要未完成",
				Actions: []RouteAction{requiredAction("save_arc_summary", map[string]any{"volume": b.Volume, "arc": b.Arc},
					[]string{"title", "summary", "key_events", "character_snapshots", "style_rules"}, "保存弧级摘要、角色快照与写作规则")},
			}
		case b.IsVolumeEnd && !s.HasVolumeSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf("生成第 %d 卷卷摘要（save_volume_summary）", b.Volume),
				Reason: "卷摘要未完成",
				Actions: []RouteAction{requiredAction("save_volume_summary", map[string]any{"volume": b.Volume},
					[]string{"title", "summary", "key_events"}, "保存卷级摘要")},
			}
		case b.NeedsExpansion && b.NextArc > 0:
			return &Instruction{
				Agent:  "architect_long",
				Task:   fmt.Sprintf("展开第 %d 卷第 %d 弧（expand_next_arc）", b.NextVolume, b.NextArc),
				Reason: "下一弧骨架待展开",
				Actions: []RouteAction{requiredAction("expand_next_arc", nil,
					[]string{"title", "goal", "chapters"}, "结合既有事实校准并展开下一故事弧")},
			}
		case b.NeedsNewVolume:
			return &Instruction{
				Agent:  "architect_long",
				Task:   "创建下一卷：按完结判定清单评估后调用 save_foundation——故事继续 → type=append_volume；故事接近终点 → type=append_volume 且卷 JSON 顶层带 \"final\": true（收官卷，整卷收线，写完自动完结）；全部完结条件当下已满足 → type=complete_book。三选一均须附 reason 参数写明判定理由",
				Reason: "卷末需决定追加新卷、收官卷或结束全书",
				Actions: []RouteAction{
					choiceAction("volume_end", "save_foundation", map[string]any{"type": "append_volume"}, []string{"content", "reason"}, "故事仍需继续时追加普通下一卷"),
					choiceAction("volume_end", "save_foundation", map[string]any{"type": "append_volume"}, []string{"content", "reason"}, "故事接近终点时追加收官卷；content 顶层必须 final=true"),
					choiceAction("volume_end", "save_foundation", map[string]any{"type": "complete_book", "content": map[string]any{}}, []string{"reason"}, "全部完结条件已经满足时结束全书"),
				},
			}
		}
	}

	// 7. 非分层全局审阅：每 ReviewInterval 章一次(事实:该章的 global review 未落盘)。
	//     原为 commit_chapter 返回值里的 review_required 信号,现按事实推导——
	//     返回值只是事实的镜像,Route 从 store 直接看同一事实。
	if !p.Layered && s.LastCompleted > 0 {
		if due, reason := domain.ShouldReview(len(p.CompletedChapters)); due && !s.HasGlobalReview {
			return &Instruction{
				Agent:   "editor",
				Task:    fmt.Sprintf("对前 %d 章做全局审阅（save_review scope=global, chapter=%d）", s.LastCompleted, s.LastCompleted),
				Reason:  reason,
				Actions: reviewActions(s.LastCompleted, "global"),
			}
		}
	}

	// 8. 非分层大纲耗尽时不能继续派发越界章节。让 Architect 基于当前故事事实
	// 决定完结，或用 revise_outline 从 next 章续接计划。
	next := p.NextChapter()
	if next <= 0 {
		return nil
	}
	if !p.Layered && p.TotalChapters > 0 && next > p.TotalChapters {
		return &Instruction{
			Agent: plannerForTier(s.PlanningTier),
			Task: fmt.Sprintf(
				"非分层大纲已写完（已完成 %d 章，共 %d 章）：若故事已收束，调用 save_foundation(type=complete_book)；若仍需继续，用 revise_outline 从第 %d 章续接后续计划",
				len(p.CompletedChapters), p.TotalChapters, next,
			),
			Reason: "非分层大纲已耗尽，需决定完结或续接",
			Actions: []RouteAction{
				choiceAction("outline_exhausted", "save_foundation", map[string]any{"type": "complete_book", "content": map[string]any{}}, []string{"reason"}, "故事已经收束时结束全书"),
				choiceAction("outline_exhausted", "revise_outline", map[string]any{"from_chapter": next}, []string{"replacement", "reason"}, "故事仍需继续时续接后续章节计划"),
			},
		}
	}

	// 9. 正常续写
	return &Instruction{
		Agent:   "writer",
		Task:    fmt.Sprintf("写第 %d 章", next),
		Reason:  "续写下一章",
		Chapter: next,
		Actions: writerActions(next),
	}
}
