package flow

import (
	"reflect"
	"testing"

	"novel-mcp/internal/domain"
	storepkg "novel-mcp/internal/store"
)

// FuzzRouteStateMachine 只生成结构上有效、但组合关系随机的状态；目标不是复制
// 路由决策表，而是锁住跨分支都必须成立的性质：纯函数、确定性、合法 Agent/Action、
// rewrite 队列优先且 writer 章节号始终有效。
func FuzzRouteStateMachine(f *testing.F) {
	f.Add(uint8(3), uint8(0), uint8(3), uint8(0), false, false, false, uint8(0), uint8(0))
	f.Add(uint8(3), uint8(1), uint8(7), uint8(2), true, true, false, uint8(2), uint8(1))
	f.Add(uint8(4), uint8(0), uint8(5), uint8(0), false, false, true, uint8(1), uint8(0))

	f.Fuzz(func(t *testing.T, phaseSeed, flowSeed, completedSeed, rewriteSeed uint8, layered, arcEnd, reviewed bool, tierSeed, refreshSeed uint8) {
		phases := []domain.Phase{domain.PhaseInit, domain.PhasePremise, domain.PhaseOutline, domain.PhaseWriting, domain.PhaseComplete}
		flows := []domain.FlowState{domain.FlowWriting, domain.FlowRewriting, domain.FlowPolishing}
		tiers := []domain.PlanningTier{"", domain.PlanningTierShort, domain.PlanningTierMid, domain.PlanningTierLong}

		completedN := int(completedSeed % 12)
		completed := make([]int, completedN)
		for i := range completed {
			completed[i] = i + 1
		}
		p := &domain.Progress{
			Phase:             phases[int(phaseSeed)%len(phases)],
			Flow:              flows[int(flowSeed)%len(flows)],
			Layered:           layered,
			TotalChapters:     max(completedN+2, 2),
			CompletedChapters: completed,
		}
		if rewriteSeed%3 != 0 {
			p.PendingRewrites = []int{int(rewriteSeed%max(uint8(completedN), 1)) + 1}
		}

		last := 0
		if completedN > 0 {
			last = completedN
		}
		state := State{
			Progress:          p,
			LastCompleted:     last,
			PlanningTier:      tiers[int(tierSeed)%len(tiers)],
			HasArcReview:      reviewed,
			HasArcSummary:     reviewed,
			HasVolumeSummary:  reviewed,
			HasGlobalReview:   reviewed,
			FoundationMissing: []string{"characters"},
		}
		if layered {
			state.ArcBoundary = &storepkg.ArcBoundary{
				IsArcEnd: arcEnd, Volume: 1, Arc: 1,
				StartChapter: max(1, last-2), EndChapter: max(last, 1),
				NextVolume: 1, NextArc: 2, NeedsExpansion: arcEnd,
			}
		}
		switch refreshSeed % 5 {
		case 1:
			state.AggregateRefresh = &AggregateRefresh{Kind: AggregateArcReview, Volume: 1, Arc: 1, StartChapter: 1, EndChapter: max(last, 1)}
		case 2:
			state.AggregateRefresh = &AggregateRefresh{Kind: AggregateArcSummary, Volume: 1, Arc: 1, EndChapter: max(last, 1)}
		case 3:
			state.AggregateRefresh = &AggregateRefresh{Kind: AggregateVolumeSummary, Volume: 1, EndChapter: max(last, 1)}
		case 4:
			state.ImmediateFeedbackCount = 1
		}

		before := snapshotState(state)
		first := Route(state)
		second := Route(state)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("Route 非确定性: first=%+v second=%+v", first, second)
		}
		if !reflect.DeepEqual(before, snapshotState(state)) {
			t.Fatal("Route 修改了输入 State")
		}
		assertConservation(t, state, first)
		assertActionContracts(t, first)
	})
}

func assertActionContracts(t *testing.T, inst *Instruction) {
	t.Helper()
	if inst == nil {
		return
	}
	for i, action := range inst.Actions {
		if action.Tool == "" || action.Purpose == "" {
			t.Fatalf("action[%d] 缺少 tool/purpose: %+v", i, action)
		}
		if action.Arguments == nil || action.RequiredInputs == nil {
			t.Fatalf("action[%d] 必须输出稳定的 arguments/required_inputs 容器: %+v", i, action)
		}
		switch action.Mode {
		case RouteActionRequired:
			if action.ChoiceGroup != "" {
				t.Fatalf("required action 不得携带 choice_group: %+v", action)
			}
		case RouteActionChoice:
			if action.ChoiceGroup == "" {
				t.Fatalf("choice action 必须携带 choice_group: %+v", action)
			}
		default:
			t.Fatalf("action[%d] mode 非法: %+v", i, action)
		}
	}
}
