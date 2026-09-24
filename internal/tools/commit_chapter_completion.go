package tools

import (
	"encoding/json"
	"fmt"

	"novel-mcp/internal/domain"
	"novel-mcp/internal/errs"
	"novel-mcp/internal/store"
)

func (t *CommitChapterTool) buildSkipResult(chapter int, progress *domain.Progress) (json.RawMessage, error) {
	_, wordCount, err := t.store.Drafts.LoadChapterContent(chapter)
	if err != nil {
		return nil, fmt.Errorf("load completed chapter: %w: %w", errs.ErrStoreRead, err)
	}

	result := domain.CommitResult{
		Chapter:     chapter,
		Committed:   true,
		WordCount:   wordCount,
		NextChapter: chapter + 1,
	}

	if progress != nil && progress.Layered {
		boundary, err := t.store.Outline.CheckArcBoundary(chapter)
		if err != nil {
			return nil, fmt.Errorf("check completed chapter boundary: %w: %w", errs.ErrStoreRead, err)
		}
		if boundary != nil {
			result.ArcEnd = boundary.IsArcEnd
			result.VolumeEnd = boundary.IsVolumeEnd
			result.Volume = boundary.Volume
			result.Arc = boundary.Arc
			result.NeedsExpansion = boundary.NeedsExpansion
			result.NeedsNewVolume = boundary.NeedsNewVolume
			result.NextVolume = boundary.NextVolume
			result.NextArc = boundary.NextArc
		}
		result.ReviewRequired, result.ReviewReason = domain.ShouldArcReview(result.ArcEnd, result.VolumeEnd, result.Volume, result.Arc)
	} else if progress != nil {
		result.ReviewRequired, result.ReviewReason = domain.ShouldReview(len(progress.CompletedChapters))
	}

	if progress != nil {
		if progress.Phase == domain.PhaseComplete {
			result.BookComplete = true
		}
		result.Flow = string(progress.Flow)
	}

	return json.Marshal(result)
}

// applyCompletion 判断本次 commit 是否使全书完结，若是则 MarkComplete 并返回 true。
//   - 非分层：写完约定总章数即完结。
//   - 分层：架构师显式 save_foundation type=complete_book 是主路径；这里再加一道
//     确定性兜底（见 layeredComplete）——防止模型在终点既不 append_volume 也不
//     complete_book，导致"写手裸跑越界章节 → 越界守卫拦截 → 反复重试"的 livelock
//     （《凡骨》ch204..347 案例的根因）。
func (t *CommitChapterTool) applyCompletion(result *domain.CommitResult, progress *domain.Progress) (bool, error) {
	if progress == nil {
		return false, nil
	}
	if progress.Phase == domain.PhaseComplete {
		return true, nil
	}
	if progress.Layered {
		complete, err := layeredComplete(t.store, progress)
		if err != nil {
			return false, fmt.Errorf("evaluate layered completion: %w: %w", errs.ErrStoreRead, err)
		}
		if complete {
			if err := t.store.Progress.MarkComplete(); err != nil {
				return false, fmt.Errorf("mark book complete: %w: %w", errs.ErrStoreWrite, err)
			}
			return true, nil
		}
		return false, nil
	}
	if progress.TotalChapters > 0 && result.NextChapter > progress.TotalChapters {
		if err := t.store.Progress.MarkComplete(); err != nil {
			return false, fmt.Errorf("mark book complete: %w: %w", errs.ErrStoreWrite, err)
		}
		return true, nil
	}
	return false, nil
}

// ── 分层完结判定（包级：commit_chapter 与 save_volume_summary 两个触发点共用）──
//
// 完结检查永远发生在"最后一块事实落地"的工具里：
//   - 未宣告收官：末章 commit（layeredBookComplete 质量级）
//   - 已宣告收官：正向主路径的最后一块拼图是卷末收尾三连（评审→弧摘要→卷摘要），
//     故触发点在 save_volume_summary；返工 drain 后三连已齐时由 commit 触发。

// layeredStructurallyComplete 判定分层长篇是否"结构上写完"：返工队列空 + 无骨架弧待展开
// + 所有已展开章节都已写。这是确定性的终态事实，不含伏笔/长线等语义判断——用作"防终态
// 死循环"的安全网（返工排空后据此重新完结）。
func layeredStructurallyComplete(st *store.Store, progress *domain.Progress) (bool, error) {
	// 1. 返工队列必须清空
	if len(progress.PendingRewrites) > 0 {
		return false, nil
	}
	volumes, err := st.Outline.LoadLayeredOutline()
	if err != nil {
		return false, fmt.Errorf("load layered outline: %w", err)
	}
	if len(volumes) == 0 {
		return false, nil
	}
	// 2. 不能还有骨架弧待展开（计划内仍有内容要写）
	for i := range volumes {
		for j := range volumes[i].Arcs {
			if !volumes[i].Arcs[j].IsExpanded() {
				return false, nil
			}
		}
	}
	// 3. 已展开章节必须全部写完
	expanded := len(domain.FlattenOutline(volumes))
	return expanded > 0 && len(progress.CompletedChapters) >= expanded, nil
}

// finaleWrapped 收官卷的卷末收尾三连（弧评审/弧摘要/卷摘要）是否齐备。
// 收官完结不要求伏笔/长线归零，但必须等末弧过完编辑质量闸——结局是全书最要紧的部分，
// 完结不能抢在 editor 评审（可能入队返工）与摘要落盘之前。
func finaleWrapped(st *store.Store, progress *domain.Progress) (bool, error) {
	last := progress.LatestCompleted()
	if last <= 0 {
		return false, nil
	}
	b, err := st.Outline.CheckArcBoundary(last)
	if err != nil {
		return false, fmt.Errorf("check finale boundary: %w", err)
	}
	if b == nil || !b.IsArcEnd {
		return false, nil
	}
	hasReview, err := st.World.HasArcReview(last)
	if err != nil {
		return false, fmt.Errorf("load finale review: %w", err)
	}
	hasArcSummary, err := st.Summaries.HasArcSummary(b.Volume, b.Arc)
	if err != nil {
		return false, fmt.Errorf("load finale arc summary: %w", err)
	}
	hasVolumeSummary, err := st.Summaries.HasVolumeSummary(b.Volume)
	if err != nil {
		return false, fmt.Errorf("load finale volume summary: %w", err)
	}
	return hasReview && hasArcSummary && hasVolumeSummary, nil
}

// layeredComplete 分层正向写作的完结总判定：
//   - 已宣告收官卷（layered_outline 最后一卷带 final）→ 结构写完 + 卷末收尾三连齐备
//     即完结，不再要求伏笔/长线归零。收官卷整卷以收线为目标（架构师规划时已把长线/
//     伏笔分配进各弧），个别遗漏属编辑质量问题，不该把全书卡在终态之外——否则
//     estimated_scale 高估的书永远无法合法完本（140 章 stop guard 熔断案例的根因侧）。
//   - 未宣告 → 质量级 layeredBookComplete，防模型既不收官也不完本时在大纲耗尽处
//     过早收尾。
func layeredComplete(st *store.Store, progress *domain.Progress) (bool, error) {
	volumes, err := st.Outline.LoadLayeredOutline()
	if err != nil {
		return false, fmt.Errorf("load layered outline: %w", err)
	}
	if domain.FinaleVolume(volumes) > 0 {
		structurallyComplete, err := layeredStructurallyComplete(st, progress)
		if err != nil || !structurallyComplete {
			return structurallyComplete, err
		}
		return finaleWrapped(st, progress)
	}
	return layeredBookComplete(st, progress)
}

// ReconcileLayeredCompletion 根据当前持久化事实补齐分层书的完结状态。
// save_volume_summary 正常路径和 Engine 崩溃恢复共用这一入口，避免卷摘要已落盘、
// Progress 尚未来得及 MarkComplete 时永久丢失自动完结触发点。
func ReconcileLayeredCompletion(st *store.Store) (bool, error) {
	progress, err := st.Progress.Load()
	if err != nil {
		return false, fmt.Errorf("load progress: %w", err)
	}
	if progress == nil || !progress.Layered {
		return false, nil
	}
	if progress.Phase == domain.PhaseComplete {
		return true, nil
	}
	if progress.Phase != domain.PhaseWriting {
		return false, nil
	}
	complete, err := layeredComplete(st, progress)
	if err != nil || !complete {
		return complete, err
	}
	if err := st.Progress.MarkComplete(); err != nil {
		return false, fmt.Errorf("mark complete: %w", err)
	}
	return true, nil
}

// layeredBookComplete 用客观事实判断分层长篇是否真正写完，对照 architect-long.md 完结判定
// 清单里可量化的几项 + 结构性事实。结构完整之上再要求伏笔归零、长线收束——任一不满足都
// 让位给架构师继续 expand_next_arc / append_volume，绝不抢在故事没写完时收尾。无 compass 时保守
// 判为未完结。这是未宣告收官卷时的"质量级"完结判定，比 layeredStructurallyComplete 更严。
func layeredBookComplete(st *store.Store, progress *domain.Progress) (bool, error) {
	structurallyComplete, err := layeredStructurallyComplete(st, progress)
	if err != nil || !structurallyComplete {
		return structurallyComplete, err
	}
	// 4. 活跃伏笔必须归零（承诺已兑现）
	active, err := st.World.LoadActiveForeshadow()
	if err != nil {
		return false, fmt.Errorf("load active foreshadow: %w", err)
	}
	if len(active) > 0 {
		return false, nil
	}
	// 5. 指南针活跃长线必须收束（无 compass / 长线未清都交回架构师裁定）
	compass, err := st.Outline.LoadCompass()
	if err != nil {
		return false, fmt.Errorf("load compass: %w", err)
	}
	if compass == nil || len(compass.OpenThreads) > 0 {
		return false, nil
	}
	return true, nil
}
