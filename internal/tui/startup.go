package tui

import (
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type StartupStepState string

const (
	StartupPending StartupStepState = "pending"
	StartupRunning StartupStepState = "running"
	StartupOK      StartupStepState = "ok"
	StartupWarn    StartupStepState = "warn"
	StartupError   StartupStepState = "error"
)

type StartupStep struct {
	ID     string
	Label  string
	State  StartupStepState
	Detail string
}

type StartupTaskOptions struct {
	Title  string
	Steps  []StartupStep
	Task   func(report func(StartupStep)) error
	Input  io.Reader
	Output io.Writer
}

func RunStartupTask(o StartupTaskOptions) error {
	in, out := o.Input, o.Output
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	progress := make(chan StartupStep)
	done := make(chan error, 1)
	go func() {
		done <- o.Task(func(step StartupStep) { progress <- step })
	}()
	m := newStartupTaskModel(o.Title, o.Steps, progress, done)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
	final, err := p.Run()
	if err != nil {
		return err
	}
	return final.(startupTaskModel).err
}

type startupProgressMsg StartupStep
type startupDoneMsg struct{ err error }
type startupTickMsg time.Time

type startupTaskModel struct {
	title    string
	steps    []StartupStep
	progress <-chan StartupStep
	done     <-chan error
	spin     int
	width    int
	finished bool
	err      error
}

func newStartupTaskModel(title string, steps []StartupStep, progress <-chan StartupStep, done <-chan error) startupTaskModel {
	copySteps := append([]StartupStep(nil), steps...)
	for i := range copySteps {
		if copySteps[i].State == "" {
			copySteps[i].State = StartupPending
		}
	}
	return startupTaskModel{title: title, steps: copySteps, progress: progress, done: done, width: 80}
}

func (m startupTaskModel) Init() tea.Cmd {
	return tea.Batch(waitStartup(m.progress, m.done), startupTick())
}

func startupTick() tea.Cmd {
	return tea.Tick(RefreshInterval, func(t time.Time) tea.Msg { return startupTickMsg(t) })
}

func waitStartup(progress <-chan StartupStep, done <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case step := <-progress:
			return startupProgressMsg(step)
		case err := <-done:
			return startupDoneMsg{err: err}
		}
	}
}

func (m startupTaskModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.width < 52 {
			m.width = 52
		}
		return m, nil
	case startupProgressMsg:
		m.applyStep(StartupStep(msg))
		return m, waitStartup(m.progress, m.done)
	case startupTickMsg:
		m.spin++
		if m.finished {
			return m, nil
		}
		return m, startupTick()
	case startupDoneMsg:
		m.finished = true
		m.err = msg.err
		if msg.err == nil {
			return m, tea.Quit
		}
		for i := range m.steps {
			if m.steps[i].State == StartupRunning {
				m.steps[i].State = StartupError
				m.steps[i].Detail = msg.err.Error()
				break
			}
		}
		return m, nil
	case tea.KeyMsg:
		if m.finished && (msg.String() == "q" || msg.String() == "ctrl+c" || msg.String() == "enter" || msg.String() == "esc") {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *startupTaskModel) applyStep(step StartupStep) {
	for i := range m.steps {
		if m.steps[i].ID == step.ID {
			m.steps[i] = step
			return
		}
	}
	m.steps = append(m.steps, step)
}

func (m startupTaskModel) View() string {
	lines := []string{stTitle.Render("novel-mcp") + " " + stDim.Render(m.title), ""}
	for _, step := range m.steps {
		mark := stDim.Render("○")
		label := stDim.Render(step.Label)
		switch step.State {
		case StartupRunning:
			mark = stAccent.Render(spinnerFrames[m.spin%len(spinnerFrames)])
			label = stText.Render(step.Label)
		case StartupOK:
			mark = stOK.Render("✓")
			label = stText.Render(step.Label)
		case StartupWarn:
			mark = stAccent.Render("!")
			label = stText.Render(step.Label)
		case StartupError:
			mark = stFail.Render("✕")
			label = stFail.Render(step.Label)
		}
		row := "  " + mark + "  " + label
		if step.Detail != "" {
			row += "  " + stDim.Render(truncateRunes(step.Detail, max(20, m.width-24)))
		}
		lines = append(lines, row)
	}
	if m.finished && m.err != nil {
		lines = append(lines, "")
		for _, line := range chunkRunes("启动失败："+m.err.Error(), max(24, m.width-8)) {
			lines = append(lines, "  "+stFail.Render(line))
		}
		lines = append(lines, "", stDim.Render("  修正上面的错误后重新启动 · Enter / q 关闭"))
	} else {
		lines = append(lines, "", stDim.Render("  正在准备 MCP 服务…"))
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(joinLines(lines))
}

func startupStep(id, label string, state StartupStepState, detail string) StartupStep {
	return StartupStep{ID: id, Label: label, State: state, Detail: detail}
}

func startupSteps(labels ...string) []StartupStep {
	steps := make([]StartupStep, 0, len(labels))
	for i, label := range labels {
		steps = append(steps, startupStep(fmt.Sprintf("step-%d", i), label, StartupPending, ""))
	}
	return steps
}
