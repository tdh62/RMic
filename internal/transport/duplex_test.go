package transport

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/codec"
	"cnb.cool/tdh6/RMic/internal/virtual"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type testInput struct {
	mu    sync.Mutex
	sink  *audio.Buffer
	muted atomic.Bool
	fault atomic.Pointer[string]
}

func (i *testInput) SetCaptureSink(b *audio.Buffer) { i.mu.Lock(); i.sink = b; i.mu.Unlock() }
func (i *testInput) CaptureGain() float64           { return .5 }
func (i *testInput) CaptureMuted() bool             { return i.muted.Load() }
func (i *testInput) CaptureError() string {
	if value := i.fault.Load(); value != nil {
		return *value
	}
	return ""
}
func (i *testInput) write(pcm []int16) {
	i.mu.Lock()
	b := i.sink
	i.mu.Unlock()
	if b != nil {
		b.Write(pcm)
	}
}

type testPeer struct {
	pc     *webrtc.PeerConnection
	track  *webrtc.TrackLocalStaticSample
	frames chan []int16
	id     string
}

func connectMode(t *testing.T, s *Server, h *httptest.Server, mode audio.Mode, qualities ...string) *testPeer {
	t.Helper()
	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(true)
	if len(s.ips) == 1 && s.ips[0].To4() == nil && s.ips[0].Equal(net.ParseIP("::1")) {
		mux, err := NewUDPMux(s.ips, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = mux.Close() })
		settings.SetICEUDPMux(mux)
	}
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	p := &testPeer{pc: pc, frames: make(chan []int16, 64)}
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		decoder, err := codec.NewDecoder()
		if err != nil {
			return
		}
		defer decoder.Close()
		pcm := make([]int16, 11520)
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			n, err := decoder.Decode(packet.Payload, pcm)
			if err == nil {
				frame := append([]int16(nil), pcm[:n]...)
				select {
				case p.frames <- frame:
				default:
				}
			}
		}
	})
	if mode.Microphone() {
		p.track, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "microphone", "test-phone")
		if err != nil {
			t.Fatal(err)
		}
		sender, err := pc.AddTrack(p.track)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			data := make([]byte, 1500)
			for {
				if _, _, err := sender.Read(data); err != nil {
					return
				}
			}
		}()
	} else {
		if _, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			t.Fatal(err)
		}
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gathered:
	case <-time.After(10 * time.Second):
		t.Fatal("client ICE timeout")
	}
	quality := "balanced"
	if len(qualities) > 0 {
		quality = qualities[0]
	}
	body, _ := json.Marshal(map[string]any{"type": "offer", "sdp": pc.LocalDescription().SDP, "mode": mode, "quality": quality})
	res := request(t, h, "POST", "/api/session", s.Token(), body)
	if res.StatusCode != 200 {
		requireStatus(t, res, 200)
	}
	var result struct {
		ID     string                    `json:"id"`
		Answer webrtc.SessionDescription `json:"answer"`
	}
	if err = json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	p.id = result.ID
	if err = pc.SetRemoteDescription(result.Answer); err != nil {
		t.Fatal(err)
	}
	return p
}
func tone(pcm []int16, frequency float64, position int) {
	for i := 0; i < len(pcm)/2; i++ {
		value := int16(10000 * math.Sin(2*math.Pi*frequency*float64(position+i)/48000))
		pcm[2*i] = value
		pcm[2*i+1] = value
	}
}
func energy(pcm []int16) float64 {
	var total float64
	for _, v := range pcm {
		total += float64(v) * float64(v)
	}
	return total / float64(len(pcm))
}
func power(pcm []int16, frequency float64) float64 {
	var re, im float64
	for i := 0; i < len(pcm)/2; i++ {
		phase := 2 * math.Pi * frequency * float64(i) / 48000
		re += float64(pcm[2*i]) * math.Cos(phase)
		im += float64(pcm[2*i]) * math.Sin(phase)
	}
	return re*re + im*im
}

// A native capture can start partway through an Opus frame. Frequency power
// from that short onset is broadband, even for a pure playback tone. Accumulate
// a fixed 400 ms window (20 decoded frames), not a search for a passing frame.
// Sum per-frame powers so phase changes across underruns do not cancel tones.
type nativeToneEvidence struct {
	ActiveFrames                   int
	PlaybackPower, MicrophonePower float64
}

func (e *nativeToneEvidence) add(pcm []int16) {
	if energy(pcm) > 1e6 {
		e.ActiveFrames++
	}
	e.PlaybackPower += power(pcm, 880)
	e.MicrophonePower += power(pcm, 440)
}

func (e nativeToneEvidence) isolatedPlayback() bool {
	return e.ActiveFrames >= 10 && e.PlaybackPower > e.MicrophonePower*10
}

func measureNativePlayback(t *testing.T, p *testPeer) nativeToneEvidence {
	t.Helper()
	var evidence nativeToneEvidence
	evidence.add(awaitFrame(t, p, func(pcm []int16) bool { return energy(pcm) > 1e6 }))
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for i := 1; i < 20; i++ {
		select {
		case frame := <-p.frames:
			evidence.add(frame)
		case <-deadline.C:
			t.Fatalf("native capture observation incomplete (%d/20 frames): %+v", i, evidence)
		}
	}
	return evidence
}

func awaitFrame(t *testing.T, p *testPeer, predicate func([]int16) bool) []int16 {
	t.Helper()
	timeout := time.NewTimer(12 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case pcm := <-p.frames:
			if predicate(pcm) {
				return pcm
			}
		case <-timeout.C:
			t.Fatal("expected remote audio did not arrive")
			return nil
		}
	}
}

// The two RTP directions run independently. A downlink frame is not a barrier
// for uplink decoding; observe actual receive progress while the tested state
// remains active, allowing several packets to exceed already in-flight audio.
func awaitMicrophoneProgress(t *testing.T, s *Server, condition string) {
	t.Helper()
	before := s.Status().Receive.Audio.Samples
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		status := s.Status()
		if status.Receive.Audio.Samples >= before+1920*6 && status.Receive.State == "streaming" {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("microphone did not progress %s (before=%d): %+v", condition, before, s.Status())
		case <-ticker.C:
		}
	}
}

func TestModesIsolationAndIndependentPause(t *testing.T) {
	for _, mode := range []audio.Mode{audio.ModeMic, audio.ModePlayback, audio.ModeDuplex} {
		t.Run(string(mode), func(t *testing.T) {
			output := &testOutput{}
			input := &testInput{}
			s, err := NewServer(output, "phone microphone", 0, WithInput(input, "computer playback"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			h := httptest.NewTLSServer(s.Handler())
			defer h.Close()
			p := connectMode(t, s, h, mode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			defer func() { cancel(); <-done }()
			go func() {
				defer close(done)
				ticker := time.NewTicker(20 * time.Millisecond)
				defer ticker.Stop()
				encoder, _ := codec.NewEncoder()
				defer encoder.Close()
				mic, computer := make([]int16, 1920), make([]int16, 1920)
				packet := make([]byte, 4000)
				position := 0
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						tone(computer, 880, position)
						input.write(computer)
						if p.track != nil {
							tone(mic, 440, position)
							if n, err := encoder.Encode(mic, packet); err == nil {
								_ = p.track.WriteSample(media.Sample{Data: packet[:n], Duration: 20 * time.Millisecond})
							}
						}
						position += 960
					}
				}
			}()
			if mode.Playback() {
				frame := awaitFrame(t, p, func(pcm []int16) bool { return energy(pcm) > 1e6 })
				if power(frame, 880) < power(frame, 440)*10 {
					t.Fatal("computer audio contains the phone microphone signal")
				}
			}
			if mode.Microphone() {
				end := time.After(10 * time.Second)
				for s.Status().Receive.Audio.Samples < 1920*4 {
					select {
					case <-end:
						t.Fatalf("phone audio missing: %+v", s.Status())
					case <-time.After(20 * time.Millisecond):
					}
				}
				output.mu.Lock()
				b := output.buffer
				output.mu.Unlock()
				raw := make([]byte, 3840)
				b.Read(raw, 1, false)
				pcm := make([]int16, 1920)
				for i := range pcm {
					pcm[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
				}
				if power(pcm, 440) < power(pcm, 880)*10 {
					t.Fatal("phone microphone contains computer playback")
				}
			}
			status := s.Status()
			if !mode.Microphone() && status.Receive.State != "disabled" {
				t.Fatal("playback-only mode enabled microphone output")
			}
			if !mode.Playback() && status.Send.State != "disabled" {
				t.Fatal("microphone-only mode enabled computer capture")
			}
			if mode == audio.ModeDuplex {
				input.muted.Store(true)
				awaitFrame(t, p, func(pcm []int16) bool { return energy(pcm) < 100 })
				awaitMicrophoneProgress(t, s, "while computer playback is muted")
				input.muted.Store(false)
				awaitFrame(t, p, func(pcm []int16) bool { return energy(pcm) > 1e6 })
				awaitMicrophoneProgress(t, s, "after computer playback is unmuted")
				fault := "capture device disconnected"
				input.fault.Store(&fault)
				awaitFrame(t, p, func(pcm []int16) bool { return energy(pcm) < 100 })
				awaitMicrophoneProgress(t, s, "while computer capture is faulted")
				status = s.Status()
				if status.Send.State != "paused" || status.Receive.State != "streaming" {
					t.Fatalf("device failure interrupted the other direction: %+v", status)
				}
			}
			requireStatus(t, request(t, h, "DELETE", "/api/session/"+p.id, s.Token(), nil), 204)
		})
	}
}

func TestNativeComputerPlayback(t *testing.T) {
	if os.Getenv("RMIC_NATIVE_TEST") != "1" {
		t.Skip("set RMIC_NATIVE_TEST=1 for native capture")
	}
	v, err := virtual.CreateFor(true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := v.Close(); err != nil {
			t.Error(err)
		}
	}()
	e, err := audio.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.Start(v.Output); err != nil {
		t.Fatal(err)
	}
	if err = e.StartCapture(v.Capture); err != nil {
		t.Fatal(err)
	}
	out, source := e.PulseRoutes()
	if err = virtual.CheckRoutes(out, source); err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(e, e.Name(), 0, WithInput(e, e.CaptureName()))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewTLSServer(s.Handler())
	defer h.Close()
	p := connectMode(t, s, h, audio.ModeDuplex)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, "pacat", "--playback", "--raw", "--format=s16le", "--rate=48000", "--channels=2", "--device=rmic_computer")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	defer func() { cancel(); _ = stdin.Close(); _ = command.Wait(); <-done }()
	go func() {
		defer close(done)
		encoder, _ := codec.NewEncoder()
		defer encoder.Close()
		pcm, mic := make([]int16, 1920), make([]int16, 1920)
		raw := make([]byte, 3840)
		packet := make([]byte, 4000)
		position := 0
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tone(pcm, 880, position)
				tone(mic, 440, position)
				position += 960
				for i, v := range pcm {
					binary.LittleEndian.PutUint16(raw[i*2:], uint16(v))
				}
				if _, err := stdin.Write(raw); err != nil {
					return
				}
				if n, err := encoder.Encode(mic, packet); err == nil {
					_ = p.track.WriteSample(media.Sample{Data: packet[:n], Duration: 20 * time.Millisecond})
				}
			}
		}
	}()
	evidence := measureNativePlayback(t, p)
	if !evidence.isolatedPlayback() {
		t.Fatalf("native capture lacks sustained isolated playback (880 Hz playback / 440 Hz microphone): evidence=%+v status=%+v", evidence, s.Status())
	}
	if s.Status().Receive.Audio.Samples == 0 {
		t.Fatal("native duplex microphone direction did not receive audio")
	}
	// Removing only the computer playback device must pause sending, without
	// selecting another source or stopping the phone microphone direction.
	modules, err := exec.Command("pactl", "list", "short", "modules").Output()
	if err != nil {
		t.Fatal(err)
	}
	var removed bool
	for _, line := range strings.Split(string(modules), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && fields[1] == "module-remap-source" && strings.Contains(line, "source_name=rmic_computer_source ") {
			if err = exec.Command("pactl", "unload-module", fields[0]).Run(); err != nil {
				t.Fatal(err)
			}
			removed = true
			break
		}
	}
	if !removed {
		t.Fatal("native capture module not found")
	}
	deadline := time.After(6 * time.Second)
	for e.CaptureError() == "" {
		select {
		case <-deadline:
			t.Fatal("removed device was not paused")
		case <-time.After(20 * time.Millisecond):
		}
	}
	before := s.Status().Receive.Audio.Samples
	awaitFrame(t, p, func(pcm []int16) bool { return energy(pcm) < 100 })
	growthDeadline := time.After(3 * time.Second)
	for s.Status().Receive.Audio.Samples <= before {
		select {
		case <-growthDeadline:
			t.Fatal("microphone stopped after capture removal")
		case <-time.After(20 * time.Millisecond):
		}
	}
	status := s.Status()
	if status.Send.State != "paused" || status.Receive.State != "streaming" || status.Receive.Audio.Samples <= before {
		t.Fatalf("capture loss affected the other direction: %+v", status)
	}
}
