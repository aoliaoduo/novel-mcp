package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 这一条是本仓库唯一"真进程"测试：编出二进制、起真服务、用官方 SDK 客户端走真 HTTP，
// 一路开到写完一章并提交，然后杀掉进程再从磁盘恢复。
//
// 为什么值得留在仓库里：上面的 server_test/flow_test 都在进程内直调 handler，
// 它们证明不了"网页客户端能连上"——门禁、Host 白名单、stateless 配置、协议握手
// 出错都会在进程内测试里被绕过。默认跳过，因为要花几秒编二进制并占用本地端口。
func TestEndToEndOverRealProcess(t *testing.T) {
	if os.Getenv("NOVEL_MCP_E2E") != "1" {
		t.Skip("设置 NOVEL_MCP_E2E=1 才跑真进程演练")
	}
	bin := buildBinary(t)

	// 第一段：URL 即凭据（回环 + Bearer 关）——对应"网页客户端只贴一条 URL"的形态。
	dataDir := filepath.Join(t.TempDir(), "data")
	base, route, stop := startServer(t, bin, dataDir, false, nil)
	defer stop()

	if body, code := rawGet(t, base+"/healthz", nil); code != 200 || !strings.Contains(body, "\"ok\":true") {
		t.Fatalf("healthz: %d %s", code, body)
	}
	// 只有 /mcp/<route> 与 /healthz 存在：其余一律 404，且不给重定向。
	if _, code := rawGet(t, base+"/", nil); code != 404 {
		t.Fatalf("根路径应 404，实得 %d", code)
	}
	if _, code := rawGet(t, base+"/mcp/"+strings.Repeat("a", 64), nil); code != 404 {
		t.Fatalf("错误路由应 404，实得 %d", code)
	}
	if _, code := rawGet(t, base+"/mcp/"+route, map[string]string{"Host": "attacker.example"}); code != 403 {
		t.Fatalf("白名单外的 Host 应 403，实得 %d", code)
	}
	// 浏览器跨源：没进 allow_origins 的 Origin 必须被挡。
	if _, code := rawGet(t, base+"/mcp/"+route, map[string]string{"Origin": "https://evil.example"}); code != 403 {
		t.Fatalf("白名单外的 Origin 应 403，实得 %d", code)
	}

	session := connect(t, base, route, nil, "")
	defer session.Close()

	// 25 个工具：16 个上游原语 + 9 个工程工具，一个不多一个不少。
	tools := listTools(t, session)
	want := []string{"audit_foundation", "check_consistency", "commit_chapter", "create_project", "delete_project", "draft_chapter",
		"edit_chapter", "expand_next_arc", "export_book", "list_projects", "next_step", "novel_context", "novel_guide",
		"plan_chapter", "project_status", "read_chapter", "reopen_book", "resolve_outline_feedback", "revise_outline", "verify_project",
		"save_arc_summary", "save_book", "save_foundation", "save_review", "save_volume_summary"}
	for _, name := range want {
		if !tools[name] {
			t.Fatalf("缺少工具 %s，实得 %v", name, keys(tools))
		}
	}
	if len(tools) != len(want) {
		t.Fatalf("工具数不对: %d != %d (%v)", len(tools), len(want), keys(tools))
	}

	// 客户端从未调过 novel_guide 也该能直接开书：工具描述与 schema 自带约束。
	out := mustCall(t, session, "list_projects", map[string]any{})
	if items, ok := out["projects"].([]any); !ok || len(items) != 0 {
		t.Fatalf("空数据根应列出 0 个项目: %v", out)
	}
	created := mustCall(t, session, "create_project", map[string]any{
		"id": "e2e-book", "brief": "短篇：守塔人发现潮汐倒退。", "style": "default"})
	rev := stringOf(created["revision"])
	if len(rev) != 64 {
		t.Fatalf("创建未回 revision: %v", created)
	}
	next := mustCall(t, session, "next_step", map[string]any{"project": "e2e-book"})
	nextResult, _ := next["result"].(map[string]any)
	nextActions, _ := nextResult["actions"].([]any)
	if len(nextActions) < 2 {
		t.Fatalf("next_step 缺机器可执行 actions: %+v", nextResult)
	}
	first, _ := nextActions[0].(map[string]any)
	second, _ := nextActions[1].(map[string]any)
	if first["id"] != "a1" || first["revision_source"] != "none" || second["id"] != "a2" || second["revision_source"] != "plan" {
		t.Fatalf("next_step action revision 链不正确: first=%+v second=%+v", first, second)
	}
	secondArgs, _ := second["arguments"].(map[string]any)
	if secondArgs["project"] != "e2e-book" || secondArgs["expected_revision"] != rev || second["expected_revision_source"] != "next_step.revision" {
		t.Fatalf("next_step 没把已知 project/revision 绑定进首个写 action: %+v", second)
	}
	if nextResult["context_resource_uri"] != "novel://project/e2e-book/context" {
		t.Fatalf("next_step context resource 不正确: %+v", nextResult)
	}
	// 重名创建：业务拒绝，不是覆盖。
	if bad := call(t, session, "create_project", map[string]any{"id": "e2e-book", "brief": "x"}); !bad.IsError {
		t.Fatal("重名创建必须失败")
	}

	book := call(t, session, "save_book", map[string]any{
		"project": "e2e-book", "expected_revision": rev, "title": "潮汐编年史", "synopsis": "守塔人发现潮汐开始倒退。"})
	if book.IsError {
		t.Fatalf("save_book 失败: %s", textOf(book))
	}
	rev = revisionOf(t, book)
	// 盲目重放：还是用刚才那个 revision 再写一次，必须被 revision 冲突拦住。
	replay := call(t, session, "save_book", map[string]any{
		"project": "e2e-book", "expected_revision": created["revision"], "title": "重复写入", "synopsis": "x"})
	if !replay.IsError || !strings.Contains(textOf(replay), "REVISION_CONFLICT") {
		t.Fatalf("过期 revision 的重放必须回 REVISION_CONFLICT: %s", textOf(replay))
	}

	// 每个元素写成自成一行的 map：跨行数括号是 bug 之源，这里刻意不省行。
	// 大纲留三章：只写一章的书在上游会被判为结构完整而直接完结，
	// 那样"崩溃重启后继续写"的用例就被完结态吞掉了。
	outline := []any{
		map[string]any{"chapter": 1, "title": "倒流", "core_event": "守塔人记录到第一次倒潮", "hook": "灯塔下出现一行湿脚印", "scenes": []any{"塔顶", "滩涂"}},
		map[string]any{"chapter": 2, "title": "第二夜", "core_event": "沿着脚印走到被淹没的城入口", "hook": "塔门被从外面敲响", "scenes": []any{"滩涂", "城门"}},
		map[string]any{"chapter": 3, "title": "回声", "core_event": "城里的回声说出守塔人的名字", "hook": "潮水退回原处", "scenes": []any{"城中", "塔顶"}},
	}
	characters := []any{
		map[string]any{"name": "林渡", "role": "protagonist", "description": "守塔人，沉默，记性极好", "arc": "从尽职到追查真相", "traits": []any{"克制", "多疑"}},
	}
	rules := []any{
		map[string]any{"category": "magic", "rule": "只有守塔人能听见潮汐声", "boundary": "离开灯塔三十步即失效"},
	}
	foundations := []map[string]any{
		{"type": "premise", "scale": "short", "content": "# 前提\n守塔人能听见海水倒流的声音，由此追查一座被淹没的城。"},
		{"type": "outline", "content": outline},
		{"type": "characters", "content": characters},
		{"type": "world_rules", "content": rules},
	}
	for _, foundation := range foundations {
		args := map[string]any{"project": "e2e-book", "expected_revision": rev}
		for k, v := range foundation {
			args[k] = v
		}
		res := call(t, session, "save_foundation", args)
		if res.IsError {
			t.Fatalf("save_foundation(%v) 失败: %s", foundation["type"], textOf(res))
		}
		rev = revisionOf(t, res)
	}

	context := mustCall(t, session, "novel_context", map[string]any{"project": "e2e-book"})
	fingerprint := stringOf(findValue(context, "fingerprint"))
	if len(fingerprint) < 32 {
		t.Fatalf("novel_context 未给出 fingerprint: %v", context)
	}
	// 审计带过期 fingerprint：上游会拒，而且必须是冲突类而不是笼统失败。
	staleAudit := call(t, session, "audit_foundation", map[string]any{
		"project": "e2e-book", "expected_revision": rev, "fingerprint": strings.Repeat("0", 64),
		"ready": true, "summary": "x", "issues": []any{}})
	if !staleAudit.IsError || !strings.Contains(textOf(staleAudit), "CONFLICT") {
		t.Fatalf("过期 fingerprint 应被拒为 CONFLICT: %s", textOf(staleAudit))
	}
	audit := call(t, session, "audit_foundation", map[string]any{
		"project": "e2e-book", "expected_revision": rev, "fingerprint": fingerprint,
		"ready": true, "summary": "设定自洽", "issues": []any{}})
	if audit.IsError {
		t.Fatalf("审计失败: %s", textOf(audit))
	}
	rev = revisionOf(t, audit)

	plan := call(t, session, "plan_chapter", map[string]any{
		"project": "e2e-book", "expected_revision": rev, "chapter": 1, "title": "倒流",
		"goal": "让第一次倒潮可感", "conflict": "记录还是追查", "hook": "滩涂上的湿脚印"})
	if plan.IsError {
		t.Fatalf("plan_chapter 失败: %s", textOf(plan))
	}
	rev = revisionOf(t, plan)
	body := "林渡把耳朵贴在灯室的铜栏杆上。\n\n海水正在倒着走。\n\n他数到第三十七下，声音停了。"
	draft := call(t, session, "draft_chapter", map[string]any{
		"project": "e2e-book", "expected_revision": rev, "chapter": 1, "content": body, "mode": "write"})
	if draft.IsError {
		t.Fatalf("draft_chapter 失败: %s", textOf(draft))
	}
	draftRaw, _ := json.Marshal(draft.StructuredContent)
	var draftEnvelope map[string]any
	if err := json.Unmarshal(draftRaw, &draftEnvelope); err != nil {
		t.Fatal(err)
	}
	if result, _ := draftEnvelope["result"].(map[string]any); result["resource_uri"] != "novel://project/e2e-book/chapter/1/draft" {
		t.Fatalf("draft resource_uri 不正确: %+v", result)
	}
	rev = revisionOf(t, draft)
	// 正文一改就要回读：append 后旧 revision 已失效，读回来的应是合起来的全文。
	appendRes := call(t, session, "draft_chapter", map[string]any{
		"project": "e2e-book", "expected_revision": rev, "chapter": 1, "content": "\n\n他没有回头。", "mode": "append"})
	if appendRes.IsError {
		t.Fatalf("append 失败: %s", textOf(appendRes))
	}
	rev = revisionOf(t, appendRes)
	// read_chapter 是纯读，当前契约不接受 expected_revision。
	readRes := call(t, session, "read_chapter", map[string]any{
		"project": "e2e-book", "chapter": 1, "source": "draft"})
	if readRes.IsError {
		t.Fatalf("read_chapter 失败: %s", textOf(readRes))
	}
	rev = revisionOf(t, readRes)
	raw, err := json.Marshal(readRes.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var read map[string]any
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(collectStrings(read), " ")
	if !strings.Contains(text, "湿脚印") && !strings.Contains(text, "他没有回头") {
		t.Fatalf("回读未拿到正文: %v", read)
	}
	check := call(t, session, "check_consistency", map[string]any{
		"project": "e2e-book", "chapter": 1})
	if check.IsError {
		t.Fatalf("check_consistency 失败: %s", textOf(check))
	}
	checkRev := revisionOf(t, check)
	if checkRev != rev {
		t.Fatalf("check_consistency 是纯读，不应改变 revision: before=%s after=%s", rev, checkRev)
	}
	// commit_chapter 的载荷就是上游的事实层契约：schema 要求这些字段全部给出，
	// 网页客户端的 SDK 会在发送前校验，所以这里必须按线上形状传。
	commit := call(t, session, "commit_chapter", map[string]any{
		"project": "e2e-book", "expected_revision": rev, "chapter": 1,
		"title":                "倒流",
		"summary":              "守塔人记录第一次倒潮，并在滩涂发现一行湿脚印",
		"characters":           []any{"林渡"},
		"key_events":           []any{"倒潮被记录", "湿脚印出现"},
		"timeline_events":      []any{map[string]any{"time": "第一夜", "event": "倒潮被记录", "characters": []any{"林渡"}}},
		"foreshadow_updates":   []any{map[string]any{"id": "F1", "action": "plant", "description": "湿脚印通向海里"}},
		"relationship_changes": []any{},
		"state_changes":        []any{map[string]any{"entity": "林渡", "field": "认知", "old_value": nil, "new_value": "确信海水在倒流", "reason": "亲耳记录"}},
		"cast_intros":          []any{},
		"hook_type":            "mystery",
		"dominant_strand":      "quest",
		"feedback":             nil,
	})
	if commit.IsError {
		t.Fatalf("commit_chapter 失败: %s", textOf(commit))
	}
	commitRawForURI, _ := json.Marshal(commit.StructuredContent)
	var commitEnvelopeForURI map[string]any
	if err := json.Unmarshal(commitRawForURI, &commitEnvelopeForURI); err != nil {
		t.Fatal(err)
	}
	if result, _ := commitEnvelopeForURI["result"].(map[string]any); result["resource_uri"] != "novel://project/e2e-book/chapter/1/final" {
		t.Fatalf("commit resource_uri 不正确: %+v", result)
	}
	rev = revisionOf(t, commit)
	commitRaw, err := json.Marshal(commit.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(commitRaw), "F1") && !strings.Contains(string(commitRaw), "next_chapter") {
		t.Fatalf("提交未回结构化事实: %s", commitRaw)
	}

	// 第二段：直接杀进程（模拟崩溃/网页会话断开），重启后从磁盘恢复事实。
	stop()
	base2, route2, stop2 := startServer(t, bin, dataDir, false, nil)
	defer stop2()
	// 端口是本次新选的，但路径令牌存在磁盘上：重启不该让已发出的 URL 失效。
	if route2 != route {
		t.Fatalf("重启后路由改变了，旧 URL 会失效: %q != %q", route2, route)
	}
	session2 := connect(t, base2, route2, nil, "")
	defer session2.Close()

	status := mustCall(t, session2, "project_status", map[string]any{"project": "e2e-book"})
	if got := stringOf(status["revision"]); got != rev {
		t.Fatalf("恢复后的 revision 应与崩溃前一致: %s != %s\n%v", got, rev, status)
	}
	if phase := stringOf(findValue(status, "phase")); phase != "writing" {
		t.Fatalf("恢复后应停在 writing 阶段，实得 %v", phase)
	}
	if pending := findValue(status, "pending_commit"); pending != nil {
		t.Fatalf("正常提交后不该留下未完成提交: %v", pending)
	}
	exported := mustCall(t, session2, "export_book", map[string]any{
		"project": "e2e-book", "from_chapter": 1, "to_chapter": 1})
	// 导出的是终稿正文：append 之后的内容必须都在里面（脚印只在大纲和提交摘要里，
	// 不在正文里，所以这里按正文原句断言）。
	if got := strings.Join(collectStrings(exported), " "); !strings.Contains(got, "海水正在倒着走") || !strings.Contains(got, "他没有回头") {
		t.Fatalf("导出未包含完整已提交正文: %s", got)
	}
	// 崩溃重启后继续写第二章：恢复的是事实，不是会话。
	plan2 := call(t, session2, "plan_chapter", map[string]any{
		"project": "e2e-book", "expected_revision": rev, "chapter": 2, "title": "第二夜",
		"goal": "追查脚印", "conflict": "留下还是离塔", "hook": "塔门被从外面敲响"})
	if plan2.IsError {
		t.Fatalf("恢复后无法继续写作: %s", textOf(plan2))
	}
}

// 第三段：要求 Bearer 的部署。网页客户端若不能带 header，这里就会明确失败，
// 而不是悄悄把所有调用都当匿名放行。
func TestEndToEndBearerRequired(t *testing.T) {
	if os.Getenv("NOVEL_MCP_E2E") != "1" {
		t.Skip("设置 NOVEL_MCP_E2E=1 才跑真进程演练")
	}
	bin := buildBinary(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	base, route, stop := startServer(t, bin, dataDir, true, nil)
	defer stop()
	creds := readCredentials(t, dataDir)

	if _, code := rawGet(t, base+"/mcp/"+route, nil); code != 401 {
		t.Fatalf("缺 Bearer 应 401，实得 %d", code)
	}
	if _, code := rawGet(t, base+"/mcp/"+route, map[string]string{"Authorization": "Bearer " + strings.Repeat("0", 64)}); code != 401 {
		t.Fatalf("错误 Bearer 应 401，实得 %d", code)
	}
	session := connect(t, base, route, nil, creds.Bearer)
	defer session.Close()
	if out := mustCall(t, session, "list_projects", map[string]any{}); out["projects"] == nil {
		t.Fatalf("带正确 Bearer 应能调工具: %v", out)
	}
}

// 第二套客户端实现：用官方 TypeScript MCP SDK 连真二进制，防止 Go SDK 的
// client/server 组合因为共享实现细节而把协议实现问题一起掩盖。默认不跑，
// 因为它需要 Node 以及 .local/mcp-ts-sdk 下的可选测试依赖。
func TestTypeScriptSDKCurrentProtocol(t *testing.T) {
	if os.Getenv("NOVEL_MCP_TS_E2E") != "1" {
		t.Skip("设置 NOVEL_MCP_TS_E2E=1 才跑 TypeScript MCP SDK 当前协议黑盒测试")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("NOVEL_MCP_TS_E2E=1 需要 Node.js: ", err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "scripts", "mcp-ts-smoke.mjs")
	sdkEntry := filepath.Join(root, ".local", "mcp-ts-sdk", "node_modules", "@modelcontextprotocol", "client", "dist", "index.mjs")
	if _, err := os.Stat(sdkEntry); err != nil {
		t.Fatalf("TypeScript MCP SDK 测试依赖未安装；执行 npm install --prefix .local/mcp-ts-sdk --save-exact @modelcontextprotocol/client@2.0.0 @modelcontextprotocol/core@2.0.0: %v", err)
	}

	bin := buildBinary(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	base, route, stop := startServer(t, bin, dataDir, true, nil)
	defer stop()
	creds := readCredentials(t, dataDir)

	cmd := exec.Command(node, script,
		"--url", base+"/mcp/"+route,
		"--bearer", creds.Bearer,
		"--project", "ts-sdk-e2e",
	)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("TypeScript MCP SDK 当前协议黑盒测试失败: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"ok":true`) || !strings.Contains(string(out), `"tools":25`) {
		t.Fatalf("TypeScript MCP SDK 返回结果不完整: %s", out)
	}
}

// buildBinary 编出真二进制：测试要验的是"用户拿到的那件东西"，不是库调用。
func buildBinary(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "novel-mcp")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "./cmd/novel-mcp")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("编译二进制失败: %v\n%s", err, out)
	}
	return bin
}

type credentials struct {
	Route  string `json:"route"`
	Bearer string `json:"bearer"`
}

func readCredentials(t *testing.T, dataDir string) credentials {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dataDir, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// startServer 用真二进制起一个服务，返回基址、路由与停止函数。
func startServer(t *testing.T, bin, dataDir string, bearer bool, origins []string) (string, string, func()) {
	t.Helper()
	port := freePort(t)
	cfgPath := filepath.Join(filepath.Dir(bin), fmt.Sprintf("cfg-%d.json", port))
	cfg := map[string]any{"host": "127.0.0.1", "port": port, "data": dataDir, "require_bearer": bearer}
	if origins != nil {
		cfg["allow_origins"] = origins
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "serve", "--config", cfgPath)
	logFile, err := os.Create(cfgPath + ".log")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stop := func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = logFile.Close()
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(20 * time.Second)
	for {
		if body, code := rawGet(t, base+"/healthz", nil); code == 200 && strings.Contains(body, "\"ok\":true") {
			break
		}
		if time.Now().After(deadline) {
			logged, _ := os.ReadFile(cfgPath + ".log")
			stop()
			t.Fatalf("服务未在 20 秒内就绪；日志:\n%s", logged)
		}
		time.Sleep(150 * time.Millisecond)
	}
	// 路由不写死：从 `url` 子命令读，避免测试复制凭据生成的细节。
	out, err := exec.Command(bin, "url", "--config", cfgPath).Output()
	if err != nil {
		stop()
		t.Fatalf("读取接入地址失败: %v", err)
	}
	route := strings.TrimSpace(string(out))
	idx := strings.LastIndex(route, "/mcp/")
	if idx < 0 {
		stop()
		t.Fatalf("接入地址形状不对: %q", route)
	}
	route = route[idx+len("/mcp/"):]
	if len(route) != 64 {
		stop()
		t.Fatalf("路由形状不对: %q", route)
	}
	return base, route, stop
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func rawGet(t *testing.T, url string, headers map[string]string) (string, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return string(body), resp.StatusCode
}

// headerTransport 给 SDK 客户端补上 Bearer：网页客户端若走 header，就是这个形态。
type headerTransport struct {
	base   http.RoundTripper
	bearer string
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+h.bearer)
	return h.base.RoundTrip(r)
}

func connect(t *testing.T, base, route string, _ http.RoundTripper, bearer string) *mcp.ClientSession {
	// 第二个参数保留给自定义 transport，当前只有 Bearer 一种需要。
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "novel-mcp-e2e", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: base + "/mcp/" + route}
	if bearer != "" {
		transport.HTTPClient = &http.Client{Transport: headerTransport{base: http.DefaultTransport, bearer: bearer}, Timeout: 30 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	instructions := session.InitializeResult().Instructions
	if instructions == "" {
		t.Fatal("initialize 未带 instructions")
	}
	for _, want := range []string{"action.arguments", "required_inputs", "expected_revision_source"} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("initialize instructions 缺执行 action 的关键约束 %q: %s", want, instructions)
		}
	}
	return session
}

func listTools(t *testing.T, session *mcp.ClientSession) map[string]bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		if tool.InputSchema == nil {
			t.Fatalf("%s 缺 inputSchema", tool.Name)
		}
		out[tool.Name] = true
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func mustCall(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res := call(t, session, name, args)
	if res.IsError {
		t.Fatalf("%s 失败: %s", name, textOf(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v %s", name, err, raw)
	}
	return out
}

func revisionOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res.IsError {
		t.Fatalf("期望成功，实得: %s", textOf(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	rev := stringOf(out["revision"])
	if len(rev) != 64 {
		t.Fatalf("未拿到 revision: %s", raw)
	}
	return rev
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// findValue 在嵌套结果里找第一个该名的值：上游嵌套形状不是本仓库的契约。
func findValue(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		if got, ok := x[key]; ok {
			return got
		}
		for _, child := range x {
			if got := findValue(child, key); got != nil {
				return got
			}
		}
	case []any:
		for _, child := range x {
			if got := findValue(child, key); got != nil {
				return got
			}
		}
	}
	return nil
}

func collectStrings(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for _, child := range x {
			out = append(out, collectStrings(child)...)
		}
	case []any:
		for _, child := range x {
			out = append(out, collectStrings(child)...)
		}
	case string:
		out = append(out, x)
	}
	return out
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
