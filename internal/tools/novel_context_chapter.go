package tools

import (
	"slices"

	"novel-mcp/internal/domain"
)

func (t *ContextTool) prepareChapterContext(chapter int, envelope *chapterContextEnvelope, reads *contextReads) contextBuildState {
	state := contextBuildState{
		chapter: chapter,
		profile: domain.NewContextProfile(0),
	}

	progress, err := t.store.Progress.Load()
	reads.require("progress", err)
	runMeta, err := t.store.RunMeta.Load()
	reads.require("run_meta", err)
	state.progress = progress
	state.runMeta = runMeta
	if progress != nil && len(progress.CompletedChapters) > 0 {
		state.cast, err = t.castIndex.Snapshot(progress.CompletedChapters)
		reads.require("supporting_cast", err)
	}

	if runMeta != nil && runMeta.PlanningTier != "" {
		envelope.Episodic["planning_tier"] = runMeta.PlanningTier
	}
	if progress != nil && progress.TotalChapters > 0 {
		state.profile = domain.NewContextProfile(progress.TotalChapters)
	}
	if progress == nil || !progress.Layered {
		state.profile.Layered = false
	}

	outline, outlineErr := t.store.Outline.LoadOutline()
	reads.require("outline", outlineErr)
	state.outline = outline
	currentEntry := findOutlineEntry(outline, chapter)
	if currentEntry != nil {
		envelope.Working["current_chapter_outline"] = currentEntry
	}
	state.currentEntry = currentEntry

	chapterPlan, chapterPlanErr := t.store.Drafts.LoadChapterPlan(chapter)
	if chapterPlanErr == nil && chapterPlan != nil {
		envelope.Working["chapter_plan"] = chapterPlan
		if len(chapterPlan.Contract.RequiredBeats) > 0 ||
			len(chapterPlan.Contract.ForbiddenMoves) > 0 ||
			len(chapterPlan.Contract.ContinuityChecks) > 0 ||
			len(chapterPlan.Contract.EvaluationFocus) > 0 ||
			chapterPlan.Contract.EmotionTarget != "" ||
			len(chapterPlan.Contract.PayoffPoints) > 0 ||
			chapterPlan.Contract.HookGoal != "" {
			envelope.Working["chapter_contract"] = chapterPlan.Contract
		}
	} else {
		reads.require("chapter_plan", chapterPlanErr)
	}
	state.chapterPlan = chapterPlan

	// 是否正在重写本章：决定 novel_context 是否补"重写专用"事实。
	isRewrite := progress != nil && slices.Contains(progress.PendingRewrites, chapter)

	// 暴露 draft 是否已存在的事实：让 writer 被重派时能自行判断跳过重写还是覆盖。
	// 只暴露 exists + word_count，不注入正文（正文让 writer 按需用 read_chapter 拉）。
	if _, draftWords, draftErr := t.store.Drafts.LoadChapterContent(chapter); draftErr == nil && draftWords > 0 {
		envelope.Working["chapter_draft"] = map[string]any{
			"exists":     true,
			"word_count": draftWords,
		}
	} else if draftErr != nil {
		reads.require("chapter_draft", draftErr)
	}

	// 重写时把"为什么改 + 改哪里"交给 writer：理由来自返工队列，具体批评来自本章评审
	// （selectReviewLessons 只召回 chapter-1..chapter-3，恰好漏掉本章本身，writer 又无读评审的工具）。
	// 正文不在此注入——保持"正文按需 read_chapter 拉"的约定不破。
	if isRewrite {
		brief := map[string]any{"reason": progress.RewriteReason}
		if reviews, reviewErr := t.store.World.LoadReviewsAffectingChapter(chapter); reviewErr == nil {
			var sources []map[string]any
			for _, review := range reviews {
				item := map[string]any{
					"review_chapter": review.Chapter,
					"scope":          review.Scope,
					"summary":        review.Summary,
				}
				var issues []domain.ConsistencyIssue
				for _, issue := range review.Issues {
					if issue.RequiresChange && slices.Contains(issue.Chapters, chapter) {
						issues = append(issues, issue)
					}
				}
				if len(issues) > 0 {
					item["issues"] = issues
				}
				if review.Scope == "chapter" && len(review.ContractMisses) > 0 {
					item["contract_misses"] = review.ContractMisses
				}
				sources = append(sources, item)
			}
			if len(sources) > 0 {
				brief["reviews"] = sources
			}
		} else {
			reads.require("rewrite_review", reviewErr)
		}
		envelope.Working["rewrite_brief"] = brief
	}

	foreshadow, foreshadowErr := t.store.World.LoadActiveForeshadow()
	reads.require("foreshadow_ledger", foreshadowErr)
	state.foreshadow = foreshadow

	relationships, relErr := t.store.World.LoadRelationships()
	reads.require("relationship_state", relErr)
	if len(relationships) > 0 {
		envelope.Episodic["relationship_state"] = relationships
	}
	state.relationships = relationships

	allStateChanges, scErr := t.store.World.LoadStateChanges()
	reads.require("recent_state_changes", scErr)
	state.allStateChanges = allStateChanges
	if len(allStateChanges) > 0 {
		start := max(chapter-2, 1)
		var recent []domain.StateChange
		for _, c := range allStateChanges {
			if c.Chapter >= start && c.Chapter < chapter {
				recent = append(recent, c)
			}
		}
		if len(recent) > 0 {
			envelope.Episodic["recent_state_changes"] = recent
		}
	}

	styleRules, styleErr := t.store.World.LoadStyleRules()
	reads.require("style_rules", styleErr)
	state.styleRules = styleRules
	state.storyThreads = t.selectStoryThreads(state)
	if len(state.storyThreads) > 0 && len(state.storyThreads) < storyThreadRecallMinSelected {
		state.storyThreads = nil
	}

	return state
}

func (t *ContextTool) buildChapterContext(result map[string]any, state contextBuildState, reads *contextReads) {
	envelope := newChapterContextEnvelope()
	result["memory_policy"] = domain.NewChapterMemoryPolicy(state.progress, state.profile, state.currentEntry != nil)

	if state.profile.Layered {
		t.loadLayeredCharacters(envelope.Episodic, state.chapter, reads)
	} else {
		t.loadFilteredCharacters(envelope.Episodic, state.chapter, reads)
	}

	t.buildChapterEpisodicMemory(&envelope, state, reads)
	t.buildChapterWorkingMemory(&envelope, state, reads)
	t.buildChapterReferencePack(&envelope, state, reads)
	t.buildChapterSelectedMemory(&envelope, state, reads)
	t.buildStyleStats(&envelope, state, reads)
	envelope.apply(result)
}

// buildStyleStats 对全部已完成章节做全书级风格统计，注入 episodic_memory.style_stats。
// 弧内评审窗口对"章均几十次的句式 tic、章末形态同构、跨章复读"天然失明，只有
// 全书统计能暴露——统计归代码（确定性），裁定归 LLM（editor 在 aesthetic 维度
// 按数字判分，writer 据此自避免）。章数不足时 stylestat 返回 nil，不注入。
func (t *ContextTool) buildStyleStats(envelope *chapterContextEnvelope, state contextBuildState, reads *contextReads) {
	if state.progress == nil || len(state.progress.CompletedChapters) == 0 {
		return
	}

	var titles []string
	if outline, err := t.store.Outline.LoadOutline(); err == nil {
		for _, entry := range outline {
			titles = append(titles, entry.Title)
		}
	} else {
		reads.warn("style_stats.outline", err)
	}

	stats, err := t.styleStats.Snapshot(
		state.progress.CompletedChapters,
		titles,
		t.styleStopwords(state.cast, reads),
	)
	if err != nil {
		reads.warn("style_stats", err)
		return
	}
	if stats == nil {
		return
	}
	envelope.Episodic["style_stats"] = stats
}

// styleStopwords 收集角色名与别名供短语挖掘过滤——出场人名天然高频，不是文风问题。
func (t *ContextTool) styleStopwords(cast []domain.CastEntry, reads *contextReads) []string {
	var words []string
	if chars, err := t.store.Characters.Load(); err == nil {
		for _, c := range chars {
			words = append(words, c.Name)
			words = append(words, c.Aliases...)
		}
	} else {
		reads.warn("style_stats.characters", err)
	}
	for _, entry := range domain.RecentCast(cast, 50) {
		words = append(words, entry.Name)
	}
	return words
}

func (t *ContextTool) buildChapterWorkingMemory(envelope *chapterContextEnvelope, state contextBuildState, reads *contextReads) {
	t.buildOutlineWindow(envelope.Working, state, reads)
	if next := findOutlineEntry(state.outline, state.chapter+1); next != nil {
		envelope.Working["next_chapter_outline"] = next
	}

	if state.profile.Layered {
		t.loadLayeredSummaries(envelope.Working, state.chapter, state.profile.SummaryWindow, reads)
		// 收官纪律：本章属于已宣告的收官卷时注入，防 writer 在收官段临章再开新钩子
		//（收官卷写完即自动完结，此时新埋的伏笔永远没有机会回收）。
		if volumes, err := t.store.Outline.LoadLayeredOutline(); err == nil {
			if fv := domain.FinaleVolume(volumes); fv > 0 {
				if b, boundaryErr := t.store.Outline.CheckArcBoundary(state.chapter); boundaryErr == nil && b != nil && b.Volume == fv {
					envelope.Working["finale"] = "本卷为全书收官卷：不再新开长线或埋新伏笔，优先回收既有伏笔、收拢关系线，按大纲把故事推向终局。"
				} else {
					reads.require("arc_boundary", boundaryErr)
				}
			}
		} else {
			reads.require("layered_outline", err)
		}
	} else {
		if summaries, err := t.store.Summaries.LoadRecentSummaries(state.chapter, state.profile.SummaryWindow); err == nil && len(summaries) > 0 {
			envelope.Working["recent_summaries"] = summaries
		} else {
			reads.require("recent_summaries", err)
		}
	}

	if timeline, err := t.store.World.LoadRecentTimeline(state.chapter, state.profile.TimelineWindow); err == nil && len(timeline) > 0 {
		envelope.Working["timeline"] = timeline
	} else {
		reads.require("timeline", err)
	}

	if state.progress != nil {
		checkpoint := map[string]any{
			"in_progress_chapter": state.progress.InProgressChapter,
		}
		if len(state.progress.StrandHistory) > 0 {
			checkpoint["strand_history"] = state.progress.StrandHistory
		}
		if len(state.progress.HookHistory) > 0 {
			checkpoint["hook_history"] = state.progress.HookHistory
		}
		envelope.Working["checkpoint"] = checkpoint
	}

	if state.chapter > 1 {
		if prevText, err := t.store.Drafts.LoadChapterText(state.chapter - 1); err == nil && prevText != "" {
			runes := []rune(prevText)
			if len(runes) > 800 {
				runes = runes[len(runes)-800:]
			}
			envelope.Working["previous_tail"] = string(runes)
		} else {
			reads.require("previous_chapter", err)
		}
	}
}

// buildOutlineWindow 为 Writer/Editor 保留与当前任务直接相关的大纲，而不是注入
// 随全书增长的完整扁平大纲。分层模式使用当前弧；非分层模式使用最近一个评审周期。
func (t *ContextTool) buildOutlineWindow(working map[string]any, state contextBuildState, reads *contextReads) {
	outline := state.outline
	if len(outline) == 0 {
		return
	}

	start := max(1, state.chapter-domain.ReviewInterval+1)
	end := min(state.chapter, len(outline))
	if state.profile.Layered {
		boundary, err := t.store.Outline.CheckArcBoundary(state.chapter)
		if err != nil {
			reads.require("outline_window.arc_boundary", err)
			return
		}
		if boundary == nil {
			return
		}
		start = boundary.StartChapter
		end = min(boundary.EndChapter, len(outline))
	}
	if start <= end {
		working["outline_window"] = outline[start-1 : end]
	}
}

func findOutlineEntry(outline []domain.OutlineEntry, chapter int) *domain.OutlineEntry {
	for i := range outline {
		if outline[i].Chapter == chapter {
			return &outline[i]
		}
	}
	return nil
}

func (t *ContextTool) buildChapterSelectedMemory(envelope *chapterContextEnvelope, state contextBuildState, reads *contextReads) {
	if len(state.storyThreads) > 0 {
		envelope.Selected["story_threads"] = state.storyThreads
	}
	if lessons := t.selectReviewLessons(state.chapter, reads); len(lessons) > 0 {
		envelope.Selected["review_lessons"] = lessons
	}
}

func (t *ContextTool) buildChapterEpisodicMemory(envelope *chapterContextEnvelope, state contextBuildState, reads *contextReads) {
	if len(state.foreshadow) > 0 && len(state.storyThreads) == 0 {
		envelope.Episodic["foreshadow_ledger"] = state.foreshadow
	}

	// 召回最近活跃的次要角色，让 Writer 在引入旧角色时能保持口吻/定位一致。
	// 不召回所有条目（长篇会膨胀），只给最近活跃的前 N 个，按 LastSeenChapter 倒序
	if recentCast := domain.RecentCast(state.cast, 15); len(recentCast) > 0 {
		simplified := make([]map[string]any, 0, len(recentCast))
		for _, e := range recentCast {
			item := map[string]any{
				"name":             e.Name,
				"first_seen":       e.FirstSeenChapter,
				"last_seen":        e.LastSeenChapter,
				"appearance_count": e.AppearanceCount,
			}
			if e.BriefRole != "" {
				item["brief_role"] = e.BriefRole
			}
			simplified = append(simplified, item)
		}
		envelope.Episodic["recent_cast"] = simplified
	}

	if state.progress != nil && state.progress.TotalChapters > 30 && state.currentEntry != nil {
		if related := t.buildRelatedChapters(
			state.chapter,
			state.currentEntry,
			state.foreshadow,
			state.relationships,
			state.allStateChanges,
			reads,
		); len(related) > 0 {
			envelope.Episodic["related_chapters"] = related
		}
	}

	if state.profile.Layered && state.progress != nil {
		pos := map[string]any{
			"volume": state.progress.CurrentVolume,
			"arc":    state.progress.CurrentArc,
		}
		if volumes, err := t.store.Outline.LoadLayeredOutline(); err == nil {
			globalCh := 1
			for _, v := range volumes {
				if v.Index == state.progress.CurrentVolume {
					pos["volume_title"] = v.Title
					pos["volume_theme"] = v.Theme
				}
				for _, arc := range v.Arcs {
					if v.Index == state.progress.CurrentVolume && arc.Index == state.progress.CurrentArc {
						pos["arc_title"] = arc.Title
						pos["arc_goal"] = arc.Goal
						if n := len(arc.Chapters); n > 0 {
							pos["arc_total_chapters"] = n
							pos["arc_chapter_index"] = state.chapter - globalCh + 1
						}
					}
					globalCh += len(arc.Chapters)
				}
			}
		} else {
			reads.require("layered_outline", err)
		}
		envelope.Episodic["position"] = pos
	}
}

func (t *ContextTool) buildChapterReferencePack(envelope *chapterContextEnvelope, state contextBuildState, reads *contextReads) {
	const contextSampleLookbackChapters = 100
	authorStyle, err := t.store.World.LoadAuthorRevisionStyle()
	reads.warn("author_revision_style", err)
	if authorStyle != nil && (len(authorStyle.Prose) > 0 || len(authorStyle.Dialogue) > 0 || len(authorStyle.Taboos) > 0) {
		envelope.References["author_revision_style"] = authorStyle
	}

	if state.styleRules != nil {
		envelope.References["style_rules"] = state.styleRules
	} else {
		var maxCompleted int
		if state.progress != nil {
			maxCompleted = maxCompletedChapter(state.progress.CompletedChapters)
		}
		anchors, err := t.store.Drafts.ExtractRecentStyleAnchors(3, maxCompleted, contextSampleLookbackChapters)
		reads.warn("style_anchors", err)
		if len(anchors) > 0 {
			envelope.References["style_anchors"] = anchors
		}

		if state.currentEntry != nil {
			var voiceSamples []map[string]any
			chars, err := t.store.Characters.Load()
			reads.warn("voice_samples.characters", err)
			for _, c := range chars {
				if c.Tier == "secondary" || c.Tier == "decorative" {
					continue
				}
				samples, err := t.store.Drafts.ExtractRecentDialogue(c.Name, c.Aliases, 3, maxCompleted, contextSampleLookbackChapters)
				reads.warn("voice_samples."+c.Name, err)
				if len(samples) > 0 {
					voiceSamples = append(voiceSamples, map[string]any{
						"character": c.Name,
						"samples":   samples,
					})
				}
				if len(voiceSamples) >= 5 {
					break
				}
			}
			if len(voiceSamples) > 0 {
				envelope.References["voice_samples"] = voiceSamples
			}
		}
	}

	envelope.References["references"] = t.writerReferences(state.chapter)
}
