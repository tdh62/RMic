package tui

import (
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSetupUltraIsAvailableOnlyForPlayback(t *testing.T) {
	settings := launcher.Default()
	settings.Mode = audio.ModePlayback
	settings.Capture = "computer"
	settings.Quality = "weak"
	ui := NewSetup(SetupOptions{Settings: settings, Discover: func() (SetupDevices, error) { return SetupDevices{Captures: []string{"computer"}}, nil }, Save: func(s launcher.Settings) error { return s.Validate() }})
	ui.Update(ui.Init()())
	ui.focus = 3
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	if ui.Settings.Quality != "ultra" {
		t.Fatal("ultra unavailable for playback")
	}
	ui.focus = 0
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	if ui.Settings.Mode != audio.ModePlayback || !strings.Contains(ui.notice, "ultra") {
		t.Fatal("changed into duplex with ultra or failed to explain restriction")
	}
	_, cmd := ui.Update(key('g'))
	if cmd == nil {
		t.Fatal("ultra playback failed to start")
	}
	ui.Update(cmd())
	if !ui.Start {
		t.Fatal("ultra playback settings not accepted")
	}
	for _, mode := range []audio.Mode{audio.ModeMic, audio.ModeDuplex} {
		settings.Mode = mode
		settings.Quality = "weak"
		ui = NewSetup(SetupOptions{Settings: settings, Discover: func() (SetupDevices, error) { return SetupDevices{}, nil }})
		ui.Update(ui.Init()())
		ui.focus = 3
		ui.Update(tea.KeyMsg{Type: tea.KeyRight})
		if ui.Settings.Quality == "ultra" {
			t.Fatal("ultra offered for microphone mode")
		}
		settings.Quality = "ultra"
		if err := settings.Validate(); err == nil {
			t.Fatal("invalid ultra settings accepted for persistence")
		}
	}
}

func TestSetupSteamSelectionAndSavedCableRetention(t *testing.T) {
	steamRender := "Speakers (Steam Streaming Microphone)"
	steamCapture := "Microphone (Steam Streaming Microphone)"
	discover := func() (SetupDevices, error) {
		return SetupDevices{Windows: true, Outputs: []string{"CABLE Input", steamRender}, Captures: []string{"CABLE Output", steamCapture, "loopback:Speakers (Steam Streaming Speakers)"}}, nil
	}
	ui := NewSetup(SetupOptions{Settings: launcher.Default(), Discover: discover, Save: func(launcher.Settings) error { return nil }})
	ui.Update(ui.Init()())
	if ui.Settings.Output != steamRender || !strings.Contains(ui.View(), steamCapture) {
		t.Fatal("fresh Steam pair not selected and explained")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyLeft})
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	if ui.Settings.Capture != "loopback:Speakers (Steam Streaming Speakers)" {
		t.Fatalf("independent Steam audio source not selectable: %q", ui.Settings.Capture)
	}
	_, command := ui.Update(key('g'))
	if command == nil {
		t.Fatal("Steam mic plus Steam Speakers loopback rejected")
	}
	ui.Update(command())
	if !ui.Start {
		t.Fatal("Steam duplex not launched")
	}
	settings := launcher.Default()
	settings.Output = "CABLE Input"
	ui = NewSetup(SetupOptions{Settings: settings, Discover: discover})
	ui.Update(ui.Init()())
	if ui.Settings.Output != "CABLE Input" {
		t.Fatal("saved cable replaced by newly available Steam")
	}
}

func TestSetupWindowsDuplexHasSeparateDeviceFields(t *testing.T) {
	settings := launcher.Default()
	settings.Output = "CABLE Input"
	settings.Capture = "CABLE Output"
	saved := false
	ui := NewSetup(SetupOptions{Settings: settings, Discover: func() (SetupDevices, error) {
		return SetupDevices{Windows: true, Outputs: []string{"CABLE Input"}, Captures: []string{"CABLE Output", "loopback:Speakers"}}, nil
	}, Save: func(launcher.Settings) error { saved = true; return nil }})
	ui.Update(ui.Init()())
	ui.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if ui.Settings.Mode != audio.ModeDuplex {
		t.Fatal("duplex not available in mode selector")
	}
	if !strings.Contains(ui.View(), "麦克风送入") || !strings.Contains(ui.View(), "电脑声音来源") {
		t.Fatal("independent duplex device fields missing")
	}
	_, command := ui.Update(key('g'))
	if command != nil || saved || !strings.Contains(ui.View(), "同一条") {
		t.Fatal("same cable duplex was accepted")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	if ui.Settings.Capture != "loopback:Speakers" || ui.Settings.Output != "CABLE Input" {
		t.Fatal("capture selection changed virtual mic destination")
	}
	_, command = ui.Update(key('g'))
	if command == nil {
		t.Fatal("independent virtual mic and loopback rejected")
	}
	ui.Update(command())
	if !saved || !ui.Start {
		t.Fatal("duplex did not hand off to startup")
	}
}

func TestSetupRejectsSavedPhysicalSpeakerAsMicrophone(t *testing.T) {
	settings := launcher.Default()
	settings.Output = "Speakers"
	ui := NewSetup(SetupOptions{Settings: settings, Discover: func() (SetupDevices, error) {
		return SetupDevices{Windows: true, Outputs: []string{"CABLE Input"}, Captures: []string{"CABLE Output"}}, nil
	}, Save: func(launcher.Settings) error { t.Fatal("physical speaker configuration saved for startup"); return nil }})
	ui.Update(ui.Init()())
	_, command := ui.Update(key('g'))
	if command != nil || ui.Settings.Output != "Speakers" || !strings.Contains(ui.View(), "不能选择物理扬声器") {
		t.Fatal("physical playback accepted or silently replaced")
	}
}

func TestSetupSelectSaveAndStart(t *testing.T) {
	var saved launcher.Settings
	ui := NewSetup(SetupOptions{Settings: launcher.Default(), Manager: &testVirtualManager{},
		Discover: func() (SetupDevices, error) {
			return SetupDevices{Outputs: []string{"Speakers", "CABLE Input (VB-Audio Virtual Cable)"}, Captures: []string{"CABLE Output (VB-Audio Virtual Cable)"}, Networks: []string{"100.101.102.103"}}, nil
		},
		Save: func(settings launcher.Settings) error { saved = settings; return nil }})
	ui.Update(ui.Init()())
	if ui.Settings.Output != "CABLE Input (VB-Audio Virtual Cable)" {
		t.Fatal("did not prefer dedicated cable")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyRight}) // Switch to playback.
	if ui.Settings.Mode != audio.ModePlayback {
		t.Fatal("mode switch did not update direction")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui.Update(tea.KeyMsg{Type: tea.KeyRight}) // voice -> music from balanced.
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	if ui.Settings.Network != "100.101.102.103" {
		t.Fatal("overlay selection failed")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	ui.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9443")})
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ui.Settings.HTTPSPort != 9443 {
		t.Fatal("port editor did not commit")
	}
	_, save := ui.Update(key('g'))
	if save == nil {
		t.Fatal("start did not save settings")
	}
	_, quit := ui.Update(save())
	if !ui.Start || quit == nil || saved.Mode != audio.ModePlayback || saved.HTTPSPort != 9443 {
		t.Fatal("configuration was not saved before launch")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("setup did not hand off to service")
	}
}

func TestSetupMissingDevicesAndDriverReturn(t *testing.T) {
	settings := launcher.Default()
	settings.Output = "Missing cable"
	ui := NewSetup(SetupOptions{Settings: settings, Manager: &testVirtualManager{},
		Discover: func() (SetupDevices, error) { return SetupDevices{Outputs: []string{"Speakers"}}, nil },
		Save:     func(launcher.Settings) error { t.Fatal("invalid device saved for startup"); return nil }})
	ui.Update(ui.Init()())
	if ui.Settings.Output != "Missing cable" {
		t.Fatal("saved device replaced with speakers")
	}
	_, command := ui.Update(key('g'))
	if command != nil || !strings.Contains(ui.View(), "不存在") {
		t.Fatal("startup did not block missing device")
	}
	_, command = ui.Update(key('v'))
	ui.Update(command())
	if !strings.Contains(ui.View(), "虚拟声卡管理") {
		t.Fatal("driver page unavailable before audio initialization")
	}
	_, command = ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ui.driver.showVirtual || command == nil {
		t.Fatal("driver return did not refresh devices")
	}
	ui.Update(command())
	if !strings.Contains(ui.View(), "启动配置") {
		t.Fatal("cannot return from driver page")
	}
}

func TestSetupCaptureChannelSelectionPersists(t *testing.T) {
	var saved launcher.Settings
	ui := NewSetup(SetupOptions{Settings: launcher.Default(), Discover: func() (SetupDevices, error) { return SetupDevices{}, nil }, Save: func(s launcher.Settings) error { saved = s; return nil }})
	ui.Update(ui.Init()())
	ui.Settings.Mode = audio.ModePlayback
	ui.focus = 7
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ui.picker == nil {
		t.Fatal("channel list did not open")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyDown})
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ui.Settings.CaptureChannels != 6 || ui.editing {
		t.Fatal("channel selection edited a port or failed to choose 5.1")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyRight})
	if ui.Settings.CaptureChannels != 8 {
		t.Fatal("7.1 unavailable")
	}
	_, cmd := ui.Update(key('S'))
	if cmd == nil {
		t.Fatal("multichannel settings not saved")
	}
	ui.Update(cmd())
	if saved.CaptureChannels != 8 || !strings.Contains(strings.Join(saved.Args("config"), " "), "--capture-channels 8") {
		t.Fatal("multichannel selection not passed to launcher")
	}
}
