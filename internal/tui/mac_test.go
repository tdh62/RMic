package tui

import (
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
)

func TestMacSetupSelectsIndependentCablesAndRejectsSameDevice(t *testing.T) {
	settings := launcher.Default()
	settings.Mode = audio.ModeDuplex
	saves := 0
	ui := NewSetup(SetupOptions{Settings: settings,
		Discover: func() (SetupDevices, error) {
			return SetupDevices{MacOS: true, Outputs: []string{"BlackHole 2ch", "BlackHole 16ch"}, Captures: []string{"BlackHole 2ch", "BlackHole 16ch"}}, nil
		},
		Save: func(launcher.Settings) error { saves++; return nil },
	})
	ui.Update(ui.Init()())
	if ui.Settings.Output != "BlackHole 2ch" || ui.Settings.Capture != "BlackHole 16ch" {
		t.Fatalf("unexpected defaults: %+v", ui.Settings)
	}
	ui.Settings.Capture = "BlackHole 2ch"
	_, cmd := ui.Update(key('g'))
	if cmd != nil || saves != 0 || !strings.Contains(ui.View(), "同一 BlackHole") {
		t.Fatal("unsafe duplex configuration was accepted")
	}
	ui.Settings.Capture = "BlackHole 16ch"
	_, cmd = ui.Update(key('g'))
	if cmd == nil {
		t.Fatal("independent BlackHole channels were rejected")
	}
	ui.Update(cmd())
	if saves != 1 || !ui.Start {
		t.Fatal("valid mac configuration did not start")
	}
}
