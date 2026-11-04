package tui

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type testVirtualManager struct {
	actions []string
	fail    bool
}

func (v *testVirtualManager) Run(action string) (string, error) {
	v.actions = append(v.actions, action)
	if action == "install" && v.fail {
		return "", errors.New("installation failed")
	}
	return "已完成 " + action, nil
}
func key(value rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}} }
func TestVirtualSteamProviderSwitchAndUninstallConfirmation(t *testing.T) {
	manager := &testVirtualManager{}
	initial := NewVirtual(manager, WithWindowsDriver("steam")).(model)
	initial.windowsDrivers = true
	var ui tea.Model = initial
	ui, _ = ui.Update(ui.Init()())
	if strings.Join(manager.actions, ",") != "steam-status" || !strings.Contains(ui.View(), "当前驱动：Steam Streaming") {
		t.Fatal("wrong initial driver")
	}
	ui, command := ui.Update(key('i'))
	if len(manager.actions) != 1 {
		t.Fatal("Steam install ran synchronously")
	}
	busy, blocked := ui.Update(key('t'))
	if blocked != nil || !busy.(model).virtualSteam {
		t.Fatal("driver switched during operation")
	}
	ui, _ = ui.Update(command())
	if strings.Join(manager.actions, ",") != "steam-status,steam-install,steam-status" {
		t.Fatal(manager.actions)
	}
	ui, _ = ui.Update(key('u'))
	if !strings.Contains(ui.View(), "会影响 Steam Remote Play") {
		t.Fatal("Steam removal scope absent")
	}
	ui, _ = ui.Update(key('n'))
	if len(manager.actions) != 3 {
		t.Fatal("canceled removal changed devices")
	}
	ui, _ = ui.Update(key('u'))
	ui, command = ui.Update(key('y'))
	ui, _ = ui.Update(command())
	if strings.Join(manager.actions, ",") != "steam-status,steam-install,steam-status,steam-uninstall,steam-status" {
		t.Fatal(manager.actions)
	}
	ui, command = ui.Update(key('t'))
	ui, _ = ui.Update(command())
	if ui.(model).virtualSteam || manager.actions[len(manager.actions)-1] != "status" || !strings.Contains(ui.View(), "当前驱动：VB-CABLE") {
		t.Fatal("VB-CABLE provider not restored")
	}
}

func TestVirtualManagerCommandsAndConfirmation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux uninstall confirmation")
	}
	manager := &testVirtualManager{}
	ui := NewVirtual(manager)
	ui, _ = ui.Update(ui.Init()())
	ui, command := ui.Update(key('i'))
	if len(manager.actions) != 1 {
		t.Fatal("operation ran synchronously in update")
	}
	busy, blocked := ui.Update(key('u'))
	if blocked != nil || busy.(model).confirmUninstall {
		t.Fatal("operation can overlap installation")
	}
	ui, _ = ui.Update(command())
	if strings.Join(manager.actions, ",") != "status,install,status" {
		t.Fatal(manager.actions)
	}
	ui, _ = ui.Update(key('u'))
	if !strings.Contains(ui.View(), "确认卸载") {
		t.Fatal("uninstall confirmation absent")
	}
	ui, _ = ui.Update(key('n'))
	if ui.(model).confirmUninstall {
		t.Fatal("cancel did not dismiss confirmation")
	}
	if len(manager.actions) != 3 {
		t.Fatal("canceled uninstall modified devices")
	}
	ui, _ = ui.Update(key('u'))
	ui, command = ui.Update(key('y'))
	ui, _ = ui.Update(command())
	if strings.Join(manager.actions, ",") != "status,install,status,uninstall,status" {
		t.Fatal(manager.actions)
	}
	manager.fail = true
	ui, command = ui.Update(key('i'))
	ui, _ = ui.Update(command())
	if !strings.Contains(ui.View(), "操作失败") || ui.(model).virtualBusy {
		t.Fatal("failed operation not surfaced or UI remained busy")
	}
	_, command = ui.Update(key('q'))
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatal("manager cannot exit")
	}
}
