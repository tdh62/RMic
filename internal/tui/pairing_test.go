package tui

import (
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/pairing"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPairingApprovalHistoryAndRevocation(t *testing.T) {
	manager, _ := pairing.Open(t.TempDir())
	ticket, _ := manager.Create("我的手机", "100.101.102.103", true)
	initial := model{pairing: manager, showPairing: true, width: 110, height: 30}
	initial.selectPairFirst()
	var ui tea.Model = initial
	if !strings.Contains(ui.View(), ticket.Code) || !strings.Contains(ui.View(), "100.101.102.103") {
		t.Fatal("approval cannot verify name/address/code")
	}
	ui, command := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(manager.Devices()) != 0 || command == nil {
		t.Fatal("grant ran synchronously")
	}
	ui, _ = ui.Update(command())
	result, _ := manager.Poll(ticket.ID, ticket.Secret)
	if !manager.Valid(result.Token) {
		t.Fatal("local approval did not grant device")
	}
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui, _ = ui.Update(key('r'))
	if !strings.Contains(ui.View(), "撤销当前设备") {
		t.Fatal("history revoke did not ask confirmation")
	}
	ui, _ = ui.Update(key('n'))
	if !manager.Valid(result.Token) {
		t.Fatal("canceled revoke changed authorization")
	}
	ui, _ = ui.Update(key('r'))
	ui, command = ui.Update(key('y'))
	ui, _ = ui.Update(command())
	if manager.Valid(result.Token) {
		t.Fatal("revoked device remains valid")
	}
}
func TestPairingDoesNotApproveShiftedRequest(t *testing.T) {
	manager, _ := pairing.Open(t.TempDir())
	first, _ := manager.Create("first", "1", true)
	second, _ := manager.Create("second", "2", true)
	ui := model{pairing: manager, showPairing: true, pairSelected: first.ID, width: 100, height: 30}
	if err := manager.Decide(first.ID, false); err != nil {
		t.Fatal(err)
	}
	updated, command := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || !strings.Contains(updated.View(), "已过期") {
		t.Fatal("shifted row silently approved")
	}
	result, _ := manager.Poll(second.ID, second.Secret)
	if result.State != "pending" {
		t.Fatal("unselected device approved")
	}
}
