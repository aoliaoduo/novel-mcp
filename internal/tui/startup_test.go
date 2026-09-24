package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStartupTaskModelProgressAndError(t *testing.T) {
	progress := make(chan StartupStep)
	done := make(chan error)
	m := newStartupTaskModel("启动测试", []StartupStep{
		{ID: "a", Label: "第一步"},
		{ID: "b", Label: "第二步"},
	}, progress, done)

	next, _ := m.Update(startupProgressMsg{ID: "a", Label: "第一步", State: StartupOK, Detail: "完成"})
	m = next.(startupTaskModel)
	if m.steps[0].State != StartupOK || !strings.Contains(m.View(), "完成") {
		t.Fatalf("progress not rendered: %+v", m.steps)
	}

	next, cmd := m.Update(startupProgressMsg{ID: "b", Label: "第二步", State: StartupRunning, Detail: "处理中"})
	m = next.(startupTaskModel)
	if cmd == nil {
		t.Fatal("progress should continue waiting")
	}
	next, _ = m.Update(startupDoneMsg{err: errors.New("boom")})
	m = next.(startupTaskModel)
	if !m.finished || m.err == nil || m.steps[1].State != StartupError {
		t.Fatalf("error state mismatch: %+v", m)
	}
	if v := m.View(); !strings.Contains(v, "启动失败") || !strings.Contains(v, "boom") {
		t.Fatalf("error view mismatch: %s", v)
	}
}

func TestStartupTaskModelQuitsAfterErrorAcknowledged(t *testing.T) {
	m := newStartupTaskModel("启动测试", nil, make(chan StartupStep), make(chan error))
	m.finished = true
	m.err = errors.New("boom")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || next.(startupTaskModel).err == nil {
		t.Fatal("Enter should close an error page while preserving the error")
	}
}
