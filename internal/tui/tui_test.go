package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"novel-mcp/internal/server"
)

func fakeSnapshot() Snapshot {
	now := time.Date(2026, 9, 23, 15, 4, 5, 0, time.Local)
	return Snapshot{
		At:        now,
		Uptime:    192 * time.Second,
		Conn:      Connection{Version: "0.1.0", AccessURL: "https://example.ts.net/mcp/route1", Bearer: "bearer1", HealthURL: "https://example.ts.net/healthz"},
		Calls:     3,
		Successes: 2,
		Failures:  1,
		Events: []server.Event{
			{At: now, Tool: "draft_chapter", Project: "p1", OK: false, Latency: 3 * time.Millisecond, Err: "boom happened"},
			{At: now, Tool: "plan_chapter", Project: "p1", OK: true, Latency: 12 * time.Millisecond},
		},
		Projects: []ProjectRow{{ID: "pub-gold-0923", Brief: "仙侠开局", CreatedAt: "2026-09-23T10:00:00+08:00", Chapters: 3, Phase: "writing", Flow: "writing", CurrentChapter: 4, TotalChapters: 10, PendingRewrites: 1}},
	}
}

func testModel() model {
	m := newModel(fakeSnapshot)
	m.width, m.height = 120, 40
	return m
}

func keyPress(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func press(m model, k string) model {
	next, _ := m.Update(keyPress(k))
	return next.(model)
}

func TestTabCycle(t *testing.T) {
	m := testModel()
	if m.tab != tabConn {
		t.Fatal("default tab should be conn")
	}
	m = press(m, "tab")
	if m.tab != tabEvents {
		t.Fatal("tab should go conn->events")
	}
	m = press(m, "tab")
	m = press(m, "tab")
	if m.tab != tabConn {
		t.Fatal("tab should wrap around")
	}
	m = press(m, "shift+tab")
	if m.tab != tabProjects {
		t.Fatal("shift+tab should go back")
	}
	for i, k := range []string{"1", "2", "3"} {
		if m = press(m, k); m.tab != viewTab(i) {
			t.Fatalf("key %s should jump to tab %d", k, i)
		}
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		m := press(testModel(), k)
		if !m.quitting {
			t.Fatalf("key %s should quit", k)
		}
		if m.View() != "" {
			t.Fatal("quitting view should be empty")
		}
	}
	// Esc 只回顶部，不退出。
	m := press(testModel(), "esc")
	if m.quitting {
		t.Fatal("esc must not quit")
	}
}

func TestScrollClamp(t *testing.T) {
	m := testModel()
	m.tab = tabEvents
	m = press(m, "up")
	if m.offsets[tabEvents] != 0 {
		t.Fatal("scroll above top must clamp to 0")
	}
	m = press(m, "end")
	if m.offsets[tabEvents] != 0 {
		t.Fatal("short content: end must stay 0")
	}
	// 造长事件流：offset 必须能下去也能 clamp。
	big := fakeSnapshot()
	for i := 0; i < 100; i++ {
		big.Events = append(big.Events, server.Event{Tool: "t", Project: "p", OK: true})
	}
	m.snap = big
	m.height = 13 // body=9
	m = press(m, "end")
	if want := 102 - 9; m.offsets[tabEvents] != want {
		t.Fatalf("end should be %d, got %d", want, m.offsets[tabEvents])
	}
	m = press(m, "down")
	if m.offsets[tabEvents] != 102-9 {
		t.Fatal("scroll past end must clamp")
	}
}

func TestViewConn(t *testing.T) {
	m := testModel()
	v := m.View()
	for _, want := range []string{"novel-mcp", "运行中", "3分12秒", "MCP 服务已就绪", "已收到 MCP 调用", "复制 MCP 配置", "MCP URL", "BEARER", "健康检查", "调用 3", "✓2", "✕1", "连接", "事件", "项目", "C 配置"} {
		if !strings.Contains(v, want) {
			t.Fatalf("conn view missing %q", want)
		}
	}
	if strings.Contains(v, "route1") || strings.Contains(v, "bearer1") {
		t.Fatal("connection secrets must be masked by default")
	}
	m = press(m, "v")
	v = m.View()
	if !strings.Contains(v, "route1") || !strings.Contains(v, "bearer1") {
		t.Fatal("V should reveal connection secrets")
	}
}

func TestConnectionActions(t *testing.T) {
	var copied string
	var opened string
	m := newModelWithActions(fakeSnapshot,
		func(text string) error { copied = text; return nil },
		func(rawURL string) error { opened = rawURL; return nil },
	)
	m.width, m.height = 120, 40
	next, cmd := m.Update(keyPress("c"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("C should return clipboard command")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if !strings.Contains(copied, "\"mcpServers\"") || !strings.Contains(copied, "\"Authorization\": \"Bearer bearer1\"") {
		t.Fatalf("copied config incomplete: %s", copied)
	}
	if !strings.Contains(m.footLine(), "已复制 MCP 配置") {
		t.Fatalf("copy notice missing: %s", m.footLine())
	}

	m.notice = ""
	next, cmd = m.Update(keyPress("o"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	if opened != "https://example.ts.net/healthz" || !strings.Contains(m.footLine(), "已打开健康检查") {
		t.Fatalf("open health failed: opened=%q footer=%q", opened, m.footLine())
	}
}

func TestEventErrorFilter(t *testing.T) {
	m := testModel()
	m.tab = tabEvents
	m = press(m, "e")
	v := m.View()
	if !strings.Contains(v, "draft_chapter") || strings.Contains(v, "plan_chapter") {
		t.Fatalf("error filter mismatch: %s", v)
	}
	if !strings.Contains(m.footLine(), "全部事件") {
		t.Fatal("footer should offer returning to all events")
	}
}

func TestViewEvents(t *testing.T) {
	m := testModel()
	m.tab = tabEvents
	v := m.View()
	for _, want := range []string{"draft_chapter", "plan_chapter", "boom happened", "12ms", "[p1]"} {
		if !strings.Contains(v, want) {
			t.Fatalf("events view missing %q", want)
		}
	}
	if strings.Index(v, "draft_chapter") > strings.Index(v, "plan_chapter") {
		t.Fatal("newest event should come first")
	}
}

func TestViewProjects(t *testing.T) {
	m := testModel()
	m.tab = tabProjects
	v := m.View()
	for _, want := range []string{"pub-gold-0923", "仙侠开局", "2026-09-23", "3/10章", "写作 · 第4章", "返工 1"} {
		if !strings.Contains(v, want) {
			t.Fatalf("projects view missing %q", want)
		}
	}
}

func TestViewEmptyStates(t *testing.T) {
	m := testModel()
	m.snap.Events = nil
	m.snap.Projects = nil
	m.tab = tabEvents
	if v := m.View(); !strings.Contains(v, "暂无调用") {
		t.Fatal("empty events should hint")
	}
	m.tab = tabProjects
	if v := m.View(); !strings.Contains(v, "还没有项目") {
		t.Fatal("empty projects should hint")
	}
	m.snap.ProjectsErr = "disk gone"
	if v := m.View(); !strings.Contains(v, "disk gone") {
		t.Fatal("projects error should show")
	}
}

func TestViewBearerOff(t *testing.T) {
	m := testModel()
	m.snap.Conn.BearerOff = true
	if v := m.View(); !strings.Contains(v, "已关闭") {
		t.Fatal("bearer-off should show notice")
	}
}

func TestFormatUptime(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Second:            "45秒",
		192 * time.Second:           "3分12秒",
		2*time.Hour + 5*time.Minute: "2时5分",
		-3 * time.Second:            "0秒",
	}
	for d, want := range cases {
		if got := formatUptime(d); got != want {
			t.Fatalf("%v: got %q want %q", d, got, want)
		}
	}
}

func TestChunkRunes(t *testing.T) {
	if got := chunkRunes("abcdef", 4); len(got) != 2 || got[0] != "abcd" || got[1] != "ef" {
		t.Fatalf("got %v", got)
	}
	// 中文按 cell 切：w=5 时“一二三四五”切成 3 块（按 rune 切只会是 1 块）。
	if got := chunkRunes("一二三四五", 5); len(got) != 3 || got[0] != "一二" || got[1] != "三四" || got[2] != "五" {
		t.Fatalf("got %v", got)
	}
	// 回归：中英混排折行不断字——各块拼回去必须与原文完全一致。
	mixed := "通过 MCP 连接 novel-mcp 小说引擎：MCP 端点填 https://x/mcp/766a17b5；连上后先调 list_projects 看看有哪些项目。"
	got := chunkRunes(mixed, 20)
	joined := ""
	for _, c := range got {
		joined += c
		if len([]rune(c)) > 20 {
			t.Fatalf("块超宽: %q", c)
		}
	}
	if joined != mixed {
		t.Fatalf("丢字: %q", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abcdef", 4); got != "abc…" {
		t.Fatalf("got %q", got)
	}
	// 中文按 cell 截：n=5 时“一二”(4 格)+… 正好。
	if got := truncateRunes("一二三四", 5); got != "一二…" {
		t.Fatalf("got %q", got)
	}
	// 不超宽原样返回。
	if got := truncateRunes("ab一", 5); got != "ab一" {
		t.Fatalf("got %q", got)
	}
}

func TestWanted(t *testing.T) {
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	if Wanted(true, w, r) {
		t.Fatal("noTUI must win")
	}
	if Wanted(false, w, r) {
		t.Fatal("pipes are not a console")
	}
}

func TestWindowSizeClamp(t *testing.T) {
	m := testModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 10, Height: 5})
	mm := next.(model)
	if mm.width != 40 || mm.height != 10 {
		t.Fatalf("got %dx%d", mm.width, mm.height)
	}
}

func TestViewEventNoProject(t *testing.T) {
	m := testModel()
	m.snap.Events = []server.Event{{Tool: "list_projects", OK: true}}
	m.tab = tabEvents
	if v := m.View(); strings.Contains(v, "[]") {
		t.Fatal("global tool must not show empty brackets")
	}
}
