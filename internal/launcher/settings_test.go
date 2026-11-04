package launcher

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/shortcuts"
)

func TestDuplexPreferencesPassBothAudioEndpoints(t *testing.T) {
	dir := t.TempDir()
	settings := Default()
	settings.Mode = audio.ModeDuplex
	settings.Output = "CABLE Input"
	settings.Capture = "loopback:Headphones"
	if err := settings.ValidateDevices([]string{settings.Output}, []string{settings.Capture}); err != nil {
		t.Fatal(err)
	}
	if err := settings.ValidateDevices([]string{settings.Output}, nil); err == nil {
		t.Fatal("duplex accepted missing computer capture")
	}
	if err := Save(dir, settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil || !reflect.DeepEqual(loaded, settings) {
		t.Fatalf("duplex preferences lost: %+v %v", loaded, err)
	}
	flags := map[string]string{}
	args := loaded.Args(dir)
	for index := 0; index < len(args); index += 2 {
		flags[args[index]] = args[index+1]
	}
	if flags["--device"] != settings.Output || flags["--capture"] != settings.Capture || flags["--mode"] != "duplex" {
		t.Fatalf("duplex endpoints not passed independently: %q", args)
	}
}

func TestShortcutIntervalPersistence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "desktop.json"), []byte(`{"shortcuts":{"enabled":false}}`), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := Load(dir)
	if err != nil || settings.Shortcuts.KeyIntervalMS != 20 {
		t.Fatalf("legacy settings default: %+v %v", settings, err)
	}
	for _, ms := range []int{0, 20, 100} {
		settings.Shortcuts.KeyIntervalMS = ms
		if err := Save(dir, settings); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(dir)
		if err != nil || loaded.Shortcuts.KeyIntervalMS != ms {
			t.Fatalf("interval lost: %+v %v", loaded, err)
		}
	}
}

func TestSettingsRoundTripAndInvalidConfiguration(t *testing.T) {
	dir := t.TempDir()
	settings, err := Load(dir)
	if err != nil || settings.Mode != audio.ModeMic {
		t.Fatalf("first launch: %+v %v", settings, err)
	}
	settings.Output = "CABLE Input (VB-Audio Virtual Cable)"
	settings.Network = "100.101.102.103"
	settings.Shortcuts.Buttons[0] = shortcuts.Button{Name: "按住说话", Mode: "hold", Action: "AltLeft+ShiftLeft"}
	settings.Shortcuts.Buttons[2] = shortcuts.Button{Name: "固定回复", Mode: "tap", Action: "text:收到🙂\nEnter"}
	if err = Save(dir, settings); err != nil {
		t.Fatal(err)
	}
	saved, err := Load(dir)
	if err != nil || !reflect.DeepEqual(settings, saved) {
		t.Fatalf("settings lost: %+v %v", saved, err)
	}
	if err = saved.ValidateDevices([]string{saved.Output}, nil); err != nil {
		t.Fatal(err)
	}
	if err = saved.ValidateDevices([]string{"Speakers"}, nil); err == nil {
		t.Fatal("missing saved device silently replaced")
	}
	expected := []string{"--mode", "mic", "--virtual", "off", "--quality", "balanced", "--listen", ":8443", "--udp-port", "50000", "--config-dir", dir, "--audio-idle-timeout", "30s", "--device", saved.Output, "--bind-ips", saved.Network}
	if !reflect.DeepEqual(saved.Args(dir), expected) {
		t.Fatalf("unexpected startup flags: %q", saved.Args(dir))
	}
	original, err := os.ReadFile(filepath.Join(dir, "desktop.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings.UDPPort = 0
	if err = Save(dir, settings); err == nil {
		t.Fatal("invalid port saved")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "desktop.json"))
	if string(original) != string(after) {
		t.Fatal("invalid save damaged existing preferences")
	}
	if err = os.WriteFile(filepath.Join(dir, "desktop.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := Load(dir)
	if err == nil || recovered.Mode != audio.ModeMic {
		t.Fatal("corrupt config did not yield recoverable defaults")
	}
}
