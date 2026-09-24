package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"novel-mcp/internal/domain"
)

type concurrentReadProbe struct {
	entered chan<- struct{}
	release <-chan struct{}
}

type malformedWriteProbe struct{ path string }

func (t *malformedWriteProbe) Name() string           { return "malformed_write" }
func (t *malformedWriteProbe) Description() string    { return "test malformed write result" }
func (t *malformedWriteProbe) Schema() map[string]any { return map[string]any{"type": "object"} }
func (t *malformedWriteProbe) Execute(context.Context, json.RawMessage) (json.RawMessage, error) {
	if err := os.WriteFile(t.path, []byte("written"), 0o600); err != nil {
		return nil, err
	}
	return json.RawMessage(`{`), nil
}

func (t *concurrentReadProbe) Name() string                         { return "probe_read" }
func (t *concurrentReadProbe) Description() string                  { return "test concurrent read probe" }
func (t *concurrentReadProbe) Schema() map[string]any               { return map[string]any{"type": "object"} }
func (t *concurrentReadProbe) ReadOnly(json.RawMessage) bool        { return true }
func (t *concurrentReadProbe) ConcurrencySafe(json.RawMessage) bool { return true }
func (t *concurrentReadProbe) Execute(context.Context, json.RawMessage) (json.RawMessage, error) {
	t.entered <- struct{}{}
	<-t.release
	return json.RawMessage(`{"ok":true}`), nil
}

func newProjects(t *testing.T) *Projects {
	t.Helper()
	p, err := NewProjects(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// create 建项目并返回 ID；revision 一律用 revisionOf 现取，测试不复制指纹算法。
func create(t *testing.T, p *Projects, id string) string {
	t.Helper()
	if _, err := p.Create(id, "测试需求", "default"); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	if revisionOf(t, p, id) == "" {
		t.Fatalf("create %s 后读不出 revision", id)
	}
	return id
}

// revisionOf 通过只读的 project_status 读当前 revision。
func revisionOf(t *testing.T, p *Projects, id string) string {
	t.Helper()
	out, err := p.Call(context.Background(), id, "project_status", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return out["revision"].(string)
}

func call(t *testing.T, p *Projects, id, tool, rev string, args map[string]any) map[string]any {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	out, err := p.Call(context.Background(), id, tool, rev, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return out
}

func TestProjectIDsCannotAddressPaths(t *testing.T) {
	p := newProjects(t)
	for _, bad := range []string{"", "..", "../../etc", "a/b", `a\b`, "UPPER", "con", strings.Repeat("x", 49), "-lead", `..\..`} {
		if _, err := p.Create(bad, "x", "default"); err == nil {
			t.Errorf("Create(%q) 应当被拒", bad)
		}
		if _, err := p.Call(context.Background(), bad, "project_status", "", nil); err == nil {
			t.Errorf("Call(%q) 应当被拒", bad)
		}
	}
}

// 未初始化的目录不能顺手套到别人头上。
func TestProjectLimitIgnoresNonProjectEntries(t *testing.T) {
	p := newProjects(t)
	for i := 0; i < maxProjects; i++ {
		name := filepath.Join(p.root, fmt.Sprintf(".creating-stale-%03d", i))
		if err := os.Mkdir(name, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Create("real-book", "测试需求", "default"); err != nil {
		t.Fatalf("stale/non-project entries must not consume project quota: %v", err)
	}
}

func TestUnknownProjectIsNotCreatedImplicitly(t *testing.T) {
	p := newProjects(t)
	if _, err := p.Call(context.Background(), "ghost", "project_status", "", nil); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("应报项目不存在，实得 %v", err)
	}
	if items, err := p.List(); err != nil || len(items) != 0 {
		t.Fatalf("空数据目录不该有项目: %v %+v", err, items)
	}
}

func TestConcurrentReadToolsShareBookLock(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "concurrent-read")
	b, err := p.load(id)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	b.tools["probe_read"] = &concurrentReadProbe{entered: entered, release: release}

	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := p.Call(context.Background(), id, "probe_read", "", nil)
			errs <- err
		}()
	}

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for count := 0; count < 2; count++ {
		select {
		case <-entered:
		case <-timer.C:
			close(release)
			t.Fatal("两个并发安全纯读工具未能同时进入 Execute")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestMalformedWriteResultReturnsLatestRevisionInsteadOfTransportError(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "malformed-write")
	b, err := p.load(id)
	if err != nil {
		t.Fatal(err)
	}
	b.tools["malformed_write"] = &malformedWriteProbe{path: filepath.Join(p.dir(id), "meta", "probe.txt")}
	before := revisionOf(t, p, id)
	out, err := p.Call(context.Background(), id, "malformed_write", before, nil)
	if err != nil {
		t.Fatalf("post-write encoding failure must stay in-band, got transport error: %v", err)
	}
	if out["revision"] == before {
		t.Fatalf("write changed disk but returned stale revision: %+v", out)
	}
	errInfo := out["error"].(map[string]any)
	if errInfo["code"] != "NOVEL_TOOL_FAILED" {
		t.Fatalf("unexpected error envelope: %+v", errInfo)
	}
	recovery := errInfo["recovery"].(map[string]any)
	if recovery["tool"] != "next_step" {
		t.Fatalf("write-side internal failure should recover via next_step: %+v", errInfo)
	}
}

func TestWarmCallStillRejectsCorruptProjectMetadata(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "damaged-meta")
	// create/revisionOf 已把 book 放进缓存；随后从磁盘外部破坏元数据，warm path
	// 不能因为跳过第二次 scan 就继续使用旧的 b.info。
	if err := os.WriteFile(filepath.Join(p.dir(id), "project.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := p.Call(context.Background(), id, "project_status", "", nil)
	var ce *codedError
	if !errors.As(err, &ce) || ce.code != "PROJECT_DAMAGED" {
		t.Fatalf("warm Call 必须拒绝损坏 project.json，实得 %v", err)
	}
}

func TestProjectStatusWarningsHideProjectRoot(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "status-redact")
	b, err := p.load(id)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := b.store.Progress.Load()
	if err != nil {
		t.Fatal(err)
	}
	progress.CompletedChapters = []int{1}
	progress.CurrentChapter = 2
	if err := b.store.Progress.Save(progress); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p.dir(id), "chapters", "01.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := p.Call(context.Background(), id, "project_status", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	result := out["result"].(map[string]any)
	warnings := result["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatal("expected consistency warning")
	}
	if strings.Contains(warnings[0].(string), p.root) {
		t.Fatalf("project_status leaked project root: %q", warnings[0])
	}
}

func TestSymlinkInsideProjectIsRefused(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "linked-book")
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	// revision 要在造链接之前取：链接一出现，项目本身就整体不可读了。
	rev := revisionOf(t, p, id)
	if err := os.Symlink(target, filepath.Join(p.dir(id), "sneak")); err != nil {
		t.Skipf("当前环境无法创建符号链接（Windows 需开发者模式）: %v", err)
	}
	_, err := p.Call(context.Background(), id, "save_book", rev, map[string]any{"title": "x", "synopsis": "y"})
	if err == nil {
		t.Fatal("含符号链接的项目应整体拒绝访问")
	}
	if !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("应给出符号链接原因，实得 %v", err)
	}
	// 也必须在进入项目之前就被拦下：错误文本里不能出现数据根下的真实路径。
	if strings.Contains(err.Error(), "sneak") && !strings.HasPrefix(err.Error(), "项目不可用") {
		t.Fatalf("拒绝原因不应回显目录内条目: %v", err)
	}
	if _, err := p.Call(context.Background(), id, "project_status", "", nil); err == nil {
		t.Fatal("读路径也必须拒绝含符号链接的项目")
	}
	if _, err := p.List(); err == nil {
		t.Fatal("List 也必须拒绝损坏项，而不是悄悄跳过")
	}
}

func TestRevisionGuardsBlindRetry(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "guarded")
	// 从未读过的乱码 revision 也走同一条拒绝路径。
	blank, err := p.Call(context.Background(), id, "save_book", strings.Repeat("0", 64), map[string]any{"title": "书名", "synopsis": "简介"})
	if err != nil {
		t.Fatalf("revision 不符是客户端错误，不该表现为传输错误: %v", err)
	}
	if code, _ := blank["error"].(map[string]any)["code"].(string); code != "REVISION_CONFLICT" {
		t.Fatalf("陈旧 revision 必须回 REVISION_CONFLICT，实得 %v", blank["error"])
	}
	recovery, _ := blank["error"].(map[string]any)["recovery"].(map[string]any)
	if recovery["tool"] != "next_step" {
		t.Fatalf("revision 冲突应直接提示重新路由，实得 %v", blank["error"])
	}
	before := revisionOf(t, p, id)
	first := call(t, p, id, "save_book", before, map[string]any{"title": "书名", "synopsis": "简介"})
	if first["error"] != nil {
		t.Fatalf("带正确 revision 的写入不应失败: %v", first["error"])
	}
	// 用写之前那个 revision 重放 append：这是超时重试最典型的事故，必须拦住。
	// 拦住的理由必须是 revision 冲突，而不是碰巧撞上阶段守卫。
	conflict := call(t, p, id, "draft_chapter", before, map[string]any{"chapter": 1, "content": "重复正文", "mode": "append"})
	code, _ := conflict["error"].(map[string]any)["code"].(string)
	if code != "REVISION_CONFLICT" {
		t.Fatalf("过期 revision 的重放必须被拦住，实得 %v", conflict)
	}
	if conflict["revision"] == nil || conflict["revision"] == before {
		t.Fatalf("被拒的写入也要回最新 revision，实得 %v", conflict["revision"])
	}
	data, err := os.ReadFile(filepath.Join(p.dir(id), "drafts", "01.draft.md"))
	if err == nil && strings.Contains(string(data), "重复正文") {
		t.Fatalf("被拒的写入落盘了: %s", data)
	}
}

func TestEditRecoveryHintOnlyForMatchFailures(t *testing.T) {
	args := map[string]any{"chapter": 3}
	hint := recoveryForTool("book", "edit_chapter", args, "PRECONDITION_FAILED", errors.New("apply edit: could not find the exact text in drafts/03.draft.md"))
	if hint["tool"] != "read_chapter" {
		t.Fatalf("精确匹配失败应先重读草稿: %#v", hint)
	}
	hintArgs, _ := hint["arguments"].(map[string]any)
	if hintArgs["project"] != "book" || hintArgs["chapter"] != 3 || hintArgs["source"] != "draft" {
		t.Fatalf("恢复参数不完整: %#v", hint)
	}
	if got := recoveryForTool("book", "edit_chapter", args, "PRECONDITION_FAILED", errors.New("章节不在返工队列")); got != nil {
		t.Fatalf("无确定恢复路径时不应瞎给建议: %#v", got)
	}
}

func TestFailedDraftCheckpointReturnsNewRevisionAndBlocksAppendReplay(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "failed-append")
	b, err := p.load(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.store.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatal(err)
	}

	// CheckpointStore 已初始化后，把 jsonl 目标占成目录：draft/progress 可以成功落盘，
	// 最后的 checkpoint append 必定失败，稳定模拟“写成功但响应失败”的窗口。
	checkpointPath := filepath.Join(p.dir(id), "meta", "checkpoints.jsonl")
	if err := os.MkdirAll(checkpointPath, 0o755); err != nil {
		t.Fatal(err)
	}
	before := revisionOf(t, p, id)
	failed := call(t, p, id, "draft_chapter", before, map[string]any{
		"chapter": 1, "content": "唯一追加片段。", "mode": "append",
	})
	errInfo, _ := failed["error"].(map[string]any)
	message, _ := errInfo["message"].(string)
	if errInfo == nil || !strings.Contains(message, "checkpoint draft") {
		t.Fatalf("应在 checkpoint 阶段失败，实得 %+v", failed)
	}
	after, _ := failed["revision"].(string)
	if after == "" || after == before {
		t.Fatalf("有副作用的失败必须返回新 revision: before=%s after=%s", before, after)
	}
	if err := os.Remove(checkpointPath); err != nil {
		t.Fatal(err)
	}

	// 模拟客户端因超时/错误盲目重试原请求：仍带旧 revision，必须在工具执行前被拦。
	replay := call(t, p, id, "draft_chapter", before, map[string]any{
		"chapter": 1, "content": "唯一追加片段。", "mode": "append",
	})
	if code, _ := replay["error"].(map[string]any)["code"].(string); code != "REVISION_CONFLICT" {
		t.Fatalf("失败写入后的旧 revision 重放必须被拦，实得 %+v", replay)
	}
	draft, err := b.store.Drafts.LoadDraft(1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(draft, "唯一追加片段。") != 1 {
		t.Fatalf("盲目重放造成重复追加: %q", draft)
	}
}

// 执行失败也可能已经落盘（上游会写 checkpoint），所以失败结果也要带 revision。
func TestToolFailureReportsRevisionAndHidesPaths(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "failing")
	out := call(t, p, id, "commit_chapter", revisionOf(t, p, id), map[string]any{"chapter": 0})
	errInfo, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("非法参数应回错误对象: %v", out)
	}
	// chapter=0 是参数问题，要归到 INVALID_REQUEST，别混进笼统的执行失败。
	if errInfo["code"] != "INVALID_REQUEST" {
		t.Fatalf("错误码 = %v", errInfo["code"])
	}
	if out["revision"] == nil {
		t.Fatal("失败结果缺少 revision")
	}
	if strings.Contains(errInfo["message"].(string), p.root) {
		t.Fatalf("错误文本含数据根路径: %v", errInfo["message"])
	}
}

func TestExportOnlyServesCommittedChapters(t *testing.T) {
	p := newProjects(t)
	id := create(t, p, "exporting")
	if _, err := p.Call(context.Background(), id, "export_book", "", map[string]any{"from_chapter": 1, "to_chapter": 500}); err == nil {
		t.Fatal("超出章数上限应报错")
	}
	if _, err := p.Call(context.Background(), id, "export_book", "", map[string]any{"from_chapter": 1, "to_chapter": 2}); err == nil || !strings.Contains(err.Error(), "无已完成章节") {
		t.Fatalf("未提交章节不该能导出，实得 %v", err)
	}
	// 草稿不算已提交：写进 drafts 不能从 export_book 流出去。
	if out := call(t, p, id, "draft_chapter", revisionOf(t, p, id), map[string]any{"chapter": 1, "content": "草稿不该被导出", "mode": "write"}); out["error"] != nil && out["error"] != "" {
		t.Logf("draft 在无规划时按上游约束被拒（正常）: %v", out["error"])
	}
	if _, err := p.Call(context.Background(), id, "export_book", "", map[string]any{"from_chapter": 1, "to_chapter": 1}); err == nil {
		t.Fatal("仅有草稿时 export 仍应拒绝")
	}
}

func TestCredentialsFileIsPrivateAndOpaque(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "credentials.json")
	creds, err := WriteCredentials(file, false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if os.PathSeparator != '\\' && info.Mode().Perm() != 0o600 {
		t.Fatalf("权限 = %o，应为 600", info.Mode().Perm())
	}
	if len(creds.Route) != 64 || len(creds.Bearer) != 64 || creds.Route == creds.Bearer {
		t.Fatalf("凭据形态不对: %+v", creds)
	}
	if again, err := WriteCredentials(file, false); err != nil || again.Route != creds.Route {
		t.Fatalf("重复写入应保留现有凭据: %v %+v", err, again)
	}
	if rotated, err := WriteCredentials(file, true); err != nil || rotated.Route == creds.Route {
		t.Fatalf("轮换必须换掉 route: %v", err)
	}
	for _, bad := range []string{"short", "zz" + strings.Repeat("0", 62), strings.Repeat("0", 64)} {
		body := `{"route":"` + bad + `","bearer":"` + bad + `"}`
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCredentials(file); bad == strings.Repeat("0", 64) && err == nil {
			// 全零形状合法但明显不是随机值：route 与 bearer 相同同样要拒。
			t.Error("route 与 bearer 相同必须拒载")
		} else if bad != strings.Repeat("0", 64) && err == nil {
			t.Errorf("弱凭据必须拒载: %q", bad)
		}
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(file, link); err == nil {
		if _, err := LoadCredentials(link); err == nil {
			t.Error("凭据文件必须是普通文件")
		}
	}
}

func TestPublicURLAcceptsOnlyOrigins(t *testing.T) {
	for _, ok := range []string{"https://abc.ngrok.app", "https://abc.ngrok.app/"} {
		u, err := ValidPublicURL(ok)
		if err != nil || u.Host != "abc.ngrok.app" {
			t.Errorf("%s 应通过: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://abc.ngrok.app", "https://abc.ngrok.app/mcp/leak", "https://user:pw@abc.ngrok.app", "https://abc.ngrok.app?x=1", "abc.ngrok.app", "https://abc.ngrok.app#f", "https://:443", "https://abc.ngrok.app:", "https://abc.ngrok.app:0", "https://abc.ngrok.app:65536"} {
		if _, err := ValidPublicURL(bad); err == nil {
			t.Errorf("%s 应被拒", bad)
		}
	}
}

func TestHTTPWildcardOriginAllowsAnyBrowserOrigin(t *testing.T) {
	p := newProjects(t)
	route := strings.Repeat("a", 64)
	bearer := strings.Repeat("b", 64)
	handler := NewHTTP(p, HTTPOptions{
		Credentials:   func() (Credentials, error) { return Credentials{Route: route, Bearer: bearer}, nil },
		Hosts:         []string{"novel.test"},
		Origins:       []string{"*"},
		RequireBearer: true,
	})
	req := httptest.NewRequest(http.MethodOptions, "http://novel.test/mcp/"+route, nil)
	req.Host = "novel.test"
	req.Header.Set("Origin", "https://any.example")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("wildcard origin status = %d, want 204", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://any.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

// 门禁逐条测：每一项都是"没配好就别接公网"的那道闸。
func TestHTTPGateRejectsEverythingButTheRoute(t *testing.T) {
	p := newProjects(t)
	route := strings.Repeat("a", 64)
	bearer := strings.Repeat("b", 64)
	handler := NewHTTP(p, HTTPOptions{
		Credentials:   func() (Credentials, error) { return Credentials{Route: route, Bearer: bearer}, nil },
		Hosts:         []string{"novel.test"},
		Origins:       []string{"https://arena.ai"},
		RequireBearer: true,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	transport := &staysLocal{addr: srv.Listener.Addr().String()}

	status := func(method, path string, hdr http.Header, reqHost string) int {
		t.Helper()
		body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"t","version":"1"},"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
		req, err := http.NewRequest(method, "http://novel.test"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", currentMCPProtocolVersion)
		for k, vs := range hdr {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
		if reqHost != "" {
			req.Host = reqHost
		}
		resp, err := transport.roundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	good := http.Header{"Authorization": []string{"Bearer " + bearer}}

	// Host 不是白名单值：既是 DNS 重绑定防线，也说明判定真的在拦。
	if got := status("POST", "/mcp/"+route, good, ""); got == http.StatusUnauthorized || got == http.StatusForbidden || got == http.StatusNotFound {
		t.Errorf("合法请求不应被安全门拒绝（实得 %d）", got)
	}
	if got := status("POST", "/mcp/"+route, good, srv.Listener.Addr().String()); got != http.StatusForbidden {
		t.Errorf("非白名单 Host = %d，应 403", got)
	}
	if got := status("POST", "/mcp/"+strings.Repeat("c", 64), good, ""); got != http.StatusNotFound {
		t.Errorf("错 route = %d，应 404", got)
	}
	if got := status("POST", "/mcp/"+route+"?x=1", good, ""); got != http.StatusNotFound {
		t.Errorf("带 query = %d，应 404", got)
	}
	if got := status("POST", "/mcp/"+route+"/", good, ""); got != http.StatusNotFound {
		t.Errorf("尾斜杠变体 = %d，应 404（不重定向、不回显）", got)
	}
	if got := status("POST", "/mcp/"+route, http.Header{}, ""); got != http.StatusUnauthorized {
		t.Errorf("无 Bearer = %d，应 401", got)
	}
	if got := status("POST", "/mcp/"+route, http.Header{"Authorization": []string{"Bearer nope"}}, ""); got != http.StatusUnauthorized {
		t.Errorf("错 Bearer = %d，应 401", got)
	}
	if got := status("POST", "/mcp/"+route, http.Header{"Origin": []string{"https://evil.example"}, "Authorization": []string{"Bearer " + bearer}}, ""); got != http.StatusForbidden {
		t.Errorf("未列入白名单的 Origin = %d，应 403", got)
	}
	if got := status("GET", "/healthz", http.Header{}, ""); got != http.StatusOK {
		t.Errorf("healthz = %d，应 200", got)
	}
	if got := status("POST", "/console/", good, "evil.example"); got != http.StatusForbidden {
		t.Errorf("其他路径 = %d，应 403（Host 先判）", got)
	}
	// 关掉 bearer 时，URL 本身就是唯一凭据：路径正确就该通过。
	noAuth := NewHTTP(p, HTTPOptions{
		Credentials: func() (Credentials, error) { return Credentials{Route: route, Bearer: bearer}, nil },
		Hosts:       []string{"novel.test"},
	})
	srvNoAuth := httptest.NewServer(noAuth)
	defer srvNoAuth.Close()
	req, _ := http.NewRequest("POST", "http://novel.test/mcp/"+route, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"t","version":"1"},"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", currentMCPProtocolVersion)
	resp, err := (&staysLocal{addr: srvNoAuth.Listener.Addr().String()}).roundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		t.Errorf("public-open 模式下合法 URL 不应被安全门拒绝，实得 %d", resp.StatusCode)
	}
}

// 只走本机端口，但保留请求里写好的 Host 头：用来验证准入判定而不是网络可达性。
type staysLocal struct{ addr string }

func (s *staysLocal) roundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Host = s.addr
	return http.DefaultTransport.RoundTrip(r)
}

// 真实协议接入：用官方 Go SDK 客户端打完整 HTTP 栈，确认工具形状与一次完整写入。
// 这是本仓库最重要的一条测试——单元测试过不代表网页客户端能用。
func TestWireProtocolWithOfficialClient(t *testing.T) {
	p := newProjects(t)
	route := strings.Repeat("d", 64)
	// 白名单要写 httptest 的实际地址，所以先起服务再装 handler。
	var inner http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { inner.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	inner = NewHTTP(p, HTTPOptions{
		Credentials: func() (Credentials, error) {
			return Credentials{Route: route, Bearer: strings.Repeat("e", 64)}, nil
		},
		Hosts: []string{srv.Listener.Addr().String()},
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "novel-mcp-test", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp/" + route, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	defer func() { _ = cs.Close() }()
	if got := cs.InitializeResult().ProtocolVersion; got != currentMCPProtocolVersion {
		t.Fatalf("protocol=%s, want current %s", got, currentMCPProtocolVersion)
	}

	if got := cs.InitializeResult().Instructions; !strings.Contains(got, "revision") || !strings.Contains(got, "check_consistency") {
		t.Errorf("连接期说明未讲清协议: %.120s", got)
	}
	caps := cs.InitializeResult().Capabilities
	if caps == nil || caps.Tools == nil || caps.Prompts == nil || caps.Resources == nil || caps.Completions == nil {
		t.Fatalf("MCP capabilities 不完整: %#v", caps)
	}
	if caps.Tools.ListChanged || caps.Prompts.ListChanged || caps.Resources.ListChanged || caps.Resources.Subscribe {
		t.Fatalf("静态 MCP surface 不应声明 listChanged/subscribe: %#v", caps)
	}
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		byName[tool.Name] = tool
	}
	for _, want := range []string{"create_project", "list_projects", "project_status", "verify_project", "novel_guide", "export_book", "next_step", "delete_project",
		"novel_context", "save_book", "save_foundation", "audit_foundation", "plan_chapter", "draft_chapter",
		"read_chapter", "check_consistency", "commit_chapter", "revise_outline", "expand_next_arc", "save_review", "reopen_book"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("缺少工具 %s", want)
		}
	}
	if len(byName) != 25 {
		t.Errorf("工具数 = %d，应为 25（16 个上游 + 9 个本项目）", len(byName))
	}
	for name, tool := range byName {
		if tool.OutputSchema == nil {
			t.Errorf("%s 缺 outputSchema", name)
		}
		if tool.Title == "" || tool.Annotations == nil || tool.Annotations.Title == "" {
			t.Errorf("%s 缺 title/annotations: %+v", name, tool)
		}
		if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Errorf("%s 应声明 closed-world: %+v", name, tool.Annotations)
		}
	}
	for name, fields := range map[string][]string{
		"save_book":      {"foundation_ready", "remaining"},
		"read_chapter":   {"content", "samples", "word_count"},
		"commit_chapter": {"review_required", "book_complete", "rule_violations"},
		"save_review":    {"affected_chapters", "next_flow", "next_chapter"},
	} {
		raw, err := json.Marshal(byName[name].OutputSchema)
		if err != nil {
			t.Fatalf("%s outputSchema 无法序列化: %v", name, err)
		}
		for _, field := range fields {
			if !strings.Contains(string(raw), `"`+field+`"`) {
				t.Errorf("%s outputSchema 仍过宽，缺稳定字段 %s: %s", name, field, raw)
			}
		}
	}
	status := byName["project_status"]
	if status.Annotations == nil || !status.Annotations.ReadOnlyHint {
		t.Error("project_status 应标注只读")
	}
	if del := byName["delete_project"].Annotations; del.DestructiveHint == nil || !*del.DestructiveHint {
		t.Errorf("delete_project 应标注 destructive: %+v", del)
	}
	if create := byName["create_project"].Annotations; create.DestructiveHint == nil || *create.DestructiveHint {
		t.Errorf("create_project 是加法操作，不应标注 destructive: %+v", create)
	}
	readChapter := byName["read_chapter"]
	if readChapter.Annotations == nil || !readChapter.Annotations.ReadOnlyHint {
		t.Errorf("read_chapter 应标注只读: %+v", readChapter.Annotations)
	}
	if readChapter.Annotations == nil || !readChapter.Annotations.IdempotentHint {
		t.Errorf("read_chapter 是纯读，应标注幂等: %+v", readChapter.Annotations)
	}
	readSchema, _ := json.Marshal(readChapter.InputSchema)
	if strings.Contains(string(readSchema), "expected_revision") {
		t.Errorf("read_chapter 是纯读，schema 不应出现 expected_revision: %s", readSchema)
	}
	checkConsistency := byName["check_consistency"]
	if checkConsistency.Annotations == nil || !checkConsistency.Annotations.ReadOnlyHint || !checkConsistency.Annotations.IdempotentHint {
		t.Errorf("check_consistency 应为纯读幂等工具: %+v", checkConsistency.Annotations)
	}
	checkSchema, _ := json.Marshal(checkConsistency.InputSchema)
	if strings.Contains(string(checkSchema), "expected_revision") {
		t.Errorf("check_consistency 是纯读，schema 不应出现 expected_revision: %s", checkSchema)
	}
	prompts, err := cs.ListPrompts(context.Background(), nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	wantPrompts := map[string]bool{
		"novel_overview": false, "novel_architect_short": false, "novel_architect_long": false,
		"novel_writer": false, "novel_editor": false,
	}
	for _, prompt := range prompts.Prompts {
		if _, ok := wantPrompts[prompt.Name]; ok {
			wantPrompts[prompt.Name] = true
		}
	}
	for name, seen := range wantPrompts {
		if !seen {
			t.Errorf("缺少 MCP prompt %s", name)
		}
	}
	writerPrompt, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "novel_writer"})
	if err != nil {
		t.Fatalf("get novel_writer prompt: %v", err)
	}
	if len(writerPrompt.Messages) != 1 {
		t.Fatalf("novel_writer messages=%d, want 1", len(writerPrompt.Messages))
	}
	text, ok := writerPrompt.Messages[0].Content.(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, "commit_chapter") {
		t.Fatalf("novel_writer prompt 内容不完整: %#v", writerPrompt.Messages[0].Content)
	}
	for _, name := range []string{"save_book", "draft_chapter"} {
		schema, _ := json.Marshal(byName[name].InputSchema)
		if !strings.Contains(string(schema), "expected_revision") || !strings.Contains(string(schema), "\"project\"") {
			t.Errorf("%s 的 schema 未包含 project/expected_revision: %s", name, schema)
		}
		if _, err := json.Marshal(byName[name].InputSchema); err != nil {
			t.Errorf("%s schema 无法序列化: %v", name, err)
		}
	}
	// 上游参数必须原样出现在 schema 里：证明我们没有另立一套字段名。
	draft, _ := json.Marshal(byName["draft_chapter"].InputSchema)
	for _, key := range []string{"chapter", "content", "mode"} {
		if !strings.Contains(string(draft), `"`+key+`"`) {
			t.Errorf("draft_chapter schema 丢了上游字段 %s: %s", key, draft)
		}
	}

	// 通过真实客户端建项目，再断言它出现在磁盘上。
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_project",
		Arguments: map[string]any{"id": "wired", "brief": "网页客户端建的书"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("create_project 失败: %s", textOf(res))
	}
	var created struct {
		Revision string `json:"revision"`
		Project  struct {
			ID    string `json:"id"`
			Brief string `json:"brief"`
		} `json:"project"`
	}
	// StructuredContent 的 Go 类型由 SDK 决定（当前是 jsontext.Value）；客户端
	// 拿到的是 JSON。这里按 JSON 往返读，不把某一版的具体类型写死。
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("structuredContent 无法序列化: %v", err)
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("structuredContent 形状不对: %v %s", err, raw)
	}
	if created.Project.ID != "wired" || len(created.Revision) != 64 {
		t.Fatalf("返回字段不对: %s", raw)
	}
	if created.Project.Brief != "网页客户端建的书" {
		t.Fatalf("brief 未落盘: %+v", created.Project)
	}
	if _, err := os.Stat(filepath.Join(p.dir("wired"), "meta", "progress.json")); err != nil {
		t.Fatalf("项目工件未建: %v", err)
	}
	// read_chapter 是纯读，当前契约不接收 expected_revision。
	read, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "read_chapter", Arguments: map[string]any{
		"project": "wired", "chapter": 1, "source": "draft",
	}})
	if err != nil || read.IsError {
		t.Fatalf("read_chapter 纯读失败 err=%v result=%s", err, textOf(read))
	}
	// 未过审计就写正文：上游前置条件必须原样回给客户端，而不是被我们吞掉。
	blocked, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "plan_chapter", Arguments: map[string]any{
		"project": "wired", "expected_revision": created.Revision, "chapter": 1, "title": "第一章", "goal": "g", "conflict": "c", "hook": "h"}})
	if err != nil {
		t.Fatal(err)
	}
	if !blocked.IsError || !strings.Contains(textOf(blocked), "writing") {
		t.Fatalf("未进写作阶段的 plan_chapter 应被上游拦住: is_error=%v %s", blocked.IsError, textOf(blocked))
	}
	// 非法 JSON 数字与越界 id 都由 schema/参数校验拦住，不能打到上游 panic。
	for _, bad := range []map[string]any{{"id": "wired", "brief": "x"}, {"id": "大写", "brief": "x"}} {
		r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_project", Arguments: bad})
		if err != nil {
			t.Fatal(err)
		}
		// 重名与非法 ID 都是业务拒绝：isError 是真，但 code 必须可分支，
		// 不能笼统混成协议错误，否则网页客户端只能去猜文本。
		if bad["id"] == "wired" {
			if !r.IsError {
				t.Errorf("重名创建必须失败: %s", textOf(r))
			}
			if !strings.Contains(textOf(r), "PROJECT_EXISTS") {
				t.Errorf("重名创建应回 PROJECT_EXISTS: %s", textOf(r))
			}
		} else if !r.IsError {
			t.Errorf("非法 ID 必须被拒: %s", textOf(r))
		}
	}
}

func TestRejectsLegacyMCPProtocol(t *testing.T) {
	p := newProjects(t)
	route := strings.Repeat("9", 64)
	var inner http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { inner.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	inner = NewHTTP(p, HTTPOptions{
		Credentials: func() (Credentials, error) {
			return Credentials{Route: route, Bearer: strings.Repeat("8", 64)}, nil
		},
		Hosts: []string{srv.Listener.Addr().String()},
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "legacy-protocol-test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp/" + route, DisableStandaloneSSE: true, MaxRetries: -1,
	}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-06-18"})
	if err == nil {
		_ = cs.Close()
		t.Fatal("旧 MCP 协议必须被拒绝")
	}
}

func TestNativeResources(t *testing.T) {
	p := newProjects(t)
	if _, err := p.Create("resource-book", "资源测试", "default"); err != nil {
		t.Fatal(err)
	}
	b, err := p.load("resource-book")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.store.Drafts.SaveDraft(1, "资源层草稿正文"); err != nil {
		t.Fatal(err)
	}
	beforeRevision, err := revision(p.dir("resource-book"))
	if err != nil {
		t.Fatal(err)
	}

	route := strings.Repeat("2", 64)
	var inner http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { inner.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	inner = NewHTTP(p, HTTPOptions{
		Credentials: func() (Credentials, error) {
			return Credentials{Route: route, Bearer: strings.Repeat("3", 64)}, nil
		},
		Hosts: []string{srv.Listener.Addr().String()},
	})
	client := mcp.NewClient(&mcp.Implementation{Name: "novel-mcp-resource-test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp/" + route, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()

	resources, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Resources) != 1 || resources.Resources[0].URI != "novel://projects" {
		t.Fatalf("resources=%#v", resources.Resources)
	}
	templates, err := cs.ListResourceTemplates(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantTemplates := map[string]bool{
		"project_status": false, "project_context": false, "chapter_context": false, "chapter_text": false,
	}
	for _, tmpl := range templates.ResourceTemplates {
		if _, ok := wantTemplates[tmpl.Name]; ok {
			wantTemplates[tmpl.Name] = true
		}
	}
	for name, seen := range wantTemplates {
		if !seen {
			t.Errorf("缺少 resource template %s: %#v", name, templates.ResourceTemplates)
		}
	}

	projects, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "novel://projects"})
	if err != nil || len(projects.Contents) != 1 || !strings.Contains(projects.Contents[0].Text, "resource-book") {
		t.Fatalf("read projects: err=%v result=%#v", err, projects)
	}
	status, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "novel://project/resource-book/status"})
	if err != nil || len(status.Contents) != 1 || !strings.Contains(status.Contents[0].Text, "foundation_missing") {
		t.Fatalf("read status: err=%v result=%#v", err, status)
	}
	for _, uri := range []string{
		"novel://project/resource-book/context",
		"novel://project/resource-book/context/1",
	} {
		contextView, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err != nil || len(contextView.Contents) != 1 || !strings.Contains(contextView.Contents[0].Text, "resource-book") {
			t.Fatalf("read context %s: err=%v result=%#v", uri, err, contextView)
		}
	}
	draftURI := resourceURI("resource-book", 1, "draft")
	draft, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: draftURI})
	if err != nil || len(draft.Contents) != 1 || draft.Contents[0].Text != "资源层草稿正文" {
		t.Fatalf("read draft: err=%v result=%#v", err, draft)
	}
	afterRevision, err := revision(p.dir("resource-book"))
	if err != nil {
		t.Fatal(err)
	}
	if afterRevision != beforeRevision {
		t.Fatalf("读取 MCP Resource 不得写 checkpoint 或改变项目 revision: before=%s after=%s", beforeRevision, afterRevision)
	}
}

func TestNativeResourceCompletions(t *testing.T) {
	p := newProjects(t)
	for _, id := range []string{"alpha-book", "beta-book"} {
		if _, err := p.Create(id, "completion 测试", "default"); err != nil {
			t.Fatal(err)
		}
	}
	b, err := p.load("alpha-book")
	if err != nil {
		t.Fatal(err)
	}
	progress, err := b.store.Progress.Load()
	if err != nil {
		t.Fatal(err)
	}
	progress.TotalChapters = 12
	progress.CurrentChapter = 3
	progress.CompletedChapters = []int{1, 2, 10}
	if err := b.store.Progress.Save(progress); err != nil {
		t.Fatal(err)
	}
	beforeRevision, err := revision(p.dir("alpha-book"))
	if err != nil {
		t.Fatal(err)
	}

	route := strings.Repeat("4", 64)
	var inner http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { inner.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	inner = NewHTTP(p, HTTPOptions{
		Credentials: func() (Credentials, error) {
			return Credentials{Route: route, Bearer: strings.Repeat("5", 64)}, nil
		},
		Hosts: []string{srv.Listener.Addr().String()},
	})
	client := mcp.NewClient(&mcp.Implementation{Name: "novel-mcp-completion-test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp/" + route, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	if cs.InitializeResult().Capabilities.Completions == nil {
		t.Fatal("server 应声明 MCP completions capability")
	}

	complete := func(refURI, name, value string, args map[string]string) *mcp.CompleteResult {
		t.Helper()
		var completeContext *mcp.CompleteContext
		if args != nil {
			completeContext = &mcp.CompleteContext{Arguments: args}
		}
		res, err := cs.Complete(context.Background(), &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/resource", URI: refURI},
			Argument: mcp.CompleteParamsArgument{Name: name, Value: value},
			Context:  completeContext,
		})
		if err != nil {
			t.Fatalf("complete %s %s=%q: %v", refURI, name, value, err)
		}
		return res
	}

	projects := complete(resourceProjectStatusTemplate, "project", "a", nil)
	if got := projects.Completion.Values; len(got) != 1 || got[0] != "alpha-book" || projects.Completion.Total != 1 || projects.Completion.HasMore {
		t.Fatalf("project completion=%#v", projects.Completion)
	}
	sources := complete(resourceChapterTextTemplate, "source", "d", map[string]string{"project": "alpha-book", "chapter": "3"})
	if got := sources.Completion.Values; len(got) != 1 || got[0] != "draft" {
		t.Fatalf("source completion=%#v", sources.Completion)
	}
	chapters := complete(resourceChapterContextTemplate, "chapter", "1", map[string]string{"project": "alpha-book"})
	// TotalChapters=12 在长篇可能只是未展开骨架的内部估算；没有真实 outline
	// 时不能把 11/12 之类估算章节通过 completion 暴露出去。
	wantChapters := []string{"1", "10"}
	if got := chapters.Completion.Values; len(got) != len(wantChapters) {
		t.Fatalf("chapter completion=%#v want=%v", chapters.Completion, wantChapters)
	} else {
		for i := range wantChapters {
			if got[i] != wantChapters[i] {
				t.Fatalf("chapter completion=%#v want=%v", chapters.Completion, wantChapters)
			}
		}
	}
	finals := complete(resourceChapterTextTemplate, "chapter", "1", map[string]string{"project": "alpha-book", "source": "final"})
	wantFinals := []string{"1", "10"}
	if got := finals.Completion.Values; len(got) != len(wantFinals) || got[0] != "1" || got[1] != "10" {
		t.Fatalf("final chapter completion=%#v want=%v", finals.Completion, wantFinals)
	}
	missingContext := complete(resourceChapterContextTemplate, "chapter", "", nil)
	if len(missingContext.Completion.Values) != 0 {
		t.Fatalf("无 project context 不应猜章节: %#v", missingContext.Completion)
	}
	unknown := complete("novel://unknown/{project}", "project", "", nil)
	if len(unknown.Completion.Values) != 0 {
		t.Fatalf("未知 resource template 应返回空补全: %#v", unknown.Completion)
	}

	afterRevision, err := revision(p.dir("alpha-book"))
	if err != nil {
		t.Fatal(err)
	}
	if afterRevision != beforeRevision {
		t.Fatalf("MCP completion 必须纯读: before=%s after=%s", beforeRevision, afterRevision)
	}
}

func textOf(r *mcp.CallToolResult) string {
	var parts []string
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, " ")
}
