package rules

import "testing"

func TestSystemDefaultsAreUsable(t *testing.T) {
	d := SystemDefaults()
	if len(d.ForbiddenPhrases) == 0 || len(d.FatigueWords) == 0 {
		t.Fatalf("mechanical defaults unexpectedly empty: %+v", d)
	}
	if d.FatigueWords["然而"] <= 0 {
		t.Fatal("expected representative fatigue-word threshold")
	}
}
