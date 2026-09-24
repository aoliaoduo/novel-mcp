// Modified for novel-mcp: local module import paths; see UPSTREAM.md.
package store

import (
	"os"
	"path/filepath"
	"testing"

	"novel-mcp/internal/domain"
)

func TestSummaryTitleCacheTracksSave(t *testing.T) {
	st := NewStore(t.TempDir())
	if err := st.Summaries.SaveSummary(domain.ChapterSummary{Chapter: 1, Title: "旧标题"}); err != nil {
		t.Fatal(err)
	}
	if title, err := st.Summaries.LoadSummaryTitle(1); err != nil || title != "旧标题" {
		t.Fatalf("首次读取标题: title=%q err=%v", title, err)
	}
	if err := st.Summaries.SaveSummary(domain.ChapterSummary{Chapter: 1, Title: "新标题"}); err != nil {
		t.Fatal(err)
	}
	if title, err := st.Summaries.LoadSummaryTitle(1); err != nil || title != "新标题" {
		t.Fatalf("保存后缓存未更新: title=%q err=%v", title, err)
	}
}

func TestProjectFormatRequiresCurrentVersion(t *testing.T) {
	st := NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckCurrentProjectFormat(); err == nil {
		t.Fatal("缺少 format.json 时必须拒绝，不再猜旧版本")
	}
	if err := st.Progress.io.WriteJSON(projectFormatPath, projectFormat{Version: CurrentProjectFormatVersion - 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckCurrentProjectFormat(); err == nil {
		t.Fatal("非当前项目格式必须拒绝")
	}
	if err := st.SaveCurrentProjectFormat(); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckCurrentProjectFormat(); err != nil {
		t.Fatalf("当前格式应通过: %v", err)
	}
}

func TestFoundationMissingReturnsReadError(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outline.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FoundationMissing(); err == nil {
		t.Fatal("损坏的大纲必须返回读取错误，不能降级成缺失项")
	}
}
