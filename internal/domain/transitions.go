// Modified for novel-mcp: local module import paths; see UPSTREAM.md.
package domain

import (
	"fmt"

	"novel-mcp/internal/errs"
)

// Phase 只前进：init -> premise -> outline -> writing -> complete。
// Flow 只保留当前 MCP 会实际产生的 writing / rewriting / polishing 三态。
var phaseOrder = map[Phase]int{
	PhaseInit:     1,
	PhasePremise:  2,
	PhaseOutline:  3,
	PhaseWriting:  4,
	PhaseComplete: 5,
}

func CanTransitionPhase(from, to Phase) bool {
	if to == "" {
		return false
	}
	if from == "" || from == to {
		return true
	}
	fromOrder, fromOK := phaseOrder[from]
	toOrder, toOK := phaseOrder[to]
	return fromOK && toOK && toOrder >= fromOrder
}

func ValidatePhaseTransition(from, to Phase) error {
	if CanTransitionPhase(from, to) {
		return nil
	}
	return fmt.Errorf("invalid phase transition: %q -> %q: %w", from, to, errs.ErrPhaseTransition)
}

func CanTransitionFlow(from, to FlowState) bool {
	if to == "" {
		return false
	}
	if from == "" || from == to {
		return true
	}
	switch from {
	case FlowWriting:
		return to == FlowRewriting || to == FlowPolishing
	case FlowRewriting, FlowPolishing:
		return to == FlowWriting
	default:
		return false
	}
}

func ValidateFlowTransition(from, to FlowState) error {
	if CanTransitionFlow(from, to) {
		return nil
	}
	return fmt.Errorf("invalid flow transition: %q -> %q: %w", from, to, errs.ErrFlowTransition)
}
