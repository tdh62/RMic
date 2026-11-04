package tui

import (
	"testing"

	"cnb.cool/tdh6/RMic/internal/launcher"
	tea "github.com/charmbracelet/bubbletea"
)

func TestShortcutIntervalEditing(t *testing.T) {
	ui := NewSetup(SetupOptions{Settings: launcher.Default()})
	ui.busy = false
	ui.Update(key('K'))
	e := ui.shortcutEditor
	e.draft.Enabled = true
	e.focus = 9
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	if e.focus != 10 {
		t.Fatal("interval unreachable")
	}
	ui.View()
	ui.Update(targetClick(t, ui.mouse, "shortcut-field:10"))
	if !e.editing || e.value != "20" {
		t.Fatal("interval editor not opened")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	ui.Update(key('0'))
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.draft.KeyIntervalMS != 0 || e.editing {
		t.Fatal("zero interval not accepted")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	ui.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("101")})
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !e.editing || e.draft.KeyIntervalMS != 0 || e.notice == "" {
		t.Fatal("invalid interval accepted")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	// Save a disabled draft on non-Windows hosts as well.
	e.draft.Enabled = false
	ui.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if ui.Settings.Shortcuts.KeyIntervalMS != 0 || ui.shortcutEditor != nil {
		t.Fatal("interval draft not confirmed")
	}
}
