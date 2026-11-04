package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func rightClick(ui tea.Model, x, y int, anonymousRelease bool) (tea.Model, tea.Cmd) {
	msg := tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonRight, Action: tea.MouseActionPress}
	ui, _ = ui.Update(msg)
	ui.View() // redraw between press/release must preserve the gesture on blank space
	msg.Action = tea.MouseActionRelease
	if anonymousRelease {
		msg.Button = tea.MouseButtonNone
	}
	return ui.Update(msg)
}

func TestPickerDoubleClickTimingAndIdentity(t *testing.T) {
	start := time.Now()
	for _, test := range []struct {
		name  string
		index int
		delay time.Duration
		want  bool
	}{
		{"same", 1, 200 * time.Millisecond, true},
		{"boundary", 1, 400 * time.Millisecond, true},
		{"late", 1, 401 * time.Millisecond, false},
		{"other", 2, 100 * time.Millisecond, false},
		{"backward", 1, -time.Millisecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := &setupPicker{}
			if p.doubleClick(1, start) {
				t.Fatal("first click counted as double")
			}
			if p.doubleClick(test.index, start.Add(test.delay)) != test.want {
				t.Fatal("wrong double click classification")
			}
		})
	}
}

func TestPickerDoubleClickConfirmsOnlyChoiceAndDoesNotLeak(t *testing.T) {
	m := pickerSetup()
	m.openPicker()
	m.View()
	m.Update(targetClick(t, m.mouse, "choice:2"))
	if m.picker == nil || m.Settings.Output != "CABLE Input" {
		t.Fatal("single click committed selection")
	}
	m.View()
	second := targetClick(t, m.mouse, "choice:2")
	m.Update(second)
	if m.picker != nil || m.Settings.Output != "USB Microphone" {
		t.Fatal("double click did not confirm device")
	}
	m.Update(second)
	if m.busy || m.picker != nil {
		t.Fatal("extra release leaked into configuration page")
	}
}

func TestPickerDoubleClickResetsAfterNavigation(t *testing.T) {
	for _, event := range []string{"wheel", "key", "resize", "search", "drag", "toggle"} {
		t.Run(event, func(t *testing.T) {
			m := pickerSetup()
			m.openPicker()
			m.View()
			m.Update(targetClick(t, m.mouse, "choice:1"))
			switch event {
			case "wheel":
				m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
			case "key":
				m.Update(mouseKey("down"))
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			case "search":
				m.Update(mouseKey("中文"))
			case "drag":
				m.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
			case "toggle":
				m.Update(tea.KeyMsg{Type: tea.KeyF2})
				m.Update(tea.KeyMsg{Type: tea.KeyF2})
			}
			m.View()
			m.Update(targetClick(t, m.mouse, "choice:1"))
			if m.picker == nil || m.Settings.Output != "CABLE Input" {
				t.Fatal("navigation did not reset double click")
			}
		})
	}
}

func TestRightClickCancelsDraftsAndHelp(t *testing.T) {
	for _, anonymous := range []bool{false, true} {
		m := pickerSetup()
		original := m.Settings
		m.openPicker()
		m.View()
		m.Update(targetClick(t, m.mouse, "choice:2"))
		rightClick(m, 0, 0, anonymous)
		if m.picker != nil || m.Settings != original {
			t.Fatal("right click did not cancel selection")
		}
		m.focus = 5
		m.Update(mouseKey("enter"))
		m.edit = "12345"
		m.View()
		rightClick(m, 0, 0, anonymous)
		if m.editing || m.Settings != original {
			t.Fatal("right click committed port edit")
		}
		m.Update(mouseKey("?"))
		m.View()
		rightClick(m, 0, 0, anonymous)
		if m.showHelp {
			t.Fatal("right click did not close help")
		}
		m.View()
		_, cmd := rightClick(m, 0, 0, anonymous)
		if cmd != nil {
			t.Fatal("right click unexpectedly exited startup configuration")
		}
	}
}

func TestRightClickCancelsDriverConfirmationWithoutUninstall(t *testing.T) {
	manager := &testVirtualManager{}
	initial := NewVirtual(manager).(model)
	initial.windowsDrivers = true
	var ui tea.Model = initial
	ui, _ = ui.Update(ui.Init()())
	ui, _ = ui.Update(mouseKey("u"))
	ui.View()
	ui, cmd := rightClick(ui, 0, 0, true)
	if cmd != nil || ui.(model).confirmUninstall || !ui.(model).showVirtual || len(manager.actions) != 1 {
		t.Fatal("right click did not safely cancel uninstall")
	}
}

func TestRightClickGestureDoesNotCrossPagesOrDrag(t *testing.T) {
	for _, scenario := range []string{"drag", "page", "resize", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			m := pickerSetup()
			m.openPicker()
			m.View()
			press := tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonRight, Action: tea.MouseActionPress}
			m.Update(press)
			if m.picker == nil {
				t.Fatal("cancel ran before release")
			}
			switch scenario {
			case "drag":
				motion := press
				motion.Action = tea.MouseActionMotion
				motion.X = 1
				m.Update(motion)
			case "page":
				m.mouse.render("other page", "other")
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
				m.View()
			case "disabled":
				m.Update(tea.KeyMsg{Type: tea.KeyF2})
			}
			press.Action = tea.MouseActionRelease
			m.Update(press)
			if m.picker == nil {
				t.Fatal("cancelled right gesture closed picker")
			}
		})
	}
}
