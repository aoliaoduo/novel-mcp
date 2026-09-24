package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestLongformLifecycleScenario 不是单个工具的单测，而是一个固定输出的“假 AI”。
// 它只使用 Projects.Call 这条公开业务入口，完整走过长篇最容易出错的状态边界：
// 写章 → 弧评审/摘要 → 骨架扩弧 → 卷摘要 → 追加收官卷 → 完结 → 重启 → 返工 → 再完结。
//
// 目标不是评价文学质量，而是保证 next_step 给 AI 的路线长期可执行、无死路、无多余动作。
func TestLongformLifecycleScenario(t *testing.T) {
	h := newLifecycleHarness(t)
	h.bootstrapLongBook()

	// 第一卷第一弧：两章写完后必须先走 Editor 聚合收尾。
	h.writeChapter(1)
	h.writeChapter(2)
	h.expectNext("editor", 0, "save_review")
	h.acceptArc(1, 1, 2)
	h.expectNext("editor", 0, "save_arc_summary")
	h.saveArcSummary(1, 1)

	// 下一弧初始只有骨架，不能直接让 writer 越界写；必须先展开。
	h.expectNext("architect_long", 0, "expand_next_arc")
	h.write("expand_next_arc", map[string]any{
		"title": "追潮", "goal": "沿倒潮追到旧城入口",
		"chapters": []any{
			map[string]any{"title": "潮沟", "core_event": "沿潮沟追索", "hook": "旧城门显现", "scenes": []any{"潮沟"}},
			map[string]any{"title": "城门", "core_event": "打开旧城门", "hook": "门后传来钟声", "scenes": []any{"旧城门"}},
		},
	})
	h.writeChapter(3)
	h.writeChapter(4)
	h.acceptArc(1, 2, 4)
	h.saveArcSummary(1, 2)
	h.expectNext("editor", 0, "save_volume_summary")
	h.saveVolumeSummary(1)

	// 卷末必须交给 Architect 决定继续/收官/完结。这里选择追加一个明确的收官卷。
	step := h.next()
	if step["agent"] != "architect_long" || !hasChoiceGroup(step, "volume_end") {
		t.Fatalf("卷末应给 architect_long 的 volume_end 选择，got %s", mustJSON(t, step))
	}
	h.write("save_foundation", map[string]any{
		"type":   "append_volume",
		"reason": "旧城真相尚未揭示，追加一卷并声明收官",
		"content": map[string]any{
			"title": "终潮", "theme": "揭示真相并收束", "final": true,
			"arcs": []any{map[string]any{
				"title": "终局", "goal": "揭示旧城真相并让潮汐复位",
				"chapters": []any{
					map[string]any{"title": "钟声", "core_event": "找到钟声来源", "hook": "潮水开始回转", "scenes": []any{"钟楼"}},
					map[string]any{"title": "归潮", "core_event": "完成最终选择", "hook": "晨光照进灯塔", "scenes": []any{"灯塔"}},
				},
			}},
		},
	})

	// 模拟服务重启：丢弃内存中的 book/tool cache，只从同一磁盘根重新加载。
	restarted, err := NewProjects(h.projects.root)
	if err != nil {
		t.Fatalf("restart projects: %v", err)
	}
	h.projects = restarted
	h.rev = revisionOf(t, restarted, h.id)

	h.writeChapter(5)
	h.writeChapter(6)
	h.acceptArc(2, 1, 6)
	h.saveArcSummary(2, 1)
	h.expectNext("editor", 0, "save_volume_summary")
	h.saveVolumeSummary(2)

	done := h.next()
	if done["done"] != true || done["need"] != "complete" || !strings.Contains(fmt.Sprint(done["summary"]), "reopen_book") {
		t.Fatalf("收官卷写完后应明确完结及返工入口，got %s", mustJSON(t, done))
	}
	h.verifyOK()

	// 完本后返工是高频 UX：next_step 不应再让 AI 调一个注定 skipped 的 plan_chapter。
	h.write("reopen_book", map[string]any{"chapters": []any{2}, "reason": "补强第二章的动机"})
	rewrite := h.expectNext("writer", 2, "draft_chapter")
	if containsAction(rewrite, "plan_chapter") || !strings.Contains(fmt.Sprint(rewrite["task"]), "不要重新 plan_chapter") {
		t.Fatalf("返工路由不应重新规划已完成章节，got %s", mustJSON(t, rewrite))
	}
	h.read("novel_context", map[string]any{"chapter": 2})
	h.write("draft_chapter", map[string]any{"chapter": 2, "mode": "write", "content": "第2章返工正文：林渡决定离塔，因为钟声第一次叫出了他的名字。"})
	h.read("read_chapter", map[string]any{"chapter": 2, "source": "draft"})
	h.read("check_consistency", map[string]any{"chapter": 2})
	h.commitChapter(2, "第二章返工后摘要")

	done = h.next()
	if done["done"] != true {
		t.Fatalf("返工队列排空后应自动重新完结，got %s", mustJSON(t, done))
	}
	h.verifyOK()
}

type lifecycleHarness struct {
	t        *testing.T
	projects *Projects
	id       string
	rev      string
}

func newLifecycleHarness(t *testing.T) *lifecycleHarness {
	t.Helper()
	p := newProjects(t)
	id := create(t, p, "long-lifecycle")
	return &lifecycleHarness{t: t, projects: p, id: id, rev: revisionOf(t, p, id)}
}

func (h *lifecycleHarness) bootstrapLongBook() {
	h.t.Helper()
	h.write("save_book", map[string]any{"title": "逆潮灯塔", "synopsis": "守塔人追查倒流潮汐与沉没旧城。"})
	h.write("save_foundation", map[string]any{"type": "premise", "scale": "long", "content": "# 逆潮灯塔\n守塔人追查倒流潮汐。"})
	h.write("save_foundation", map[string]any{
		"type": "layered_outline", "scale": "long",
		"content": []any{map[string]any{
			"title": "倒潮", "theme": "发现与追索",
			"arcs": []any{
				map[string]any{
					"title": "异兆", "goal": "确认倒潮不是幻觉",
					"chapters": []any{
						map[string]any{"title": "倒流", "core_event": "第一次记录倒潮", "hook": "出现湿脚印", "scenes": []any{"灯塔"}},
						map[string]any{"title": "脚印", "core_event": "追查湿脚印", "hook": "发现潮沟", "scenes": []any{"滩涂"}},
					},
				},
				map[string]any{"title": "追潮", "goal": "进入旧城", "estimated_chapters": 2, "chapters": []any{}},
			},
		}},
	})
	h.write("save_foundation", map[string]any{"type": "characters", "content": []any{
		map[string]any{"name": "林渡", "role": "protagonist", "description": "守塔人", "arc": "从守塔到主动追索", "traits": []any{"克制"}},
	}})
	h.write("save_foundation", map[string]any{"type": "world_rules", "content": []any{
		map[string]any{"category": "magic", "rule": "守塔人能听见潮声", "boundary": "离塔太远会变弱"},
	}})
	h.write("save_foundation", map[string]any{"type": "update_compass", "content": map[string]any{
		"ending_direction": "林渡理解旧城为何沉没并让潮汐恢复", "open_threads": []any{"旧城沉没原因"}, "estimated_scale": "2卷",
	}})
	ctx := h.read("novel_context", nil)
	fingerprint, _ := findString(ctx, "fingerprint").(string)
	if fingerprint == "" {
		h.t.Fatalf("foundation context 缺 fingerprint: %s", mustJSON(h.t, ctx))
	}
	h.write("audit_foundation", map[string]any{"fingerprint": fingerprint, "ready": true, "summary": "设定一致", "issues": []any{}})
}

func (h *lifecycleHarness) writeChapter(chapter int) {
	h.t.Helper()
	h.expectNext("writer", chapter, "plan_chapter")
	h.write("plan_chapter", map[string]any{
		"chapter": chapter, "title": fmt.Sprintf("第%d章", chapter), "goal": "推进主线", "conflict": "追索与风险", "hook": "继续追索",
	})
	h.write("draft_chapter", map[string]any{
		"chapter": chapter, "mode": "write", "content": fmt.Sprintf("第%d章正文：林渡沿着倒潮留下的痕迹继续追索。", chapter),
	})
	h.read("read_chapter", map[string]any{"chapter": chapter, "source": "draft"})
	h.read("check_consistency", map[string]any{"chapter": chapter})
	h.commitChapter(chapter, fmt.Sprintf("第%d章推进倒潮主线", chapter))
}

func (h *lifecycleHarness) commitChapter(chapter int, summary string) {
	h.t.Helper()
	h.write("commit_chapter", map[string]any{
		"chapter": chapter, "title": fmt.Sprintf("第%d章", chapter), "summary": summary,
		"characters": []any{"林渡"}, "key_events": []any{fmt.Sprintf("完成第%d章推进", chapter)},
	})
}

func (h *lifecycleHarness) acceptArc(volume, arc, chapter int) {
	h.t.Helper()
	h.expectNext("editor", 0, "save_review")
	h.write("save_review", map[string]any{
		"chapter": chapter, "scope": "arc",
		"dimensions": []any{map[string]any{"dimension": "continuity", "score": 90, "comment": "事实连续"}},
		"issues":     []any{}, "contract_status": nil, "contract_misses": []any{}, "contract_notes": nil,
		"verdict": "accept", "summary": fmt.Sprintf("第%d卷第%d弧通过", volume, arc),
	})
}

func (h *lifecycleHarness) saveArcSummary(volume, arc int) {
	h.t.Helper()
	h.expectNext("editor", 0, "save_arc_summary")
	h.write("save_arc_summary", map[string]any{
		"volume": volume, "arc": arc, "title": fmt.Sprintf("第%d卷第%d弧", volume, arc),
		"summary": "本弧完成预定推进。", "key_events": []any{"主线推进"}, "character_snapshots": []any{},
		"style_rules": map[string]any{
			"prose":    []any{"保持克制叙事"},
			"dialogue": []any{map[string]any{"name": "林渡", "rules": []any{"短句，少解释"}}},
			"taboos":   []any{"避免旁白剧透"},
		},
	})
}

func (h *lifecycleHarness) saveVolumeSummary(volume int) {
	h.t.Helper()
	h.write("save_volume_summary", map[string]any{
		"volume": volume, "title": fmt.Sprintf("第%d卷", volume), "summary": "本卷主线推进完成。", "key_events": []any{"卷级推进"},
	})
}

func (h *lifecycleHarness) next() map[string]any {
	h.t.Helper()
	out, err := h.projects.Call(context.Background(), h.id, "next_step", "", nil)
	if err != nil {
		h.t.Fatalf("next_step: %v", err)
	}
	h.rev, _ = out["revision"].(string)
	res, _ := out["result"].(map[string]any)
	return res
}

func (h *lifecycleHarness) expectNext(agent string, chapter int, tool string) map[string]any {
	h.t.Helper()
	res := h.next()
	if agent != "" && res["agent"] != agent {
		h.t.Fatalf("next agent=%v want=%s: %s", res["agent"], agent, mustJSON(h.t, res))
	}
	if chapter > 0 && intValue(res["chapter"]) != chapter {
		h.t.Fatalf("next chapter=%v want=%d: %s", res["chapter"], chapter, mustJSON(h.t, res))
	}
	if tool != "" && !containsAction(res, tool) {
		h.t.Fatalf("next actions 缺 %s: %s", tool, mustJSON(h.t, res))
	}
	return res
}

func (h *lifecycleHarness) write(tool string, args map[string]any) map[string]any {
	h.t.Helper()
	out := call(h.t, h.projects, h.id, tool, h.rev, args)
	if errInfo := out["error"]; errInfo != nil {
		h.t.Fatalf("%s 失败: %s", tool, mustJSON(h.t, errInfo))
	}
	if rev, ok := out["revision"].(string); ok {
		h.rev = rev
	}
	res, _ := out["result"].(map[string]any)
	return res
}

func (h *lifecycleHarness) read(tool string, args map[string]any) map[string]any {
	h.t.Helper()
	out := call(h.t, h.projects, h.id, tool, "", args)
	if errInfo := out["error"]; errInfo != nil {
		h.t.Fatalf("%s 失败: %s", tool, mustJSON(h.t, errInfo))
	}
	if rev, ok := out["revision"].(string); ok {
		h.rev = rev
	}
	res, _ := out["result"].(map[string]any)
	return res
}

func (h *lifecycleHarness) verifyOK() {
	h.t.Helper()
	res := h.read("verify_project", nil)
	if res["ok"] != true {
		h.t.Fatalf("verify_project 未通过: %s", mustJSON(h.t, res))
	}
}

func containsAction(step map[string]any, tool string) bool {
	actions, _ := step["actions"].([]any)
	for _, raw := range actions {
		item, _ := raw.(map[string]any)
		if item["tool"] == tool {
			return true
		}
	}
	return false
}

func hasChoiceGroup(step map[string]any, group string) bool {
	actions, _ := step["actions"].([]any)
	for _, raw := range actions {
		item, _ := raw.(map[string]any)
		if item["mode"] == "choice" && item["choice_group"] == group {
			return true
		}
	}
	return false
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
