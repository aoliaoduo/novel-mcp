package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"novel-mcp/internal/domain"
	"novel-mcp/internal/errs"
	"novel-mcp/internal/store"
)

// completedBook 构造一本已完结的 N 章小说（phase=complete，CompletedChapters=1..n）。
func completedBook(t *testing.T, n int) *store.Store {
	t.Helper()
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(n); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	for ch := 1; ch <= n; ch++ {
		if err := s.Progress.MarkChapterComplete(ch, 100, "", ""); err != nil {
			t.Fatalf("MarkChapterComplete(%d): %v", ch, err)
		}
	}
	if err := s.Progress.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}
	return s
}

func execReopen(t *testing.T, s *store.Store, chapters []int, reason string) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"chapters": chapters, "reason": reason})
	data, err := NewReopenBookTool(s).Execute(context.Background(), raw)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("reopen 返回非法 JSON: %v", err)
	}
	return out, nil
}

func TestReopenBookToolReopensCompletedBook(t *testing.T) {
	s := completedBook(t, 3)
	out, err := execReopen(t, s, []int{2}, "改错字")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if out["phase"] != string(domain.PhaseWriting) {
		t.Errorf("phase = %v, want writing", out["phase"])
	}
	if out["flow"] != string(domain.FlowRewriting) {
		t.Errorf("flow = %v, want rewriting", out["flow"])
	}
	got, _ := json.Marshal(out["pending_rewrites"])
	if string(got) != "[2]" {
		t.Errorf("pending_rewrites = %s, want [2]", got)
	}
	p, _ := s.Progress.Load()
	if !p.ReopenedFromComplete {
		t.Error("ReopenedFromComplete 应为 true（排空后按原结构自动重新完结）")
	}
}

func TestReopenBookToolRejects(t *testing.T) {
	// 未完结的书：上游 phase 守卫原样透出。
	writing := store.NewStore(t.TempDir())
	if err := writing.Init(); err != nil {
		t.Fatal(err)
	}
	if err := writing.Progress.Init(5); err != nil {
		t.Fatal(err)
	}
	if _, err := execReopen(t, writing, []int{1}, "x"); !errors.Is(err, errs.ErrToolPrecondition) {
		t.Errorf("非完结书应 PRECONDITION，实得 %v", err)
	}
	// 未写过的章：属续写/越界。
	if _, err := execReopen(t, completedBook(t, 3), []int{2, 9}, "x"); err == nil {
		t.Error("未写章节应被拒")
	}
	// 空 chapters 与非法章号：参数问题。
	s := completedBook(t, 3)
	if _, err := execReopen(t, s, nil, "x"); !errors.Is(err, errs.ErrToolArgs) {
		t.Errorf("空 chapters 应 INVALID，实得 %v", err)
	}
	if _, err := execReopen(t, s, []int{0}, "x"); !errors.Is(err, errs.ErrToolArgs) {
		t.Errorf("0 章号应 INVALID，实得 %v", err)
	}
}

// reopen_book 必须挂进 CoreTools：挂不上就是“写了但调不到”。
func TestCoreToolsExposesReopenBook(t *testing.T) {
	s := store.NewStore(filepath.Join(t.TempDir(), "contracts"))
	found := false
	for _, tool := range CoreTools(s, "default") {
		if tool.Name() == "reopen_book" {
			found = true
			raw, err := json.Marshal(tool.Schema())
			if err != nil {
				t.Fatalf("schema 无法序列化: %v", err)
			}
			for _, key := range []string{"chapters", "reason"} {
				if !strings.Contains(string(raw), `"`+key+`"`) {
					t.Errorf("schema 丢了字段 %s: %s", key, raw)
				}
			}
		}
	}
	if !found {
		t.Fatal("CoreTools 缺 reopen_book")
	}
}
