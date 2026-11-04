package tui

import (
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/shortcuts"
	tea "github.com/charmbracelet/bubbletea"
)

func TestShortcutSetupButton(t *testing.T) {
	for _, running := range []bool{false, true} {
		for _, width := range []int{28, 80, 100} {
			ui := NewSetup(SetupOptions{Settings: launcher.Default(), Running: running})
			ui.busy = false
			ui.devices.Windows = true
			ui.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			if !strings.Contains(ui.View(), "快捷键") {
				t.Fatal("shortcut entry missing")
			}
			ui.Update(targetClick(t, ui.mouse, "action:4"))
			if ui.shortcutEditor == nil {
				t.Fatal("shortcut button did not open editor")
			}
			ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
			ui.focus = 0
			for i := 0; i < 13; i++ {
				ui.Update(tea.KeyMsg{Type: tea.KeyTab})
			}
			ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if ui.shortcutEditor == nil {
				t.Fatal("keyboard navigation did not reach shortcut editor")
			}
			ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
			ui.Update(tea.KeyMsg{Type: tea.KeyRight})
			if ui.focus != 14 {
				t.Fatal("exit button unreachable")
			}
			ui.Update(tea.KeyMsg{Type: tea.KeyTab})
			if ui.focus != 0 {
				t.Fatal("focus did not wrap")
			}
		}
	}
}

func TestShortcutEditorHidesSlotsWhenDisabledAndCancelsDraft(t *testing.T) {
	original := launcher.Default()
	original.Shortcuts.Buttons[0] = shortcuts.Button{Name: "已保存", Mode: "hold", Action: "AltLeft+ShiftLeft"}
	ui := NewSetup(SetupOptions{Settings: original})
	ui.busy = false
	ui.Update(key('K'))
	if ui.shortcutEditor == nil || strings.Contains(ui.View(), "按钮 1 名称") {
		t.Fatal("disabled slots visible")
	}
	// Simulate an enabled draft on every host; this tests editing, not injection.
	ui.shortcutEditor.draft.Enabled = true
	ui.shortcutEditor.focus = 1
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	ui.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("中文🙂")})
	ui.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ui.shortcutEditor.draft.Buttons[0].Name != "中文" {
		t.Fatal("unicode editing failed")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ui.Settings != original {
		t.Fatal("cancel modified settings")
	}
}
func TestShortcutEditorConfirmsDisabledConfiguration(t *testing.T) {
	ui := NewSetup(SetupOptions{Settings: launcher.Default()})
	ui.busy = false
	ui.Update(key('K'))
	ui.shortcutEditor.draft.Buttons[0] = shortcuts.Button{Name: "输入", Mode: "tap", Action: "text:你好\nEnter"}
	ui.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if ui.shortcutEditor != nil || ui.Settings.Shortcuts.Buttons[0].Action != "text:你好\nEnter" || ui.Settings.Shortcuts.Enabled {
		t.Fatal("draft was not copied")
	}
}
