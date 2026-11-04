package tui

import (
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/internal/transport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/skip2/go-qrcode"
)

func TestQRPageContainsCodeCurrentLinkAndControls(t *testing.T) {
	control := &controls{gain: 1}
	server, err := transport.NewServer(control, "test output", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var ui tea.Model = New(server, control, []string{"https://192.168.1.2:8443/", "https://100.101.102.103:8443/"}, "test mic")
	ui, _ = ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := ui.View()
	if !strings.Contains(view, "扫码连接 · 1/2") || strings.Contains(view, "终端空间不足") {
		t.Fatalf("short URL QR should fit 80x24: %s", view)
	}
	if !strings.Contains(view, "https://192.168.1.2:8443/") || strings.Contains(view, "https://100.101.102.103:8443/") {
		t.Fatal("QR page must show only the current link")
	}
	for _, text := range []string{"RMic", "等待设备", "test output", "证书", "应用麦克风"} {
		if strings.Contains(view, text) {
			t.Fatalf("QR page contains unrelated text: %s", text)
		}
	}
	if strings.Count(view, "\n") >= 24 || lipgloss.Width(view) > 80 {
		t.Fatal("QR page exceeds terminal")
	}
	ui, _ = ui.Update(key('d'))
	if !strings.Contains(ui.View(), "2/2") || ui.(model).urlIndex != 1 {
		t.Fatal("D navigation did not change QR address")
	}
	if !strings.Contains(ui.View(), "https://100.101.102.103:8443/") || strings.Contains(ui.View(), "https://192.168.1.2:8443/") {
		t.Fatal("switching QR address must also switch its displayed link")
	}
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ui.(model).showQR {
		t.Fatal("Enter did not return")
	}
}

func TestQRLongIPv6LinkWrapsBelowCode(t *testing.T) {
	url := "https://[fd00:1234:5678:abcd::1234]:8443/" + strings.Repeat("long-path/", 14)
	code, err := qrcode.New(url, qrcode.Low)
	if err != nil {
		t.Fatal(err)
	}
	m := model{mouse: &mouseFrame{}, showQR: true, urls: []string{url}, qr: code.ToSmallString(false), width: 140, height: 100}
	view := m.View()
	if strings.Contains(view, "终端空间不足") {
		t.Fatal("QR and wrapped link should fit")
	}
	if !strings.Contains(strings.Join(strings.Fields(view), ""), url) {
		t.Fatal("wrapped link was truncated or changed")
	}
	if strings.Index(view, "https://") <= strings.LastIndexAny(view, "▀▄█") {
		t.Fatal("link must appear below the complete QR")
	}
	if lipgloss.Width(view) > m.width || strings.Count(view, "\n") >= m.height {
		t.Fatal("QR page exceeds terminal")
	}
}

func TestSetupNavigationAndEnterActions(t *testing.T) {
	saves := 0
	ui := NewSetup(SetupOptions{Settings: launcher.Default(), Discover: func() (SetupDevices, error) { return SetupDevices{}, nil }, Save: func(launcher.Settings) error { saves++; return nil }})
	ui.Update(ui.Init()())
	ui.Update(key('s'))
	if ui.focus != 1 || saves != 0 {
		t.Fatal("S must navigate without saving")
	}
	ui.Update(key('w'))
	ui.Update(key('d'))
	if ui.Settings.Mode != "playback" {
		t.Fatal("D did not change field value")
	}
	ui.Update(key('a'))
	if ui.Settings.Mode != "mic" {
		t.Fatal("A did not reverse field value")
	}
	for range 10 {
		ui.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if ui.focus != 10 {
		t.Fatal("save action not reachable with arrows")
	}
	_, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || saves != 0 {
		t.Fatal("Enter should schedule save")
	}
	ui.Update(cmd())
	if saves != 1 || !strings.Contains(ui.View(), "已保存") {
		t.Fatal("focused save action failed")
	}
	ui.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	ui.focus = 6
	view := ui.View()
	if !strings.Contains(view, "音频端口") || strings.Count(view, "\n") >= 10 {
		t.Fatalf("focused field or controls clipped: %s", view)
	}
	ui.Update(key('?'))
	if !strings.Contains(ui.View(), "?/Esc 返回") {
		t.Fatal("help unreachable")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ui.showHelp {
		t.Fatal("help did not return")
	}
}

func TestDriverNavigationAndSafeEnterConfirmation(t *testing.T) {
	manager := &testVirtualManager{}
	initial := NewVirtual(manager).(model)
	initial.windowsDrivers = true
	initial.virtualSteam = true
	var ui tea.Model = initial
	ui, _ = ui.Update(ui.Init()())
	ui, _ = ui.Update(key('d'))
	ui, _ = ui.Update(key('d'))
	ui, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !ui.(model).confirmUninstall {
		t.Fatal("uninstall action not focused")
	}
	ui, cmd = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || ui.(model).confirmUninstall || len(manager.actions) != 1 {
		t.Fatal("default confirmation must cancel")
	}
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	ui, _ = ui.Update(key('a'))
	ui, cmd = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("explicit confirmation not executed")
	}
	ui, _ = ui.Update(cmd())
	if strings.Join(manager.actions, ",") != "steam-status,steam-uninstall,steam-status" {
		t.Fatal(manager.actions)
	}
	if strings.Contains(ui.View(), "Linux：") || strings.Contains(ui.View(), "macOS：") {
		t.Fatal("unrelated platform instructions visible")
	}
}

func TestPairingWASDDoesNotAuthorizeAndEnterUsesFocusedAction(t *testing.T) {
	manager, _ := pairing.Open(t.TempDir())
	ticket, _ := manager.Create("phone", "127.0.0.1", true)
	initial := model{pairing: manager, showPairing: true, width: 80, height: 24}
	initial.selectPairFirst()
	var ui tea.Model = initial
	ui, _ = ui.Update(key('d')) // only this time
	ui, _ = ui.Update(key('s'))
	ui, _ = ui.Update(key('w'))
	result, _ := manager.Poll(ticket.ID, ticket.Secret)
	if result.State != "pending" {
		t.Fatal("navigation authorized a device")
	}
	ui, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("focused grant not executed")
	}
	ui, _ = ui.Update(cmd())
	result, _ = manager.Poll(ticket.ID, ticket.Secret)
	if result.State != "approved" || result.Remember {
		t.Fatal("wrong grant scope")
	}
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	if strings.Contains(ui.View(), "仅本次 ]") || strings.Contains(ui.View(), "核对手机确认码") {
		t.Fatal("history page contains request instructions")
	}
}
