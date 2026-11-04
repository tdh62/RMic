package audio

import "testing"

func TestQualityBufferTargets(t *testing.T) {
	for _, quality := range Qualities() {
		t.Run(quality.ID, func(t *testing.T) {
			buffer := quality.NewBuffer()
			samples := SampleRate * Channels * quality.BufferMS / 1000
			pcm := make([]int16, samples)
			for i := range pcm {
				pcm[i] = 1000
			}
			buffer.Write(pcm[:samples-Channels])
			out := make([]byte, 480*Channels*2)
			buffer.Read(out, 1, false)
			if buffer.Stats().Level != 0 {
				t.Fatal("played before selected prebuffer target")
			}
			buffer.Write(pcm[:Channels])
			buffer.Read(out, 1, false)
			if buffer.Stats().Level == 0 {
				t.Fatal("did not start after selected prebuffer target")
			}
		})
	}
	if _, err := FindQuality("invalid"); err == nil {
		t.Fatal("unknown quality accepted")
	}
}
