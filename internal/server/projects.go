// Package server 是 novel-mcp 的新增层：项目隔离、连接门禁和 MCP 适配。
// 小说业务逻辑一律来自 internal/tools 与 internal/store 的上游实现，这里不重写。
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"novel-mcp/assets"
	"novel-mcp/internal/errs"
	"novel-mcp/internal/exp"
	"novel-mcp/internal/store"
	"novel-mcp/internal/tools"
)

// 项目 ID 是唯一被允许的寻址方式：远端永远不能提交宿主路径。
var projectID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

// Windows 保留名即使符合上面的模式也不能当目录名，先挡在创建入口。
var reservedID = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])$`)

const (
	maxProjectBytes   int64 = 256 << 20 // MVP 单本上限，防止无界扫描拖死服务
	maxProjectFiles         = 20000
	maxExportBytes          = 2 << 20 // 单次 export_book 的正文上限
	maxExportChapters       = 50
	maxProjects             = 100
)

// Tool 只用到上游工具实际实现的方法子集：不引入 agentcore 的 Agent 生命周期。
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(context.Context, json.RawMessage) (json.RawMessage, error)
}

type ProjectInfo struct {
	ID        string `json:"id"`
	Brief     string `json:"brief"`
	Style     string `json:"style"`
	CreatedAt string `json:"created_at"`
}

type book struct {
	mu    sync.RWMutex // 一本内单写、多读；只有显式声明并发安全的纯读工具共享锁
	info  ProjectInfo
	store *store.Store
	tools map[string]Tool
}

type Projects struct {
	root  string
	mu    sync.Mutex
	books map[string]*book
}

func NewProjects(root string) (*Projects, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	// 运维选定的数据根只解析一次；此后远端调用无法改变它。
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	return &Projects{root: abs, books: make(map[string]*book)}, nil
}

// codedError 让网页客户端按类别分支，而不是去匹配中文错误文本。
type codedError struct {
	code string
	msg  string
}

func (e *codedError) Error() string { return e.msg }

func coded(code, msg string) error { return &codedError{code: code, msg: msg} }

// codeOf 把上游 sentinel 翻成稳定 code。分类只映射 errs 包已有的语义，
// 不改写上游的错误消息：客户端据此决定"先重读"还是"先补前置条件"。
func codeOf(err error) string {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code
	}
	switch {
	case errors.Is(err, errs.ErrToolConflict):
		return "CONFLICT"
	case errors.Is(err, errs.ErrToolPrecondition), errors.Is(err, errs.ErrPhaseTransition), errors.Is(err, errs.ErrFlowTransition):
		return "PRECONDITION_FAILED"
	case errors.Is(err, errs.ErrToolArgs):
		return "INVALID_REQUEST"
	case errors.Is(err, errs.ErrStoreRead), errors.Is(err, errs.ErrStoreWrite):
		return "STORE_ERROR"
	default:
		return "NOVEL_TOOL_FAILED"
	}
}

func validID(id string) bool {
	return projectID.MatchString(id) && !reservedID.MatchString(id)
}

// walk 一次遍历同时做两件事：路径安全门（类型、数量、体积配额）与内容指纹。
// hash=false 时只做安全门并返回空串——列项目这类元数据操作付不起全量读盘，
// 但"读任何文件之前先确认路径安全"这条不能省，否则一个指向项目外的
// project.json 符号链接就能把宿主文件带进结果。
//
// 指纹每次调用都要算一遍，所以调用方只算一次并复用；一本 500 章的书约几 MB，
// 成本可以接受。若将来书目录大到扫不动，正确做法是换成 mtime+size 快照加
// 显式失效，而不是取消写前置检查。
func walk(dir string, hash bool) (string, error) {
	h := sha256.New()
	var copyBuf []byte
	if hash {
		copyBuf = make([]byte, 32<<10)
	}
	var size int64
	var count int
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("项目目录含符号链接，已拒绝访问")
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("项目目录含非普通文件，已拒绝访问")
		}
		count++
		if count > maxProjectFiles {
			return fmt.Errorf("项目文件数超过 %d", maxProjectFiles)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		if size > maxProjectBytes {
			return fmt.Errorf("项目体积超过 %d MiB 上限", maxProjectBytes>>20)
		}
		if !hash {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		fileHash := sha256.New()
		for {
			n, readErr := f.Read(copyBuf)
			if n > 0 {
				_, _ = fileHash.Write(copyBuf[:n])
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				_ = f.Close()
				return readErr
			}
		}
		closeErr := f.Close()
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(h, "%s\x00%x\n", filepath.ToSlash(rel), fileHash.Sum(nil))
		return nil
	})
	if err != nil {
		return "", err
	}
	if !hash {
		return "", nil
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// scan 只做安全门：确认目录里没有符号链接、设备文件，且没超配额。
func scan(dir string) error {
	_, err := walk(dir, false)
	return err
}

// revision 是内容指纹，用于乐观锁：任何一次写入都会改变它。
func revision(dir string) (string, error) {
	return walk(dir, true)
}

// jsonShape 让进程内返回的形状与线上一致：直接把结构体塞进 map 的话，
// 调用方看到的字段路径和网页客户端 JSON 解析出来的并不一样。
func jsonShape(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CoreTools 是暴露给网页客户端的原语集合：只保存事实、只校验前置条件，
// 不做模型调用，也不注入创作指令。ContextTool 与 CommitChapterTool 必须共享
// 同一份 StyleStatsIndex，否则重写章节后上下文读到旧统计（上游同款约束）。
func CoreTools(s *store.Store, style string) []Tool {
	stats := tools.NewStyleStatsIndex(s)
	cast := tools.NewCastIndex(s)
	// 只使用随二进制内嵌的创作资产，不读取宿主侧覆盖文件。
	bundle := assets.Load(style)
	return []Tool{
		tools.NewContextTool(s, bundle.References, style, stats, cast),
		tools.NewReadChapterTool(s),
		tools.NewSaveBookTool(s),
		tools.NewSaveFoundationTool(s),
		tools.NewAuditFoundationTool(s),
		tools.NewPlanChapterTool(s),
		tools.NewDraftChapterTool(s),
		tools.NewEditChapterTool(s),
		tools.NewCheckConsistencyTool(s),
		tools.NewCommitChapterTool(s, stats, cast),
		tools.NewReviseOutlineTool(s),
		tools.NewResolveOutlineFeedbackTool(s),
		tools.NewExpandNextArcTool(s),
		tools.NewSaveReviewTool(s),
		tools.NewSaveArcSummaryTool(s),
		tools.NewSaveVolumeSummaryTool(s),
		NewReopenBookTool(s),
	}
}

func (p *Projects) dir(id string) string { return filepath.Join(p.root, id) }

func (p *Projects) checkedDir(id string) (string, error) {
	if !validID(id) {
		return "", coded("INVALID_REQUEST", "项目 ID 非法：1-48 位小写字母、数字或连字符，且以字母开头")
	}
	dir := p.dir(id)
	info, err := os.Lstat(dir)
	if err != nil {
		return "", coded("PROJECT_NOT_FOUND", "项目不存在")
	}
	if !info.IsDir() {
		return "", coded("PROJECT_DAMAGED", "项目条目不是目录")
	}
	return dir, nil
}

// readInfoAfterSafety 只解析项目元数据和格式。调用前必须已经通过 scan/revision
// 完成目录安全门，避免先跟随项目内符号链接再做检查。
func (p *Projects) readInfoAfterSafety(id, dir string) (ProjectInfo, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "project.json"))
	if err != nil {
		return ProjectInfo{}, coded("PROJECT_DAMAGED", "项目未初始化")
	}
	var meta ProjectInfo
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ProjectInfo{}, coded("PROJECT_DAMAGED", "project.json 已损坏")
	}
	if meta.ID != id {
		return ProjectInfo{}, coded("PROJECT_DAMAGED", "project.json 的 ID 与目录名不一致")
	}
	if err := store.NewStore(dir).CheckCurrentProjectFormat(); err != nil {
		return ProjectInfo{}, coded("PROJECT_DAMAGED", "项目格式不受支持: "+err.Error())
	}
	return meta, nil
}

// loadInfo 只在缺失或损坏时报错，绝不 Init、迁移或修复：远端读取不能改磁盘事实。
// List/Resource 这类没有 revision 的入口仍先 scan；Projects.Call 的 warm path 则由
// revision 同一遍遍历完成安全门，随后只解析 project.json/format，避免重复 WalkDir。
func (p *Projects) loadInfo(id string) (ProjectInfo, error) {
	dir, err := p.checkedDir(id)
	if err != nil {
		return ProjectInfo{}, err
	}
	if err := scan(dir); err != nil {
		return ProjectInfo{}, coded("PROJECT_UNSAFE", "项目不可用: "+err.Error())
	}
	return p.readInfoAfterSafety(id, dir)
}

// load 拿到可用的 book：元数据每次现读，store 与工具表复用。
func (p *Projects) load(id string) (*book, error) {
	meta, err := p.loadInfo(id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if b := p.books[id]; b != nil {
		return b, nil
	}
	s := store.NewStore(p.dir(id))
	b := &book{info: meta, store: s, tools: make(map[string]Tool)}
	for _, t := range CoreTools(s, meta.Style) {
		b.tools[t.Name()] = t
	}
	p.books[id] = b
	return b, nil
}

func (p *Projects) cachedBook(id string) *book {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.books[id]
}

// Create 先写临时目录再改名，所以中断不会留下半个可被读取的项目。
// project.json 最后写：上游工件齐了但缺这个文件的项目不会被 load 接受。
func (p *Projects) Create(id, brief, style string) (map[string]any, error) {
	if !validID(id) {
		return nil, coded("INVALID_REQUEST", "项目 ID 非法：1-48 位小写字母、数字或连字符，且以字母开头")
	}
	brief = strings.TrimSpace(brief)
	if brief == "" || len(brief) > 32000 {
		return nil, coded("INVALID_REQUEST", "brief 必须是 1-32000 字节")
	}
	if style == "" {
		style = "default"
	}
	if !assets.HasStyle(style) {
		return nil, coded("INVALID_REQUEST", "未知文风；可用文风见 novel_guide")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := os.Lstat(p.dir(id)); err == nil {
		return nil, coded("PROJECT_EXISTS", "项目已存在；不会覆盖")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	entries, err := os.ReadDir(p.root)
	if err != nil {
		return nil, err
	}
	if len(entries) >= maxProjects {
		return nil, coded("PROJECT_LIMIT", fmt.Sprintf("项目数已达上限 %d", maxProjects))
	}
	stage, err := os.MkdirTemp(p.root, ".creating-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	s := store.NewStore(stage)
	if err := s.Init(); err != nil {
		return nil, err
	}
	if err := s.Progress.Init(0); err != nil {
		return nil, err
	}
	// 没有 provider 参与创作，Provider/Model 记录调用形态而非模型配置。
	if err := s.RunMeta.Init(style, "mcp", "web-client"); err != nil {
		return nil, err
	}
	if err := s.SaveCurrentProjectFormat(); err != nil {
		return nil, err
	}
	meta := ProjectInfo{ID: id, Brief: brief, Style: style, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := writePrivateJSON(filepath.Join(stage, "project.json"), meta); err != nil {
		return nil, err
	}
	if err := os.Rename(stage, p.dir(id)); err != nil {
		return nil, err
	}
	rev, err := revision(p.dir(id))
	if err != nil {
		return nil, err
	}
	delete(p.books, id)
	return map[string]any{"project": meta, "revision": rev, "next_tool": "novel_context"}, nil
}

// Delete 删整本项目：不可逆。健康项目必须带最新 revision（防盲目重放误删）；
// 读不出指纹的坏项目 expected 仍必填（表意确认）但不比对——坏项目正是要删的。
// 成功返回数据根的新 revision（项目已不存在，无项目 revision 可返回）。
func (p *Projects) Delete(id, expected string) (map[string]any, error) {
	if !validID(id) {
		return nil, coded("INVALID_REQUEST", "项目 ID 非法：1-48 位小写字母、数字或连字符，且以字母开头")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if b := p.books[id]; b != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
	}
	dir := p.dir(id)
	if _, err := os.Lstat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, coded("PROJECT_NOT_FOUND", "项目不存在")
		}
		return nil, err
	}
	if expected == "" {
		return nil, coded("INVALID_REQUEST", "删除项目必须带 expected_revision，用最近一次看到的项目 revision 确认")
	}
	if before, err := revision(dir); err == nil {
		if expected != before {
			return failure(id, before, "REVISION_CONFLICT", "磁盘状态已变化。先读取最新项目状态，再决定是否删除。", recovery("project_status", map[string]any{"project": id})), nil
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	delete(p.books, id)
	after, err := revision(p.root)
	if err != nil {
		// 目录已删成功，根指纹失败（通常是别的项目坏了）不能翻供：如实返回空 revision。
		after = ""
	}
	return map[string]any{"project": id, "revision": after, "result": map[string]any{"deleted": true}}, nil
}

func writePrivateJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (p *Projects) List() ([]ProjectInfo, error) {
	entries, err := os.ReadDir(p.root)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectInfo, 0, len(entries))
	for _, d := range entries {
		if !d.IsDir() || !validID(d.Name()) {
			continue // 临时目录与其他条目不属于任何项目
		}
		meta, err := p.loadInfo(d.Name())
		if err != nil {
			// 坏掉的项目要报出来：静默跳过会让客户端以为它不存在。
			return nil, fmt.Errorf("项目 %s: %w", d.Name(), err)
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

// Call 是唯一的工具入口：读工具返回当前 revision，写工具用 expected 做乐观锁。
// 恢复语义不另建一套——上游的 checkpoint 与 pending_commit 就是事实源，这里只汇报。
func (p *Projects) Call(ctx context.Context, id, name, expected string, args map[string]any) (map[string]any, error) {
	b := p.cachedBook(id)
	if b == nil {
		var err error
		b, err = p.load(id)
		if err != nil {
			return nil, err
		}
	} else if _, err := p.checkedDir(id); err != nil {
		return nil, err
	}

	tool, toolKnown := b.tools[name]
	var raw json.RawMessage
	if toolKnown {
		encoded, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		raw = encoded
	}
	readOnly := serverReadOperation(name)
	concurrentRead := readOnly
	if toolKnown {
		readOnly = false
		if ro, ok := tool.(interface{ ReadOnly(json.RawMessage) bool }); ok {
			readOnly = ro.ReadOnly(raw)
		}
		if safe, ok := tool.(interface{ ConcurrencySafe(json.RawMessage) bool }); ok {
			concurrentRead = readOnly && safe.ConcurrencySafe(raw)
		} else {
			concurrentRead = false
		}
	}
	if concurrentRead {
		b.mu.RLock()
		defer b.mu.RUnlock()
	} else {
		b.mu.Lock()
		defer b.mu.Unlock()
	}

	before, err := revision(b.store.Dir())
	if err != nil {
		return nil, coded("PROJECT_UNSAFE", "项目不可用: "+err.Error())
	}
	// revision 已在同一遍 WalkDir 中完成路径类型/配额安全门；此后只解析两份
	// 小元数据，避免 warm Call 再做一次完整 scan，同时仍能发现外部损坏。
	meta, err := p.readInfoAfterSafety(id, b.store.Dir())
	if err != nil {
		return nil, err
	}
	// project.json 没有 MCP 写入口。缓存建立后若它发生变化，CoreTools 的 style 等
	// 构造事实也已可能过期，因此拒绝热修改，而不是只改 b.info 制造半刷新状态。
	if meta != b.info {
		return nil, coded("PROJECT_DAMAGED", "project.json 在服务运行期间被外部修改；请重启服务后重新打开项目")
	}
	if expected != "" && expected != before {
		return failure(id, before, "REVISION_CONFLICT", "磁盘状态已变化。先重新路由，再决定重放还是改写参数。", recovery("next_step", map[string]any{"project": id})), nil
	}

	switch name {
	case "project_status":
		out, err := p.status(b)
		if err != nil {
			return nil, err
		}
		out["resource_uri"] = projectStatusResourceURI(id)
		shaped, err := jsonShape(out)
		if err != nil {
			return nil, err
		}
		return map[string]any{"project": id, "revision": before, "result": shaped}, nil
	case "verify_project":
		shaped, err := jsonShape(p.verifyProject(b))
		if err != nil {
			return nil, err
		}
		return map[string]any{"project": id, "revision": before, "result": shaped}, nil
	case "export_book":
		out, err := p.exportChapters(b, args)
		if err != nil {
			return nil, err
		}
		shaped, err := jsonShape(out)
		if err != nil {
			return nil, err
		}
		return map[string]any{"project": id, "revision": before, "result": shaped}, nil
	case "next_step":
		out, err := NextStep(b.store, meta)
		if err != nil {
			return nil, err
		}
		bindPlanRevision(out, before)
		shaped, err := jsonShape(out)
		if err != nil {
			return nil, err
		}
		return map[string]any{"project": id, "revision": before, "result": shaped}, nil
	}
	if !toolKnown {
		return nil, errors.New("未知工具")
	}

	data, err := tool.Execute(ctx, raw)
	if err != nil {
		code := codeOf(err)
		recover := recoveryForTool(id, name, args, code, err)
		// 真正的纯读工具失败也不会改变磁盘，直接复用调用前 revision；写工具
		// 失败可能已落盘（例如 checkpoint 中断），仍必须重新计算最新 revision。
		if readOnly {
			return failure(id, before, code, p.safeError(err), recover), nil
		}
		after, revErr := revision(b.store.Dir())
		if revErr != nil {
			return nil, revErr
		}
		return failure(id, after, code, p.safeError(err), recover), nil
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.New("上游工具返回了非法 JSON")
	}
	if name == "novel_context" {
		injectCompassHint(result)
	}
	enrichToolResourceURI(id, name, args, result)
	if readOnly {
		return map[string]any{"project": id, "revision": before, "result": result}, nil
	}
	after, err := revision(b.store.Dir())
	if err != nil {
		return nil, err
	}
	return map[string]any{"project": id, "revision": after, "result": result}, nil
}

func serverReadOperation(name string) bool {
	switch name {
	case "project_status", "verify_project", "export_book", "next_step":
		return true
	default:
		return false
	}
}

func (p *Projects) status(b *book) (map[string]any, error) {
	progress, err := b.store.Progress.Load()
	if err != nil {
		return nil, err
	}
	missing, err := b.store.FoundationMissing()
	if err != nil {
		return nil, err
	}
	if missing == nil {
		missing = []string{}
	}
	var pending any
	pendingCommit, err := b.store.Signals.LoadPendingCommit()
	if err != nil {
		return nil, err
	}
	if pendingCommit != nil {
		pending = map[string]any{
			"chapter": pendingCommit.Chapter, "stage": pendingCommit.Stage,
			"resume_tool": "commit_chapter",
			"note":        "上次提交未收尾；按同一章节重放 commit_chapter，上游会用落盘的冻结载荷继续，不要用聊天记忆重建正文",
		}
	}
	warnings := b.store.CheckConsistency()
	if warnings == nil {
		warnings = []string{}
	}
	return map[string]any{
		"info": b.info, "progress": progress, "foundation_missing": missing,
		"pending_commit": pending, "warnings": warnings,
	}, nil
}

// exportChapters 只读导出：与上游 /export 的 TXT 语义对齐（《书名》→卷分隔→
// “第 N 章 标题”→正文，剥正文首行重复标题，范围内未完成跳过进 skipped），
// 但不写盘，只把文本返回给网页客户端，由客户端转存。
func (p *Projects) exportChapters(b *book, args map[string]any) (map[string]any, error) {
	from, to := intArg(args, "from_chapter", 1), intArg(args, "to_chapter", 0)
	progress, err := b.store.Progress.Load()
	if err != nil {
		return nil, err
	}
	if progress == nil || len(progress.CompletedChapters) == 0 {
		return nil, fmt.Errorf("尚无已完成章节，无内容可导出")
	}
	book, err := b.store.Book.Load()
	if err != nil {
		return nil, err
	}
	if book == nil {
		return nil, fmt.Errorf("作品信息不存在，无法导出")
	}
	completed := make(map[int]bool, len(progress.CompletedChapters))
	maxCh := 0
	for _, c := range progress.CompletedChapters {
		completed[c] = true
		if c > maxCh {
			maxCh = c
		}
	}
	if from < 1 {
		from = 1
	}
	if to <= 0 {
		to = maxCh
	}
	if from > to {
		return nil, fmt.Errorf("章节范围无效：from=%d > to=%d", from, to)
	}
	if to-from+1 > maxExportChapters {
		return nil, fmt.Errorf("每次导出 1-%d 章", maxExportChapters)
	}
	var chapters, skipped []int
	for c := from; c <= to; c++ {
		if completed[c] {
			chapters = append(chapters, c)
		} else {
			skipped = append(skipped, c)
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("范围 %d..%d 内无已完成章节", from, to)
	}
	bodies := make(map[int]string, len(chapters))
	for _, ch := range chapters {
		text, err := b.store.Drafts.LoadChapterText(ch)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("progress 标记第 %d 章已完成，但终稿缺失或为空", ch)
		}
		bodies[ch] = text
	}
	// 大纲/摘要缺失即降级（标题缺失只输出“第 N 章”），与上游导出一致。
	outline, _ := b.store.Outline.LoadOutline()
	titleIdx := exp.BuildTitleIndex(outline)
	for _, ch := range chapters {
		summary, err := b.store.Summaries.LoadSummary(ch)
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 章摘要失败：%w", ch, err)
		}
		if summary != nil && strings.TrimSpace(summary.Title) != "" {
			titleIdx[ch] = summary.Title
		}
	}
	var locations map[int]exp.ChapterLocation
	if progress.Layered {
		if volumes, _ := b.store.Outline.LoadLayeredOutline(); len(volumes) > 0 {
			locations = exp.BuildLocations(volumes)
		}
	}
	content := exp.RenderTXT(book.Title, chapters, titleIdx, locations, bodies)
	if len(content) > maxExportBytes {
		return nil, fmt.Errorf("导出超过 %d 字节，请减少章节数", maxExportBytes)
	}
	if chapters == nil {
		chapters = []int{}
	}
	if skipped == nil {
		skipped = []int{}
	}
	return map[string]any{
		"format": "txt", "title": book.Title,
		"from_chapter": from, "to_chapter": to,
		"chapters": chapters, "skipped": skipped,
		"content": content,
	}, nil
}

// 宿主机路径会泄露用户名与目录布局，只出现在给运维看的日志里也不行。
func (p *Projects) safeError(err error) string {
	msg := err.Error()
	if p.root != "" {
		msg = strings.ReplaceAll(msg, p.root, "<projects>")
	}
	return strings.ToValidUTF8(msg, "\uFFFD")
}

// errorEnvelope 给非工具路径的失败（协议层、项目加载）统一出口：
// 有类别的按类别回，其余算参数问题。
func (p *Projects) errorEnvelope(err error) map[string]any {
	code := "INVALID_REQUEST"
	var ce *codedError
	if errors.As(err, &ce) {
		code = ce.code
	}
	return map[string]any{"code": code, "message": p.safeError(err)}
}

// failure 保持与成功路径相同的字段形状：错误也带最新 revision，客户端不必解析
// 字符串就能决定“重放还是先读”。上游工具的错误文本原样保留，不改写。
func recovery(tool string, arguments map[string]any) map[string]any {
	return map[string]any{"tool": tool, "arguments": arguments}
}

func recoveryForTool(id, name string, args map[string]any, code string, err error) map[string]any {
	if code == "REVISION_CONFLICT" {
		return recovery("next_step", map[string]any{"project": id})
	}
	if name != "edit_chapter" || code != "PRECONDITION_FAILED" {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not find the exact text") && !strings.Contains(msg, "occurrences of the text") {
		return nil
	}
	chapter := intArg(args, "chapter", 0)
	if chapter <= 0 {
		return nil
	}
	return recovery("read_chapter", map[string]any{"project": id, "chapter": chapter, "source": "draft"})
}

func failure(id, rev, code, message string, recover map[string]any) map[string]any {
	err := map[string]any{"code": code, "message": message}
	if recover != nil {
		err["recovery"] = recover
	}
	return map[string]any{"project": id, "revision": rev, "error": err}
}

// intArg 读整数参数：线上 JSON 数字解出来是 float64，进程内直调传的是 int，
// 两种都认，认不出才回 fallback。
func intArg(args map[string]any, key string, fallback int) int {
	switch v := args[key].(type) {
	case float64:
		if v >= 0 && v <= 1<<31 {
			return int(v)
		}
	case int:
		if v >= 0 {
			return v
		}
	case int64:
		if v >= 0 && v <= 1<<31 {
			return int(v)
		}
	case json.Number:
		if n, err := v.Int64(); err == nil && n >= 0 && n <= 1<<31 {
			return int(n)
		}
	}
	return fallback
}

// injectCompassHint 长篇缺 compass 时给模型指路：缺失项只说缺什么不说怎么做，
// 字段说明在 save_foundation 描述里，完整模板在 novel_guide role=architect_long。
// 上游 novel_context 不动，适配层在这里追加。
func injectCompassHint(result any) {
	root, _ := result.(map[string]any)
	if root == nil {
		return
	}
	fm, _ := root["foundation_memory"].(map[string]any)
	if fm == nil {
		return
	}
	st, _ := fm["foundation_status"].(map[string]any)
	if st == nil {
		return
	}
	missing, _ := st["missing"].([]any)
	for _, m := range missing {
		if m == "compass" {
			fm["compass_hint"] = "compass 缺失：调 save_foundation(type=update_compass) 落盘，content 为 StoryCompass JSON（ending_direction 终局主题必填、open_threads 活跃长线、estimated_scale 预计规模、last_updated 已完成章数）；模板见 novel_guide role=architect_long。"
			return
		}
	}
}
