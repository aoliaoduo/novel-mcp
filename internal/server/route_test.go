package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"novel-mcp/internal/domain"
	"novel-mcp/internal/flow"
)

// TestNextStepNewProjectReturnsPlanStart：新项目无进度，路由必须给出开局步骤而不是 nil。
func TestNextStepNewProjectReturnsPlanStart(t *testing.T) {
	p, err := NewProjects(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Create("plan-book", "短篇悬疑练手", "default"); err != nil {
		t.Fatal(err)
	}
	out, err := p.Call(context.Background(), "plan-book", "next_step", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := out["revision"].(string)
	if len(rev) != 64 {
		t.Fatalf("next_step 缺 revision: %v", out)
	}
	res, _ := out["result"].(map[string]any)
	if res["done"] == true {
		t.Fatalf("新项目不应 done: %v", res)
	}
	if res["need"] != "plan_start" || res["agent"] != "architect" {
		t.Fatalf("新项目应走 plan_start: %v", res)
	}
	task, _ := res["task"].(string)
	for _, want := range []string{"save_book", "save_foundation", "audit_foundation", "next_step"} {
		if !strings.Contains(task, want) {
			t.Fatalf("plan_start task 缺 %q: %s", want, task)
		}
	}
	if res["brief"] != "短篇悬疑练手" || res["style"] != "default" {
		t.Fatalf("plan_start 应回显 brief/style: %v", res)
	}
	actions, ok := res["actions"].([]any)
	if !ok || len(actions) != 5 {
		t.Fatalf("plan_start 应返回 5 个结构化 actions: %#v", res["actions"])
	}
	wantActions := []string{"novel_guide", "save_book", "save_foundation", "novel_context", "audit_foundation"}
	for i, want := range wantActions {
		item, _ := actions[i].(map[string]any)
		if item["tool"] != want {
			t.Fatalf("actions[%d] = %v, want %s", i, item, want)
		}
	}
}

// TestWriterProtocolOrder：writer task 内联的写作协议顺序必须与上游固定顺序一致。
func TestWriterProtocolOrder(t *testing.T) {
	task := "写第 3 章" + writerProtocol(3)
	want := []string{"novel_context(chapter=3)", "plan_chapter", "draft_chapter", "read_chapter(source=draft)", "check_consistency", "commit_chapter"}
	last := -1
	for _, w := range want {
		i := strings.Index(task, w)
		if i < 0 {
			t.Fatalf("协议缺 %q: %s", w, task)
		}
		if i < last {
			t.Fatalf("协议顺序错乱（%q 提前）: %s", w, task)
		}
		last = i
	}
}

func TestWriterActionsAreMachineReadable(t *testing.T) {
	inst := flow.Route(flow.State{Progress: &domain.Progress{
		Phase: domain.PhaseWriting, Flow: domain.FlowWriting,
		TotalChapters: 5, CompletedChapters: []int{1, 2},
	}})
	if inst == nil || inst.Chapter != 3 {
		t.Fatalf("writer route 不对: %#v", inst)
	}
	actions := routeActions(inst.Actions)
	want := []string{"novel_context", "plan_chapter", "draft_chapter", "read_chapter", "check_consistency", "commit_chapter"}
	if len(actions) != len(want) {
		t.Fatalf("actions len=%d, want %d: %#v", len(actions), len(want), actions)
	}
	for i, name := range want {
		if actions[i]["tool"] != name {
			t.Fatalf("actions[%d].tool=%v, want %s", i, actions[i]["tool"], name)
		}
		if _, ok := actions[i]["arguments"].(map[string]any); !ok {
			t.Fatalf("actions[%d] arguments 不是对象: %#v", i, actions[i])
		}
		if _, ok := actions[i]["requires_revision"].(bool); !ok {
			t.Fatalf("actions[%d] 缺 requires_revision: %#v", i, actions[i])
		}
	}
	if actions[0]["requires_revision"] != false || actions[1]["requires_revision"] != true {
		t.Fatalf("读/写 revision 标记不对: %#v", actions[:2])
	}
}

func TestDecoratedWriterPlanCarriesDependenciesRevisionSourcesAndResources(t *testing.T) {
	inst := flow.Route(flow.State{Progress: &domain.Progress{
		Phase: domain.PhaseWriting, Flow: domain.FlowWriting,
		TotalChapters: 5, CompletedChapters: []int{1, 2},
	}})
	if inst == nil {
		t.Fatal("expected writer instruction")
	}
	out := decoratePlan("machine-book", map[string]any{
		"done": false, "chapter": inst.Chapter, "actions": routeActions(inst.Actions),
	})
	actions := out["actions"].([]map[string]any)
	if len(actions) != 6 {
		t.Fatalf("actions=%d, want 6", len(actions))
	}
	for i, a := range actions {
		wantID := fmt.Sprintf("a%d", i+1)
		if a["id"] != wantID {
			t.Fatalf("actions[%d].id=%v want %s", i, a["id"], wantID)
		}
		deps, ok := a["depends_on"].([]string)
		if !ok {
			t.Fatalf("actions[%d] depends_on=%T", i, a["depends_on"])
		}
		if i == 0 && len(deps) != 0 {
			t.Fatalf("first action dependencies=%v", deps)
		}
		if i > 0 && (len(deps) != 1 || deps[0] != fmt.Sprintf("a%d", i)) {
			t.Fatalf("actions[%d] dependencies=%v", i, deps)
		}
	}
	if actions[0]["revision_source"] != "none" || actions[1]["revision_source"] != "a1" || actions[5]["revision_source"] != "a5" {
		t.Fatalf("revision chain unexpected: a1=%v a2=%v a6=%v", actions[0]["revision_source"], actions[1]["revision_source"], actions[5]["revision_source"])
	}
	if actions[2]["resource_uri"] != "novel://project/machine-book/chapter/3/draft" {
		t.Fatalf("draft resource=%v", actions[2]["resource_uri"])
	}
	if actions[5]["resource_uri"] != "novel://project/machine-book/chapter/3/final" {
		t.Fatalf("commit resource=%v", actions[5]["resource_uri"])
	}
	if out["context_resource_uri"] != "novel://project/machine-book/context/3" {
		t.Fatalf("context resource=%v", out["context_resource_uri"])
	}
}

func TestChoiceActionsPreserveMutualExclusion(t *testing.T) {
	inst := flow.Route(flow.State{
		Progress:               &domain.Progress{Phase: domain.PhaseWriting, Flow: domain.FlowWriting},
		PlanningTier:           domain.PlanningTierLong,
		ImmediateFeedbackCount: 1,
	})
	if inst == nil {
		t.Fatal("writer feedback 应派发 architect choice")
	}
	actions := routeActions(inst.Actions)
	if len(actions) != 3 {
		t.Fatalf("feedback actions=%d, want 3: %#v", len(actions), actions)
	}
	if actions[0]["mode"] != "required" {
		t.Fatalf("首个 context 动作应 required: %#v", actions[0])
	}
	for _, i := range []int{1, 2} {
		if actions[i]["mode"] != "choice" || actions[i]["choice_group"] != "feedback_resolution" {
			t.Fatalf("互斥动作未保留 choice_group: %#v", actions[i])
		}
	}
}

func TestDecoratedChoiceActionsDependOnSharedPrerequisite(t *testing.T) {
	inst := flow.Route(flow.State{
		Progress:               &domain.Progress{Phase: domain.PhaseWriting, Flow: domain.FlowWriting},
		PlanningTier:           domain.PlanningTierLong,
		ImmediateFeedbackCount: 1,
	})
	if inst == nil {
		t.Fatal("expected feedback instruction")
	}
	out := decoratePlan("choice-book", map[string]any{"done": false, "actions": routeActions(inst.Actions)})
	actions := out["actions"].([]map[string]any)
	for _, i := range []int{1, 2} {
		deps := actions[i]["depends_on"].([]string)
		if len(deps) != 1 || deps[0] != "a1" || actions[i]["revision_source"] != "a1" {
			t.Fatalf("choice action %d metadata=%#v", i, actions[i])
		}
	}
}

func TestNextStepPendingCommitCarriesFrozenArguments(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "resume-actions")
	b, err := p.load(id)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"chapter": 2, "title": "冻结标题", "summary": "冻结摘要",
		"characters": []string{"甲"}, "key_events": []string{"事件"},
		"timeline_events": []any{}, "foreshadow_updates": []any{}, "relationship_changes": []any{},
		"state_changes": []any{}, "cast_intros": []any{}, "hook_type": nil, "dominant_strand": nil, "feedback": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.store.Signals.SavePendingCommit(domain.PendingCommit{Chapter: 2, Stage: domain.CommitStageStarted, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	out, err := NextStep(b.store, b.info)
	if err != nil {
		t.Fatal(err)
	}
	actions, ok := out["actions"].([]map[string]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("resume actions 不对: %#v", out)
	}
	args, _ := actions[0]["arguments"].(map[string]any)
	if actions[0]["tool"] != "commit_chapter" || args["title"] != "冻结标题" {
		t.Fatalf("未返回冻结 commit 参数: %#v", actions[0])
	}
	if actions[0]["id"] != "a1" || actions[0]["revision_source"] != "plan" ||
		actions[0]["resource_uri"] != "novel://project/resume-actions/chapter/2/final" {
		t.Fatalf("resume action 执行元数据不完整: %#v", actions[0])
	}
	required, _ := actions[0]["required_inputs"].([]string)
	if len(required) != 0 {
		t.Fatalf("已有冻结 payload 时不应要求模型重建参数: %#v", required)
	}
}
