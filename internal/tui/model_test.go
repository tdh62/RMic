package tui

import (
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/transport"
	tea "github.com/charmbracelet/bubbletea"
)

type controls struct {
	gain      float64
	muted     bool
	sendGain  float64
	sendMuted bool
}

func (c *controls) SetSource(*audio.Buffer)      {}
func (c *controls) Gain() float64                { return c.gain }
func (c *controls) SetGain(value float64)        { c.gain = value }
func (c *controls) Muted() bool                  { return c.muted }
func (c *controls) ToggleMute()                  { c.muted = !c.muted }
func (c *controls) OutputError() string          { return "" }
func (c *controls) CaptureGain() float64         { return c.sendGain }
func (c *controls) SetCaptureGain(g float64)     { c.sendGain = g }
func (c *controls) CaptureMuted() bool           { return c.sendMuted }
func (c *controls) ToggleCaptureMute()           { c.sendMuted = !c.sendMuted }
func (c *controls) SetCaptureSink(*audio.Buffer) {}
func (c *controls) CaptureError() string         { return "" }

func TestControlsAndSmallTerminal(t *testing.T) {
	c := &controls{gain: 1}
	s, err := transport.NewServer(c, "test output", 0, transport.WithInput(c, "test capture"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ui := New(s, c, []string{"https://192.168.1.2:8443/#token=" + s.Token()}, "test mic", WithConfiguration())
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if !c.muted {
		t.Fatal("mute hotkey did not update output")
	}
	ui, _ = ui.Update(key('+'))
	if c.gain < 1.09 || c.gain > 1.11 {
		t.Fatal("gain hotkey did not update output")
	}
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui, _ = ui.Update(key('+'))
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if !c.sendMuted || c.sendGain < .09 || c.sendGain > .11 || c.gain < 1.09 {
		t.Fatal("direction controls are not independent")
	}
	ui, _ = ui.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if !strings.Contains(ui.View(), "终端空间不足") {
		t.Fatal("QR must not be clipped in a small terminal")
	}
	ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	ui, _ = ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(ui.View(), "等待设备") {
		t.Fatal("escape must return to status")
	}
	for _, label := range []string{"二维码 x", "静音 m", "断开 D", "配置 c", "退出 q", "网络往返"} {
		if !strings.Contains(ui.View(), label) {
			t.Fatalf("missing visible shortcut or buffer: %s", label)
		}
	}
	_, quit := ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("quit hotkey did not exit")
	}
	ui, quit = ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !ui.(model).ConfigurationRequested() || quit == nil {
		t.Fatal("configuration action did not stop the service view")
	}
}
