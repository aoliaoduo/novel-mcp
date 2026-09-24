package server

import (
	"context"
	"testing"
)

func TestVerifyProjectCleanProjectIsPureRead(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "verify-clean")
	before := revisionOf(t, p, id)

	out, err := p.Call(context.Background(), id, "verify_project", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := out["revision"].(string); got != before {
		t.Fatalf("verify_project returned different revision: before=%s got=%s", before, got)
	}
	result := out["result"].(map[string]any)
	if result["ok"] != true || result["errors"].(float64) != 0 {
		t.Fatalf("fresh project should verify cleanly: %+v", result)
	}
	after := revisionOf(t, p, id)
	if after != before {
		t.Fatalf("verify_project must not mutate project: before=%s after=%s", before, after)
	}
}

func TestVerifyProjectFindsCompletedChapterWithoutFinal(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "verify-damaged")
	b, err := p.load(id)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := b.store.Progress.Load()
	if err != nil {
		t.Fatal(err)
	}
	progress.CompletedChapters = []int{1}
	progress.CurrentChapter = 2
	if err := b.store.Progress.Save(progress); err != nil {
		t.Fatal(err)
	}

	out, err := p.Call(context.Background(), id, "verify_project", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	result := out["result"].(map[string]any)
	if result["ok"] != false {
		t.Fatalf("damaged project must fail verification: %+v", result)
	}
	issues := result["issues"].([]any)
	found := false
	for _, raw := range issues {
		issue := raw.(map[string]any)
		if issue["code"] == "FINAL_MISSING" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected FINAL_MISSING, got %+v", issues)
	}
}
