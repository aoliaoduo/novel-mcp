package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"novel-mcp/internal/domain"
	revisionpkg "novel-mcp/internal/revision"
	"novel-mcp/internal/store"
	toolspkg "novel-mcp/internal/tools"
)

func benchmarkProjectsCallBook(b *testing.B, chapters int) (*Projects, string) {
	b.Helper()
	p, err := NewProjects(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	const id = "benchmark-book"
	if _, err := p.Create(id, "Projects.Call benchmark", "default"); err != nil {
		b.Fatal(err)
	}
	bk, err := p.load(id)
	if err != nil {
		b.Fatal(err)
	}
	st := bk.store
	if err := st.Book.Save(domain.BookMetadata{Title: "长篇基准", Synopsis: "Projects.Call benchmark"}); err != nil {
		b.Fatal(err)
	}
	if err := st.Outline.SavePremise("# 长篇基准\n\n## 主角目标\n完成旅程。"); err != nil {
		b.Fatal(err)
	}
	outline := make([]domain.OutlineEntry, chapters+1)
	completed := make([]int, chapters)
	counts := make(map[int]int, chapters)
	totalWords := 0
	bodySuffix := strings.Repeat("正文基准数据。", 128)
	for chapter := 1; chapter <= chapters+1; chapter++ {
		outline[chapter-1] = domain.OutlineEntry{
			Chapter:   chapter,
			Title:     fmt.Sprintf("第%d章", chapter),
			CoreEvent: fmt.Sprintf("推进第%d章事件", chapter),
			Scenes:    []string{"推进主线"},
		}
		if chapter > chapters {
			continue
		}
		body := fmt.Sprintf("第%d章正文。%s", chapter, bodySuffix)
		if err := st.Drafts.SaveFinalChapter(chapter, body); err != nil {
			b.Fatal(err)
		}
		if _, err := st.ChapterRecords.Accept(chapter, domain.ChapterOriginGenerated, body, domain.ChapterFacts{
			Title: fmt.Sprintf("第%d章", chapter), Summary: "章节摘要",
			KeyEvents: []string{"事件推进"}, Characters: []string{"林舟"},
		}, domain.StyleDelta{}); err != nil {
			b.Fatal(err)
		}
		completed[chapter-1] = chapter
		wc := domain.WordCount(body)
		counts[chapter] = wc
		totalWords += wc
	}
	if err := st.Outline.SaveOutline(outline); err != nil {
		b.Fatal(err)
	}
	if err := st.Characters.Save([]domain.Character{{Name: "林舟", Role: "主角", Description: "旅行者"}}); err != nil {
		b.Fatal(err)
	}
	if err := st.World.SaveWorldRules([]domain.WorldRule{{Category: "society", Rule: "规则存在", Boundary: "不可违背"}}); err != nil {
		b.Fatal(err)
	}
	if err := st.Progress.Save(&domain.Progress{
		Phase: domain.PhaseWriting, Flow: domain.FlowWriting,
		TotalChapters: chapters + 1, CurrentChapter: chapters + 1,
		CompletedChapters: completed, ChapterWordCounts: counts, TotalWordCount: totalWords,
	}); err != nil {
		b.Fatal(err)
	}
	return p, id
}

func BenchmarkProjectsCallLongBook(b *testing.B) {
	for _, chapters := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("chapters_%d", chapters), func(b *testing.B) {
			p, id := benchmarkProjectsCallBook(b, chapters)
			cases := []struct {
				name string
				tool string
				args map[string]any
			}{
				{name: "project_status", tool: "project_status"},
				{name: "next_step", tool: "next_step"},
				{name: "novel_context", tool: "novel_context", args: map[string]any{"chapter": chapters + 1}},
			}
			for _, tc := range cases {
				b.Run(tc.name, func(b *testing.B) {
					if _, err := p.Call(context.Background(), id, tc.tool, "", tc.args); err != nil {
						b.Fatal(err)
					}
					b.ResetTimer()
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						if _, err := p.Call(context.Background(), id, tc.tool, "", tc.args); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func benchmarkLongWritingStore(b *testing.B, chapters int, withFinals bool) *store.Store {
	b.Helper()
	st := store.NewStore(b.TempDir())
	if err := st.Init(); err != nil {
		b.Fatal(err)
	}
	if err := st.Book.Save(domain.BookMetadata{Title: "长篇基准", Synopsis: "用于 synthetic benchmark 的小说。"}); err != nil {
		b.Fatal(err)
	}
	if err := st.Outline.SavePremise(`# 长篇基准

## 主角目标
完成旅程。`); err != nil {
		b.Fatal(err)
	}
	outline := make([]domain.OutlineEntry, chapters+1)
	completed := make([]int, chapters)
	counts := make(map[int]int, chapters)
	totalWords := 0
	for i := 1; i <= chapters+1; i++ {
		outline[i-1] = domain.OutlineEntry{
			Chapter: i, Title: fmt.Sprintf("第%d章", i),
			CoreEvent: fmt.Sprintf("推进第%d章事件", i),
			Scenes:    []string{"推进主线"},
		}
		if i <= chapters {
			completed[i-1] = i
			if withFinals {
				body := fmt.Sprintf("第%d章正文。主角继续推进旅程。", i)
				if err := st.Drafts.SaveFinalChapter(i, body); err != nil {
					b.Fatal(err)
				}
				if _, err := st.ChapterRecords.Accept(i, domain.ChapterOriginGenerated, body, domain.ChapterFacts{
					Title: fmt.Sprintf("第%d章", i), Summary: "章节摘要", KeyEvents: []string{"事件推进"},
					Characters: []string{"林舟"},
				}, domain.StyleDelta{}); err != nil {
					b.Fatal(err)
				}
				wc := domain.WordCount(body)
				counts[i] = wc
				totalWords += wc
			}
		}
	}
	if err := st.Outline.SaveOutline(outline); err != nil {
		b.Fatal(err)
	}
	if err := st.Characters.Save([]domain.Character{{Name: "林舟", Role: "主角", Description: "旅行者"}}); err != nil {
		b.Fatal(err)
	}
	if err := st.World.SaveWorldRules([]domain.WorldRule{{Category: "society", Rule: "规则存在", Boundary: "不可违背"}}); err != nil {
		b.Fatal(err)
	}
	if err := st.Progress.Save(&domain.Progress{
		Phase: domain.PhaseWriting, Flow: domain.FlowWriting,
		TotalChapters: chapters + 1, CurrentChapter: chapters + 1,
		CompletedChapters: completed, ChapterWordCounts: counts, TotalWordCount: totalWords,
	}); err != nil {
		b.Fatal(err)
	}
	if err := st.RunMeta.Init("default", "benchmark", "local"); err != nil {
		b.Fatal(err)
	}
	return st
}

func BenchmarkNextStepLongBook(b *testing.B) {
	for _, chapters := range []int{100, 1000, 3000} {
		b.Run(fmt.Sprintf("chapters_%d", chapters), func(b *testing.B) {
			st := benchmarkLongWritingStore(b, chapters, false)
			info := ProjectInfo{ID: "benchmark-book", Brief: "benchmark", Style: "default"}
			if _, err := NextStep(st, info); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := NextStep(st, info); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNovelContextLongBook(b *testing.B) {
	for _, chapters := range []int{100, 1000, 3000} {
		b.Run(fmt.Sprintf("chapters_%d", chapters), func(b *testing.B) {
			st := benchmarkLongWritingStore(b, chapters, true)
			tool := toolspkg.NewContextTool(st, toolspkg.References{}, "default", toolspkg.NewStyleStatsIndex(st))
			args := []byte(fmt.Sprintf(`{"chapter":%d}`, chapters+1))
			// 首次调用会恢复 style stats 索引；这是一次性启动成本，不计入稳态延迟。
			if _, err := tool.Execute(context.Background(), args); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := tool.Execute(context.Background(), args); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkVerifyBook(b *testing.B, chapters int) (*Projects, *book) {
	b.Helper()
	st := store.NewStore(b.TempDir())
	if err := st.Init(); err != nil {
		b.Fatal(err)
	}
	records := make([]domain.ChapterRecord, 0, chapters)
	completed := make([]int, chapters)
	counts := make(map[int]int, chapters)
	totalWords := 0
	for chapter := 1; chapter <= chapters; chapter++ {
		content := fmt.Sprintf("第%d章正文。", chapter)
		if err := st.Drafts.SaveFinalChapter(chapter, content); err != nil {
			b.Fatal(err)
		}
		record, err := st.ChapterRecords.Accept(chapter, domain.ChapterOriginGenerated, content, domain.ChapterFacts{
			Title: fmt.Sprintf("第%d章", chapter), Summary: "章节摘要",
			KeyEvents: []string{"事件推进"},
		}, domain.StyleDelta{})
		if err != nil {
			b.Fatal(err)
		}
		records = append(records, *record)
		completed[chapter-1] = chapter
		wc := domain.WordCount(content)
		counts[chapter] = wc
		totalWords += wc
	}
	if err := st.Progress.Save(&domain.Progress{
		Phase: domain.PhaseWriting, Flow: domain.FlowWriting,
		TotalChapters: chapters + 1, CurrentChapter: chapters + 1,
		CompletedChapters: completed, ChapterWordCounts: counts, TotalWordCount: totalWords,
	}); err != nil {
		b.Fatal(err)
	}
	if err := revisionpkg.NewProjector(st).Apply(records); err != nil {
		b.Fatal(err)
	}
	return &Projects{}, &book{store: st}
}

func BenchmarkVerifyProjectLongBook(b *testing.B) {
	for _, chapters := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("chapters_%d", chapters), func(b *testing.B) {
			projects, bk := benchmarkVerifyBook(b, chapters)
			report := projects.verifyProject(bk)
			if !report.OK {
				b.Fatalf("benchmark fixture is inconsistent: %+v", report.Issues)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				report := projects.verifyProject(bk)
				if !report.OK {
					b.Fatalf("verification failed: %+v", report.Issues)
				}
			}
		})
	}
}
