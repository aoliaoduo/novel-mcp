// Modified for novel-mcp: local module import paths; see UPSTREAM.md.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/voocel/agentcore/schema"
	"novel-mcp/internal/domain"
	"novel-mcp/internal/store"
)

// References 嵌入的参考资料。
type References struct {
	// V0
	ChapterGuide      string
	HookTechniques    string
	QualityChecklist  string
	OutlineTemplate   string
	CharacterTemplate string
	ChapterTemplate   string
	// V1
	Consistency      string
	ContentExpansion string
	DialogueWriting  string
	// V2
	StyleReference   string // 风格补充参考（可为空）
	LongformPlanning string // 通用长篇规划参考
	Differentiation  string // 通用差异化设计参考
	ArcTemplates     string // 题材弧型模板（按 style 加载，可为空）
	AntiAITone       string // 去 AI 味判据库（writer/editor 共用，全程注入）
}

// ContextTool 组装当前章节所需上下文。
type ContextTool struct {
	store      *store.Store
	refs       References
	style      string
	styleStats *StyleStatsIndex
	castIndex  *CastIndex
}

type contextReads struct {
	warnings []string
	seen     map[string]struct{}
	err      error
	root     string
}

func (r *contextReads) warn(scope string, err error) {
	if err == nil || os.IsNotExist(err) {
		return
	}
	msg := diagnosticWarning(r.root, scope, err)
	if r.seen == nil {
		r.seen = make(map[string]struct{})
	}
	if _, ok := r.seen[msg]; ok {
		return
	}
	r.seen[msg] = struct{}{}
	r.warnings = append(r.warnings, msg)
}

func (r *contextReads) require(scope string, err error) {
	if r.err != nil || err == nil || os.IsNotExist(err) || errors.Is(err, store.ErrOutlineChapterNotFound) {
		return
	}
	r.err = fmt.Errorf("%s 读取失败: %w", scope, err)
}

func (r *contextReads) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// NewContextTool 创建上下文工具。styleStats 必须与 commit_chapter 共享，
// 否则重写章节后上下文会继续读取旧统计。
func NewContextTool(
	store *store.Store,
	refs References,
	style string,
	styleStats *StyleStatsIndex,
	castIndexes ...*CastIndex,
) *ContextTool {
	if styleStats == nil {
		panic("tools: NewContextTool requires StyleStatsIndex")
	}
	castIndex := NewCastIndex(store)
	if len(castIndexes) > 0 && castIndexes[0] != nil {
		castIndex = castIndexes[0]
	}
	return &ContextTool{store: store, refs: refs, style: style, styleStats: styleStats, castIndex: castIndex}
}

func (t *ContextTool) Name() string { return "novel_context" }
func (t *ContextTool) Description() string {
	return "获取小说当前状态和创作上下文。" +
		"不传 chapter：返回 progress_status（phase/flow/next_chapter/pending_rewrites 等进度字段）+ 精简规划概览，用于判断下一步该做什么；" +
		"长篇 Architect 可传 volume + arc 聚焦读取指定弧：已展开弧包含章节详情，骨架弧包含 title/goal/estimated_chapters。" +
		"传 chapter=N：额外返回该章的前情摘要、伏笔、角色状态、风格规则等写作上下文"
}
func (t *ContextTool) Label() string { return "加载上下文" }

// 纯读工具，可被并发调度。
func (t *ContextTool) ReadOnly(_ json.RawMessage) bool        { return true }
func (t *ContextTool) ConcurrencySafe(_ json.RawMessage) bool { return true }

func (t *ContextTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapter", schema.Int("章节号。不传则返回进度状态和基础设定（Architect 用）；传入则额外返回该章的写作上下文（Writer/Editor 用）")),
		schema.Property("volume", schema.Int("长篇 Architect 可选：聚焦读取的卷序号；已展开弧返回章节详情，骨架弧返回规划目标；必须与 arc 同时传入，不能与 chapter 同时使用")),
		schema.Property("arc", schema.Int("长篇 Architect 可选：聚焦读取的卷内弧序号；已展开弧返回章节详情，骨架弧返回规划目标；必须与 volume 同时传入，不能与 chapter 同时使用")),
	)
}

func (t *ContextTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter int `json:"chapter"`
		Volume  int `json:"volume"`
		Arc     int `json:"arc"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if a.Chapter < 0 || a.Volume < 0 || a.Arc < 0 {
		return nil, fmt.Errorf("chapter, volume and arc must be >= 0")
	}
	if a.Chapter > 0 && (a.Volume > 0 || a.Arc > 0) {
		return nil, fmt.Errorf("chapter cannot be combined with volume or arc")
	}
	if (a.Volume > 0) != (a.Arc > 0) {
		return nil, fmt.Errorf("volume and arc must be provided together")
	}

	result := make(map[string]any)
	reads := &contextReads{root: t.store.Dir()}

	if a.Chapter > 0 {
		// Writer 路径：加载全量基础数据 + 章节上下文
		t.buildBaseContext(result, reads)
		seed := newChapterContextEnvelope()
		state := t.prepareChapterContext(a.Chapter, &seed, reads)
		seed.apply(result)
		t.buildChapterContext(result, state, reads)
		// episodic 是已写入正文的备忘，不是待写素材。
		if epi, ok := result["episodic_memory"].(map[string]any); ok && len(epi) > 0 {
			epi["_usage"] = "本容器为已写入正文的事实备忘（供一致性与衔接对照）；在新章正文中原样复述这些内容属于重复缺陷"
		}
	} else {
		// Architect 路径：只返回状态 + 结构化数据，不加载全量原文
		t.buildProgressStatus(result, reads)
		t.buildArchitectContext(result, reads, a.Volume, a.Arc)
	}

	mechanicalRules := t.buildUserRules(result)
	if a.Chapter > 0 {
		t.buildRuleViolations(result, a.Chapter, mechanicalRules, reads)
	}

	if reads.err != nil {
		return nil, reads.err
	}
	if len(reads.warnings) > 0 {
		result["_warnings"] = reads.warnings
	}

	// 工具层只做与任务相关的语义选择；上下文体积由各 Worker 按实际模型窗口管理。
	result["_loading_summary"] = buildLoadingSummary(result, a.Chapter)

	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal context payload: %w", err)
	}
	return data, nil
}

// buildLoadingSummary 从已组装的 result 中统计各项数据量，生成一行可读摘要。
func buildLoadingSummary(result map[string]any, chapter int) string {
	var parts []string
	working, _ := result["working_memory"].(map[string]any)
	episodic, _ := result["episodic_memory"].(map[string]any)
	planning, _ := result["planning_memory"].(map[string]any)
	foundation, _ := result["foundation_memory"].(map[string]any)
	referencePack, _ := result["reference_pack"].(map[string]any)

	if chapter > 0 {
		parts = append(parts, fmt.Sprintf("ch=%d", chapter))
		if tier, ok := episodic["planning_tier"].(domain.PlanningTier); ok && tier != "" {
			parts = append(parts, fmt.Sprintf("tier=%s", tier))
		}
	} else {
		parts = append(parts, "architect")
		if tier, ok := planning["planning_tier"].(domain.PlanningTier); ok && tier != "" {
			parts = append(parts, fmt.Sprintf("tier=%s", tier))
		}
	}

	if pos, ok := episodic["position"].(map[string]any); ok {
		parts = append(parts, fmt.Sprintf("V%dA%d", pos["volume"], pos["arc"]))
	}

	var items []string

	if n := firstSliceLen(episodic["character_snapshots"], foundation["character_snapshots"]); n > 0 {
		items = append(items, fmt.Sprintf("角色:%d(快照)", n))
	} else if n := firstSliceLen(episodic["characters"], foundation["characters"]); n > 0 {
		items = append(items, fmt.Sprintf("角色:%d", n))
	}

	if len(working) > 0 {
		items = append(items, fmt.Sprintf("工作记忆:%d", len(working)))
	}
	if len(episodic) > 0 {
		items = append(items, fmt.Sprintf("情节记忆:%d", len(episodic)))
	}
	if len(planning) > 0 {
		items = append(items, fmt.Sprintf("规划记忆:%d", len(planning)))
	}
	if len(foundation) > 0 {
		items = append(items, fmt.Sprintf("基础记忆:%d", len(foundation)))
	}

	if n := firstSliceLen(working["volume_summaries"], planning["volume_summaries"]); n > 0 {
		items = append(items, fmt.Sprintf("卷摘要:%d", n))
	}
	if n := firstSliceLen(working["arc_summaries"], planning["arc_summaries"]); n > 0 {
		items = append(items, fmt.Sprintf("弧摘要:%d", n))
	}
	if n := sliceLen(working["recent_summaries"]); n > 0 {
		items = append(items, fmt.Sprintf("章摘要:%d", n))
	}

	if n := sliceLen(planning["layered_outline"]); n > 0 {
		items = append(items, fmt.Sprintf("分层大纲:%d卷", n))
	}

	if n := sliceLen(working["timeline"]); n > 0 {
		items = append(items, fmt.Sprintf("时间线:%d", n))
	}
	if n := firstSliceLen(episodic["foreshadow_ledger"], foundation["foreshadow_ledger"]); n > 0 {
		items = append(items, fmt.Sprintf("伏笔:%d", n))
	}
	if n := sliceLen(episodic["relationship_state"]); n > 0 {
		items = append(items, fmt.Sprintf("关系:%d", n))
	}
	if n := sliceLen(episodic["recent_state_changes"]); n > 0 {
		items = append(items, fmt.Sprintf("状态变化:%d", n))
	}
	if _, ok := working["previous_tail"]; ok {
		items = append(items, "前章尾部:ok")
	}
	if _, ok := referencePack["style_rules"]; ok {
		items = append(items, "风格规则:ok")
	}
	if n := sliceLen(episodic["related_chapters"]); n > 0 {
		items = append(items, fmt.Sprintf("相关章:%d", n))
	}
	if selected, ok := result["selected_memory"].(map[string]any); ok && len(selected) > 0 {
		if n := sliceLen(selected["story_threads"]); n > 0 {
			items = append(items, fmt.Sprintf("线索召回:%d", n))
		}
		if n := sliceLen(selected["review_lessons"]); n > 0 {
			items = append(items, fmt.Sprintf("评审召回:%d", n))
		}
	}

	if refs, ok := referencePack["references"].(map[string]string); ok && len(refs) > 0 {
		items = append(items, fmt.Sprintf("参考:%d项", len(refs)))
	}
	if len(referencePack) > 0 {
		items = append(items, fmt.Sprintf("参考包:%d", len(referencePack)))
	}
	if _, ok := result["memory_policy"]; ok {
		items = append(items, "记忆策略:ok")
	}
	if warnings, ok := result["_warnings"].([]string); ok && len(warnings) > 0 {
		items = append(items, fmt.Sprintf("告警:%d", len(warnings)))
	}
	if len(items) > 0 {
		parts = append(parts, strings.Join(items, " "))
	}
	return strings.Join(parts, " | ")
}

// sliceLen 对 any 类型尝试取 slice 长度。
func sliceLen(v any) int {
	switch s := v.(type) {
	case []domain.ChapterSummary:
		return len(s)
	case []domain.ArcSummary:
		return len(s)
	case []domain.VolumeSummary:
		return len(s)
	case []domain.CharacterSnapshot:
		return len(s)
	case []domain.TimelineEvent:
		return len(s)
	case []domain.ForeshadowEntry:
		return len(s)
	case []domain.RelationshipEntry:
		return len(s)
	case []domain.StateChange:
		return len(s)
	case []domain.VolumeOutline:
		return len(s)
	case []domain.Character:
		return len(s)
	case []domain.RelatedChapter:
		return len(s)
	case []domain.RecallItem:
		return len(s)
	case []planningVolumeOutline:
		return len(s)
	default:
		return 0
	}
}

func firstSliceLen(values ...any) int {
	for _, value := range values {
		if n := sliceLen(value); n > 0 {
			return n
		}
	}
	return 0
}

// loadFilteredCharacters 按 Tier 和场景出场过滤角色。
// core/important 始终返回；secondary/decorative 只在当前章节大纲提及时返回。
