package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/transport"
	"cnb.cool/tdh6/RMic/internal/virtual"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type inspectedManager struct {
	items []virtual.DriverInfo
	calls []string
	fail  string
}

func (v *inspectedManager) Inspect() []virtual.DriverInfo {
	v.calls = append(v.calls, "inspect")
	return append([]virtual.DriverInfo(nil), v.items...)
}
func (v *inspectedManager) Run(action string) (string, error) {
	v.calls = append(v.calls, action)
	if action == v.fail {
		return "", errors.New("UAC cancelled")
	}
	return "需要重启", nil
}
func TestDriverListBatchCancellationAndRunningGuard(t *testing.T) {
	manager := &inspectedManager{items: []virtual.DriverInfo{{Name: "Steam", Prefix: "steam-"}, {Name: "Cable", Prefix: "cable-a-"}}, fail: "steam-install"}
	var ui tea.Model = NewVirtual(manager)
	ui, _ = ui.Update(ui.Init()())
	ui, _ = ui.Update(mouseKey(" "))
	ui, _ = ui.Update(mouseKey("down"))
	ui, _ = ui.Update(mouseKey(" "))
	ui, cmd := ui.Update(mouseKey("i"))
	if cmd != nil || len(manager.calls) != 1 {
		t.Fatal("installation bypassed confirmation")
	}
	ui, cmd = ui.Update(mouseKey("y"))
	for cmd != nil {
		ui, cmd = ui.Update(cmd())
	}
	if !reflect.DeepEqual(manager.calls, []string{"inspect", "steam-install", "inspect"}) {
		t.Fatal(manager.calls)
	}
	if !strings.Contains(ui.View(), "操作失败") {
		t.Fatal("failure hidden")
	}
	m := ui.(model)
	m.virtualReadOnly = true
	m.canMaintain = true
	ui, cmd = m.Update(mouseKey("i"))
	if cmd != nil {
		t.Fatal("maintenance started while service active")
	}
	ui, cmd = ui.Update(mouseKey("enter"))
	if cmd != nil || ui.(model).maintenanceRequested {
		t.Fatal("unsafe default confirmation")
	}
	ui, _ = ui.Update(mouseKey("i"))
	ui, cmd = ui.Update(mouseKey("y"))
	if !ui.(model).maintenanceRequested || cmd == nil || len(manager.calls) != 3 {
		t.Fatal("maintenance did not hand off without mutation")
	}
}
func TestScopedSaveAndDraftReturn(t *testing.T) {
	saved := launcher.Default()
	saved.Output = "mic"
	draft := saved
	var written launcher.Settings
	m := NewSetup(SetupOptions{Running: true, Settings: saved, Draft: &draft, Discover: func() (SetupDevices, error) { return SetupDevices{Outputs: []string{"mic"}}, nil }, Save: func(value launcher.Settings) error { written = value; return nil }})
	m.Update(m.Init()())
	m.Settings.HTTPSPort = 0
	m.Settings.Quality = "music"
	_, cmd := m.Update(mouseKey("S"))
	if cmd == nil {
		t.Fatal("invalid network blocked audio save")
	}
	m.Update(cmd())
	if written.HTTPSPort != 8443 || written.Quality != "music" || m.Settings.HTTPSPort != 0 {
		t.Fatal("save consumed another scope")
	}
	m.Settings.Quality = "voice"
	m.Update(mouseKey("esc"))
	reopened := NewSetup(SetupOptions{Settings: written, Draft: &draft})
	if reopened.Settings.Quality != "voice" || reopened.Settings.HTTPSPort != 0 {
		t.Fatal("return lost drafts")
	}
	reopened.focus = 4
	reopened.resetScope()
	if reopened.Settings.Quality != "voice" || reopened.Settings.HTTPSPort != 8443 {
		t.Fatal("reset crossed scopes")
	}
}

func TestApplyConfirmsOnlyCurrentScope(t *testing.T) {
	saved := launcher.Default()
	saved.Output = "mic"
	calls := 0
	m := NewSetup(SetupOptions{Running: true, Settings: saved, Discover: func() (SetupDevices, error) { return SetupDevices{Outputs: []string{"mic"}}, nil }, ApplyScope: func(value launcher.Settings, scope string) error {
		calls++
		if scope != "network" || value.Output != "mic" || value.HTTPSPort != 9443 {
			t.Fatal("apply consumed audio draft")
		}
		return nil
	}})
	m.Update(m.Init()())
	m.Settings.Output = "invalid draft"
	m.Settings.HTTPSPort = 9443
	m.focus = 5
	_, cmd := m.Update(mouseKey("g"))
	if cmd != nil || !m.confirmApply {
		t.Fatal("network restart bypassed confirmation")
	}
	m.Update(mouseKey("enter"))
	if calls != 0 || m.confirmApply {
		t.Fatal("default confirmation was destructive")
	}
	m.Update(mouseKey("g"))
	_, cmd = m.Update(mouseKey("y"))
	if cmd == nil {
		t.Fatal("confirmed apply missing")
	}
	m.Update(cmd())
	if calls != 1 || m.Settings.Output != "invalid draft" {
		t.Fatal("application lost other draft")
	}
}
func TestFirstUseNeverSelectsOrInstallsWithoutConfirmation(t *testing.T) {
	manager := &inspectedManager{items: []virtual.DriverInfo{{Name: "Steam"}}}
	m := NewSetup(SetupOptions{Settings: launcher.Default(), Manager: manager, Discover: func() (SetupDevices, error) { return SetupDevices{Outputs: []string{"CABLE Input"}}, nil }})
	m.Update(m.Init()())
	if !m.guide || m.Settings.Output != "" || len(manager.calls) != 0 {
		t.Fatal("first use silently selected or installed")
	}
	_, cmd := m.Update(mouseKey("enter"))
	m.Update(cmd())
	if !m.driver.showVirtual || !reflect.DeepEqual(manager.calls, []string{"inspect"}) {
		t.Fatal("guide did not inspect")
	}
	_, cmd = m.Update(mouseKey("esc"))
	m.Update(cmd())
	if m.guide || m.Settings.Output != "" {
		t.Fatal("return restarted guide or guessed an endpoint")
	}
}
func TestWorkspaceFitsAndNumericGainIsScoped(t *testing.T) {
	c := &controls{gain: 1, sendGain: 1}
	server, err := transport.NewServer(c, "mic", 0, transport.WithInput(c, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var ui tea.Model = New(server, c, []string{"https://192.168.1.2:8443"}, "mic")
	for _, size := range [][2]int{{80, 24}, {100, 30}, {60, 20}} {
		ui, _ = ui.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ui.View()
		if lipgloss.Width(view) > size[0] || strings.Count(view, "\n") >= size[1] {
			t.Fatalf("layout overflow %v", size)
		}
		if size[0] == 80 && (!strings.Contains(view, "mic") || !strings.Contains(view, "capture")) {
			t.Fatal("normal duplex route scrolled away")
		}
	}
	ui, _ = ui.Update(mouseKey("g"))
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	ui, _ = ui.Update(mouseKey("250"))
	ui, _ = ui.Update(mouseKey("enter"))
	if c.gain != 2.5 || c.sendGain != 1 {
		t.Fatal("numeric gain crossed directions")
	}
	ui, _ = ui.Update(mouseKey("g"))
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	ui, _ = ui.Update(mouseKey("999"))
	ui, _ = ui.Update(mouseKey("enter"))
	if !ui.(model).editGain || c.gain != 2.5 {
		t.Fatal("invalid gain accepted")
	}
	ui, _ = ui.Update(mouseKey("esc"))
	if ui.(model).editGain {
		t.Fatal("cancel did not exit gain editing")
	}
}
func TestShortcutStepEditsAreTransactional(t *testing.T) {
	m := NewSetup(SetupOptions{Settings: launcher.Default()})
	m.busy = false
	m.Update(mouseKey("K"))
	m.Update(mouseKey("1"))
	m.Update(mouseKey("f4"))
	m.Update(mouseKey("a"))
	m.Update(mouseKey("w"))
	m.Update(mouseKey("["))
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := m.shortcutEditor.draft.Buttons[0].Action; got != "wait:100\nControlLeft+KeyC" {
		t.Fatal(got)
	}
	if m.Settings.Shortcuts.Buttons[0].Action != "" {
		t.Fatal("step editor saved configuration implicitly")
	}
	m.Update(mouseKey("f4"))
	m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	m.Update(mouseKey("esc"))
	if m.shortcutEditor.draft.Buttons[0].Action != "wait:100\nControlLeft+KeyC" {
		t.Fatal("cancel changed steps")
	}
}
