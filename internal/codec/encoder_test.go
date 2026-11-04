package codec

import (
	"math"
	"testing"
)

func TestEncoderDecoderRoundTrip(t *testing.T) {
	encoder, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	decoder, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	pcm := make([]int16, FrameSamples)
	for i := 0; i < len(pcm)/2; i++ {
		v := int16(10000 * math.Sin(2*math.Pi*880*float64(i)/48000))
		pcm[2*i] = v
		pcm[2*i+1] = v
	}
	packet := make([]byte, 4000)
	n, err := encoder.Encode(pcm, packet)
	if err != nil {
		t.Fatal(err)
	}
	decoded := make([]int16, 11520)
	count, err := decoder.Decode(packet[:n], decoded)
	if err != nil {
		t.Fatal(err)
	}
	if count != FrameSamples {
		t.Fatalf("unexpected round trip duration: %d", count)
	}
	var total float64
	for _, v := range decoded[:count] {
		total += float64(v) * float64(v)
	}
	if total/float64(count) < 1e6 {
		t.Fatal("round trip audio is silent")
	}
	if _, err = encoder.Encode(pcm[:100], packet); err == nil {
		t.Fatal("partial frame accepted")
	}
	encoder.Close()
	if _, err = encoder.Encode(pcm, packet); err == nil {
		t.Fatal("closed encoder accepted input")
	}
}
