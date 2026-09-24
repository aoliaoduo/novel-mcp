package domain

import "testing"

func TestCanTransitionPhase(t *testing.T) {
	tests := []struct {
		from Phase
		to   Phase
		want bool
	}{
		{"", PhaseInit, true},
		{PhaseInit, PhasePremise, true},
		{PhaseInit, PhaseOutline, true},
		{PhaseOutline, PhaseWriting, true},
		{PhaseWriting, PhaseComplete, true},
		{PhaseOutline, PhasePremise, false},
		{PhaseComplete, PhaseWriting, false},
	}
	for _, tt := range tests {
		if got := CanTransitionPhase(tt.from, tt.to); got != tt.want {
			t.Fatalf("CanTransitionPhase(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestCanTransitionFlow(t *testing.T) {
	tests := []struct {
		from FlowState
		to   FlowState
		want bool
	}{
		{"", FlowRewriting, true},
		{FlowWriting, FlowRewriting, true},
		{FlowWriting, FlowPolishing, true},
		{FlowRewriting, FlowWriting, true},
		{FlowPolishing, FlowWriting, true},
		{FlowRewriting, FlowPolishing, false},
		{FlowPolishing, FlowRewriting, false},
		{FlowWriting, FlowState("reviewing"), false},
		{FlowWriting, FlowState("steering"), false},
	}
	for _, tt := range tests {
		if got := CanTransitionFlow(tt.from, tt.to); got != tt.want {
			t.Fatalf("CanTransitionFlow(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}
