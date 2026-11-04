package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestDriverCycleTargetsSeparateCablePackages(t *testing.T) {
	manager := &testVirtualManager{}
	initial := NewVirtual(manager, WithWindowsDriver("steam")).(model)
	initial.windowsDrivers = true
	var ui tea.Model = initial
	ui, _ = ui.Update(ui.Init()())
	for _, variant := range []struct{ name, prefix string }{{"VB-CABLE", ""}, {"VB-CABLE A", "cable-a-"}, {"VB-CABLE B", "cable-b-"}, {"Steam Streaming", "steam-"}} {
		next, command := ui.Update(key('t'))
		ui, _ = next.Update(command())
		if !strings.Contains(ui.View(), "当前驱动："+variant.name) {
			t.Fatal("wrong visible provider")
		}
		next, command = ui.Update(key('i'))
		ui, _ = next.Update(command())
		actions := manager.actions[len(manager.actions)-3:]
		expected := variant.prefix + "status," + variant.prefix + "install," + variant.prefix + "status"
		if strings.Join(actions, ",") != expected {
			t.Fatalf("wrong package operation: %v", actions)
		}
	}
}
