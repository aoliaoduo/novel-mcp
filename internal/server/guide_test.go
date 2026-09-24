package server

import (
	"strings"
	"testing"
)

// 长篇规划师必须拿得到指南针模板：以前 architect role 只给短篇协议，
// ArchitectLong 在包里但无入口，长篇开书卡在 missing=compass。
func TestGuideArchitectLongServesCompassTemplate(t *testing.T) {
	guide, styles := guideForRole("architect_long")
	for _, want := range []string{"Story Compass", "ending_direction", "open_threads", "通用长篇规划参考"} {
		if !strings.Contains(guide, want) {
			t.Errorf("architect_long 缺 %q", want)
		}
	}
	if len(styles) == 0 {
		t.Error("styles 不应为空")
	}
	// 短篇入口保持原样：别把长篇内容串进去。
	short, _ := guideForRole("architect")
	if strings.Contains(short, "Story Compass") {
		t.Error("architect 不应包含长篇指南针模板")
	}
}

// 缺 compass 的长篇调 novel_context：除了 missing，还要直接给指引。
func TestCompassHintAppearsWhenLongformMissesCompass(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "long-no-compass")
	layered := []any{
		map[string]any{"index": 1, "title": "进城", "theme": "安顿", "arcs": []any{
			map[string]any{"index": 1, "title": "落脚", "goal": "安顿下来", "estimated_chapters": 3, "chapters": []any{}},
		}},
	}
	out := call(t, p, id, "save_foundation", revisionOf(t, p, id), map[string]any{"type": "layered_outline", "scale": "long", "content": layered})
	if out["error"] != nil {
		t.Fatalf("save layered_outline: %v", out["error"])
	}
	ctx := call(t, p, id, "novel_context", "", map[string]any{})
	res, _ := ctx["result"].(map[string]any)
	fm, _ := res["foundation_memory"].(map[string]any)
	st, _ := fm["foundation_status"].(map[string]any)
	missing, _ := st["missing"].([]any)
	found := false
	for _, m := range missing {
		if m == "compass" {
			found = true
		}
	}
	if !found {
		t.Fatalf("长篇无 compass 应进 missing: %v", missing)
	}
	hint, _ := fm["compass_hint"].(string)
	if !strings.Contains(hint, "architect_long") || !strings.Contains(hint, "update_compass") {
		t.Fatalf("compass_hint 应指路: %q", hint)
	}
}

// 短篇（无分层大纲）不应出现 compass 指引。
func TestCompassHintAbsentForShortBook(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "short-book")
	ctx := call(t, p, id, "novel_context", "", map[string]any{})
	res, _ := ctx["result"].(map[string]any)
	fm, _ := res["foundation_memory"].(map[string]any)
	if _, ok := fm["compass_hint"]; ok {
		t.Fatalf("短篇不应有 compass_hint: %v", fm["compass_hint"])
	}
}
