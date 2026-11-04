package transport

import (
	"encoding/binary"
	"time"

	"cnb.cool/tdh6/RMic/internal/codec"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// PeakLimit leaves about 1 dB of headroom. This is a hard peak ceiling, not
// acoustic echo cancellation; routing isolation and headphones remain needed.
const PeakLimit int16 = 29204

func (s *Server) send(a *session, track *webrtc.TrackLocalStaticSample) {
	encoder, err := codec.NewEncoderWithConfig(a.quality.Bitrate, a.quality.Channels, a.quality.FrameMS)
	if err != nil {
		s.sendFailed(a, err.Error())
		return
	}
	defer encoder.Close()
	select {
	case <-a.connected:
	case <-a.ctx.Done():
		return
	}
	duration := time.Duration(a.quality.FrameMS) * time.Millisecond
	ticker := time.NewTicker(duration)
	defer ticker.Stop()
	samples := 48000 * 2 * a.quality.FrameMS / 1000
	raw := make([]byte, samples*2)
	pcm := make([]int16, samples)
	packet := make([]byte, 4000)
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.active != a {
				s.mu.Unlock()
				return
			}
			paused := s.input.CaptureError() != ""
			a.sendBuffer.Read(raw, s.input.CaptureGain(), s.input.CaptureMuted() || paused)
			s.mu.Unlock()
			for i := range pcm {
				value := int16(binary.LittleEndian.Uint16(raw[i*2:]))
				pcm[i] = max(-PeakLimit, min(PeakLimit, value))
			}
			n, err := encoder.Encode(pcm, packet)
			if err != nil {
				s.sendFailed(a, err.Error())
				return
			}
			if err = track.WriteSample(media.Sample{Data: packet[:n], Duration: duration}); err != nil {
				s.sendFailed(a, "电脑音频发送失败；此方向已暂停")
				return
			}
			s.mu.Lock()
			if s.active == a {
				a.lastSent = time.Now()
			}
			s.mu.Unlock()
		}
	}
}
func (s *Server) sendFailed(a *session, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == a {
		a.sendError = reason
		s.input.SetCaptureSink(nil)
	}
}
