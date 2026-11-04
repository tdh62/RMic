package transport

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/codec"
	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/web"
	"github.com/gorilla/websocket"
	"github.com/pion/ice/v4"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

type Output interface {
	SetSource(*audio.Buffer)
	Gain() float64
	Muted() bool
	OutputError() string
}
type Input interface {
	SetCaptureSink(*audio.Buffer)
	CaptureGain() float64
	CaptureMuted() bool
	CaptureError() string
}
type Option func(*Server)

func WithNetwork(ips []net.IP) Option {
	return func(s *Server) { s.ips = append([]net.IP(nil), ips...) }
}
func WithQuality(id string) Option { return func(s *Server) { s.defaultQuality = id } }

func WithInput(input Input, name string) Option {
	return func(s *Server) { s.input = input; s.inputName = name }
}

type session struct {
	credential                   string
	pcmChannels                  int
	pcmCompression               string
	sentFrames                   atomic.Uint64
	transport                    string
	buffers                      Buffers
	metrics                      PlaybackMetrics
	metricsAt                    time.Time
	ticket                       string
	connectionMu                 sync.Mutex
	ws                           *websocket.Conn
	id                           string
	mode                         audio.Mode
	quality                      audio.Quality
	pc                           *webrtc.PeerConnection
	buffer, sendBuffer           *audio.Buffer
	state                        string
	created, lastAudio, lastSent time.Time
	receiveError, sendError      string
	hasTrack                     atomic.Bool
	connected                    chan struct{}
	once                         sync.Once
	ctx                          context.Context
	cancel                       context.CancelFunc
}
type Direction struct {
	DeviceState string      `json:"device_state,omitempty"` // released, idle (release pending), open, fault
	State       string      `json:"state"`
	Device      string      `json:"device"`
	Gain        float64     `json:"gain"`
	Muted       bool        `json:"muted"`
	Audio       audio.Stats `json:"audio"`
	Error       string      `json:"error,omitempty"`
}
type Status struct {
	Shortcuts       *ShortcutStatus `json:"shortcuts,omitempty"`
	InstanceID      string          `json:"instance_id"`
	ConfigRevision  uint64          `json:"config_revision"`
	DefaultMode     audio.Mode      `json:"default_mode"`
	DefaultQuality  audio.Quality   `json:"default_quality"`
	CaptureChannels int             `json:"capture_channels"`
	PCMCompression  string          `json:"pcm_compression,omitempty"`
	Quality         audio.Quality   `json:"quality"`
	Transport       string          `json:"transport"`
	Buffers         Buffers         `json:"buffers"`
	Playback        PlaybackMetrics `json:"playback"`
	MetricsAgeMS    int64           `json:"metrics_age_ms"`
	Qualities       []audio.Quality `json:"qualities"`
	Network         []string        `json:"network"`
	State           string          `json:"state"`
	Mode            audio.Mode      `json:"mode"`
	Modes           []audio.Mode    `json:"modes"`
	Receive         Direction       `json:"receive"`
	Send            Direction       `json:"send"`
	// Original receive-only fields remain available for existing clients.
	Output      string      `json:"output"`
	Gain        float64     `json:"gain"`
	Muted       bool        `json:"muted"`
	Audio       audio.Stats `json:"audio"`
	LastAudioMS int64       `json:"last_audio_ms"`
	Error       string      `json:"error,omitempty"`
}
type Server struct {
	shortcuts      *shortcutControl
	configMu       sync.RWMutex
	instanceID     string
	configRevision uint64
	defaultMode    audio.Mode
	pairing        *pairing.Manager
	token          string
	output         Output
	outputName     string
	input          Input
	inputName      string
	ips            []net.IP
	defaultQuality string
	settings       webrtc.SettingEngine
	mux            ice.UDPMux
	mu             sync.Mutex
	active         *session
	lastError      string
	closed         bool
	stop, done     chan struct{}
	workers        sync.WaitGroup
}

func RandomToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func NewServer(output Output, name string, udpPort int, options ...Option) (*Server, error) {
	token, err := RandomToken()
	if err != nil {
		return nil, err
	}
	instance, err := RandomToken()
	if err != nil {
		return nil, err
	}
	s := &Server{shortcuts: newShortcutControl(), token: token, instanceID: instance, configRevision: 1, output: output, outputName: name, stop: make(chan struct{}), done: make(chan struct{})}
	for _, option := range options {
		option(s)
	}
	if err := s.shortcuts.config.Validate(); err != nil {
		return nil, err
	}
	if err := s.shortcuts.engine.SetKeyInterval(s.shortcuts.config.KeyIntervalMS); err != nil {
		return nil, err
	}
	if s.output == nil && s.input == nil {
		return nil, fmt.Errorf("at least one audio direction must be configured")
	}
	if s.defaultMode == "" {
		switch {
		case s.output != nil && s.input != nil:
			s.defaultMode = audio.ModeMic
			if s.defaultQuality == "ultra" {
				s.defaultMode = audio.ModePlayback
			}
		case s.input != nil:
			s.defaultMode = audio.ModePlayback
		default:
			s.defaultMode = audio.ModeMic
		}
	}
	if s.defaultQuality == "" {
		s.defaultQuality = "balanced"
	}
	quality, err := audio.FindQuality(s.defaultQuality)
	if err != nil {
		return nil, err
	}
	if quality.PlaybackOnly && s.input == nil {
		return nil, fmt.Errorf("ultra 需要配置电脑 → 手机播放通道")
	}
	if _, err := audio.ParseMode(string(s.defaultMode)); err != nil {
		return nil, err
	}
	if s.defaultMode.Microphone() && s.output == nil || s.defaultMode.Playback() && s.input == nil {
		return nil, fmt.Errorf("default mode requires configured audio devices")
	}
	if err := quality.ValidateMode(s.defaultMode); err != nil {
		return nil, err
	}
	if s.ips == nil {
		s.ips = LocalIPs()
	}
	mux, err := NewUDPMux(s.ips, udpPort)
	if err != nil {
		return nil, err
	}
	s.mux = mux
	settings := webrtc.SettingEngine{}
	settings.SetICEUDPMux(s.mux)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	settings.SetIPFilter(func(ip net.IP) bool {
		for _, selected := range s.ips {
			if selected.Equal(ip) {
				return true
			}
		}
		return false
	})
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetICETimeouts(5*time.Second, 10*time.Second, 2*time.Second)
	s.settings = settings
	go s.watch()
	return s, nil
}
func (s *Server) Token() string      { return s.token }
func (s *Server) UDPAddress() string { return s.mux.GetListenAddresses()[0].String() }
func (s *Server) modes() []audio.Mode {
	modes := []audio.Mode{}
	if s.output != nil {
		modes = append(modes, audio.ModeMic)
	}
	if s.input != nil {
		modes = append(modes, audio.ModePlayback)
	}
	if s.output != nil && s.input != nil {
		modes = append(modes, audio.ModeDuplex)
	}
	return modes
}
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := Status{Transport: "webrtc", Buffers: defaultBuffers(), MetricsAgeMS: -1, State: "waiting", Modes: s.modes(), LastAudioMS: -1, Error: s.lastError, Receive: Direction{State: "disabled"}, Send: Direction{State: "disabled"}}
	status.Shortcuts = s.shortcuts.status()
	status.InstanceID = s.instanceID
	status.ConfigRevision = s.configRevision
	status.DefaultMode = s.defaultMode
	status.DefaultQuality, _ = audio.FindQuality(s.defaultQuality)
	status.CaptureChannels = s.captureChannels()
	status.Quality, _ = audio.FindQuality(s.defaultQuality)
	status.Qualities = audio.Qualities()
	for _, address := range s.mux.GetListenAddresses() {
		status.Network = append(status.Network, address.String())
	}
	if s.output != nil {
		status.Receive = Direction{State: "idle", Device: s.outputName, Gain: s.output.Gain(), Muted: s.output.Muted(), Error: s.output.OutputError()}
		if device, ok := s.output.(interface{ OutputDeviceState() string }); ok {
			status.Receive.DeviceState = device.OutputDeviceState()
		}
	}
	if s.input != nil {
		status.Send = Direction{State: "idle", Device: s.inputName, Gain: s.input.CaptureGain(), Muted: s.input.CaptureMuted(), Error: s.input.CaptureError()}
		if device, ok := s.input.(interface{ CaptureDeviceState() string }); ok {
			status.Send.DeviceState = device.CaptureDeviceState()
		}
	}
	if a := s.active; a != nil {
		status.Mode = a.mode
		status.Transport = a.transport
		status.PCMCompression = a.pcmCompression
		status.Buffers = a.buffers
		status.Playback = a.metrics
		if !a.metricsAt.IsZero() {
			status.MetricsAgeMS = time.Since(a.metricsAt).Milliseconds()
		}
		status.Quality = a.quality
		status.State = a.state
		status.Receive.State = "disabled"
		status.Send.State = "disabled"
		if !a.mode.Microphone() {
			status.Receive.Error = ""
		}
		if !a.mode.Playback() {
			status.Send.Error = ""
		}
		if a.mode.Microphone() {
			status.Receive.State = "connecting"
			status.Receive.Audio = a.buffer.Stats()
			if !a.lastAudio.IsZero() {
				status.LastAudioMS = time.Since(a.lastAudio).Milliseconds()
				status.Receive.State = "streaming"
				if status.LastAudioMS > 2000 {
					status.Receive.State = "waiting_audio"
				}
			}
			if status.Receive.Error == "" {
				status.Receive.Error = a.receiveError
			}
			if status.Receive.Error != "" {
				status.Receive.State = "paused"
			}
		}
		if a.mode.Playback() {
			status.Send.State = "connecting"
			status.Send.Audio = a.sendBuffer.Stats()
			if !a.lastSent.IsZero() {
				status.Send.State = "streaming"
			}
			if status.Send.Error == "" {
				status.Send.Error = a.sendError
			}
			if status.Send.Error != "" {
				status.Send.State = "paused"
			}
		}
		if a.state != "reconnecting" {
			switch {
			case status.Receive.State == "streaming" || status.Send.State == "streaming":
				status.State = "streaming"
			case status.Receive.State == "paused" || status.Send.State == "paused":
				status.State = "paused"
			case status.Receive.State == "waiting_audio":
				status.State = "waiting_audio"
			}
		}
	}
	status.Output = status.Receive.Device
	status.Gain = status.Receive.Gain
	status.Muted = status.Receive.Muted
	status.Audio = status.Receive.Audio
	return status
}
func (s *Server) Disconnect() {
	s.mu.Lock()
	a := s.active
	s.mu.Unlock()
	if a != nil {
		s.release(a, "电脑端已断开连接")
	}
}
func (s *Server) release(a *session, reason string) {
	s.mu.Lock()
	if s.active != a {
		s.mu.Unlock()
		return
	}
	s.StopShortcuts()
	s.active = nil
	s.lastError = reason
	if s.output != nil {
		s.output.SetSource(nil)
	}
	if s.input != nil {
		s.input.SetCaptureSink(nil)
	}
	a.cancel()
	s.mu.Unlock()
	a.closeConnections()
}
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.stop)
	s.StopShortcuts()
	s.mu.Unlock()
	<-s.done
	s.Disconnect()
	s.workers.Wait()
	_ = s.mux.Close()
}
func (s *Server) watch() {
	defer close(s.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
			s.mu.Lock()
			a := s.active
			expired := false
			if a != nil {
				lastSeen := a.created
				if a.lastAudio.After(lastSeen) {
					lastSeen = a.lastAudio
				}
				if a.lastSent.After(lastSeen) {
					lastSeen = a.lastSent
				}
				expired = time.Since(lastSeen) > 30*time.Second || a.transport == "ws-pcm" && a.lastSent.IsZero() && a.lastAudio.IsZero() && time.Since(a.created) > 10*time.Second
			}
			s.mu.Unlock()
			if a != nil && !s.validCredential(a.credential) {
				s.release(a, "设备授权已撤销，请重新申请配对")
				continue
			}
			if expired {
				s.release(a, "音频连接超时，请重新连接")
			}
		}
	}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.pairingRoutes(mux)
	s.shortcutRoutes(mux)
	s.pcmRoutes(mux)
	mux.HandleFunc("POST /api/session/{id}/metrics", s.authorize(s.metrics))
	assets, _ := fs.Sub(web.Files, "static")
	mux.Handle("GET /", http.FileServer(http.FS(assets)))
	mux.HandleFunc("GET /api/status", s.authorize(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Status()) }))
	mux.HandleFunc("POST /api/session", s.authorize(s.offer))
	mux.HandleFunc("DELETE /api/session/{id}", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		a := s.active
		s.mu.Unlock()
		if a != nil && a.id == r.PathValue("id") && constantToken(a.credential, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
			s.release(a, "")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "microphone=(self), camera=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; media-src 'self' blob:; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", 403)
			return
		}
		token, hasBearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !hasBearer || !s.validCredential(token) {
			apiError(w, "authorization_required", "授权已失效，请在手机重新申请配对并由电脑许可", 401)
			return
		}
		next(w, r)
	}
}
func constantToken(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) offer(w http.ResponseWriter, r *http.Request) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if !s.checkRevision(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var request struct {
		webrtc.SessionDescription
		Mode    audio.Mode `json:"mode"`
		Quality string     `json:"quality"`
		Buffers *Buffers   `json:"buffers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Type != webrtc.SDPTypeOffer {
		http.Error(w, "invalid SDP offer", 400)
		return
	}
	mode := request.Mode
	if mode == "" {
		mode = s.defaultMode
	}
	if _, err := audio.ParseMode(string(mode)); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if (mode.Microphone() && s.output == nil) || (mode.Playback() && s.input == nil) {
		apiError(w, "mode_unavailable", "电脑未配置此模式需要的音频设备", 422)
		return
	}
	qualityID := request.Quality
	if qualityID == "" {
		qualityID = s.defaultQuality
	}
	quality, err := audio.FindQuality(qualityID)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err = quality.ValidateMode(mode); err != nil {
		http.Error(w, err.Error(), 400)
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
	pc, err := s.newPeerConnection(quality)
	if err != nil {
		http.Error(w, "cannot create connection", 500)
		return
	}
	id, err := RandomToken()
	if err != nil {
		_ = pc.Close()
		http.Error(w, "cannot create session", 500)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &session{transport: "webrtc", buffers: buffers, credential: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), id: id, mode: mode, pc: pc, quality: quality, buffer: quality.NewBuffer(), sendBuffer: buffers.sendBuffer(), state: "connecting", created: time.Now(), connected: make(chan struct{}), ctx: ctx, cancel: cancel}
	s.mu.Lock()
	if s.closed || (s.active != nil && (s.active.id != r.Header.Get("X-RMic-Session") || !constantToken(s.active.credential, a.credential))) {
		s.mu.Unlock()
		cancel()
		_ = pc.Close()
		apiError(w, "session_busy", "电脑已被其他设备占用，请先断开现有连接", 409)
		return
	}
	previous := s.active
	s.StopShortcuts()
	s.active = a
	s.lastError = ""
	s.detachAudioLocked()
	// The lifecycle count is reserved under the same lock as Close's closed flag.
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	if previous != nil {
		previous.cancel()
		previous.closeConnections()
	}
	success := false
	defer func() {
		if !success {
			s.release(a, "")
		}
	}()
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			s.release(a, "音频连接已断开")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.active != a {
			return
		}
		switch state {
		case webrtc.PeerConnectionStateConnected:
			if err := s.activateAudioLocked(a); err != nil {
				a.receiveError, a.sendError = err.Error(), err.Error()
				// release takes s.mu and closes the peer; do it outside this callback lock.
				go s.release(a, "打开音频设备失败："+err.Error())
				return
			}
			a.state = "connected"
			a.once.Do(func() { close(a.connected) })
		case webrtc.PeerConnectionStateDisconnected:
			a.state = "reconnecting"
		}
	})
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if !mode.Microphone() || track.Kind() != webrtc.RTPCodecTypeAudio || !strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeOpus) || !a.hasTrack.CompareAndSwap(false, true) {
			return
		}
		s.receive(a, track)
	})
	if err = pc.SetRemoteDescription(request.SessionDescription); err != nil {
		http.Error(w, "unsupported SDP offer", 400)
		return
	}
	if mode.Playback() {
		track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "computer-audio", "rmic")
		if err != nil {
			http.Error(w, "cannot create audio sender", 500)
			return
		}
		sender, err := pc.AddTrack(track)
		if err != nil {
			http.Error(w, "cannot attach audio sender", 400)
			return
		}
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			data := make([]byte, 1500)
			for {
				if _, _, err := sender.Read(data); err != nil {
					return
				}
			}
		}()
		s.workers.Add(1)
		go func() { defer s.workers.Done(); s.send(a, track) }()
	}
	preferred := opusCodec(quality)
	preferred.PayloadType = 0
	for _, transceiver := range pc.GetTransceivers() {
		if transceiver.Kind() == webrtc.RTPCodecTypeAudio {
			if err = transceiver.SetCodecPreferences([]webrtc.RTPCodecParameters{preferred}); err != nil {
				http.Error(w, "cannot select audio quality", 400)
				return
			}
		}
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		http.Error(w, "cannot create answer", 500)
		return
	}
	gathering := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(answer); err != nil {
		http.Error(w, "cannot set answer", 500)
		return
	}
	gatherCtx, gatherCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer gatherCancel()
	select {
	case <-gathering:
	case <-gatherCtx.Done():
		http.Error(w, "ICE gathering timed out", 504)
		return
	}
	s.mu.Lock()
	valid := s.active == a
	s.mu.Unlock()
	if !valid {
		apiError(w, "session_replaced", "会话已更新，正在重新协商", 409)
		return
	}
	success = true
	writeJSON(w, struct {
		ID     string                     `json:"id"`
		Answer *webrtc.SessionDescription `json:"answer"`
		Mode   audio.Mode                 `json:"mode"`
	}{id, pc.LocalDescription(), mode})
}
func (s *Server) receive(a *session, track *webrtc.TrackRemote) {
	decoder, err := codec.NewDecoder()
	if err != nil {
		s.receiveFailed(a, err.Error())
		return
	}
	defer decoder.Close()
	builder := samplebuilder.New(8, &codecs.OpusPacket{}, 48000, samplebuilder.WithMaxTimeDelay(60*time.Millisecond))
	pcm := make([]int16, 11520)
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			s.receiveFailed(a, "设备麦克风音轨已结束；此方向已暂停")
			return
		}
		builder.Push(packet)
		for sample := builder.Pop(); sample != nil; sample = builder.Pop() {
			n, err := decoder.Decode(sample.Data, pcm)
			if err != nil {
				continue
			}
			a.buffer.Write(pcm[:n])
			s.mu.Lock()
			if s.active == a {
				a.lastAudio = time.Now()
			}
			s.mu.Unlock()
		}
	}
}
func (s *Server) receiveFailed(a *session, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == a {
		a.receiveError = reason
		if s.output != nil {
			s.output.SetSource(nil)
		}
	}
}
