package transport

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"github.com/gorilla/websocket"
)

func (a *session) closeConnections() {
	a.connectionMu.Lock()
	conn := a.ws
	a.connectionMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	if a.pc != nil {
		_ = a.pc.Close()
	}
}
func (s *Server) captureChannels() int {
	if input, ok := s.input.(interface{ CaptureChannels() int }); ok {
		return input.CaptureChannels()
	}
	return 2
}
func (s *Server) pcmRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/pcm", s.authorize(s.createPCM))
	mux.HandleFunc("GET /api/pcm", s.connectPCM)
}
func (s *Server) createPCM(w http.ResponseWriter, r *http.Request) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if !s.checkRevision(w, r) {
		return
	}
	var request struct {
		Mode        audio.Mode `json:"mode"`
		Buffers     *Buffers   `json:"buffers"`
		Channels    int        `json:"channels"`
		Compression string     `json:"compression"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&request) != nil {
		http.Error(w, "invalid PCM request", 400)
		return
	}
	if _, err := audio.ParseMode(string(request.Mode)); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if request.Mode.Microphone() && s.output == nil || request.Mode.Playback() && s.input == nil {
		apiError(w, "mode_unavailable", "电脑未配置此模式需要的音频设备", 422)
		return
	}
	if request.Compression == "" {
		request.Compression = "none"
	}
	if request.Compression != "none" && request.Compression != "deflate" {
		http.Error(w, "unsupported PCM compression", 400)
		return
	}
	channels := s.captureChannels()
	if request.Channels == 0 {
		request.Channels = channels
	}
	if audio.ValidateChannels(request.Channels) != nil || request.Channels != channels {
		apiError(w, "configuration_changed", "请求声道必须匹配电脑配置的采集声道", 409)
		return
	}
	buffers := defaultBuffers()
	if request.Buffers != nil {
		buffers = *request.Buffers
	}
	if err := buffers.validate(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	id, err := RandomToken()
	if err != nil {
		http.Error(w, "cannot create session", 500)
		return
	}
	ticket, err := RandomToken()
	if err != nil {
		http.Error(w, "cannot create ticket", 500)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	quality, _ := audio.FindQuality(s.defaultQuality)
	a := &session{transport: "ws-pcm", credential: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), id: id, ticket: ticket, mode: request.Mode, quality: quality, buffers: buffers, pcmChannels: channels, pcmCompression: request.Compression, buffer: audio.NewBuffer(), sendBuffer: audio.NewBufferForChannels(buffers.SendMS, max(200, buffers.SendMS*4), channels), state: "connecting", created: time.Now(), ctx: ctx, cancel: cancel}
	s.mu.Lock()
	if s.closed || s.active != nil && (s.active.id != r.Header.Get("X-RMic-Session") || !constantToken(s.active.credential, a.credential)) {
		s.mu.Unlock()
		cancel()
		apiError(w, "session_busy", "电脑已被其他设备占用，请先断开现有连接", 409)
		return
	}
	previous := s.active
	s.StopShortcuts()
	s.active = a
	s.lastError = ""
	s.detachAudioLocked()
	s.mu.Unlock()
	if previous != nil {
		previous.cancel()
		previous.closeConnections()
	}
	writeJSON(w, map[string]any{"id": id, "ticket": ticket, "buffers": buffers})
}
func (s *Server) connectPCM(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", 403)
		return
	}
	ticket := r.URL.Query().Get("ticket")
	s.mu.Lock()
	a := s.active
	if s.closed || a == nil || a.transport != "ws-pcm" || ticket == "" || !constantToken(ticket, a.ticket) || time.Since(a.created) > 10*time.Second || !s.validCredential(a.credential) {
		s.mu.Unlock()
		http.Error(w, "连接票据无效或已过期", 401)
		return
	}
	a.ticket = ""
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	upgrader := websocket.Upgrader{CheckOrigin: sameOrigin, HandshakeTimeout: 5 * time.Second, ReadBufferSize: 2048, WriteBufferSize: 4096}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.release(a, "WebSocket 握手失败")
		return
	}
	a.connectionMu.Lock()
	if a.ctx.Err() != nil {
		a.connectionMu.Unlock()
		conn.Close()
		return
	}
	a.ws = conn
	a.connectionMu.Unlock()
	s.mu.Lock()
	if s.active != a || s.closed {
		s.mu.Unlock()
		conn.Close()
		return
	}
	err = s.activateAudioLocked(a)
	s.mu.Unlock()
	if err != nil {
		s.release(a, "打开音频设备失败："+err.Error())
		conn.Close()
		return
	}
	done := make(chan struct{})
	go func() { defer close(done); s.readPCM(a, conn) }()
	defer func() { s.release(a, "PCM 音频连接已断开"); conn.Close(); <-done }()
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err = conn.WriteJSON(map[string]any{"type": "format", "version": 2, "codec": "pcm_s16le", "compression": a.pcmCompression, "sample_rate": 48000, "channels": a.pcmChannels, "layout": audio.Layout(a.pcmChannels), "microphone_channels": 2, "frame_samples": 960, "mode": a.mode, "buffers": a.buffers}); err != nil {
		return
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	encoder := newPCMEncoder(a.pcmCompression == "deflate")
	raw := make([]byte, 960*a.pcmChannels*2)
	var sent uint64
	lastProgress := time.Now()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if !a.mode.Playback() {
				continue
			}
			s.mu.Lock()
			metrics := a.metrics
			metricsAt := a.metricsAt
			s.mu.Unlock()
			window := uint64((max(120, metrics.TargetMS) + 200) * 48)
			if sent > metrics.ReceivedFrames+window {
				if time.Since(lastProgress) > 2*time.Second {
					return
				}
				continue
			}
			if !metricsAt.IsZero() && time.Since(metricsAt) > 5*time.Second {
				return
			}
			s.mu.Lock()
			if s.active != a {
				s.mu.Unlock()
				return
			}
			a.sendBuffer.Read(raw, s.input.CaptureGain(), s.input.CaptureMuted() || s.input.CaptureError() != "")
			s.mu.Unlock()
			packet, encodeErr := encoder.encode(raw, a.pcmChannels, sent)
			if encodeErr != nil {
				return
			}
			// Publish credit before sending, so fast receiver feedback cannot race it.
			a.sentFrames.Store(sent + 960)
			conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if conn.WriteMessage(websocket.BinaryMessage, packet) != nil {
				return
			}
			sent += 960
			lastProgress = time.Now()
			s.mu.Lock()
			if s.active == a {
				a.lastSent = time.Now()
			}
			s.mu.Unlock()
		}
	}
}
func (s *Server) readPCM(a *session, conn *websocket.Conn) {
	defer a.cancel()
	conn.SetReadLimit(pcmHeader + 960*8*2)
	var received uint64
	started := time.Now()
	lastFeedback := time.Now()
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if kind == websocket.BinaryMessage {
			if !a.mode.Microphone() || received > uint64(time.Since(started).Seconds()*48000)+48000 {
				return
			}
			raw, err := decodePCM(data, 2, received, a.pcmCompression == "deflate")
			if err != nil {
				return
			}
			pcm := make([]int16, len(raw)/2)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
			}
			a.buffer.Write(pcm)
			received += 960
			s.mu.Lock()
			if s.active == a {
				a.lastAudio = time.Now()
			}
			s.mu.Unlock()
			if time.Since(lastFeedback) > 5*time.Second {
				return
			}
			continue
		}
		if kind != websocket.TextMessage || len(data) > 2048 {
			return
		}
		var feedback struct {
			Type string `json:"type"`
			PlaybackMetrics
		}
		if json.Unmarshal(data, &feedback) != nil || feedback.Type != "metrics" || !feedback.PlaybackMetrics.valid() || feedback.ReceivedFrames > a.sentFrames.Load() {
			return
		}
		s.mu.Lock()
		if s.active != a || feedback.ReceivedFrames < a.metrics.ReceivedFrames {
			s.mu.Unlock()
			return
		}
		a.metrics = feedback.PlaybackMetrics
		a.metricsAt = time.Now()
		s.mu.Unlock()
		lastFeedback = time.Now()
	}
}
