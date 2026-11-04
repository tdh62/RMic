package audio

import (
	"encoding/binary"
	"testing"
)

func TestMultichannelQueueAlignmentAndDownmix(t *testing.T) {
	for _, channels := range []int{2, 6, 8} {
		buffer := NewBufferForChannels(20, 40, channels)
		pcm := make([]int16, 48000*channels*60/1000)
		for i := range pcm {
			pcm[i] = int16(i%channels + 1)
		}
		buffer.Write(pcm)
		stats := buffer.Stats()
		if stats.BufferedMS != 40 || stats.Dropped != uint64(48000*channels*20/1000) {
			t.Fatalf("wrong multichannel capacity: %+v", stats)
		}
		out := make([]byte, 960*channels*2)
		buffer.Read(out, 1, false)
		for i := 0; i < len(out)/2; i++ {
			if binary.LittleEndian.Uint16(out[i*2:]) != uint16(i%channels+1) {
				t.Fatal("queue lost channel alignment")
			}
		}
		if channels > 2 {
			for channel := 0; channel < channels; channel++ {
				frame := make([]int16, channels)
				frame[channel] = 10000
				stereo := Stereo(frame, channels)
				if channel == 3 {
					if stereo[0] != 0 || stereo[1] != 0 {
						t.Fatal("LFE unexpectedly included")
					}
					continue
				}
				if channel == 2 {
					if stereo[0] == 0 || stereo[0] != stereo[1] {
						t.Fatal("center not mixed equally")
					}
					continue
				}
				if channel%2 == 0 && (stereo[0] == 0 || stereo[1] != 0) || channel%2 == 1 && (stereo[0] != 0 || stereo[1] == 0) {
					t.Fatal("surround downmix swapped channels")
				}
			}
		}
	}
}
