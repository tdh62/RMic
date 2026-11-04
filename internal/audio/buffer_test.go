package audio

import (
	"encoding/binary"
	"testing"
)

func TestBufferGainMuteAndUnderrun(t *testing.T) {
	b := NewBuffer()
	pcm := make([]int16, SampleRate*Channels*40/1000)
	for i := range pcm {
		pcm[i] = 20000
	}
	b.Write(pcm)
	out := make([]byte, 960*Channels*2)
	b.Read(out, 2, false)
	if got := int16(binary.LittleEndian.Uint16(out)); got != 32767 {
		t.Fatalf("gain clipping = %d", got)
	}
	b.Read(out, 1, true)
	for _, v := range out {
		if v != 0 {
			t.Fatal("muted output contains audio")
		}
	}
	b.Read(out, 1, false)
	if b.Stats().Underruns != 1 {
		t.Fatal("expected one underrun")
	}
	b.Write([]int16{100, 200})
	b.Read(out, 1, false)
	for _, v := range out {
		if v != 0 {
			t.Fatal("must rebuffer after underrun")
		}
	}
}

func TestBufferDropsOldStereoFrames(t *testing.T) {
	b := NewBuffer()
	pcm := make([]int16, len(b.data)+200)
	for i := 0; i < len(pcm); i += 2 {
		pcm[i] = 1234
		pcm[i+1] = -1234
	}
	b.Write(pcm)
	stats := b.Stats()
	if stats.BufferedMS != 200 || stats.Dropped != 200 {
		t.Fatalf("unbounded or incorrect overflow: %+v", stats)
	}
	out := make([]byte, 400)
	b.Read(out, 1, false)
	for i := 0; i < len(out); i += 4 {
		if int16(binary.LittleEndian.Uint16(out[i:])) != 1234 || int16(binary.LittleEndian.Uint16(out[i+2:])) != -1234 {
			t.Fatal("stereo channels became misaligned")
		}
	}
}
