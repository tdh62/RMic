package launcher

import (
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
)

func TestDirectionsAndDefaultSessionAreIndependent(t *testing.T) {
	settings := Default()
	settings.Mode = audio.ModeDuplex
	settings.DefaultMode = audio.ModePlayback
	settings.Quality = "ultra"
	settings.Output, settings.Capture = "mic cable", "computer cable"
	if err := settings.ValidateDevices([]string{settings.Output}, []string{settings.Capture}); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(settings.Args(t.TempDir()), " ")
	for _, flag := range []string{"--mode duplex", "--default-mode playback", "--device mic cable", "--capture computer cable"} {
		if !strings.Contains(args, flag) {
			t.Fatalf("missing %q: %s", flag, args)
		}
	}
	settings.DefaultMode = audio.ModeMic
	if settings.Validate() == nil {
		t.Fatal("ultra allowed for a microphone session")
	}
	settings.Quality = "balanced"
	settings.Mode = audio.ModeMic
	settings.DefaultMode = audio.ModePlayback
	if settings.Validate() == nil {
		t.Fatal("default mode enables an unconfigured direction")
	}
}
func TestConfigurationImpact(t *testing.T) {
	current := Default()
	next := current
	next.Quality = "music"
	if current.AudioChanged(next) || current.NetworkChanged(next) {
		t.Fatal("defaults require restart")
	}
	next.CaptureChannels = 2
	if current.AudioChanged(next) {
		t.Fatal("legacy channel default requires restart")
	}
	next.Output = "other mic"
	if !current.AudioChanged(next) || current.NetworkChanged(next) {
		t.Fatal("device replacement impact incorrect")
	}
	next.HTTPSPort++
	if !current.NetworkChanged(next) {
		t.Fatal("listener change missed")
	}
}
