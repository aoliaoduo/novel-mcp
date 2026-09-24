// Package tui 是双击 exe 后的运行态界面：备用屏幕 + 500ms 重绘的只读查看面。
// 设计语言对标 open-bridge 的 serve-console TUI（顶栏胶囊 + 卡片 + 事件流 +
// 底部按键栏），实现用 bubbletea + lipgloss（与 ainovel-cli 同栈）。
//
// 非控制台（管道/重定向/服务）或 --no-tui 时不进 TUI，main 保持现有纯文本输出。
package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"novel-mcp/internal/server"
)

// RefreshInterval 与 open-bridge 一致：500ms 重绘一帧。
const RefreshInterval = 500 * time.Millisecond

type viewTab int

const (
	tabConn viewTab = iota
	tabEvents
	tabProjects
	numTabs
)

var tabNames = []string{"连接", "事件", "项目"}

// Options 是 Run 的参数。Input/Output 留空就用进程标准流；
// Snapshot 由 main 提供，每 500ms 被调用一次；Stop 关闭时 TUI 主动退出
// （服务崩溃时 main 用它把前台 TUI 带走）。
type Options struct {
	Snapshot SnapshotFunc
	Input    io.Reader
	Output   io.Writer
	Stop     <-chan struct{}
	Copy     func(string) error
	OpenURL  func(string) error
}

// Wanted 决定双击后进 TUI 还是走纯文本：前后都是控制台才进 TUI。
func Wanted(noTUI bool, stdout, stdin *os.File) bool {
	if noTUI {
		return false
	}
	return isCharDevice(stdout) && isCharDevice(stdin)
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Run 阻塞到用户退出（q/Ctrl+C）。返回后调用方负责停服务。
func Run(o Options) error {
	in, out := o.Input, o.Output
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	p := tea.NewProgram(newModelWithActions(o.Snapshot, o.Copy, o.OpenURL), tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
	if o.Stop != nil {
		go func() {
			<-o.Stop
			p.Quit()
		}()
	}
	_, err := p.Run()
	return err
}

type tickMsg time.Time
type snapMsg Snapshot
type actionMsg struct {
	label string
	err   error
}

type model struct {
	snapshot      SnapshotFunc
	snap          Snapshot
	tab           viewTab
	width         int
	height        int
	offsets       [numTabs]int
	spin          int
	quitting      bool
	revealSecrets bool
	errorsOnly    bool
	notice        string
	noticeTicks   int
	copy          func(string) error
	openURL       func(string) error
}

func newModelWithActions(fn SnapshotFunc, copyFn func(string) error, openFn func(string) error) model {
	return model{snapshot: fn, width: 80, height: 24, snap: fn(), copy: copyFn, openURL: openFn}
}

func (m model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(RefreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func fetch(fn SnapshotFunc) tea.Cmd {
	return func() tea.Msg { return snapMsg(fn()) }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.width < 40 {
			m.width = 40
		}
		if m.height < 10 {
			m.height = 10
		}
		m.clampOffsets()
		return m, nil
	case tickMsg:
		m.spin++
		if m.noticeTicks > 0 {
			m.noticeTicks--
			if m.noticeTicks == 0 {
				m.notice = ""
			}
		}
		return m, tea.Batch(tick(), fetch(m.snapshot))
	case snapMsg:
		m.snap = Snapshot(msg)
		m.clampOffsets()
		return m, nil
	case actionMsg:
		if msg.err != nil {
			m.notice = "✕ " + msg.err.Error()
		} else {
			m.notice = "✓ " + msg.label
		}
		m.noticeTicks = 6
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			m.tab = (m.tab + 1) % numTabs
			return m, nil
		case "shift+tab":
			m.tab = (m.tab + numTabs - 1) % numTabs
			return m, nil
		case "1", "2", "3":
			m.tab = viewTab(msg.String()[0] - '1')
			return m, nil
		case "up", "down", "pgup", "pgdown", "home", "end":
			m.scroll(msg.String())
			return m, nil
		case "esc":
			// Esc 只回到最新/顶部，不退出：误触 Esc 就停服务太贵了。
			m.offsets[m.tab] = 0
			return m, nil
		case "v":
			if m.tab == tabConn {
				m.revealSecrets = !m.revealSecrets
			}
			return m, nil
		case "c":
			if m.tab == tabConn {
				return m, m.copyCommand("已复制 MCP 配置", connectionConfig(m.snap.Conn))
			}
		case "u":
			if m.tab == tabConn {
				return m, m.copyCommand("已复制 MCP URL", m.snap.Conn.AccessURL)
			}
		case "b":
			if m.tab == tabConn && !m.snap.Conn.BearerOff {
				return m, m.copyCommand("已复制 Bearer", m.snap.Conn.Bearer)
			}
		case "p":
			if m.tab == tabConn {
				return m, m.copyCommand("已复制初始提示词", server.ConnectionPrompt(m.snap.Conn.AccessURL, m.snap.Conn.Bearer, m.snap.Conn.BearerOff, m.snap.Conn.HealthURL))
			}
		case "o":
			if m.tab == tabConn {
				return m, m.openCommand(m.snap.Conn.HealthURL)
			}
		case "e":
			if m.tab == tabEvents {
				m.errorsOnly = !m.errorsOnly
				m.offsets[tabEvents] = 0
			}
			return m, nil
		}
	}
	return m, nil
}

// bodyHeight 是内容区行数：顶栏 + 分隔线 + 视图栏 + 底栏之外全归内容。
func (m model) bodyHeight() int {
	if h := m.height - 4; h > 3 {
		return h
	}
	return 3
}

func (m model) contentLen(tab viewTab) int {
	switch tab {
	case tabConn:
		return len(m.connLines())
	case tabEvents:
		n := len(m.filteredEvents())
		if n == 0 {
			return 1
		}
		return n
	case tabProjects:
		n := len(m.snap.Projects)
		if n == 0 {
			return 1
		}
		return n
	}
	return 1
}

func (m model) maxOffsetFor(tab viewTab) int {
	if max := m.contentLen(tab) - m.bodyHeight(); max > 0 {
		return max
	}
	return 0
}

func (m *model) clampOffsets() {
	for i := range m.offsets {
		if max := m.maxOffsetFor(viewTab(i)); m.offsets[i] > max {
			m.offsets[i] = max
		}
	}
}

func (m *model) scroll(key string) {
	max := m.maxOffsetFor(m.tab)
	o := &m.offsets[m.tab]
	switch key {
	case "up":
		*o--
	case "down":
		*o++
	case "pgup":
		*o -= m.bodyHeight()
	case "pgdown":
		*o += m.bodyHeight()
	case "home":
		*o = 0
	case "end":
		*o = max
	}
	if *o < 0 {
		*o = 0
	}
	if *o > max {
		*o = max
	}
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	body := m.bodyLines()
	lines := []string{m.topLine(), m.ruleLine(), m.tabLine()}
	lines = append(lines, body...)
	lines = append(lines, m.footLine())
	return strings.Join(lines, "\n")
}

func (m model) topLine() string {
	frame := spinnerFrames[m.spin%len(spinnerFrames)]
	left := stTitle.Render("novel-mcp") + " " +
		stDim.Render("v"+m.snap.Conn.Version) + "  " +
		stOK.Render(frame+" 运行中") + "  " +
		stDim.Render("已运行 "+formatUptime(m.snap.Uptime))
	right := stDim.Render(fmt.Sprintf("调用 %d ", m.snap.Calls)) +
		stOK.Render(fmt.Sprintf("✓%d ", m.snap.Successes)) +
		stFail.Render(fmt.Sprintf("✕%d", m.snap.Failures))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// ruleLine 是顶栏下的通栏分隔线。
func (m model) ruleLine() string {
	return stDim.Render(strings.Repeat("─", m.width))
}

func (m model) tabLine() string {
	parts := make([]string, 0, numTabs)
	for i, name := range tabNames {
		label := fmt.Sprintf("[%d]%s", i+1, name)
		switch viewTab(i) {
		case tabEvents:
			label += fmt.Sprintf("(%d)", len(m.snap.Events))
		case tabProjects:
			label += fmt.Sprintf("(%d)", len(m.snap.Projects))
		}
		if viewTab(i) == m.tab {
			parts = append(parts, stTabOn.Render(label))
		} else {
			parts = append(parts, stTabOff.Render(label))
		}
	}
	return strings.Join(parts, "  ")
}

func (m model) footLine() string {
	if m.notice != "" {
		if strings.HasPrefix(m.notice, "✕") {
			return stFail.Render(m.notice)
		}
		return stOK.Render(m.notice)
	}
	switch m.tab {
	case tabConn:
		if m.width < 72 {
			return stDim.Render("C 配置 · V 凭据 · Tab 切页 · q 退出")
		}
		if m.width < 110 {
			return stDim.Render("C 配置 · U URL · P 提示词 · V 凭据 · Tab 切页 · q 退出")
		}
		return stDim.Render("C 配置 · U URL · B Bearer · P 提示词 · O 健康检查 · V 显示/隐藏 · Tab 切页 · q 退出")
	case tabEvents:
		filter := "仅错误"
		if m.errorsOnly {
			filter = "全部事件"
		}
		return stDim.Render("E " + filter + " · ↑↓/PgUp/PgDn 滚动 · Tab 切页 · q 退出")
	default:
		return stDim.Render("↑↓/PgUp/PgDn 滚动 · Tab 切页 · q 退出")
	}
}

// bodyLines 取当前视图的可见行并补齐高度，保证底栏永远钉在底部。
func (m model) bodyLines() []string {
	var full []string
	switch m.tab {
	case tabConn:
		full = m.connLines()
	case tabEvents:
		full = m.eventLines()
	case tabProjects:
		full = m.projectLines()
	}
	h := m.bodyHeight()
	off := m.offsets[m.tab]
	if off > len(full) {
		off = len(full)
	}
	vis := full[off:]
	if len(vis) > h {
		vis = vis[:h]
	}
	out := make([]string, h)
	copy(out, vis)
	return out
}

func (m model) connLines() []string {
	c := m.snap.Conn
	lines := []string{""}
	lines = append(lines, "  "+stOK.Render("✓ MCP 服务已就绪"))
	if strings.Contains(c.Note, "未发布") {
		lines = append(lines, "  "+stAccent.Render("! "+c.Note))
	} else if c.Note != "" {
		lines = append(lines, "  "+stOK.Render("✓ "+c.Note))
	}
	if m.snap.Calls == 0 {
		lines = append(lines, "  "+stDim.Render("○ 等待客户端首次连接"))
	} else {
		lines = append(lines, "  "+stOK.Render("✓ 已收到 MCP 调用")+stDim.Render(" · 最近调用 "+m.recentCall()))
	}
	lines = append(lines, "")
	lines = append(lines, "  "+stAccent.Render("C  复制 MCP 配置")+stDim.Render("  → 粘贴到 AI 客户端即可"))
	lines = append(lines, "  "+stDim.Render("U URL · B Bearer · P 初始提示词 · O 打开健康检查 · V 显示/隐藏凭据"))
	lines = append(lines, "")
	lines = append(lines, "  "+stDim.Render("── 连接详情 ──"))
	label := func(name string) string { return stDim.Render(name) }
	access := c.AccessURL
	if !m.revealSecrets {
		access = maskAccessURL(access)
	}
	lines = append(lines, "  "+label("MCP URL")+"     "+stBold.Render(firstChunk(access, m.width-14)))
	for _, chunk := range restChunks(access, m.width-14) {
		lines = append(lines, "                "+stBold.Render(chunk))
	}
	if c.BearerOff {
		lines = append(lines, "  "+label("BEARER")+"      "+stAccent.Render("已关闭——这串 URL 本身就是凭据"))
	} else {
		bearer := c.Bearer
		if !m.revealSecrets {
			bearer = maskSecret(bearer)
		}
		lines = append(lines, "  "+label("BEARER")+"      "+stTool.Render(firstChunk(bearer, m.width-14)))
		for _, chunk := range restChunks(bearer, m.width-14) {
			lines = append(lines, "                "+stTool.Render(chunk))
		}
	}
	lines = append(lines, "  "+label("健康检查")+"    "+c.HealthURL)
	lines = append(lines, "")
	lines = append(lines, "  "+stDim.Render("── 本机 ──"))
	lines = append(lines, "  "+stDim.Render("数据目录 "+truncateRunes(c.DataDir, 28)+"  ·  "+fmt.Sprintf("%d 个项目", len(m.snap.Projects))))
	return lines
}

// recentCall 一句话最近调用，无调用时返回“暂无调用”。
func (m model) recentCall() string {
	if len(m.snap.Events) == 0 {
		return "暂无调用"
	}
	e := m.snap.Events[0]
	mark := "✓"
	if !e.OK {
		mark = "✕"
	}
	return e.Tool + " " + mark + " " + formatLatency(e.Latency)
}

func (m model) eventLines() []string {
	events := m.filteredEvents()
	if len(events) == 0 {
		if m.errorsOnly && len(m.snap.Events) > 0 {
			return []string{"", stDim.Render("  当前没有失败事件——按 E 查看全部")}
		}
		return []string{"", stDim.Render("  暂无调用——客户端连上后，这里的事件会实时滚动")}
	}
	lines := make([]string, 0, len(events))
	for _, e := range events {
		mark := stOK.Render("✓")
		if !e.OK {
			mark = stFail.Render("✕")
		}
		proj := ""
		if e.Project != "" {
			proj = "[" + e.Project + "] "
		}
		row := fmt.Sprintf("  %s %s %s %s",
			stDim.Render(e.At.Format("15:04:05")), mark,
			stTool.Render(e.Tool), stDim.Render(proj+formatLatency(e.Latency)))
		if !e.OK && e.Err != "" {
			row += " " + stFail.Render(truncateRunes(e.Err, 40))
		}
		lines = append(lines, row)
	}
	return lines
}

func (m model) filteredEvents() []server.Event {
	if !m.errorsOnly {
		return m.snap.Events
	}
	out := make([]server.Event, 0, len(m.snap.Events))
	for _, e := range m.snap.Events {
		if !e.OK {
			out = append(out, e)
		}
	}
	return out
}

func (m model) copyCommand(label, value string) tea.Cmd {
	return func() tea.Msg {
		if m.copy == nil {
			return actionMsg{err: fmt.Errorf("当前环境没有可用的剪贴板")}
		}
		return actionMsg{label: label, err: m.copy(value)}
	}
}

func (m model) openCommand(rawURL string) tea.Cmd {
	return func() tea.Msg {
		if m.openURL == nil {
			return actionMsg{err: fmt.Errorf("当前环境不能自动打开浏览器")}
		}
		if err := m.openURL(rawURL); err != nil {
			return actionMsg{err: fmt.Errorf("打开健康检查失败: %w", err)}
		}
		return actionMsg{label: "已打开健康检查"}
	}
}

func connectionConfig(c Connection) string {
	serverCfg := map[string]any{"type": "streamable-http", "url": c.AccessURL}
	if !c.BearerOff && c.Bearer != "" {
		serverCfg["headers"] = map[string]string{"Authorization": "Bearer " + c.Bearer}
	}
	raw, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"novel-mcp": serverCfg}}, "", "  ")
	return string(raw)
}

func maskSecret(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return "••••"
	}
	return "••••••••…" + string(r[len(r)-4:])
}

func maskAccessURL(s string) string {
	i := strings.Index(s, "/mcp/")
	if i < 0 {
		return s
	}
	return s[:i+5] + maskSecret(s[i+5:])
}

func (m model) projectLines() []string {
	if m.snap.ProjectsErr != "" {
		return []string{"", stFail.Render("  项目列表读取失败：" + m.snap.ProjectsErr)}
	}
	if len(m.snap.Projects) == 0 {
		return []string{"", stDim.Render("  还没有项目——客户端 create_project 后显示在这里")}
	}
	lines := make([]string, 0, len(m.snap.Projects))
	for _, p := range m.snap.Projects {
		brief := truncateRunes(p.Brief, 20)
		if brief == "" {
			brief = "（无简介）"
		}
		status := projectStatusText(p)
		progress := ""
		if p.TotalChapters > 0 {
			progress = fmt.Sprintf("%d/%d章", p.Chapters, p.TotalChapters)
		} else if p.Chapters >= 0 {
			progress = fmt.Sprintf("%d章", p.Chapters)
		}
		parts := []string{stTool.Render(truncateRunes(p.ID, 28)), stDim.Render(brief)}
		if p.Style != "" {
			parts = append(parts, stDim.Render(p.Style))
		}
		parts = append(parts, stText.Render(status))
		if progress != "" {
			parts = append(parts, stDim.Render(progress))
		}
		if p.PendingRewrites > 0 {
			parts = append(parts, stAccent.Render(fmt.Sprintf("返工 %d", p.PendingRewrites)))
		}
		parts = append(parts, stDim.Render(shortDate(p.CreatedAt)))
		lines = append(lines, "  "+strings.Join(parts, "  "))
	}
	return lines
}

func projectStatusText(p ProjectRow) string {
	if p.Phase == "complete" {
		return "已完结"
	}
	if p.Flow == "rewriting" {
		return "返工中"
	}
	if p.Flow == "polishing" {
		return "润色中"
	}
	switch p.Phase {
	case "init":
		return "待开始"
	case "premise":
		return "立意"
	case "outline":
		return "大纲"
	case "writing":
		if p.CurrentChapter > 0 {
			return fmt.Sprintf("写作 · 第%d章", p.CurrentChapter)
		}
		return "写作"
	case "":
		return "状态未知"
	default:
		return p.Phase
	}
}

// chunkRunes 按终端 cell 宽切分：中文占 2 格，URL、Bearer 与提示词太长时折行不断 rune。
// 必须按 cell 切：bubbletea 会把超窗口宽的行静默截断，按 rune 数切会把中文块的尾巴丢掉（凭据缺字）。
func chunkRunes(s string, w int) []string {
	if w < 1 {
		return []string{s}
	}
	var out []string
	var cur []rune
	cw := 0
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
			cw = 0
		}
	}
	for _, r := range s {
		if rw := runewidth.RuneWidth(r); cw+rw > w && len(cur) > 0 {
			flush()
		}
		cur = append(cur, r)
		cw += runewidth.RuneWidth(r)
	}
	flush()
	if len(out) == 0 {
		return []string{s}
	}
	return out
}

func firstChunk(s string, w int) string { return chunkRunes(s, w)[0] }

func restChunks(s string, w int) []string {
	c := chunkRunes(s, w)
	if len(c) <= 1 {
		return nil
	}
	return c[1:]
}

// truncateRunes 按 cell 宽截断并加 …：与 chunkRunes 同口径，中文不超宽。
func truncateRunes(s string, n int) string {
	if runewidth.StringWidth(s) <= n {
		return s
	}
	var cur []rune
	cw := 0
	for _, r := range s {
		if cw+runewidth.RuneWidth(r) > n-1 { // 给 … 留 1 格
			break
		}
		cur = append(cur, r)
		cw += runewidth.RuneWidth(r)
	}
	return string(cur) + "…"
}

// shortDate 把创建日期压成短日期：RFC3339 取前 10 位，其他原样。
func shortDate(s string) string {
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	return truncateRunes(s, 16)
}

func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	return d.Truncate(time.Millisecond).String()
}

// formatUptime 中文时长：45秒 / 3分12秒 / 2时5分。
func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%d秒", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%d分%d秒", s/60, s%60)
	}
	return fmt.Sprintf("%d时%d分", s/3600, (s%3600)/60)
}
