package tui

import (
	"fmt"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/internal/transport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func targetClick(t *testing.T, frame *mouseFrame, target string) tea.MouseMsg {
	t.Helper()
	for cell, id := range frame.cells {
		if id == target {
			msg := tea.MouseMsg{X: cell.x, Y: cell.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
			frame.target(msg, frame.page)
			msg.Action = tea.MouseActionRelease
			return msg
		}
	}
	t.Fatalf("target %q is not visible on %s", target, frame.page)
	return tea.MouseMsg{}
}

func TestMouseLayoutMatchesWrappedChineseAndClippedRows(t *testing.T) {
	text := "中文设备 e\u0301 🎤 abcdefghijklmnopqrstuvwxyz"
	marked := clickable("device", text)
	style := lipgloss.NewStyle().Width(12)
	wrapped := style.Render(marked)
	frame := &mouseFrame{}
	view := frame.render(wrapped, "test")
	if ansi.Strip(view) != ansi.Strip(style.Render(text)) || strings.Contains(view, "rmic-click:") {
		t.Fatalf("markers changed layout or escaped into output: %q", view)
	}
	if frame.cells[mouseCell{0, 0}] != "device" || frame.cells[mouseCell{1, 0}] != "device" {
		t.Fatal("both cells of a Chinese glyph must be clickable")
	}
	lines := strings.Split(wrapped, "\n")
	frame.render(strings.Join(lines[1:], "\n"), "test")
	if frame.cells[mouseCell{0, 0}] != "device" {
		t.Fatal("clipped opening row lost the target on subsequent rows")
	}
}

func TestMouseSetupControlsEditingAndNarrowActions(t *testing.T) {
	saves := 0
	ui := NewSetup(SetupOptions{Settings: launcher.Default(), Discover: func() (SetupDevices, error) { return SetupDevices{}, nil }, Save: func(launcher.Settings) error { saves++; return nil }})
	ui.Update(ui.Init()())
	ui.View()
	ui.Update(targetClick(t, ui.mouse, "field:0"))
	if ui.picker == nil {
		t.Fatal("mode row did not open selection")
	}
	ui.Update(mouseKey("down"))
	ui.Update(mouseKey("enter"))
	if ui.Settings.Mode != "playback" {
		t.Fatal("mode selection failed")
	}
	ui.View()
	ui.Update(targetClick(t, ui.mouse, "field:5"))
	if !ui.editing || ui.focus != 5 {
		t.Fatal("click did not edit HTTPS port")
	}
	ui.View()
	ui.edit = "12345"
	ui.Update(targetClick(t, ui.mouse, "key:enter"))
	if ui.editing || ui.Settings.HTTPSPort != 12345 {
		t.Fatal("click did not commit port")
	}
	ui.Update(tea.WindowSizeMsg{Width: 28, Height: 12})
	ui.View()
	_, cmd := ui.Update(targetClick(t, ui.mouse, "action:1"))
	if cmd == nil {
		t.Fatal("save was not scheduled")
	}
	ui.Update(cmd())
	if saves != 1 {
		t.Fatal("save must execute once")
	}
}

func TestMouseRuntimeDirectionGainReleaseAndModalIsolation(t *testing.T) {
	control := &controls{gain: 1, sendGain: 1}
	server, err := transport.NewServer(control, "output", 0, transport.WithInput(control, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var ui tea.Model = New(server, control, []string{"https://192.168.1.2:8443/", "https://100.100.100.100:8443/"}, "mic")
	ui.View()
	frame := ui.(model).mouse
	ui, _ = ui.Update(targetClick(t, frame, "send"))
	ui.View()
	click := targetClick(t, frame, "key:+")
	ui, _ = ui.Update(click)
	click.Action = tea.MouseActionRelease
	ui, _ = ui.Update(click)
	if control.sendGain < 1.09 || control.sendGain > 1.11 || control.gain != 1 {
		t.Fatal("gain repeated or changed the wrong direction")
	}
	ui.View()
	stale := targetClick(t, frame, "action:1") // mute
	ui, _ = ui.Update(mouseKey("?"))
	ui, _ = ui.Update(stale)
	if control.sendMuted {
		t.Fatal("stale main-page click leaked through help")
	}
	ui.View()
	ui, _ = ui.Update(targetClick(t, frame, "key:esc"))
	ui.View()
	ui, _ = ui.Update(targetClick(t, frame, "action:0"))
	ui.View()
	ui, _ = ui.Update(targetClick(t, frame, "key:right"))
	if ui.(model).urlIndex != 1 {
		t.Fatal("QR next button did not change URL")
	}
	ui.View()
	ui, _ = ui.Update(targetClick(t, frame, "key:esc"))
	if ui.(model).showQR {
		t.Fatal("QR return button failed")
	}
}

func TestMousePairingSelectionDoesNotAuthorizeAndExpiredTargetIsIgnored(t *testing.T) {
	manager, err := pairing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, _ := manager.Create("first", "127.0.0.1", true)
	second, _ := manager.Create("second", "127.0.0.2", true)
	initial := model{mouse: &mouseFrame{}, pairing: manager, showPairing: true, width: 90, height: 24, pairSelected: first.ID}
	var ui tea.Model = initial
	ui.View()
	frame := initial.mouse
	click := targetClick(t, frame, "pair:"+second.ID)
	ui, cmd := ui.Update(click)
	if cmd != nil || ui.(model).pairSelected != second.ID {
		t.Fatal("click must only select the device")
	}
	result, _ := manager.Poll(second.ID, second.Secret)
	if result.State != "pending" {
		t.Fatal("selection authorized a device")
	}
	ui.View()
	allow := targetClick(t, frame, "action:0")
	if err := manager.Decide(second.ID, false); err != nil {
		t.Fatal(err)
	}
	ui, cmd = ui.Update(allow)
	if cmd != nil || !strings.Contains(ui.(model).pairNotice, "已过期") {
		t.Fatal("stale allow must not approve another request")
	}
	result, _ = manager.Poll(first.ID, first.Secret)
	if result.State != "pending" {
		t.Fatal("another request was approved")
	}
}

func TestMouseDriverConfirmationBusyAndToggle(t *testing.T) {
	manager := &testVirtualManager{}
	initial := NewVirtual(manager).(model)
	initial.windowsDrivers = true
	initial.virtualSteam = true
	var ui tea.Model = initial
	ui, _ = ui.Update(ui.Init()())
	ui.View()
	frame := initial.mouse
	ui, cmd := ui.Update(targetClick(t, frame, "action:2"))
	if cmd != nil || !ui.(model).confirmUninstall {
		t.Fatal("uninstall must ask for confirmation")
	}
	ui.View()
	ui, cmd = ui.Update(targetClick(t, frame, "action:0"))
	if cmd != nil || ui.(model).confirmUninstall {
		t.Fatal("cancel ran uninstall")
	}
	ui.View()
	ui, _ = ui.Update(targetClick(t, frame, "action:2"))
	ui.View()
	confirm := targetClick(t, frame, "action:1")
	ui, cmd = ui.Update(confirm)
	if cmd == nil {
		t.Fatal("explicit confirmation failed")
	}
	_, duplicate := ui.Update(confirm)
	if duplicate != nil {
		t.Fatal("busy state accepted another uninstall")
	}
	ui, _ = ui.Update(cmd())
	if strings.Join(manager.actions, ",") != "steam-status,steam-uninstall,steam-status" {
		t.Fatal(manager.actions)
	}
	ui, cmd = ui.Update(tea.KeyMsg{Type: tea.KeyF2})
	if cmd == nil || frame.enabled() {
		t.Fatal("F2 did not disable mouse")
	}
	ui.View()
	ui, cmd = ui.Update(targetClick(t, frame, "action:1"))
	if cmd != nil {
		t.Fatal("disabled mouse accepted installation")
	}
	ui, cmd = ui.Update(tea.KeyMsg{Type: tea.KeyF2})
	if cmd == nil || !frame.enabled() {
		t.Fatal("F2 did not restore mouse")
	}
}

func TestMouseSetupScrollAndResize(t *testing.T) {
	ui := NewSetup(SetupOptions{Settings: launcher.Default()})
	ui.busy = false
	ui.devices.Outputs = []string{strings.Repeat("长设备名称", 20)}
	ui.Settings.Output = ui.devices.Outputs[0]
	for _, width := range []int{80, 28, 40} {
		ui.Update(tea.WindowSizeMsg{Width: width, Height: 12})
		ui.focus = 5
		view := ui.View()
		if strings.Count(view, "\n") >= 12 || lipgloss.Width(view) > width {
			t.Fatalf("layout exceeds %d columns", width)
		}
		ui.Update(targetClick(t, ui.mouse, "field:5"))
		if !ui.editing {
			t.Fatal("scrolled field was not clickable")
		}
		ui.Update(mouseKey("esc"))
	}
	ui.focus = 0
	ui.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if ui.focus != 1 {
		t.Fatal("wheel did not browse fields")
	}
	ui.Update(mouseKey("?"))
	ui.View()
	ui.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if ui.helpScroll != 3 {
		t.Fatal("wheel did not scroll help")
	}
	ui.View()
	ui.Update(targetClick(t, ui.mouse, "key:esc"))
	if ui.showHelp {
		t.Fatal("help return was not clickable")
	}
}

func TestMouseActionsSurviveCompactBar(t *testing.T) {
	for selected := range 5 {
		frame := &mouseFrame{}
		view := frame.render(page(nil, actionBar([]string{"开始", "保存配置", "驱动", "刷新", "退出"}, selected, 18), 18, 8, 0), "test")
		if strings.Contains(view, "rmic-click:") {
			t.Fatal("marker leaked")
		}
		targetClick(t, frame, fmt.Sprintf("action:%d", selected))
		for index := range 5 {
			targetClick(t, frame, fmt.Sprintf("action:%d", index))
		}
	}
}

func TestMouseScrollClampsAndImmediatelyReversesAtBottom(t *testing.T) {
	initial := NewVirtual(&testVirtualManager{}).(model)
	initial.virtualBusy = false
	initial.width, initial.height = 40, 12
	initial.virtualResult = strings.Repeat("一行操作结果\n", 30)
	var ui tea.Model = initial
	before := ui.View()
	ui, _ = ui.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	after := ui.View()
	if before == after || ui.(model).virtualScroll != 3 {
		t.Fatal("wheel must move the viewport immediately")
	}
	for range 50 {
		ui, _ = ui.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
		ui.View()
	}
	frame := initial.mouse
	if ui.(model).virtualScroll != frame.scrollLimit {
		t.Fatal("scroll offset grew past the end of the content")
	}
	bottom := frame.scrollLimit
	ui, _ = ui.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if ui.(model).virtualScroll != bottom-3 {
		t.Fatal("reversing the wheel at the bottom must move immediately")
	}
	ui.View()
	targetClick(t, frame, "key:?") // footer stays reachable while scrolling
}
