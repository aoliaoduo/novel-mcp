package tui

import "github.com/charmbracelet/lipgloss"

// 主题色板沿用 ainovel-cli 的暖调书卷气（AdaptiveColor 按终端深浅自动切）。
// 只取 TUI 需要的子集：正文/暗淡/点缀/成功/失败/工具名。
var (
	cText    = lipgloss.AdaptiveColor{Light: "#3d3529", Dark: "#e8e0d0"}
	cDim     = lipgloss.AdaptiveColor{Light: "#8a7e6b", Dark: "#8a8175"}
	cAccent  = lipgloss.AdaptiveColor{Light: "#b8860b", Dark: "#e5b449"}
	cSuccess = lipgloss.AdaptiveColor{Light: "#3d7a42", Dark: "#7ec488"}
	cError   = lipgloss.AdaptiveColor{Light: "#b5433a", Dark: "#e07060"}
	cTool    = lipgloss.AdaptiveColor{Light: "#3a7a8a", Dark: "#7ec5d8"}
)

var (
	stTitle  = lipgloss.NewStyle().Bold(true).Foreground(cText)
	stBold   = lipgloss.NewStyle().Bold(true)
	stDim    = lipgloss.NewStyle().Foreground(cDim)
	stOK     = lipgloss.NewStyle().Foreground(cSuccess)
	stFail   = lipgloss.NewStyle().Foreground(cError)
	stTool   = lipgloss.NewStyle().Foreground(cTool)
	stText   = lipgloss.NewStyle().Foreground(cText)
	stAccent = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stTabOn  = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stTabOff = lipgloss.NewStyle().Foreground(cDim)
)

// spinnerFrames 是顶栏“运行中”胶囊的动画帧（500ms 步进）。
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠿"}
