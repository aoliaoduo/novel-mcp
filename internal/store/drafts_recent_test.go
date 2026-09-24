package store

import (
	"strings"
	"testing"
)

func TestExtractRecentDialogueHonorsLookback(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveFinalChapter(1, "林舟说：\"这是很久以前的旧声音。\""); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveFinalChapter(5, "林舟说：\"这是最近才形成的新声音。\""); err != nil {
		t.Fatal(err)
	}

	got, err := s.Drafts.ExtractRecentDialogue("林舟", nil, 8, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "最近") {
		t.Fatalf("recent dialogue = %#v", got)
	}

	got, err = s.Drafts.ExtractRecentDialogue("林舟", nil, 8, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("lookback 不得回扫到第 1 章: %#v", got)
	}
}

func TestExtractRecentStyleAnchorsPrefersRecentWindow(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	old := "旧风格：" + strings.Repeat("山风掠过石阶。", 8)
	recent := "新风格：" + strings.Repeat("雨声落在檐角。", 8)
	if err := s.Drafts.SaveFinalChapter(1, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveFinalChapter(5, recent); err != nil {
		t.Fatal(err)
	}

	got, err := s.Drafts.ExtractRecentStyleAnchors(3, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != recent {
		t.Fatalf("recent style anchors = %#v", got)
	}
}
