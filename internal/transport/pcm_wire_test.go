package transport

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"encoding/binary"
	"testing"
)

func TestPCMCompressionRoundTripAndBounds(t *testing.T) {
	for _, channels := range []int{2, 6, 8} {
		for _, noise := range []bool{false, true} {
			raw := make([]byte, 960*channels*2)
			if noise {
				if _, err := rand.Read(raw); err != nil {
					t.Fatal(err)
				}
			} else {
				for i := 0; i < len(raw); i += 2 {
					binary.LittleEndian.PutUint16(raw[i:], uint16(i%channels))
				}
			}
			packet, err := encodePCM(raw, channels, 960, true)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodePCM(packet, channels, 960, true)
			if err != nil || !bytes.Equal(decoded, raw) {
				t.Fatal("compressed PCM changed sample bytes", err)
			}
			if len(packet) > len(raw)+pcmHeader {
				t.Fatal("compression expanded a frame")
			}
			if !noise && binary.LittleEndian.Uint32(packet[20:]) != 1 {
				t.Fatal("compressible signal not compressed")
			}
			if _, err = decodePCM(packet, channels, 0, true); err == nil {
				t.Fatal("wrong sequence accepted")
			}
			if _, err = decodePCM(packet[:len(packet)-1], channels, 960, true); err == nil {
				t.Fatal("truncated frame accepted")
			}
		}
	}
	var bomb bytes.Buffer
	writer := zlib.NewWriter(&bomb)
	writer.Write(make([]byte, 1<<20))
	writer.Close()
	packet, _ := encodePCM(make([]byte, 3840), 2, 0, false)
	binary.LittleEndian.PutUint32(packet[20:], 1)
	packet = append(packet[:pcmHeader], bomb.Bytes()...)
	if _, err := decodePCM(packet, 2, 0, true); err == nil {
		t.Fatal("unbounded decompression accepted")
	}
	if _, err := decodePCM(packet, 2, 0, false); err == nil {
		t.Fatal("unnegotiated compression accepted")
	}
}
