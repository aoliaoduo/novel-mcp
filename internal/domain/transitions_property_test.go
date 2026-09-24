package domain

import "testing"

func TestTransitionGraphsAreClosedAndMonotonic(t *testing.T) {
	phases := []Phase{PhaseInit, PhasePremise, PhaseOutline, PhaseWriting, PhaseComplete}
	for i, from := range phases {
		for j, to := range phases {
			want := j >= i
			if got := CanTransitionPhase(from, to); got != want {
				t.Fatalf("phase %q -> %q = %v, want %v", from, to, got, want)
			}
		}
	}
	for _, unknown := range []Phase{"reviewing", "future", "invalid"} {
		if CanTransitionPhase(unknown, PhaseWriting) || CanTransitionPhase(PhaseWriting, unknown) {
			t.Fatalf("未知 phase %q 不得进入状态图", unknown)
		}
	}

	flows := []FlowState{FlowWriting, FlowRewriting, FlowPolishing}
	for _, from := range flows {
		if !CanTransitionFlow(from, from) {
			t.Fatalf("flow %q 必须允许幂等自环", from)
		}
	}
	if !CanTransitionFlow(FlowWriting, FlowRewriting) || !CanTransitionFlow(FlowWriting, FlowPolishing) ||
		!CanTransitionFlow(FlowRewriting, FlowWriting) || !CanTransitionFlow(FlowPolishing, FlowWriting) {
		t.Fatal("合法 flow 边缺失")
	}
	if CanTransitionFlow(FlowRewriting, FlowPolishing) || CanTransitionFlow(FlowPolishing, FlowRewriting) {
		t.Fatal("rewrite/polish 不得横跳，必须先回 writing")
	}
	for _, unknown := range []FlowState{"reviewing", "steering", "future"} {
		if CanTransitionFlow(unknown, FlowWriting) || CanTransitionFlow(FlowWriting, unknown) {
			t.Fatalf("未知 flow %q 不得进入状态图", unknown)
		}
	}
}
