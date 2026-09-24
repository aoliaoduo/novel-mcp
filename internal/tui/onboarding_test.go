package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStartupModelDefaultsToPublic(t *testing.T) {
	m := newStartupModel()
	if got := m.choice(); got != StartupPublic {
		t.Fatalf("default startup mode = %q", got)
	}
	v := m.View()
	for _, want := range []string{"首次启动", "网页 / 云端 AI 客户端", "仅本机 AI 客户端", "Enter 确认"} {
		if !contains(v, want) {
			t.Fatalf("startup view missing %q", want)
		}
	}
}

func TestStartupModelSelection(t *testing.T) {
	m := newStartupModel()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(startupModel)
	if m.choice() != StartupLocal {
		t.Fatalf("down should select local, got %q", m.choice())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = next.(startupModel)
	if m.choice() != StartupPublic {
		t.Fatalf("1 should select public, got %q", m.choice())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = next.(startupModel)
	if m.choice() != StartupLocal {
		t.Fatalf("2 should select local, got %q", m.choice())
	}
}

func TestStartupModelCancel(t *testing.T) {
	m := newStartupModel()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(startupModel)
	if !m.cancelled || cmd == nil {
		t.Fatal("esc should cancel and quit")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
