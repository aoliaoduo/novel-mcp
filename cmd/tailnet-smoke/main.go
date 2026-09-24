// tailnet-smoke 通过 Tailscale 提供的 HTTPS 地址跑一遍完整写作闭环。
//
// 为什么需要它：仓库里的 e2e 测试自己起服务、自己连回环地址，证明不了
// “经 ts.net 证书 + tailscale serve 转发 + 真实 DNS 名”这条路径可用。
// 这个程序不 mock 任何东西：真 DNS、真 TLS、真 HTTP、官方 SDK 客户端。
//
//	go run ./cmd/tailnet-smoke -url "https://<node>.<tailnet>.ts.net/mcp/<route>"
//
// 它会创建一个名为 smoke-<时间戳> 的临时项目，完整闭环通过后再用
// delete_project 删除，避免每次公网自检都在用户数据目录留下垃圾项目。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	target  = flag.String("url", "", "完整接入地址，含 /mcp/<route>")
	bearer  = flag.String("bearer", "", "需要 Bearer 时给出令牌")
	timeout = flag.Duration("timeout", 60*time.Second, "单次调用超时")
)

func main() {
	flag.Parse()
	if *target == "" {
		exit("缺少 -url")
	}
	if err := run(); err != nil {
		exit(err.Error())
	}
}

func exit(msg string) {
	fmt.Println("FAIL:", msg)
	os.Exit(1)
}

func ok(format string, a ...any) { fmt.Printf("  ok   "+format+"\n", a...) }

func run() error {
	base, route, err := split(*target)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(base, "https://") {
		fmt.Println("注意: 当前不是 https，浏览器端客户端会因混合内容被拦；仅同源/非浏览器客户端可用")
	}
	probeClient := &http.Client{
		Timeout:       *timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}

	// 反向验证：错路径必须 404，不能有重定向把客户端引到正确路径上。
	if code := probe(probeClient, base, "/mcp/"+strings.Repeat("0", 64)); code != 404 {
		return fmt.Errorf("错误路由应 404，实得 %d", code)
	}
	ok("错误路由 404")
	health, code := healthz(probeClient, base)
	if code != 200 || !strings.Contains(health, `"ok":true`) {
		return fmt.Errorf("healthz 异常: %d %s", code, health)
	}
	ok("healthz %s", strings.TrimSpace(health))

	session, err := connect(base, route)
	if err != nil {
		return err
	}
	defer session.Close()
	ok("已完成 MCP 握手（instructions 长度 %d）", len(session.InitializeResult().Instructions))

	tools, err := listTools(session)
	if err != nil {
		return err
	}
	want := []string{"list_projects", "create_project", "delete_project", "novel_guide", "project_status", "verify_project", "export_book",
		"novel_context", "save_book", "save_foundation", "audit_foundation", "plan_chapter",
		"draft_chapter", "read_chapter", "check_consistency", "commit_chapter"}
	missing := []string{}
	for _, name := range want {
		if !tools[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少工具: %v（共看到 %d 个）", missing, len(tools))
	}
	ok("工具清单 %d 个，关键工具齐全", len(tools))

	id := "smoke-" + time.Now().Format("20060102-150405")
	created, err := call(session, "create_project", map[string]any{"id": id, "brief": "Tailscale 连通性演练：短篇。", "style": "default"})
	if err != nil {
		return err
	}
	rev := str(created["revision"])
	ok("create_project %s revision=%s", id, short(rev))

	step := func(name string, args map[string]any) (map[string]any, error) {
		args["project"] = id
		args["expected_revision"] = rev
		out, err := call(session, name, args)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if next := str(out["revision"]); next != "" {
			rev = next
		}
		return out, nil
	}

	if out, err := step("save_book", map[string]any{"title": "尾网纪事", "synopsis": "一次经 Tailscale 的连通性演练。"}); err != nil {
		return err
	} else {
		ok("save_book revision=%s", short(str(out["revision"])))
	}

	foundations := []map[string]any{
		{"type": "premise", "scale": "short", "content": "# 前提\n守塔人发现潮汐开始倒退。"},
		{"type": "outline", "content": []any{
			map[string]any{"chapter": 1, "title": "倒流", "core_event": "记录第一次倒潮", "hook": "滩涂上的湿脚印", "scenes": []any{"塔顶", "滩涂"}},
			map[string]any{"chapter": 2, "title": "第二夜", "core_event": "沿脚印追查", "hook": "塔门被敲响", "scenes": []any{"滩涂", "城门"}},
		}},
		{"type": "characters", "content": []any{
			map[string]any{"name": "林渡", "role": "protagonist", "description": "守塔人，沉默", "arc": "从尽职到追查真相", "traits": []any{"克制"}},
		}},
		{"type": "world_rules", "content": []any{
			map[string]any{"category": "magic", "rule": "只有守塔人能听见潮汐声", "boundary": "离开灯塔三十步即失效"},
		}},
	}
	for _, f := range foundations {
		if _, err := step("save_foundation", f); err != nil {
			return err
		}
		ok("save_foundation %v", f["type"])
	}

	// 过期 revision 必须被拦：这是网页客户端最需要依赖的一条。
	stale := map[string]any{"project": id, "expected_revision": strings.Repeat("f", 64), "title": "x", "synopsis": "y"}
	if _, err := call(session, "save_book", stale); err == nil {
		return fmt.Errorf("过期 revision 未被拦住")
	} else if !strings.Contains(err.Error(), "REVISION_CONFLICT") {
		return fmt.Errorf("过期 revision 应回 REVISION_CONFLICT，实得 %v", err)
	}
	ok("过期 revision 被拒为 REVISION_CONFLICT")

	ctxOut, err := call(session, "novel_context", map[string]any{"project": id})
	if err != nil {
		return err
	}
	fingerprint := str(find(ctxOut, "fingerprint"))
	if len(fingerprint) < 32 {
		return fmt.Errorf("novel_context 未给出 fingerprint: %v", ctxOut)
	}
	ok("novel_context fingerprint=%s", short(fingerprint))

	if _, err := step("audit_foundation", map[string]any{"fingerprint": fingerprint, "ready": true, "summary": "设定自洽", "issues": []any{}}); err != nil {
		return err
	}
	ok("audit_foundation 通过")

	if _, err := step("plan_chapter", map[string]any{"chapter": 1, "title": "倒流", "goal": "让第一次倒潮可感", "conflict": "记录还是追查", "hook": "滩涂上的湿脚印"}); err != nil {
		return err
	}
	ok("plan_chapter")
	if _, err := step("draft_chapter", map[string]any{"chapter": 1, "content": "林渡把耳朵贴在灯室的铜栏杆上。\n\n海水正在倒着走。", "mode": "write"}); err != nil {
		return err
	}
	if _, err := step("draft_chapter", map[string]any{"chapter": 1, "content": "\n\n他没有回头。", "mode": "append"}); err != nil {
		return err
	}
	ok("draft_chapter write+append")
	if _, err := step("read_chapter", map[string]any{"chapter": 1, "source": "draft"}); err != nil {
		return err
	}
	ok("read_chapter draft")
	if _, err := step("check_consistency", map[string]any{"chapter": 1}); err != nil {
		return err
	}
	ok("check_consistency")
	if _, err := step("commit_chapter", map[string]any{
		"chapter": 1, "title": "倒流", "summary": "记录第一次倒潮并发现湿脚印",
		"characters": []any{"林渡"}, "key_events": []any{"倒潮被记录", "湿脚印出现"},
		"timeline_events":      []any{map[string]any{"time": "第一夜", "event": "倒潮被记录", "characters": []any{"林渡"}}},
		"foreshadow_updates":   []any{map[string]any{"id": "F1", "action": "plant", "description": "湿脚印通向海里"}},
		"relationship_changes": []any{},
		"state_changes":        []any{map[string]any{"entity": "林渡", "field": "认知", "old_value": nil, "new_value": "确信海水在倒流", "reason": "亲耳记录"}},
		"cast_intros":          []any{},
		"hook_type":            "mystery",
		"dominant_strand":      "quest",
		"feedback":             nil,
	}); err != nil {
		return err
	}
	ok("commit_chapter revision=%s", short(rev))

	exported, err := call(session, "export_book", map[string]any{"project": id, "from_chapter": 1, "to_chapter": 1})
	if err != nil {
		return err
	}
	body := strings.Join(collect(exported), " ")
	if !strings.Contains(body, "海水正在倒着走") || !strings.Contains(body, "他没有回头") {
		return fmt.Errorf("导出缺少正文: %s", body)
	}
	ok("export_book 含完整正文")

	status, err := call(session, "project_status", map[string]any{"project": id})
	if err != nil {
		return err
	}
	if got := str(status["revision"]); got != rev {
		return fmt.Errorf("project_status 的 revision 与写入后不一致: %s != %s", short(got), short(rev))
	}
	if phase := str(find(status, "phase")); phase != "writing" {
		return fmt.Errorf("阶段应为 writing，实得 %v", phase)
	}
	ok("project_status 阶段 writing，revision 一致")

	if _, err := call(session, "delete_project", map[string]any{"project": id, "expected_revision": rev}); err != nil {
		return fmt.Errorf("闭环已通过，但清理临时项目失败: %w", err)
	}
	ok("delete_project 已清理临时 smoke 项目")

	fmt.Println("\nPASS  经 Tailscale 的完整闭环可用，临时项目已清理。")
	return nil
}

// ---- 以下是把 SDK 的返回值摊平所需的最小胶水 -------------------------------

func split(url string) (string, string, error) {
	i := strings.Index(url, "/mcp/")
	if i < 0 {
		return "", "", fmt.Errorf("URL 里没有 /mcp/<route>: %s", url)
	}
	base, route := url[:i], url[i+len("/mcp/"):]
	if len(route) != 64 {
		return "", "", fmt.Errorf("route 长度应为 64，实得 %d", len(route))
	}
	return base, route, nil
}

func probe(client *http.Client, base, path string) int {
	resp, err := client.Get(base + path)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode
}

func healthz(client *http.Client, base string) (string, int) {
	resp, err := client.Get(base + "/healthz")
	if err != nil {
		return "", 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return string(body), resp.StatusCode
}

type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}

func connect(base, route string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "tailnet-smoke", Version: "1.0.0"}, nil)
	tr := &mcp.StreamableClientTransport{Endpoint: base + "/mcp/" + route}
	httpClient := &http.Client{
		Timeout:       *timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	if *bearer != "" {
		httpClient.Transport = bearerTransport{token: *bearer}
	}
	tr.HTTPClient = httpClient
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	return client.Connect(ctx, tr, nil)
}

func listTools(s *mcp.ClientSession) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	res, err := s.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(res.Tools))
	for _, t := range res.Tools {
		out[t.Name] = true
	}
	return out, nil
}

func call(s *mcp.ClientSession, name string, args map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if res.IsError {
		msg := ""
		for _, c := range res.Content {
			if tc, isText := c.(*mcp.TextContent); isText {
				msg += tc.Text
			}
		}
		if e, isMap := out["error"].(map[string]any); isMap {
			msg = fmt.Sprintf("%v: %v", e["code"], e["message"])
		}
		return out, fmt.Errorf("%s", msg)
	}
	return out, nil
}

func str(v any) string { s, _ := v.(string); return s }

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func find(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		if got, present := x[key]; present {
			return got
		}
		for _, child := range x {
			if got := find(child, key); got != nil {
				return got
			}
		}
	case []any:
		for _, child := range x {
			if got := find(child, key); got != nil {
				return got
			}
		}
	}
	return nil
}

func collect(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for _, child := range x {
			out = append(out, collect(child)...)
		}
	case []any:
		for _, child := range x {
			out = append(out, collect(child)...)
		}
	case string:
		out = append(out, x)
	}
	return out
}
