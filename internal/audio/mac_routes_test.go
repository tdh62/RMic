package audio

import "testing"

func TestMacDuplexRequiresIndependentKnownCables(t *testing.T) {
	for _, test := range []struct {
		output, capture string
		allowed         bool
	}{
		{"BlackHole 2ch", "BlackHole 16ch", true},
		{"BlackHole 16ch", "BlackHole 64ch", true},
		{"BlackHole 2ch", "blackhole 2CH", false},
		{"MacBook Pro Speakers", "BlackHole 16ch", false},
		{"BlackHole 2ch", "Multi-Output Device", false},
		{"BlackHole 2ch", "Aggregate Device", false},
		{"BlackHole 2ch", "BlackHole 2ch copy", false},
		{"", "BlackHole 16ch", false},
	} {
		if err := ValidateMacDuplex(test.output, test.capture); (err == nil) != test.allowed {
			t.Errorf("%q → %q: %v", test.output, test.capture, err)
		}
	}
	names := MacMicrophoneDevices([]string{"MacBook Speakers", "BlackHole 2ch", "BlackHole 16ch", "Multi-Output Device"})
	if len(names) != 2 || names[0] != "BlackHole 2ch" || names[1] != "BlackHole 16ch" {
		t.Fatalf("unsafe microphone destinations: %v", names)
	}
}
