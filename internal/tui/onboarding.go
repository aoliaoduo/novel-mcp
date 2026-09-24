package tui

import (
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type StartupMode string

const (
	StartupPublic StartupMode = "public"
	StartupLocal  StartupMode = "local"
)

type StartupChoiceOptions struct {
	Input  io.Reader
	Output io.Writer
}

func ChooseStartupMode(o StartupChoiceOptions) (StartupMode, error) {
	in, out := o.Input, o.Output
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	p := tea.NewProgram(newStartupModel(), tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
	result, err := p.Run()
	if err != nil {
		return "", err
	}
	m := result.(startupModel)
	if m.cancelled {
		return "", nil
	}
	return m.choice(), nil
}

type startupModel struct {
	selected  int
	width     int
	cancelled bool
	done      bool
}

func newStartupModel() startupModel {
	return startupModel{width: 80}
}

func (m startupModel) choice() StartupMode {
	if m.selected == 1 {
		return StartupLocal
	}
	return StartupPublic
}

func (m startupModel) Init() tea.Cmd { return nil }

func (m startupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.width < 52 {
			m.width = 52
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			m.selected = 0
		case "down", "j":
			m.selected = 1
		case "1":
			m.selected = 0
		case "2":
			m.selected = 1
		case "enter":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m startupModel) View() string {
	if m.done || m.cancelled {
		return ""
	}
	title := stTitle.Render("novel-mcp") + " " + stDim.Render("首次启动")
	lines := []string{
		title,
		stDim.Render("选择 AI 客户端从哪里连接。这个选择会被记住，以后双击直接启动。"),
		"",
		m.startupOption(0, "1  网页 / 云端 AI 客户端", "推荐 · 自动检查 Tailscale，并通过 Funnel 提供 HTTPS MCP 地址"),
		"",
		m.startupOption(1, "2  仅本机 AI 客户端", "不使用 Tailscale · 只监听 127.0.0.1，不暴露公网"),
		"",
		stDim.Render("↑↓ 选择 · Enter 确认 · q 取消"),
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(joinLines(lines))
}

func (m startupModel) startupOption(index int, title, detail string) string {
	mark := "  "
	titleStyle := stText
	if m.selected == index {
		mark = stAccent.Render("› ")
		titleStyle = stAccent
	}
	lines := []string{mark + titleStyle.Render(title)}
	for _, line := range chunkRunes(detail, max(20, m.width-8)) {
		lines = append(lines, "  "+stDim.Render(line))
	}
	return joinLines(lines)
}

func joinLines(lines []string) string {
	out := ""
	for i, line := range lines {
		if i > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}
