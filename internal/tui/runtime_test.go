package tui

import (
	"errors"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	tea "github.com/charmbracelet/bubbletea"
)

func TestRunningConfigurationCancelDoesNotApply(t *testing.T) {
	settings := launcher.Default()
	settings.Output = "mic"
	calls := 0
	ui := NewSetup(SetupOptions{Running: true, Settings: settings,
		Discover: func() (SetupDevices, error) { return SetupDevices{Outputs: []string{"mic"}}, nil },
		Save:     func(launcher.Settings) error { calls++; return nil },
		Apply:    func(launcher.Settings) error { calls++; return nil },
	})
	ui.Update(ui.Init()())
	ui.Settings.Quality = "music"
	if !strings.Contains(ui.View(), "传音保持运行") {
		t.Fatal("missing live-edit explanation")
	}
	_, quit := ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit == nil || ui.Start || calls != 0 {
		t.Fatal("cancel applied draft")
	}
}
func TestRunningConfigurationApplyFailureRetainsEditor(t *testing.T) {
	settings := launcher.Default()
	settings.Output = "mic"
	ui := NewSetup(SetupOptions{Running: true, Settings: settings,
		Discover: func() (SetupDevices, error) { return SetupDevices{Outputs: []string{"mic"}}, nil },
		Save:     func(launcher.Settings) error { t.Fatal("apply used save-only callback"); return nil },
		Apply:    func(launcher.Settings) error { return errors.New("device busy") },
	})
	ui.Update(ui.Init()())
	_, apply := ui.Update(key('g'))
	if apply == nil {
		t.Fatal("no apply command")
	}
	_, quit := ui.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quit != nil {
		t.Fatal("closed editor while async apply owns runtime")
	}
	_, quit = ui.Update(apply())
	if quit != nil || ui.Start || ui.busy || !strings.Contains(ui.View(), "device busy") {
		t.Fatal("failed apply closed editor")
	}
}
func TestDefaultModeSelectorDoesNotChangeDirections(t *testing.T) {
	settings := launcher.Default()
	settings.Mode = audio.ModeDuplex
	ui := NewSetup(SetupOptions{Settings: settings, Discover: func() (SetupDevices, error) { return SetupDevices{}, nil }})
	ui.Update(ui.Init()())
	ui.focus = 8
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	if ui.Settings.Mode != audio.ModeDuplex || ui.Settings.SessionMode() != audio.ModeMic {
		t.Fatal("default selection changed permitted directions")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	if ui.Settings.SessionMode() != audio.ModePlayback {
		t.Fatal("playback default unavailable")
	}
	ui.focus = 0
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	if ui.Settings.Mode != audio.ModeMic || ui.Settings.SessionMode() != audio.ModeMic {
		t.Fatal("direction restriction left invalid default")
	}
}
