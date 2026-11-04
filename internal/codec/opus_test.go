package codec

import (
	"os"
	"testing"
)

func TestDecodeTone(t *testing.T) {
	packet, err := os.ReadFile("testdata/tone.opus")
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	pcm := make([]int16, 11520)
	n, err := decoder.Decode(packet, pcm)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1920 {
		t.Fatalf("expected stereo 20ms, got %d samples", n)
	}
	var energy int64
	for _, v := range pcm[:n] {
		energy += int64(v) * int64(v)
	}
	if energy < int64(n)*1000*1000 {
		t.Fatal("decoded tone is silent")
	}
	if _, err := decoder.Decode(nil, pcm); err == nil {
		t.Fatal("empty packet accepted")
	}
	decoder.Close()
	if _, err := decoder.Decode(packet, pcm); err == nil {
		t.Fatal("closed decoder accepted input")
	}
}
