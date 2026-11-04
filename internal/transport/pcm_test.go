package transport

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cnb.cool/tdh6/RMic/internal/pairing"
	"github.com/gorilla/websocket"
)

type pcmTicket struct {
	ID     string `json:"id"`
	Ticket string `json:"ticket"`
}

func createPCMTest(t *testing.T, h *httptest.Server, token string) pcmTicket {
	t.Helper()
	response := request(t, h, "POST", "/api/pcm", token, []byte(`{"mode":"playback","buffers":{"send_ms":20,"play_ms":80}}`))
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("PCM creation HTTP %d", response.StatusCode)
	}
	var result pcmTicket
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
func dialPCMTest(t *testing.T, h *httptest.Server, ticket string, expectedChannels ...int) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	conn, response, err := dialer.Dial(strings.Replace(h.URL, "https:", "wss:", 1)+"/api/pcm?ticket="+ticket, http.Header{"Origin": {h.URL}})
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	var format map[string]any
	if err = conn.ReadJSON(&format); err != nil {
		t.Fatal(err)
	}
	channels := 2
	if len(expectedChannels) > 0 {
		channels = expectedChannels[0]
	}
	if format["codec"] != "pcm_s16le" || format["channels"] != float64(channels) || format["version"] != float64(2) {
		t.Fatalf("invalid format: %v", format)
	}
	return conn
}
func TestPCMActualStereoAndSessionBoundaries(t *testing.T) {
	input := &testInput{}
	s, err := NewServer(&testOutput{}, "mic", 0, WithInput(input, "computer"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewTLSServer(s.Handler())
	defer h.Close()
	requireStatus(t, request(t, h, "POST", "/api/pcm", "", []byte(`{"mode":"playback"}`)), 401)
	for _, body := range []string{`{"mode":"invalid"}`, `{"mode":"playback","buffers":{"send_ms":0,"play_ms":80}}`, `{"mode":"playback","buffers":{"send_ms":20,"play_ms":4000}}`} {
		requireStatus(t, request(t, h, "POST", "/api/pcm", s.Token(), []byte(body)), 400)
	}
	ticket := createPCMTest(t, h, s.Token())
	requireStatus(t, request(t, h, "GET", "/api/pcm?ticket="+ticket.Ticket, "", nil, http.Header{"Origin": {"https://evil.example"}}), 403)
	conn := dialPCMTest(t, h, ticket.Ticket)
	requireStatus(t, request(t, h, "GET", "/api/pcm?ticket="+ticket.Ticket, "", nil), 401)
	requireStatus(t, request(t, h, "POST", "/api/pcm", s.Token(), []byte(`{"mode":"playback"}`)), 409)
	requireStatus(t, request(t, h, "POST", "/api/session", s.Token(), []byte(`{"type":"offer","mode":"playback"}`)), 409)
	requireStatus(t, request(t, h, "POST", "/api/session/"+ticket.ID+"/metrics", s.Token(), []byte(`{"queued_ms":80,"target_ms":80,"rtt_ms":12}`)), 204)
	// A burst larger than the capture queue must not create a reconnect loop.
	burst := make([]int16, 48000*2*300/1000)
	for i := 0; i < len(burst); i += 2 {
		burst[i] = 1234
		burst[i+1] = -2345
	}
	input.write(burst)
	if s.Status().Send.Audio.Dropped == 0 {
		t.Fatal("test did not exercise capture overflow")
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		pcm := make([]int16, 1920)
		for i := 0; i < len(pcm); i += 2 {
			pcm[i] = 1234
			pcm[i+1] = -2345
		}
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				input.write(pcm)
			}
		}
	}()
	defer func() { close(stop); <-done }()
	var frames uint64
	found := false
	for i := 0; i < 12; i++ {
		kind, packet, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind != websocket.BinaryMessage || len(packet) != 3864 || string(packet[:4]) != "RMPC" || binary.LittleEndian.Uint64(packet[8:]) != frames {
			t.Fatal("invalid PCM framing")
		}
		frames += 960
		conn.WriteJSON(map[string]any{"type": "metrics", "queued_ms": 80, "target_ms": 80, "rtt_ms": 12, "received_frames": frames})
		if int16(binary.LittleEndian.Uint16(packet[24:])) == 617 && int16(binary.LittleEndian.Uint16(packet[26:])) == -1172 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("independent PCM channels were not preserved")
	}
	if status := s.Status(); status.Transport != "ws-pcm" || status.Receive.State != "disabled" || status.Buffers.PlayMS != 80 {
		t.Fatalf("wrong session status: %+v", status)
	}
	requireStatus(t, request(t, h, "DELETE", "/api/session/"+ticket.ID, s.Token(), nil), 204)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	if s.Status().State != "waiting" {
		t.Fatal("PCM capture not released")
	}
}
func TestPCMRevocationAndSlowReaderCleanup(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "slow reader", true: "revoked"}[revoke], func(t *testing.T) {
			manager, err := pairing.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			grant, err := manager.Remember("phone")
			if err != nil {
				t.Fatal(err)
			}
			s, err := NewServer(nil, "", 0, WithInput(&testInput{}, "computer"), WithPairing(manager))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			h := httptest.NewTLSServer(s.Handler())
			defer h.Close()
			ticket := createPCMTest(t, h, grant.Token)
			conn := dialPCMTest(t, h, ticket.Ticket)
			other, err := manager.Remember("other phone")
			if err != nil {
				t.Fatal(err)
			}
			requireStatus(t, request(t, h, "DELETE", "/api/session/"+ticket.ID, other.Token, nil), 204)
			if s.Status().State == "waiting" {
				t.Fatal("foreign credential deleted PCM session")
			}
			requireStatus(t, request(t, h, "POST", "/api/session/"+ticket.ID+"/metrics", other.Token, []byte(`{"queued_ms":0,"target_ms":120,"rtt_ms":10}`)), 409)
			requireStatus(t, request(t, h, "POST", "/api/pcm", other.Token, []byte(`{"mode":"playback"}`), http.Header{"X-RMic-Session": {ticket.ID}}), 409)
			if revoke {
				if err = manager.Revoke(grant.DeviceID); err != nil {
					t.Fatal(err)
				}
			}
			// No feedback: sender credit must run out and close the connection.
			deadline := time.Now().Add(4 * time.Second)
			for time.Now().Before(deadline) {
				if s.Status().State == "waiting" {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			conn.Close()
			t.Fatal("PCM session failed to release after revocation or missing feedback")
		})
	}
}

type multichannelInput struct {
	testInput
	channels int
}

func (m *multichannelInput) CaptureChannels() int { return m.channels }
func TestPCMCompressedMultichannelDuplex(t *testing.T) {
	for _, channels := range []int{2, 6, 8} {
		t.Run(string(rune('0'+channels)), func(t *testing.T) {
			input := &multichannelInput{channels: channels}
			output := &testOutput{}
			s, err := NewServer(output, "mic", 0, WithInput(input, "computer"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			h := httptest.NewTLSServer(s.Handler())
			defer h.Close()
			body, _ := json.Marshal(map[string]any{"mode": "duplex", "compression": "deflate", "channels": channels})
			response := request(t, h, "POST", "/api/pcm", s.Token(), body)
			if response.StatusCode != 200 {
				requireStatus(t, response, 200)
				return
			}
			var ticket pcmTicket
			json.NewDecoder(response.Body).Decode(&ticket)
			response.Body.Close()
			conn := dialPCMTest(t, h, ticket.Ticket, channels)
			// Feed three independently compressed microphone frames into the real WS.
			microphone := make([]byte, 3840)
			for i := 0; i < len(microphone); i += 4 {
				binary.LittleEndian.PutUint16(microphone[i:], 1000)
				binary.LittleEndian.PutUint16(microphone[i+2:], 2000)
			}
			for i := 0; i < 3; i++ {
				packet, _ := encodePCM(microphone, 2, uint64(i*960), true)
				if err = conn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
					t.Fatal(err)
				}
			}
			computer := make([]int16, 960*channels*4)
			for i := range computer {
				computer[i] = int16((i%channels + 1) * 1000)
			}
			input.write(computer)
			found := false
			for frame := 0; frame < 8; frame++ {
				_, packet, err := conn.ReadMessage()
				if err != nil {
					t.Fatal(err)
				}
				raw, err := decodePCM(packet, channels, uint64(frame*960), true)
				if err != nil {
					t.Fatal(err)
				}
				conn.WriteJSON(map[string]any{"type": "metrics", "queued_ms": 80, "target_ms": 80, "received_frames": (frame + 1) * 960})
				if binary.LittleEndian.Uint16(raw) == 500 {
					for channel := 0; channel < channels; channel++ {
						if binary.LittleEndian.Uint16(raw[channel*2:]) != uint16((channel+1)*500) {
							t.Fatal("multichannel ordering corrupted")
						}
					}
					found = true
					break
				}
			}
			if !found {
				t.Fatal("computer multichannel audio not received")
			}
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				if s.Status().Receive.Audio.Samples >= 5760 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			output.mu.Lock()
			buffer := output.buffer
			output.mu.Unlock()
			if buffer == nil {
				t.Fatal("microphone not attached")
			}
			raw := make([]byte, 3840)
			buffer.Read(raw, 1, false)
			if !bytes.Equal(raw, microphone) {
				t.Fatal("compressed microphone audio changed")
			}
			if status := s.Status(); status.CaptureChannels != channels || status.PCMCompression != "deflate" || status.Receive.State != "streaming" {
				t.Fatalf("wrong duplex status: %+v", status)
			}
			input.muted.Store(true)
			if s.Status().Receive.State != "streaming" {
				t.Fatal("computer mute stopped microphone")
			}
		})
	}
}
func TestPCMMicrophoneOnlyAndMalformedUplink(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(map[bool]string{false: "microphone", true: "malformed"}[bad], func(t *testing.T) {
			output := &testOutput{}
			s, err := NewServer(output, "mic", 0)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			h := httptest.NewTLSServer(s.Handler())
			defer h.Close()
			response := request(t, h, "POST", "/api/pcm", s.Token(), []byte(`{"mode":"mic","compression":"deflate"}`))
			if response.StatusCode != 200 {
				requireStatus(t, response, 200)
				return
			}
			var ticket pcmTicket
			json.NewDecoder(response.Body).Decode(&ticket)
			response.Body.Close()
			conn := dialPCMTest(t, h, ticket.Ticket)
			packet, _ := encodePCM(make([]byte, 3840), 2, 0, true)
			if bad {
				packet[8] = 1
			}
			conn.WriteMessage(websocket.BinaryMessage, packet)
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				status := s.Status()
				if bad && status.State == "waiting" {
					return
				}
				if !bad && status.Receive.Audio.Samples == 1920 && status.Send.State == "disabled" {
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatal("invalid microphone state or malformed uplink not closed")
		})
	}
}
