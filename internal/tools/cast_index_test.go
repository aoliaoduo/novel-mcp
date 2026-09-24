package tools

import (
	"reflect"
	"testing"

	"novel-mcp/internal/domain"
	"novel-mcp/internal/store"
)

func TestCastIndexAppendRewriteAndRemove(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Characters.Save([]domain.Character{{Name: "林墨", Aliases: []string{"阿墨"}}}); err != nil {
		t.Fatal(err)
	}
	save := func(chapter int, facts domain.ChapterFacts) {
		t.Helper()
		if _, err := st.ChapterRecords.Accept(chapter, domain.ChapterOriginGenerated, "正文", facts, domain.StyleDelta{}); err != nil {
			t.Fatal(err)
		}
	}
	save(1, domain.ChapterFacts{Characters: []string{"林墨", "老周"}, CastIntros: []domain.CastIntro{{Name: "老周", BriefRole: "守门人"}}})
	save(2, domain.ChapterFacts{Characters: []string{"老周", "阿云"}, CastIntros: []domain.CastIntro{{Name: "阿云", BriefRole: "药铺学徒"}}})

	index := NewCastIndex(st)
	assertCastIndexMatchesStore(t, index, st, []int{1, 2})

	save(3, domain.ChapterFacts{Characters: []string{"阿云"}})
	assertCastIndexMatchesStore(t, index, st, []int{1, 2, 3})

	// 重写已有章节不会改变 completed 集合，必须靠主动刷新更新投影。
	save(2, domain.ChapterFacts{Characters: []string{"老周", "阿雨"}, CastIntros: []domain.CastIntro{{Name: "阿雨", BriefRole: "驿站伙计"}}})
	if err := index.ChapterCommitted(2); err != nil {
		t.Fatal(err)
	}
	assertCastIndexMatchesStore(t, index, st, []int{1, 2, 3})

	assertCastIndexMatchesStore(t, index, st, []int{1, 2})
}

func TestCastIndexSurfacesMissingCompletedRecord(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCastIndex(st).Snapshot([]int{1}); err == nil {
		t.Fatal("缺少已完成章节记录时应报错")
	}
}

func assertCastIndexMatchesStore(t *testing.T, index *CastIndex, st *store.Store, completed []int) {
	t.Helper()
	want, err := st.BuildCast(completed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := index.Snapshot(completed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cast index mismatch\n got: %+v\nwant: %+v", got, want)
	}
}
