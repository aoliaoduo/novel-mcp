package tools

import (
	"fmt"

	"novel-mcp/internal/domain"
)

func (t *ContextTool) buildArchitectContext(result map[string]any, reads *contextReads, volume, arc int) {
	envelope := newArchitectContextEnvelope()
	result["memory_policy"] = domain.NewArchitectMemoryPolicy()
	t.buildArchitectPlanning(&envelope, reads, volume, arc)
	t.buildArchitectFoundation(&envelope, reads)
	t.buildArchitectReferences(&envelope, reads)
	envelope.apply(result)
}

func (t *ContextTool) buildArchitectPlanning(envelope *architectContextEnvelope, reads *contextReads, requestedVolume, requestedArc int) {
	runMeta, err := t.store.RunMeta.Load()
	reads.require("run_meta", err)
	if runMeta != nil && runMeta.PlanningTier != "" {
		envelope.Planning["planning_tier"] = runMeta.PlanningTier
	}
	progress, progressErr := t.store.Progress.Load()
	reads.require("progress_for_planning", progressErr)

	layered, err := t.store.Outline.LoadLayeredOutline()
	reads.require("layered_outline", err)
	if err != nil {
		return
	}
	if len(layered) > 0 {
		latestCompleted := 0
		if progress != nil {
			latestCompleted = progress.LatestCompleted()
		}
		if requestedVolume > 0 {
			_, ok := findPlanningArc(layered, requestedVolume, requestedArc)
			if !ok {
				reads.fail(fmt.Errorf("planning scope v%da%d not found", requestedVolume, requestedArc))
				return
			}
		}
		detailVolume, detailArc := planningDetailScope(layered, progress, requestedVolume, requestedArc)
		outline, detailIncluded := projectLayeredOutlineForPlanning(
			layered,
			latestCompleted,
			detailVolume,
			detailArc,
		)
		envelope.Planning["layered_outline"] = outline
		if detailIncluded {
			envelope.Planning["outline_detail"] = map[string]int{"volume": detailVolume, "arc": detailArc}
		}
		var skeletonArcs []map[string]any
		for _, v := range layered {
			for _, a := range v.Arcs {
				if !a.IsExpanded() {
					skeletonArcs = append(skeletonArcs, map[string]any{
						"volume":             v.Index,
						"arc":                a.Index,
						"title":              a.Title,
						"goal":               a.Goal,
						"estimated_chapters": a.EstimatedChapters,
					})
				}
			}
		}
		if len(skeletonArcs) > 0 {
			envelope.Planning["skeleton_arcs"] = skeletonArcs
		}
	} else {
		if requestedVolume > 0 {
			reads.fail(fmt.Errorf("planning scope requires a layered outline"))
			return
		}
		if outline, err := t.store.Outline.LoadOutline(); err == nil && len(outline) > 0 {
			envelope.Planning["outline"] = outline
		} else {
			reads.require("outline", err)
		}
	}

	var compass *domain.StoryCompass
	if c, err := t.store.Outline.LoadCompass(); err == nil && c != nil {
		compass = c
		envelope.Planning["compass"] = compass
	} else {
		reads.require("compass", err)
	}
	if volSummaries, err := t.store.Summaries.LoadAllVolumeSummaries(); err == nil && len(volSummaries) > 0 {
		envelope.Planning["volume_summaries"] = volSummaries
	} else {
		reads.require("volume_summaries", err)
	}
	// 卷摘要承接已完成卷；当前卷的弧摘要承接最近实际剧情。扩弧时两者与
	// 骨架目标同时交给 Architect，让模型自行决定保留还是修订未写计划。
	if progressErr == nil && progress != nil && progress.CurrentVolume > 0 {
		if arcSummaries, err := t.store.Summaries.LoadArcSummaries(progress.CurrentVolume); err == nil && len(arcSummaries) > 0 {
			envelope.Planning["arc_summaries"] = arcSummaries
		} else {
			reads.require("arc_summaries", err)
		}
	} else {
		reads.require("progress_for_arc_summaries", progressErr)
	}

	// completion_signals 把"全书是否该结尾"的关键事实集中呈现，
	// 让架构师在裁定 complete_book / append_volume 时一眼看到对照面。
	// 散落在 progress / compass / foreshadow / layered_outline 里靠 LLM 脑算容易漏。
	envelope.Planning["completion_signals"] = t.completionSignals(layered, compass, reads)
}

// planningDetailScope 选择本轮唯一携带完整章节的大纲弧。显式请求优先；
// 默认使用当前进度弧，状态尚未建立时选择首个已展开弧。
func planningDetailScope(volumes []domain.VolumeOutline, progress *domain.Progress, requestedVolume, requestedArc int) (int, int) {
	if requestedVolume > 0 {
		return requestedVolume, requestedArc
	}
	if progress != nil {
		if arc, ok := findPlanningArc(volumes, progress.CurrentVolume, progress.CurrentArc); ok && arc.IsExpanded() {
			return progress.CurrentVolume, progress.CurrentArc
		}
	}
	for _, volume := range volumes {
		for _, arc := range volume.Arcs {
			if arc.IsExpanded() {
				return volume.Index, arc.Index
			}
		}
	}
	return 0, 0
}

func findPlanningArc(volumes []domain.VolumeOutline, volumeIndex, arcIndex int) (*domain.ArcOutline, bool) {
	for vi := range volumes {
		if volumes[vi].Index != volumeIndex {
			continue
		}
		for ai := range volumes[vi].Arcs {
			if volumes[vi].Arcs[ai].Index == arcIndex {
				return &volumes[vi].Arcs[ai], true
			}
		}
	}
	return nil, false
}

func projectLayeredOutlineForPlanning(volumes []domain.VolumeOutline, latestCompleted, detailVolume, detailArc int) ([]planningVolumeOutline, bool) {
	projected := make([]planningVolumeOutline, 0, len(volumes))
	chapter := 1
	detailIncluded := false
	for _, volume := range volumes {
		pv := planningVolumeOutline{
			Index: volume.Index, Title: volume.Title, Theme: volume.Theme, Final: volume.Final,
			Arcs: make([]planningArcOutline, 0, len(volume.Arcs)),
		}
		for _, arc := range volume.Arcs {
			pa := planningArcOutline{
				Index: arc.Index, Title: arc.Title, Goal: arc.Goal,
				EstimatedChapters: arc.EstimatedChapters,
			}
			if len(arc.Chapters) == 0 {
				pa.Status = "skeleton"
				pv.Arcs = append(pv.Arcs, pa)
				continue
			}
			pa.StartChapter = chapter
			pa.EndChapter = chapter + len(arc.Chapters) - 1
			pa.ChapterCount = len(arc.Chapters)
			if pa.EndChapter <= latestCompleted {
				pa.Status = "completed"
			} else {
				pa.Status = "expanded"
			}
			if volume.Index == detailVolume && arc.Index == detailArc {
				pa.Chapters = arc.Chapters
				detailIncluded = true
			} else {
				pa.ChaptersOmitted = true
			}
			chapter = pa.EndChapter + 1
			pv.Arcs = append(pv.Arcs, pa)
		}
		projected = append(projected, pv)
	}
	return projected, detailIncluded
}

func (t *ContextTool) completionSignals(layered []domain.VolumeOutline, compass *domain.StoryCompass, reads *contextReads) map[string]any {
	signals := map[string]any{}
	if progress, err := t.store.Progress.Load(); progress != nil {
		signals["completed_chapters"] = len(progress.CompletedChapters)
		signals["total_word_count"] = progress.TotalWordCount
		signals["phase"] = string(progress.Phase)
	} else {
		reads.require("completion_signals.progress", err)
	}
	if len(layered) > 0 {
		signals["planned_chapters"] = len(domain.FlattenOutline(layered))
		signals["volumes_total"] = len(layered)
		if fv := domain.FinaleVolume(layered); fv > 0 {
			signals["final_volume"] = fv
		}
	}
	if compass != nil {
		if compass.EstimatedScale != "" {
			signals["compass_estimated_scale"] = compass.EstimatedScale
		}
		signals["open_threads_count"] = len(compass.OpenThreads)
	}
	if active, err := t.store.World.LoadActiveForeshadow(); err == nil {
		signals["active_foreshadow_count"] = len(active)
	} else {
		reads.require("completion_signals.foreshadow", err)
	}
	return signals
}

func (t *ContextTool) buildArchitectFoundation(envelope *architectContextEnvelope, reads *contextReads) {
	if book, err := t.store.Book.Load(); err == nil && book != nil {
		envelope.Foundation["book"] = book
	} else {
		reads.require("book", err)
	}
	if premise, err := t.store.Outline.LoadPremise(); err == nil && premise != "" {
		envelope.Foundation["premise"] = premise
		if sections := parsePremiseSections(premise); len(sections) > 0 {
			envelope.Foundation["premise_sections"] = sections
		}
		tier := domain.PlanningTier("")
		if meta, err := t.store.RunMeta.Load(); err == nil && meta != nil {
			tier = meta.PlanningTier
		} else {
			reads.require("run_meta", err)
		}
		envelope.Foundation["premise_structure"] = premiseStructure(premise, tier)
	} else {
		reads.require("premise", err)
	}

	if chars, err := t.store.Characters.Load(); err == nil && chars != nil {
		envelope.Foundation["characters"] = chars
	} else {
		reads.require("characters", err)
	}

	if snapshots, err := t.store.Characters.LoadLatestSnapshots(); err == nil && len(snapshots) > 0 {
		envelope.Foundation["character_snapshots"] = snapshots
	} else {
		reads.require("character_snapshots", err)
	}
	if rules, err := t.store.World.LoadWorldRules(); err == nil && len(rules) > 0 {
		envelope.Foundation["world_rules"] = rules
	} else {
		reads.require("world_rules", err)
	}
	if foreshadow, err := t.store.World.LoadActiveForeshadow(); err == nil && len(foreshadow) > 0 {
		envelope.Foundation["foreshadow_ledger"] = foreshadow
	} else {
		reads.require("foreshadow_ledger", err)
	}
	if status, err := t.foundationStatus(); err == nil {
		envelope.Foundation["foundation_status"] = status
	} else {
		reads.require("foundation_status", err)
	}
	// Writer 反馈池:commit_chapter 落盘的大纲偏离/建议,规划下一弧/卷时必须参考;
	// expand_next_arc / append_volume / update_compass 成功后自动清空(已消费)。
	if fbs, err := t.store.Outline.LoadPendingOutlineFeedback(); err == nil && len(fbs) > 0 {
		envelope.Foundation["writer_feedback"] = fbs
	} else {
		reads.require("writer_feedback", err)
	}
}

func (t *ContextTool) buildArchitectReferences(envelope *architectContextEnvelope, reads *contextReads) {
	if styleRules, err := t.store.World.LoadStyleRules(); err == nil && styleRules != nil {
		envelope.References["style_rules"] = styleRules
	} else {
		reads.warn("style_rules", err)
	}

	envelope.References["references"] = t.architectReferences()
}
