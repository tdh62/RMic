package tui

import (
	"maps"
	"strconv"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestActionGridGeometryDoesNotChangeWithFocus(t *testing.T) {
	labels := []string{"开始 g", "保存 S", "驱动 v", "刷新 r", "退出 q"}
	for _, width := range []int{18, 28, 40, 80, 120} {
		frame := &mouseFrame{}
		baseline := frame.render(page(nil, actionBar(labels, -1, width), width, 12, 0), "actions")
		cells := maps.Clone(frame.cells)
		for focus := range labels {
			view := frame.render(page(nil, actionBar(labels, focus, width), width, 12, 0), "actions")
			if ansi.Strip(view) != ansi.Strip(baseline) || !maps.Equal(cells, frame.cells) {
				t.Fatalf("focus %d moved controls at width %d", focus, width)
			}
			if lipgloss.Width(view) > width {
				t.Fatal("grid overflow")
			}
		}
		for cell := range cells {
			// Gutters and the blank separator above the toolbar never activate actions.
			if cell.y > 0 && strings.TrimSpace(strings.Split(baseline, "\n")[cell.y-1]) == "" && frame.cells[mouseCell{cell.x, cell.y - 1}] != "" {
				t.Fatal("blank separator is clickable")
			}
		}
	}
}

func TestMouseGestureRequiresMatchingReleaseAndCancelsOnChanges(t *testing.T) {
	for _, scenario := range []string{"click", "drag", "other", "page", "resize", "wheel", "key", "moved"} {
		t.Run(scenario, func(t *testing.T) {
			m := pickerSetup()
			m.View()
			frame := m.mouse
			release := targetClick(t, frame, "field:0")
			frame.pressedTarget = "" // send the actual press through Update
			press := release
			press.Action = tea.MouseActionPress
			m.Update(press)
			if m.picker != nil {
				t.Fatal("press opened control before release")
			}
			m.View() // normal repaint during a gesture must remain usable
			switch scenario {
			case "drag":
				motion := press
				motion.Action = tea.MouseActionMotion
				motion.X, motion.Y = 0, 0
				m.Update(motion)
			case "other":
				release.X, release.Y = 0, 0
			case "page":
				m.showHelp = true
				m.View()
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
				m.View()
			case "wheel":
				m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
				m.View()
			case "key":
				m.Update(mouseKey("down"))
				m.View()
			case "moved":
				frame.render("\n"+clickable("field:0", "moved"), m.mousePage())
			}
			m.Update(release)
			if scenario == "click" {
				if m.picker == nil {
					t.Fatal("matching release did not open control")
				}
			} else if m.picker != nil {
				t.Fatal("cancelled gesture opened control")
			}
		})
	}
}

func TestSetupRowsHaveOneTargetAndDisabledRowsAreInert(t *testing.T) {
	m := pickerSetup()
	m.width, m.height = 100, 32
	for focus := range 9 {
		m.focus = focus
		m.View()
		for _, id := range m.mouse.cells {
			if strings.HasPrefix(id, "prev:") || strings.HasPrefix(id, "next:") || id == "key:enter" {
				t.Fatalf("redundant or implicit control remains: %s", id)
			}
		}
	}
	m.Settings.Mode = audio.ModeMic
	m.View()
	for _, id := range m.mouse.cells {
		if id == "field:2" || id == "field:7" {
			t.Fatal("disabled field is clickable")
		}
	}
}

func TestPickerEnumSelectionAndValidation(t *testing.T) {
	for _, field := range []int{0, 3, 7, 8} {
		m := pickerSetup()
		m.focus = field
		original := m.Settings
		m.Update(mouseKey("enter"))
		if m.picker == nil {
			t.Fatalf("field %d did not open list", field)
		}
		m.Update(tea.KeyMsg{Type: tea.KeyEnd})
		if m.Settings != original {
			t.Fatal("browsing changed settings")
		}
		selected := m.picker.values[m.picker.selected]
		m.Update(mouseKey("enter"))
		if m.picker != nil {
			t.Fatal("selection did not close")
		}
		switch field {
		case 0:
			if string(m.Settings.Mode) != selected {
				t.Fatal("mode not applied")
			}
		case 3:
			if m.Settings.Quality != selected {
				t.Fatal("quality not applied")
			}
		case 7:
			if m.Settings.CaptureChannels != 8 {
				t.Fatal("channels not applied")
			}
		case 8:
			if string(m.Settings.DefaultMode) != selected {
				t.Fatal("default mode not applied")
			}
		}
	}
	m := pickerSetup()
	m.Settings.Mode, m.Settings.Quality = audio.ModePlayback, "ultra"
	m.focus = 0
	m.openPicker()
	for _, value := range m.picker.values {
		if value != "playback" {
			t.Fatal("Ultra exposed incompatible mode")
		}
	}
}

func TestSetupHintsStayBesideValuesOnWideTerminal(t *testing.T) {
	m := pickerSetup()
	m.width, m.height = 140, 32
	for _, focus := range []int{1, 5} {
		m.focus = focus
		view := ansi.Strip(m.View())
		text := "麦克风送入：CABLE Input ›"
		if focus == 5 {
			text = "HTTPS 端口：8443 ✎"
		}
		if !strings.Contains(view, text) {
			t.Fatalf("hint is separated from value: %q", text)
		}
		click := targetClick(t, m.mouse, "field:"+strconv.Itoa(focus))
		m.Update(click)
		if focus == 1 && m.picker == nil {
			t.Fatal("row no longer opens picker")
		}
		if focus == 5 && !m.editing {
			t.Fatal("port row no longer opens editor")
		}
		m.Update(mouseKey("esc"))
	}
}
