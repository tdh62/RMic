package virtual

import (
	"runtime"
	"testing"
)

func TestSteamDeviceRemovalWhitelistIsExact(t *testing.T) {
	for _, test := range []struct {
		ids  []string
		want int
	}{
		{[]string{`ROOT\SteamStreamingMicrophone`}, 0},
		{[]string{`root\steamstreamingspeakers`}, 1},
		{[]string{`ROOT\MEDIA`, `ROOT\SteamStreamingSpeakers`}, 1},
		{[]string{`ROOT\SteamStreamingMicrophoneExtra`}, -1},
		{[]string{`USB\SteamStreamingMicrophone`}, -1},
		{[]string{`ROOT\VBAudioVACWDM`}, -1},
		{nil, -1},
	} {
		if got := steamHardwareIndex(test.ids); got != test.want {
			t.Errorf("hardware IDs %q: %d != %d", test.ids, got, test.want)
		}
	}
}
func TestSteamManagementRejectsUnsupportedPlatformAndAction(t *testing.T) {
	if _, err := Manage("steam-other", t.TempDir(), true, true); err == nil {
		t.Fatal("unsupported elevated action accepted")
	}
	if runtime.GOOS == "windows" {
		t.Skip("avoid real driver operations")
	}
	manager := NewManager(t.TempDir(), true, true, nil)
	defer manager.Close()
	if _, err := manager.Run("steam-uninstall"); err == nil {
		t.Fatal("Steam driver operation attempted on non-Windows")
	}
}
