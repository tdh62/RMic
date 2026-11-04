package transport

import "testing"

func TestNativeToneEvidenceRejectsFeedbackButAllowsPartialOnset(t *testing.T) {
	// Reproduce the old predicate's false positive without relying on native
	// scheduling: the first 20 ms frame contains only 1 ms of pure 880 Hz audio.
	onset := make([]int16, 1920)
	tone(onset[len(onset)-96:], 880, 0)
	if energy(onset) <= 1e6 || power(onset, 880) >= power(onset, 440)*10 {
		t.Fatal("fixture no longer demonstrates the single-frame false positive")
	}
	for _, tc := range []struct {
		name                 string
		playback, microphone bool
		activeFrames         int
		want                 bool
	}{
		{"partial onset then clean playback", true, false, 19, true},
		{"microphone routed into playback", false, true, 19, false},
		{"playback mixed with microphone feedback", true, true, 19, false},
		{"onset followed by silence", false, false, 0, false},
		{"insufficient sustained audio", true, false, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var evidence nativeToneEvidence
			evidence.add(onset)
			for i := 0; i < 19; i++ {
				frame, mic := make([]int16, 1920), make([]int16, 1920)
				if i < tc.activeFrames {
					if tc.playback {
						tone(frame, 880, i*960)
					}
					if tc.microphone {
						tone(mic, 440, i*960)
					}
					for j := range frame {
						frame[j] += mic[j]
					}
				}
				evidence.add(frame)
			}
			if got := evidence.isolatedPlayback(); got != tc.want {
				t.Fatalf("isolatedPlayback=%t, want %t: %+v", got, tc.want, evidence)
			}
		})
	}
}
