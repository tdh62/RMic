package tui

import (
	"fmt"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func pickerSetup() *Setup {
	m := NewSetup(SetupOptions{Settings: launcher.Default()})
	m.busy = false
	m.Settings.Mode = audio.ModeDuplex
	m.devices.Outputs = []string{"CABLE Input", "中文虚拟设备", "USB Microphone"}
	m.devices.Captures = []string{"CABLE Output", "loopback: Speakers"}
	m.devices.Networks = []string{"192.168.1.2", "100.64.0.2"}
	m.Settings.Output = "CABLE Input"
	m.focus = 1
	return m
}

func TestPickerMouseSelectionIsDraftUntilConfirmed(t *testing.T) {
	m := pickerSetup()
	m.View()
	m.Update(targetClick(t, m.mouse, "field:1"))
	if m.picker == nil {
		t.Fatal("device click did not open list")
	}
	m.View()
	m.Update(targetClick(t, m.mouse, "choice:1"))
	if m.Settings.Output != "CABLE Input" {
		t.Fatal("selection changed configuration before confirmation")
	}
	m.View()
	m.Update(targetClick(t, m.mouse, "picker:cancel"))
	if m.picker != nil || m.Settings.Output != "CABLE Input" {
		t.Fatal("cancel changed settings")
	}
	m.Update(mouseKey("enter"))
	m.View()
	m.Update(targetClick(t, m.mouse, "choice:2"))
	m.View()
	click := targetClick(t, m.mouse, "picker:confirm")
	m.Update(click)
	if m.picker != nil || m.Settings.Output != "USB Microphone" {
		t.Fatal("confirmation did not select device")
	}
	// A second queued click must not activate a control on the underlying page.
	m.Update(click)
	if m.picker != nil || m.busy {
		t.Fatal("click leaked to underlying page")
	}
}

func TestPickerSearchUnicodeAndShortcutIsolation(t *testing.T) {
	m := pickerSetup()
	m.Update(mouseKey("enter"))
	m.View()
	oldClick := targetClick(t, m.mouse, "choice:0")
	m.Update(mouseKey("中文"))
	if len(m.picker.matches()) != 1 || m.picker.selected != 1 {
		t.Fatal("Chinese search failed")
	}
	m.Update(oldClick)
	if m.picker.selected != 1 {
		t.Fatal("stale pre-filter click changed selection")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.picker.query != "中" {
		t.Fatal("backspace corrupted Chinese input")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m.Update(mouseKey("USB"))
	if m.picker.selected != 2 {
		t.Fatal("case insensitive search failed")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, name := range []string{"q", "g", "r", "v", "?"} {
		_, cmd := m.Update(mouseKey(name))
		if cmd != nil || m.picker == nil || m.showHelp || m.driver.showVirtual || m.busy {
			t.Fatalf("search character %q invoked shortcut", name)
		}
	}
	m.Update(mouseKey("enter"))
	if m.picker == nil || m.Settings.Output != "CABLE Input" {
		t.Fatal("empty search result was confirmed")
	}
	m.View()
	m.Update(targetClick(t, m.mouse, "picker:clear"))
	if m.picker.query != "" || len(m.picker.matches()) != 3 {
		t.Fatal("clear search failed")
	}
	m.Update(mouseKey("esc"))
	if m.picker != nil {
		t.Fatal("Escape did not cancel")
	}
}

func TestPickerMissingDeviceAndUnavailableDirection(t *testing.T) {
	m := pickerSetup()
	m.Settings.Output = "Missing device"
	m.openPicker()
	if !strings.Contains(m.View(), "不可用") || m.Settings.Output != "Missing device" {
		t.Fatal("missing device silently replaced")
	}
	m.devices.Outputs = nil
	m.Update(mouseKey("enter"))
	if m.picker == nil || !strings.Contains(m.picker.notice, "失效") || m.Settings.Output != "Missing device" {
		t.Fatal("stale device accepted")
	}
	m.Update(mouseKey("esc"))
	m.openPicker()
	if !strings.Contains(m.View(), "暂无可用设备") {
		t.Fatal("empty list lacks explanation")
	}
	m.Update(mouseKey("enter"))
	if m.picker == nil {
		t.Fatal("empty list confirmed")
	}
	m.Update(mouseKey("esc"))
	m.Settings.Mode = audio.ModePlayback
	m.Update(mouseKey("enter"))
	if m.picker != nil || m.Settings.Output != "Missing device" {
		t.Fatal("disabled microphone direction was editable")
	}
}

func TestPickerCaptureAndNetwork(t *testing.T) {
	m := pickerSetup()
	m.focus = 2
	m.Update(mouseKey("enter"))
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m.Update(mouseKey("enter"))
	if m.Settings.Capture != "loopback: Speakers" || m.Settings.Output != "CABLE Input" {
		t.Fatal("capture selection affected wrong direction")
	}
	m.focus = 4
	m.Settings.Network = "192.168.1.2"
	m.Update(mouseKey("enter"))
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m.Update(mouseKey("enter"))
	if m.Settings.Network != "" {
		t.Fatal("automatic network choice failed")
	}
}

func TestPickerLongListResizePagingAndWheel(t *testing.T) {
	m := pickerSetup()
	m.devices.Outputs = nil
	for i := range 80 {
		m.devices.Outputs = append(m.devices.Outputs, fmt.Sprintf("设备 %02d %s", i, strings.Repeat("长名称", 15)))
	}
	m.Settings.Output = m.devices.Outputs[50]
	for _, size := range [][2]int{{80, 24}, {40, 12}, {28, 10}, {20, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.openPicker()
		view := m.View()
		if lipgloss.Width(view) > size[0] || strings.Count(view, "\n") >= size[1] {
			t.Fatalf("picker exceeds %v: %s", size, view)
		}
		targetClick(t, m.mouse, "choice:50")
		targetClick(t, m.mouse, "picker:cancel")
		m.Update(tea.KeyMsg{Type: tea.KeyEnd})
		m.View()
		targetClick(t, m.mouse, "choice:79")
		m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
		if m.picker.selected != 76 {
			t.Fatal("wheel failed")
		}
		m.View()
		targetClick(t, m.mouse, "choice:76")
		m.Update(tea.KeyMsg{Type: tea.KeyHome})
		m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		if m.picker.selected != m.pickerCapacity() {
			t.Fatal("page down failed")
		}
		m.Update(mouseKey("esc"))
		if m.Settings.Output != m.devices.Outputs[50] {
			t.Fatal("browsing changed device")
		}
	}
}
