package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 整本书的开书→写章→提交→导出闭环，全程走真实上游工具与其前置条件。
// 这里刻意不 mock：这个仓库的价值就在"上游的 Saga、checkpoint 与阶段守卫原样可用"，
// 一旦 mock 就只剩测试自己的断言了。
func TestFoundationAndChapterFlowOverTools(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "flow")
	rev := func() string { return revisionOf(t, p, id) }
	send := func(tool string, args map[string]any) map[string]any {
		full := map[string]any{}
		for k, v := range args {
			full[k] = v
		}
		out := call(t, p, id, tool, rev(), full)
		if e, bad := out["error"]; bad {
			t.Fatalf("%s 失败: %v", tool, e)
		}
		if r, ok := out["revision"].(string); ok {
			rev = func() string { return r }
		}
		res, _ := out["result"].(map[string]any)
		return res
	}

	// 规划期就该拦下写作。上游的阶段守卫是业务错误，走 error 信封而不是传输失败。
	raw, err := p.Call(context.Background(), id, "plan_chapter", rev(), map[string]any{"chapter": 1})
	if err != nil {
		t.Fatalf("阶段守卫不该表现为传输错误: %v", err)
	}
	if raw["error"] == nil {
		t.Fatal("未过基础设定审计就能规划章节")
	}
	if status := call(t, p, id, "project_status", "", nil); !strings.Contains(mustJSON(t, status), "book") {
		t.Fatalf("project_status 未列出缺失的设定: %v", status)
	}

	send("save_book", map[string]any{"title": "潮汐编年史", "synopsis": "守塔人发现潮汐开始倒退。"})
	premise := "# 前提\n守塔人能听见海水倒流的声音，由此追查一座被淹没的城。"
	send("save_foundation", map[string]any{"type": "premise", "content": premise, "scale": "short"})
	send("save_foundation", map[string]any{"type": "outline", "content": []any{
		map[string]any{"chapter": 1, "title": "倒流", "core_event": "守塔人记录到第一次倒潮", "hook": "灯塔下出现一行湿脚印", "scenes": []any{"塔顶", "滩涂"}},
	}})
	send("save_foundation", map[string]any{"type": "characters", "content": []any{
		map[string]any{"name": "林渡", "role": "protagonist", "description": "守塔人，沉默，记性极好", "arc": "从尽职到追查真相", "traits": []any{"克制", "多疑"}},
	}})
	send("save_foundation", map[string]any{"type": "world_rules", "content": []any{
		map[string]any{"category": "magic", "rule": "只有守塔人能听见潮汐声", "boundary": "离开灯塔三十步即失效"},
	}})

	// 审计必须针对当前落盘版本：fingerprint 由 novel_context 给出，过期即被拒。
	ctx := send("novel_context", map[string]any{})
	fingerprint, ok := findString(ctx, "fingerprint").(string)
	if !ok || len(fingerprint) < 32 {
		t.Fatalf("novel_context 未给出 foundation fingerprint: %s", mustJSON(t, ctx))
	}
	stale, err := p.Call(context.Background(), id, "audit_foundation", rev(), map[string]any{
		"fingerprint": strings.Repeat("0", 64), "ready": true, "summary": "x", "issues": []any{}})
	if err != nil {
		t.Fatalf("fingerprint 不符是客户端错误，不该表现为传输错误: %v", err)
	}
	if errInfo, _ := stale["error"].(map[string]any); errInfo == nil {
		t.Fatal("过期 fingerprint 的审计应被拒")
	} else if errInfo["code"] != "CONFLICT" {
		t.Fatalf("过期 fingerprint 应归 CONFLICT（上游 sentinel 原样分类），实得 %v", errInfo)
	}
	audit := send("audit_foundation", map[string]any{"fingerprint": fingerprint, "ready": true, "summary": "设定自洽，边界清晰", "issues": []any{}})
	if audit["foundation_ready"] != true {
		t.Fatalf("审计未通过: %v", audit)
	}
	if phase := findString(call(t, p, id, "project_status", "", nil), "phase"); phase != "writing" {
		t.Fatalf("审计通过后应进入 writing 阶段，实得 %v", phase)
	}

	send("plan_chapter", map[string]any{"chapter": 1, "title": "倒流", "goal": "让第一次倒潮可感", "conflict": "记录还是追查", "hook": "滩涂上的湿脚印"})
	body := "林渡把耳朵贴在灯室的铜栏杆上。\n\n海水正在倒着走。\n\n他数到第三十七下，声音停了。塔下的滩涂上多出一行湿脚印，朝着海的方向去，没有回来。"
	draft := send("draft_chapter", map[string]any{"chapter": 1, "content": body, "mode": "write"})
	if draft["written"] != true {
		t.Fatalf("draft 未落盘: %v", draft)
	}
	// 改一个字就要重新回读：append 之后的字数以回读为准。
	send("draft_chapter", map[string]any{"chapter": 1, "content": "\n\n他没有回头。", "mode": "append"})
	read := send("read_chapter", map[string]any{"chapter": 1, "source": "draft"})
	text, _ := read["content"].(string)
	if !strings.Contains(text, "他没有回头") || !strings.Contains(text, "湿脚印") {
		t.Fatalf("回读缺内容: %s", text)
	}
	check := send("check_consistency", map[string]any{"chapter": 1})
	if !strings.Contains(mustJSON(t, check), "world_rules") {
		t.Fatalf("一致性检查未带出对照资料: %s", mustJSON(t, check))
	}
	commit := send("commit_chapter", map[string]any{"chapter": 1, "title": "倒流", "summary": "守塔人记录第一次倒潮，并在滩涂发现一行湿脚印",
		"characters": []any{"林渡"}, "key_events": []any{"倒潮被记录", "湿脚印出现"}})
	if len(commit) == 0 {
		t.Fatal("提交未回任何事实")
	}

	out, err := p.Call(context.Background(), id, "export_book", "", map[string]any{"from_chapter": 1, "to_chapter": 1})
	if err != nil {
		t.Fatalf("导出已提交章节失败: %v", err)
	}
	content := mustJSON(t, out)
	if !strings.Contains(content, "湿脚印") {
		t.Fatalf("导出内容不含正文: %s", content)
	}
	if _, err := os.Stat(filepath.Join(p.dir(id), "chapters", "01.md")); err != nil {
		t.Fatalf("终稿未落盘: %v", err)
	}
	// 重放同一个 commit_chapter：上游的 pending/Saga 语义保证不会重复计入进度。
	status := call(t, p, id, "project_status", "", nil)
	if strings.Contains(mustJSON(t, status), "pending_commit\":{") {
		t.Fatalf("提交后仍有未完成提交: %s", mustJSON(t, status))
	}
}

// 已存在的项目必须原样留着，且拒绝被同名创建覆盖。
func TestCreateNeverOverwrites(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "keepme")
	if _, err := p.Create(id, "再来一遍", "default"); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("同名创建应被拒: %v", err)
	}
	if got := revisionOf(t, p, id); got == "" {
		t.Fatal("项目 revision 读不出来")
	}
}

// 两本书互不可见：这是"远端只能按 ID 寻址"的实际含义。
func TestProjectsAreIsolated(t *testing.T) {
	p := newProjects(t)
	a := create(t, p, "book-a")
	b := create(t, p, "book-b")
	call(t, p, "book-a", "save_book", a, map[string]any{"title": "甲书", "synopsis": "只有甲书有"})
	if strings.Contains(mustJSON(t, call(t, p, "book-b", "project_status", "", nil)), "甲书") {
		t.Fatal("项目之间串了")
	}
	if strings.Contains(mustJSON(t, call(t, p, "book-b", "novel_context", b, map[string]any{})), "甲书") {
		t.Fatal("上下文串了")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// findString 在结果树里找第一个该名的字符串值：上游的嵌套形状不是本仓库的契约，
// 测试不该把它写死。
func findString(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		if got, ok := x[key]; ok {
			return got
		}
		for _, child := range x {
			if got := findString(child, key); got != nil {
				return got
			}
		}
	case []any:
		for _, child := range x {
			if got := findString(child, key); got != nil {
				return got
			}
		}
	}
	return nil
}
