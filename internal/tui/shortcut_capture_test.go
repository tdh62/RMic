package tui

import (
	"testing"
	"time"

	"cnb.cool/tdh6/RMic/internal/launcher"
	tea "github.com/charmbracelet/bubbletea"
)

func TestShortcutCaptureSidesReleaseAndConfirm(t *testing.T) {
	ui := NewSetup(SetupOptions{Settings: launcher.Default()})
	c := &shortcutCapture{deadline: time.Now().Add(time.Minute)}
	ui.shortcutEditor = &shortcutEditor{focus: 3, editing: true, value: "F8", capture: c}
	e := ui.shortcutEditor
	for _, keys := range [][]string{{"F9"}, nil, {"AltLeft"}, {"AltLeft", "ShiftLeft"}, {"ShiftLeft"}, nil} {
		ui.Update(shortcutCaptureTick{c, keys})
	}
	if e.capture != nil || e.value != "AltLeft+ShiftLeft" || e.draft.Buttons[0].Action != "" {
		t.Fatalf("unexpected captured draft: %+v", e)
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.draft.Buttons[0].Action != "AltLeft+ShiftLeft" {
		t.Fatal(e.draft.Buttons[0])
	}
	ui.Update(shortcutCaptureTick{c, []string{"AltRight"}})
	if e.draft.Buttons[0].Action != "AltLeft+ShiftLeft" {
		t.Fatal("stale capture changed action")
	}
}

func TestShortcutCaptureCancelAndTimeout(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		ui := NewSetup(SetupOptions{Settings: launcher.Default()})
		c := &shortcutCapture{deadline: time.Now().Add(-time.Second)}
		ui.shortcutEditor = &shortcutEditor{focus: 3, editing: true, value: "F8", capture: c}
		if cancel {
			ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
		} else {
			ui.Update(shortcutCaptureTick{c, nil})
		}
		if ui.shortcutEditor.capture != nil || ui.shortcutEditor.value != "F8" {
			t.Fatal("cancel/timeout changed action")
		}
	}
}
