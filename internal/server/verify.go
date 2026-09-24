package server

import (
	"fmt"
	"slices"
	"strings"

	"novel-mcp/internal/domain"
	revisionpkg "novel-mcp/internal/revision"
)

type verifyIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Artifact string `json:"artifact"`
	Chapter  int    `json:"chapter,omitempty"`
	Message  string `json:"message"`
}

type verifyReport struct {
	OK              bool          `json:"ok"`
	CheckedChapters int           `json:"checked_chapters"`
	Errors          int           `json:"errors"`
	Warnings        int           `json:"warnings"`
	Issues          []verifyIssue `json:"issues"`
}

func (r *verifyReport) add(severity, code, artifact string, chapter int, message string) {
	r.Issues = append(r.Issues, verifyIssue{
		Severity: severity, Code: code, Artifact: artifact, Chapter: chapter, Message: message,
	})
	if severity == "error" {
		r.Errors++
	} else {
		r.Warnings++
	}
}

// verifyProject 对持久化事实做只读全量核验。它刻意不自动修复，避免一次
// “看看状态”就覆盖用户的磁盘事实。
func (p *Projects) verifyProject(b *book) verifyReport {
	report := verifyReport{Issues: []verifyIssue{}}
	progress, err := b.store.Progress.Load()
	if err != nil {
		report.add("error", "PROGRESS_UNREADABLE", "meta/progress.json", 0, p.safeError(err))
		report.OK = false
		return report
	}
	if progress == nil {
		report.add("error", "PROGRESS_MISSING", "meta/progress.json", 0, "progress 未初始化")
		report.OK = false
		return report
	}

	seen := make(map[int]struct{}, len(progress.CompletedChapters))
	records := make([]domain.ChapterRecord, 0, len(progress.CompletedChapters))
	allRecords := true
	computedTotalWords := 0
	lastChapter := 0

	for _, chapter := range progress.CompletedChapters {
		if chapter <= 0 {
			report.add("error", "COMPLETED_CHAPTER_INVALID", "meta/progress.json", chapter, "completed_chapters 含非正章节号")
			continue
		}
		if _, ok := seen[chapter]; ok {
			report.add("error", "COMPLETED_CHAPTER_DUPLICATE", "meta/progress.json", chapter, "completed_chapters 含重复章节")
			continue
		}
		seen[chapter] = struct{}{}
		if lastChapter > 0 && chapter <= lastChapter {
			report.add("warning", "COMPLETED_CHAPTER_ORDER", "meta/progress.json", chapter, "completed_chapters 不是严格递增顺序")
		}
		lastChapter = chapter
		report.CheckedChapters++

		finalText, loadErr := b.store.Drafts.LoadChapterText(chapter)
		if loadErr != nil {
			report.add("error", "FINAL_UNREADABLE", fmt.Sprintf("chapters/%02d.md", chapter), chapter, p.safeError(loadErr))
			allRecords = false
			continue
		}
		if strings.TrimSpace(finalText) == "" {
			report.add("error", "FINAL_MISSING", fmt.Sprintf("chapters/%02d.md", chapter), chapter, "progress 标记已完成，但终稿不存在或为空")
			allRecords = false
			continue
		}

		wordCount := domain.WordCount(finalText)
		computedTotalWords += wordCount
		if progress.ChapterWordCounts == nil {
			report.add("error", "WORD_COUNT_INDEX_MISSING", "meta/progress.json", chapter, "缺少 chapter_word_counts")
		} else if stored, ok := progress.ChapterWordCounts[chapter]; !ok {
			report.add("error", "WORD_COUNT_MISSING", "meta/progress.json", chapter, "已完成章节缺少字数投影")
		} else if stored != wordCount {
			report.add("error", "WORD_COUNT_MISMATCH", "meta/progress.json", chapter,
				fmt.Sprintf("字数投影=%d，终稿计算=%d", stored, wordCount))
		}

		record, recordErr := b.store.ChapterRecords.Load(chapter)
		if recordErr != nil {
			report.add("error", "CHAPTER_RECORD_UNREADABLE", fmt.Sprintf("meta/chapter_records/%06d.json", chapter), chapter, p.safeError(recordErr))
			allRecords = false
		} else if record == nil {
			report.add("error", "CHAPTER_RECORD_MISSING", fmt.Sprintf("meta/chapter_records/%06d.json", chapter), chapter, "已完成章节缺少接纳记录")
			allRecords = false
		} else {
			records = append(records, *record)
			if domain.ChapterContentSHA256(finalText) != record.ContentSHA256 {
				report.add("error", "FINAL_RECORD_MISMATCH", fmt.Sprintf("meta/chapter_records/%06d.json", chapter), chapter,
					"终稿正文与接纳记录 content_sha256 不一致")
			}
		}

		summary, summaryErr := b.store.Summaries.LoadSummary(chapter)
		if summaryErr != nil {
			report.add("error", "SUMMARY_UNREADABLE", fmt.Sprintf("summaries/%02d.json", chapter), chapter, p.safeError(summaryErr))
		} else if summary == nil {
			report.add("error", "SUMMARY_MISSING", fmt.Sprintf("summaries/%02d.json", chapter), chapter, "已完成章节缺少摘要")
		} else if record != nil && (summary.Title != record.Facts.Title || summary.Summary != record.Facts.Summary) {
			report.add("error", "SUMMARY_RECORD_MISMATCH", fmt.Sprintf("summaries/%02d.json", chapter), chapter,
				"章节摘要与接纳记录中的 title/summary 不一致")
		}
	}

	if computedTotalWords != progress.TotalWordCount {
		report.add("error", "TOTAL_WORD_COUNT_MISMATCH", "meta/progress.json", 0,
			fmt.Sprintf("total_word_count=%d，逐章终稿合计=%d", progress.TotalWordCount, computedTotalWords))
	}

	for _, chapter := range progress.PendingRewrites {
		if !slices.Contains(progress.CompletedChapters, chapter) {
			report.add("error", "REWRITE_TARGET_NOT_COMPLETED", "meta/progress.json", chapter, "pending_rewrites 指向未完成章节")
		}
	}

	if allRecords && len(records) == len(seen) {
		if err := revisionpkg.ValidateRecords(records); err != nil {
			report.add("error", "FACT_CHAIN_INVALID", "meta/chapter_records", 0, p.safeError(err))
		} else if err := revisionpkg.VerifyProjection(b.store, records); err != nil {
			report.add("error", "DERIVED_PROJECTION_MISMATCH", "derived-state", 0, p.safeError(err))
		}
	}

	pending, pendingErr := b.store.Signals.LoadPendingCommit()
	if pendingErr != nil {
		report.add("error", "PENDING_COMMIT_UNREADABLE", "meta/pending_commit.json", 0, p.safeError(pendingErr))
	} else if pending != nil {
		if pending.Chapter <= 0 {
			report.add("error", "PENDING_COMMIT_CHAPTER_INVALID", "meta/pending_commit.json", pending.Chapter, "pending commit 章节号非法")
		}
		switch pending.Stage {
		case domain.CommitStageStarted, domain.CommitStageStateApplied, domain.CommitStageProgressMarked, domain.CommitStageSignalSaved:
		default:
			report.add("error", "PENDING_COMMIT_STAGE_INVALID", "meta/pending_commit.json", pending.Chapter,
				fmt.Sprintf("未知 commit stage %q", pending.Stage))
		}
		if len(pending.Payload) == 0 {
			report.add("error", "PENDING_COMMIT_PAYLOAD_MISSING", "meta/pending_commit.json", pending.Chapter,
				"pending commit 缺少冻结 payload，当前项目状态已损坏")
		}
	}

	for _, warning := range b.store.CheckConsistency() {
		warning = strings.ReplaceAll(strings.ToValidUTF8(warning, "\uFFFD"), p.root, "<projects>")
		report.add("warning", "STORE_SHALLOW_WARNING", "project", 0, warning)
	}

	report.OK = report.Errors == 0
	return report
}
