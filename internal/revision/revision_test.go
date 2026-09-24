// Modified for novel-mcp: local module import paths; see UPSTREAM.md.
package revision

import (
	"testing"
	"time"

	"novel-mcp/internal/domain"
	"novel-mcp/internal/store"
)

func TestProjectorRebuildsWorldStateFromChapterRecords(t *testing.T) {
	st := newRevisionTestStore(t, 2)
	if err := st.World.SaveTimeline([]domain.TimelineEvent{{Chapter: 1, Time: "旧", Event: "应被删除"}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	records := []domain.ChapterRecord{
		testRecord(1, "正文一", domain.ChapterFacts{
			Title: "第一章", Summary: "新摘要", Characters: []string{"林墨", "店主"}, KeyEvents: []string{"离城"},
			TimelineEvents:      []domain.TimelineEvent{{Time: "当夜", Event: "林墨离城", Characters: []string{"林墨"}}},
			ForeshadowUpdates:   []domain.ForeshadowUpdate{{ID: "信件", Action: "plant", Description: "未拆的信"}},
			RelationshipChanges: []domain.RelationshipEntry{{CharacterA: "林墨", CharacterB: "店主", Relation: "互相信任"}},
			StateChanges:        []domain.StateChange{{Entity: "林墨", Field: "location", NewValue: "城外"}},
			CastIntros:          []domain.CastIntro{{Name: "店主", BriefRole: "客栈店主"}}, HookType: "mystery", DominantStrand: "quest",
		}, domain.StyleDelta{Prose: []string{"减少解释性心理描写"}}, now),
		testRecord(2, "正文二", domain.ChapterFacts{
			Title: "第二章", Summary: "后续", Characters: []string{"林墨", "店主"}, KeyEvents: []string{"拆信"},
			ForeshadowUpdates:   []domain.ForeshadowUpdate{{ID: "信件", Action: "resolve"}},
			RelationshipChanges: []domain.RelationshipEntry{{CharacterA: "店主", CharacterB: "林墨", Relation: "决裂"}},
		}, domain.StyleDelta{}, now.Add(time.Minute)),
	}
	if err := NewProjector(st).Apply(records); err != nil {
		t.Fatal(err)
	}
	timeline, _ := st.World.LoadTimeline()
	if len(timeline) != 1 || timeline[0].Event != "林墨离城" || timeline[0].Chapter != 1 {
		t.Fatalf("时间线未按记录重建: %+v", timeline)
	}
	ledger, _ := st.World.LoadForeshadowLedger()
	if len(ledger) != 1 || ledger[0].Status != "resolved" || ledger[0].ResolvedAt != 2 {
		t.Fatalf("伏笔投影错误: %+v", ledger)
	}
	relationships, _ := st.World.LoadRelationships()
	if len(relationships) != 1 || relationships[0].Relation != "决裂" || relationships[0].Chapter != 2 {
		t.Fatalf("关系投影错误: %+v", relationships)
	}
	style, _ := st.World.LoadAuthorRevisionStyle()
	if style == nil || len(style.Prose) != 1 || style.Prose[0] != "减少解释性心理描写" {
		t.Fatalf("用户修订风格未投影: %+v", style)
	}
}

func TestVerifyProjectionDetectsDerivedStateDrift(t *testing.T) {
	st := newRevisionTestStore(t, 1)
	records := []domain.ChapterRecord{testRecord(1, "正文", domain.ChapterFacts{
		Title: "第一章", Summary: "摘要", Characters: []string{"林墨"}, KeyEvents: []string{"离城"},
		TimelineEvents: []domain.TimelineEvent{{Time: "夜", Event: "林墨离城", Characters: []string{"林墨"}}},
		HookType:       "mystery", DominantStrand: "quest",
	}, domain.StyleDelta{}, time.Now())}
	if err := NewProjector(st).Apply(records); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProjection(st, records); err != nil {
		t.Fatalf("fresh projection should verify: %v", err)
	}
	if err := st.World.SaveTimeline([]domain.TimelineEvent{{Chapter: 1, Time: "夜", Event: "被篡改的事件"}}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProjection(st, records); err == nil {
		t.Fatal("derived timeline drift must be detected")
	}
}

func newRevisionTestStore(t *testing.T, total int) *store.Store {
	t.Helper()
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(total); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatal(err)
	}
	return st
}

func testRecord(chapter int, content string, facts domain.ChapterFacts, style domain.StyleDelta, acceptedAt time.Time) domain.ChapterRecord {
	return domain.ChapterRecord{
		Version: domain.ChapterRecordVersion, Chapter: chapter, Revision: 1, Origin: domain.ChapterOriginUser,
		Content: content, ContentSHA256: domain.ChapterContentSHA256(content), Facts: facts, StyleDelta: style, AcceptedAt: acceptedAt,
	}
}
