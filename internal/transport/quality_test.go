package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
)

func TestNegotiatedQualitiesAndPacketDuration(t *testing.T) {
	for _, quality := range audio.Qualities() {
		t.Run(quality.ID, func(t *testing.T) {
			input := &testInput{}
			s, err := NewServer(nil, "", 0, WithInput(input, "computer"), WithQuality("weak"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			h := httptest.NewTLSServer(s.Handler())
			defer h.Close()
			p := connectMode(t, s, h, audio.ModePlayback, quality.ID)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			defer func() { cancel(); <-done }()
			go func() {
				defer close(done)
				tick := time.NewTicker(20 * time.Millisecond)
				defer tick.Stop()
				pcm := make([]int16, 1920)
				position := 0
				for {
					select {
					case <-ctx.Done():
						return
					case <-tick.C:
						tone(pcm, 880, position)
						if quality.ID == "ultra" {
							for i := 0; i < len(pcm)/2; i++ {
								pcm[2*i+1] = int16(10000 * math.Sin(2*math.Pi*440*float64(position+i)/48000))
							}
						}
						position += 960
						input.write(pcm)
					}
				}
			}()
			frame := awaitFrame(t, p, func(pcm []int16) bool { return energy(pcm) > 1e6 })
			if quality.ID == "ultra" {
				if buffers := s.Status().Buffers; buffers.SendMS != 20 || buffers.PlayMS != 0 {
					t.Fatal("ultra did not use independent short send buffer and automatic playout")
				}
				awaitFrame(t, p, func(pcm []int16) bool {
					return power(pcm, 880) > 10*power(pcm, 440) && power(pcm[1:], 440) > 10*power(pcm[1:], 880)
				})
			}
			if len(frame) != 48000*2*quality.FrameMS/1000 {
				t.Fatalf("incorrect packet duration: %d samples", len(frame))
			}
			if s.Status().Quality.ID != quality.ID {
				t.Fatal("requested quality not applied")
			}
			if !strings.Contains(p.pc.RemoteDescription().SDP, fmt.Sprintf("maxaveragebitrate=%d", quality.Bitrate)) {
				t.Fatal("Opus parameters were not negotiated")
			}
		})
	}
}

func TestUltraRejectsMicrophoneModesWithoutReplacingSession(t *testing.T) {
	s, err := NewServer(&testOutput{}, "mic", 0, WithInput(&testInput{}, "computer"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewTLSServer(s.Handler())
	defer h.Close()
	connectMode(t, s, h, audio.ModePlayback, "balanced")
	for _, mode := range []audio.Mode{audio.ModeMic, audio.ModeDuplex, ""} {
		body, _ := json.Marshal(map[string]string{"type": "offer", "mode": string(mode), "quality": "ultra"})
		requireStatus(t, request(t, h, http.MethodPost, "/api/session", s.Token(), body), http.StatusBadRequest)
		if status := s.Status(); status.Mode != audio.ModePlayback || status.Quality.ID != "balanced" {
			t.Fatal("invalid ultra request replaced active playback")
		}
	}
	if server, err := NewServer(&testOutput{}, "mic", 0, WithQuality("ultra")); err == nil {
		server.Close()
		t.Fatal("ultra default accepted without a playback input")
	}
}

func TestIPv6OnlyWebRTC(t *testing.T) {
	conn, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.ParseIP("::1")})
	if err != nil {
		t.Skip("IPv6 unavailable")
	}
	_ = conn.Close()
	s, err := NewServer(nil, "", 0, WithInput(&testInput{}, "computer"), WithNetwork([]net.IP{net.ParseIP("::1")}))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewTLSServer(s.Handler())
	defer h.Close()
	p := connectMode(t, s, h, audio.ModePlayback)
	frame := awaitFrame(t, p, func(pcm []int16) bool { return len(pcm) > 0 })
	if len(frame) != 1920 {
		t.Fatal("IPv6 audio was not decoded")
	}
	if !strings.Contains(p.pc.RemoteDescription().SDP, " ::1 ") {
		t.Fatal("IPv6 ICE candidate missing")
	}
	if len(s.Status().Network) != 1 || !strings.HasPrefix(s.Status().Network[0], "[::1]:") {
		t.Fatal("WebRTC listened outside the selected IPv6 address")
	}
}
