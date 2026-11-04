package transport

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/virtual"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type testOutput struct {
	mu     sync.Mutex
	buffer *audio.Buffer
}

func (o *testOutput) SetSource(b *audio.Buffer) { o.mu.Lock(); o.buffer = b; o.mu.Unlock() }
func (o *testOutput) Gain() float64             { return 1 }
func (o *testOutput) Muted() bool               { return false }
func (o *testOutput) OutputError() string       { return "" }

func request(t *testing.T, server *httptest.Server, method, path, token string, body []byte, extra ...http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for _, headers := range extra {
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
func requireStatus(t *testing.T, res *http.Response, status int) {
	t.Helper()
	defer res.Body.Close()
	if res.StatusCode != status {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("HTTP %d, want %d: %s", res.StatusCode, status, b)
	}
}

func TestAuthenticationAndWebAssets(t *testing.T) {
	s, err := NewServer(&testOutput{}, "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewTLSServer(s.Handler())
	defer h.Close()
	requireStatus(t, request(t, h, "GET", "/api/status", "wrong", nil), 401)
	requireStatus(t, request(t, h, "GET", "/api/status", s.Token(), nil), 200)
	requireStatus(t, request(t, h, "POST", "/api/session", s.Token(), []byte(`{"type":"answer","sdp":"invalid"}`)), 400)
	requireStatus(t, request(t, h, "POST", "/api/session", s.Token(), bytes.Repeat([]byte("x"), 100000)), 400)
	req, _ := http.NewRequest("GET", h.URL+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token())
	req.Header.Set("Origin", "https://untrusted.example")
	res, err := h.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, res, 403)
	res, err = h.Client().Get(h.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP")
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !bytes.Contains(b, []byte("RMic")) {
		t.Fatal("embedded web client missing")
	}
}

// Set RMIC_NATIVE_TEST=1 in a desktop PulseAudio/PipeWire session to also
// verify decoded WebRTC audio at the virtual microphone's actual recording end.
func TestWebRTCAudioAndSessionLifecycle(t *testing.T) {
	var output Output = &testOutput{}
	var captured chan bool
	if os.Getenv("RMIC_NATIVE_TEST") == "1" {
		v, err := virtual.Create()
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
		output = e
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		command := exec.CommandContext(ctx, "parecord", "--raw", "--format=s16le", "--rate=48000", "--channels=2", "--device=rmic_mic")
		stdout, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = command.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { cancel(); _ = command.Wait() }()
		captured = make(chan bool, 1)
		go func() {
			data := make([]byte, 3840)
			for {
				n, err := io.ReadFull(stdout, data)
				if err != nil {
					return
				}
				for i := 0; i+1 < n; i += 2 {
					v := int16(binary.LittleEndian.Uint16(data[i:]))
					if v > 1000 || v < -1000 {
						select {
						case captured <- true:
						default:
						}
						return
					}
				}
			}
		}()
	}
	s, err := NewServer(output, "test output", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewTLSServer(s.Handler())
	defer h.Close()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "rmic-test")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		b := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(b); err != nil {
				return
			}
		}
	}()
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
		t.Fatal("client gathering timeout")
	}
	body, _ := json.Marshal(pc.LocalDescription())
	res := request(t, h, "POST", "/api/session", s.Token(), body)
	if res.StatusCode != 200 {
		requireStatus(t, res, 200)
	}
	var answer struct {
		ID     string                    `json:"id"`
		Answer webrtc.SessionDescription `json:"answer"`
	}
	if err = json.NewDecoder(res.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	requireStatus(t, request(t, h, "POST", "/api/session", s.Token(), body), 409)
	if err = pc.SetRemoteDescription(answer.Answer); err != nil {
		t.Fatal(err)
	}
	packet, err := os.ReadFile("../codec/testdata/tone.opus")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(12 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	received := false
	for !received {
		select {
		case <-deadline:
			t.Fatalf("audio did not arrive: %+v", s.Status())
		case <-ticker.C:
			if err = track.WriteSample(media.Sample{Data: packet, Duration: 20 * time.Millisecond}); err != nil {
				t.Fatal(err)
			}
			received = s.Status().Audio.Samples >= 1920*6
		}
	}
	if captured != nil {
		for {
			select {
			case <-captured:
				goto verified
			case <-deadline:
				t.Fatal("native virtual microphone remained silent")
			case <-ticker.C:
				_ = track.WriteSample(media.Sample{Data: packet, Duration: 20 * time.Millisecond})
			}
		}
	}
verified:
	previousID := answer.ID
	res = request(t, h, "POST", "/api/session", s.Token(), body, http.Header{"X-RMic-Session": []string{previousID}})
	if res.StatusCode != 200 {
		requireStatus(t, res, 200)
	}
	if err = json.NewDecoder(res.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if answer.ID == previousID {
		t.Fatal("resumed connection must receive a fresh session ID")
	}
	requireStatus(t, request(t, h, "DELETE", "/api/session/"+previousID, s.Token(), nil), 204)
	if s.Status().State == "waiting" {
		t.Fatal("stale disconnect released another session")
	}
	requireStatus(t, request(t, h, "DELETE", "/api/session/"+answer.ID, s.Token(), nil), 204)
	if s.Status().State != "waiting" {
		t.Fatalf("session not released: %+v", s.Status())
	}
	// A new session can be reserved immediately, even while the old peer closes.
	res = request(t, h, "POST", "/api/session", s.Token(), body)
	requireStatus(t, res, 200)
	s.Disconnect()
}
