// Modified for novel-mcp: local module import paths; see UPSTREAM.md.
package tools

import (
	"novel-mcp/internal/domain"
	"novel-mcp/internal/rules"
)

type contextBuildState struct {
	chapter         int
	profile         domain.ContextProfile
	progress        *domain.Progress
	runMeta         *domain.RunMeta
	outline         []domain.OutlineEntry
	currentEntry    *domain.OutlineEntry
	chapterPlan     *domain.ChapterPlan
	storyThreads    []domain.RecallItem
	foreshadow      []domain.ForeshadowEntry
	relationships   []domain.RelationshipEntry
	allStateChanges []domain.StateChange
	styleRules      *domain.WritingStyleRules
	cast            []domain.CastEntry
}

type chapterContextEnvelope struct {
	Working    map[string]any
	Episodic   map[string]any
	References map[string]any
	Selected   map[string]any
}

type architectContextEnvelope struct {
	Planning   map[string]any
	Foundation map[string]any
	References map[string]any
}

// planningVolumeOutline 是 Architect 的只读结构投影。全局保留卷弧骨架，
// 仅当前弧或显式聚焦弧携带章节详情，避免章节详情随规划规模线性膨胀。
type planningVolumeOutline struct {
	Index int                  `json:"index"`
	Title string               `json:"title"`
	Theme string               `json:"theme"`
	Final bool                 `json:"final,omitempty"`
	Arcs  []planningArcOutline `json:"arcs"`
}

type planningArcOutline struct {
	Index             int                   `json:"index"`
	Title             string                `json:"title"`
	Goal              string                `json:"goal"`
	Status            string                `json:"status"`
	StartChapter      int                   `json:"start_chapter,omitempty"`
	EndChapter        int                   `json:"end_chapter,omitempty"`
	ChapterCount      int                   `json:"chapter_count,omitempty"`
	EstimatedChapters int                   `json:"estimated_chapters,omitempty"`
	Chapters          []domain.OutlineEntry `json:"chapters,omitempty"`
	ChaptersOmitted   bool                  `json:"chapters_omitted,omitempty"`
}

func newChapterContextEnvelope() chapterContextEnvelope {
	return chapterContextEnvelope{
		Working:    make(map[string]any),
		Episodic:   make(map[string]any),
		References: make(map[string]any),
		Selected:   make(map[string]any),
	}
}

func newArchitectContextEnvelope() architectContextEnvelope {
	return architectContextEnvelope{
		Planning:   make(map[string]any),
		Foundation: make(map[string]any),
		References: make(map[string]any),
	}
}

func (e chapterContextEnvelope) apply(result map[string]any) {
	// 章节路径会先后应用准备阶段和构建阶段的内容，因此合并已有分区。
	mergeEnvelopeSection(result, "working_memory", e.Working)
	mergeEnvelopeSection(result, "episodic_memory", e.Episodic)
	mergeEnvelopeSection(result, "reference_pack", e.References)
	if len(e.Selected) > 0 {
		mergeEnvelopeSection(result, "selected_memory", e.Selected)
	}
}

// mergeEnvelopeSection 把 section 合并进 result[key] 的既有容器；容器不存在时直接挂载。
func mergeEnvelopeSection(result map[string]any, key string, section map[string]any) {
	if existing, ok := result[key].(map[string]any); ok {
		for k, v := range section {
			existing[k] = v
		}
		return
	}
	result[key] = section
}

func (e architectContextEnvelope) apply(result map[string]any) {
	result["planning_memory"] = e.Planning
	result["foundation_memory"] = e.Foundation
	result["reference_pack"] = e.References
}

// buildProgressStatus 在 Architect 不传 chapter 时返回进度摘要。
// Writer/Editor 的章节路径不需要这些信息，避免干扰写作。
func (t *ContextTool) buildProgressStatus(result map[string]any, reads *contextReads) {
	progress, err := t.store.Progress.Load()
	if err != nil {
		reads.require("progress_status", err)
		return
	}
	if progress == nil {
		return
	}
	status := map[string]any{
		"phase":              string(progress.Phase),
		"flow":               string(progress.Flow),
		"completed_chapters": len(progress.CompletedChapters),
		"next_chapter":       progress.NextChapter(),
		"total_word_count":   progress.TotalWordCount,
	}
	if progress.InProgressChapter > 0 {
		status["in_progress_chapter"] = progress.InProgressChapter
	}
	if len(progress.PendingRewrites) > 0 {
		status["pending_rewrites"] = progress.PendingRewrites
		status["rewrite_reason"] = progress.RewriteReason
	}
	if progress.Layered {
		status["layered"] = true
		status["dynamic_planning"] = true
		outline, outlineErr := t.store.Outline.LoadOutline()
		if outlineErr != nil {
			reads.require("progress_status.outline", outlineErr)
		} else {
			status["outlined_chapters"] = len(outline)
		}
		status["current_volume"] = progress.CurrentVolume
		status["current_arc"] = progress.CurrentArc
	} else {
		status["total_chapters"] = progress.TotalChapters
	}
	if progress.Phase == domain.PhaseComplete {
		status["finished"] = true
	}
	result["progress_status"] = status
}

// buildUserRules 保留稳定的 user_rules.structured 输出字段；它代表服务端内置机械规则，
// 不读取 HOME/cwd，也不依赖宿主侧归一化服务。
func (t *ContextTool) buildUserRules(result map[string]any) rules.Structured {
	structured := rules.SystemDefaults()
	working, ok := result["working_memory"].(map[string]any)
	if !ok {
		working = map[string]any{}
		result["working_memory"] = working
	}
	working["user_rules"] = map[string]any{"structured": structured}
	return structured
}

func (t *ContextTool) buildRuleViolations(result map[string]any, chapter int, structured rules.Structured, reads *contextReads) {
	record, err := t.store.ChapterRecords.Load(chapter)
	if err != nil {
		reads.require("chapter_record", err)
		return
	}
	if record == nil {
		return
	}
	violations := rules.Lint(record.Content)
	violations = append(violations, rules.Check(record.Content, structured)...)
	if len(violations) > 0 {
		result["rule_violations"] = violations
	}
}

func (t *ContextTool) buildBaseContext(result map[string]any, reads *contextReads) {
	if book, err := t.store.Book.Load(); err == nil && book != nil {
		result["book"] = book
	} else {
		reads.require("book", err)
	}
	if premise, err := t.store.Outline.LoadPremise(); err == nil && premise != "" {
		result["premise"] = premise
		if sections := parsePremiseSections(premise); len(sections) > 0 {
			result["premise_sections"] = sections
		}
		tier := domain.PlanningTier("")
		if meta, err := t.store.RunMeta.Load(); err == nil && meta != nil {
			tier = meta.PlanningTier
		} else {
			reads.require("run_meta", err)
		}
		result["premise_structure"] = premiseStructure(premise, tier)
	} else {
		reads.require("premise", err)
	}
	if rules, err := t.store.World.LoadWorldRules(); err == nil && len(rules) > 0 {
		result["world_rules"] = rules
	} else {
		reads.require("world_rules", err)
	}
}
