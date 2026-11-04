package transport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
)

// Buffers separates capture scheduling from receiver playout. PlayMS=0 lets
// WebRTC choose its own target; the PCM player adapts its bounded queue.
type Buffers struct {
	SendMS int `json:"send_ms"`
	PlayMS int `json:"play_ms"`
}

func defaultBuffers() Buffers { return Buffers{SendMS: 20} }
func (b Buffers) validate() error {
	if b.SendMS < 10 || b.SendMS > 200 {
		return fmt.Errorf("电脑发送缓冲必须为 10–200 ms")
	}
	if b.PlayMS != 0 && (b.PlayMS < 20 || b.PlayMS > 2000) {
		return fmt.Errorf("手机播放缓冲必须为自动或 20–2000 ms")
	}
	return nil
}
func (b Buffers) sendBuffer() *audio.Buffer {
	return audio.NewBufferWithLatency(b.SendMS, max(200, b.SendMS*4))
}

type PlaybackMetrics struct {
	QueuedMS       float64 `json:"queued_ms"`
	TargetMS       float64 `json:"target_ms"`
	RTTMS          float64 `json:"rtt_ms"`
	Underruns      uint64  `json:"underruns"`
	ReceivedFrames uint64  `json:"received_frames"`
}

func (m PlaybackMetrics) valid() bool {
	// Positive comparisons reject NaN as well as values outside protocol bounds.
	return m.QueuedMS >= 0 && m.QueuedMS <= 3000 && m.TargetMS >= 0 && m.TargetMS <= 2000 && m.RTTMS >= 0 && m.RTTMS <= 30000
}
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	var metrics PlaybackMetrics
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&metrics) != nil || !metrics.valid() {
		http.Error(w, "invalid playback metrics", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.active
	if a == nil || a.id != r.PathValue("id") || !constantToken(a.credential, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
		http.Error(w, "session not owned", 409)
		return
	}
	if a.transport == "ws-pcm" && (metrics.ReceivedFrames > a.sentFrames.Load() || metrics.ReceivedFrames < a.metrics.ReceivedFrames) {
		http.Error(w, "invalid PCM feedback", 400)
		return
	}
	a.metrics = metrics
	a.metricsAt = time.Now()
	w.WriteHeader(http.StatusNoContent)
}
