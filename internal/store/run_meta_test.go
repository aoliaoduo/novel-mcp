// Modified for novel-mcp: local module import paths; see UPSTREAM.md.
package store

import (
	"testing"

	"novel-mcp/internal/domain"
)

func TestSaveAndLoadRunMeta(t *testing.T) {
	store := NewStore(t.TempDir())
	meta := domain.RunMeta{
		StartedAt: "2026-03-07T10:00:00+08:00",
		Provider:  "mcp",
		Style:     "fantasy",
		Model:     "web-client",
	}
	if err := store.RunMeta.Save(meta); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.RunMeta.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Style != "fantasy" || loaded.Provider != "mcp" || loaded.Model != "web-client" {
		t.Fatalf("run meta mismatch: %+v", loaded)
	}
}

func TestLoadRunMetaEmpty(t *testing.T) {
	store := NewStore(t.TempDir())
	meta, err := store.RunMeta.Load()
	if err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		t.Fatalf("expected nil, got %+v", meta)
	}
}

func TestInitRunMetaPreservesPlanningTier(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.RunMeta.Save(domain.RunMeta{
		StartedAt:    "old",
		Provider:     "mcp",
		Style:        "fantasy",
		Model:        "old-client",
		PlanningTier: domain.PlanningTierLong,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RunMeta.Init("suspense", "mcp", "web-client"); err != nil {
		t.Fatal(err)
	}
	meta, err := store.RunMeta.Load()
	if err != nil {
		t.Fatal(err)
	}
	if meta.Style != "suspense" || meta.Provider != "mcp" || meta.Model != "web-client" {
		t.Fatalf("runtime fields not refreshed: %+v", meta)
	}
	if meta.PlanningTier != domain.PlanningTierLong {
		t.Fatalf("planning tier must survive Init: %+v", meta)
	}
}

func TestSetPlanningTier(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.RunMeta.SetPlanningTier(domain.PlanningTierLong); err != nil {
		t.Fatal(err)
	}
	meta, err := store.RunMeta.Load()
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || meta.PlanningTier != domain.PlanningTierLong {
		t.Fatalf("planning tier mismatch: %+v", meta)
	}
}
