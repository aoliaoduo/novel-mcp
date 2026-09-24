package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"novel-mcp/internal/domain"
	"novel-mcp/internal/store"
)

// seedExportBook 搭两章已完成 + 分层两卷的导出夹具：第 1 章正文首行带重复标题
// （应被剥掉），第 3 章未写（应进 skipped）。
func seedExportBook(t *testing.T, p *Projects, id string) {
	t.Helper()
	create(t, p, id)
	s := store.NewStore(p.dir(id))
	if err := s.Book.Save(domain.BookMetadata{Title: "潮汐编年史", Synopsis: "守塔人发现潮汐开始倒退。"}); err != nil {
		t.Fatalf("save book: %v", err)
	}
	if err := s.Outline.SaveLayeredOutline([]domain.VolumeOutline{
		{Index: 1, Title: "上卷·倒潮", Theme: "发现", Arcs: []domain.ArcOutline{
			{Index: 1, Title: "落脚", Goal: "安顿", Chapters: []domain.OutlineEntry{{Chapter: 1, Title: "倒流"}}},
		}},
		{Index: 2, Title: "下卷·回声", Theme: "追查", Arcs: []domain.ArcOutline{
			{Index: 1, Title: "入城", Goal: "追查", Chapters: []domain.OutlineEntry{{Chapter: 2, Title: "第二夜"}}},
		}},
	}); err != nil {
		t.Fatalf("save layered: %v", err)
	}
	finals := map[int]string{
		1: "# 倒流\n\n林渡把耳朵贴在灯室的铜栏杆上。",
		2: "塔门被从外面敲响。",
	}
	for ch, text := range finals {
		if err := s.Drafts.SaveFinalChapter(ch, text); err != nil {
			t.Fatalf("save final %d: %v", ch, err)
		}
	}
	titles := map[int]string{1: "倒流", 2: "第二夜"}
	for ch, title := range titles {
		if err := s.Summaries.SaveSummary(domain.ChapterSummary{Chapter: ch, Title: title, Summary: "略"}); err != nil {
			t.Fatalf("save summary %d: %v", ch, err)
		}
	}
	prog, err := s.Progress.Load()
	if err != nil {
		t.Fatalf("load progress: %v", err)
	}
	prog.Layered = true
	prog.CompletedChapters = []int{1, 2}
	if err := s.Progress.Save(prog); err != nil {
		t.Fatalf("save progress: %v", err)
	}
}

func exportCall(t *testing.T, p *Projects, id string, from, to int) map[string]any {
	t.Helper()
	out, err := p.Call(context.Background(), id, "export_book", "", map[string]any{"from_chapter": from, "to_chapter": to})
	if err != nil {
		t.Fatalf("export %d-%d: %v", from, to, err)
	}
	res, _ := out["result"].(map[string]any)
	return res
}

func TestExportRendersTXTLayout(t *testing.T) {
	p := newProjects(t)
	seedExportBook(t, p, "txt-book")
	res := exportCall(t, p, "txt-book", 1, 2)
	if res["format"] != "txt" {
		t.Errorf("format = %v, want txt", res["format"])
	}
	content, _ := res["content"].(string)
	for _, want := range []string{"《潮汐编年史》", "第 1 卷", "上卷·倒潮", "第 2 卷", "下卷·回声", "第 1 章  倒流", "第 2 章  第二夜", "铜栏杆", "塔门被从外面敲响"} {
		if !strings.Contains(content, want) {
			t.Errorf("导出缺 %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "# 倒流") {
		t.Errorf("正文首行重复标题应被剥掉:\n%s", content)
	}
}

func TestExportSkipsUnfinishedChapters(t *testing.T) {
	p := newProjects(t)
	seedExportBook(t, p, "skip-book")
	res := exportCall(t, p, "skip-book", 1, 3)
	chapters, _ := json.Marshal(res["chapters"])
	if string(chapters) != "[1,2]" {
		t.Errorf("chapters = %s, want [1,2]", chapters)
	}
	skipped, _ := json.Marshal(res["skipped"])
	if string(skipped) != "[3]" {
		t.Errorf("skipped = %s, want [3]", skipped)
	}
}

func TestExportRangeLimit(t *testing.T) {
	p := newProjects(t)
	seedExportBook(t, p, "limit-book")
	_, err := p.Call(context.Background(), "limit-book", "export_book", "", map[string]any{"from_chapter": 1, "to_chapter": 51})
	if err == nil || !strings.Contains(err.Error(), "每次导出 1-50 章") {
		t.Fatalf("超限应报错，实得 %v", err)
	}
}

func TestExportRejectsOversizeFinalBeforeRendering(t *testing.T) {
	p := newProjects(t)
	seedExportBook(t, p, "oversize-book")
	path := filepath.Join(p.dir("oversize-book"), "chapters", "01.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxExportBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := p.Call(context.Background(), "oversize-book", "export_book", "", map[string]any{"from_chapter": 1, "to_chapter": 1})
	if err == nil || !strings.Contains(err.Error(), "导出超过") {
		t.Fatalf("oversize export should fail before rendering, got %v", err)
	}
}
