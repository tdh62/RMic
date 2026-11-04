package transport

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/internal/shortcuts"
	"github.com/gorilla/websocket"
)

type shortcutRecorder struct{ events chan string }

func (b *shortcutRecorder) Key(key string, down bool) error {
	kind := "up:"
	if down {
		kind = "down:"
	}
	b.events <- kind + key
	return nil
}
func (b *shortcutRecorder) Text(r rune) error { b.events <- "text:" + string(r); return nil }
func expectShortcutEvent(t *testing.T, b *shortcutRecorder, want string) {
	t.Helper()
	select {
	case got := <-b.events:
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("missing %s", want)
	}
}
func shortcutFixture(t *testing.T) (*Server, *httptest.Server, *shortcutRecorder) {
	t.Helper()
	config := shortcuts.Config{Enabled: true, KeyIntervalMS: 20, Buttons: [3]shortcuts.Button{{Name: "保持", Mode: "hold", Action: "AltLeft+ShiftLeft"}, {Name: "输入", Mode: "tap", Action: "text:hello"}}}
	b := &shortcutRecorder{make(chan string, 100)}
	s, err := NewServer(&testOutput{}, "mic", 0, WithInput(&testInput{}, "computer"), WithShortcuts(config), func(s *Server) { s.shortcuts.engine = shortcuts.NewEngine(b) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := httptest.NewTLSServer(s.Handler())
	t.Cleanup(h.Close)
	return s, h, b
}
func TestShortcutIntervalAppliedAtStartupAndReconfiguration(t *testing.T) {
	s, _, _ := shortcutFixture(t)
	button := shortcuts.Button{Name: "保持", Mode: "hold", Action: "AltLeft+ShiftLeft"}
	for _, ms := range []int{20, 40, 0} {
		if ms != 20 {
			config := s.shortcuts.config
			config.Enabled = false // also exercise configuration on non-Windows hosts
			config.KeyIntervalMS = ms
			if err := s.ConfigureShortcuts(config); err != nil {
				t.Fatal(err)
			}
		}
		start := time.Now()
		if err := s.shortcuts.engine.Begin("interval", button); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed < time.Duration(ms)*time.Millisecond {
			t.Fatalf("key-down interval: %v, want >=%dms", elapsed, ms)
		}
		start = time.Now()
		if err := s.shortcuts.engine.End("interval"); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed < time.Duration(ms)*time.Millisecond {
			t.Fatalf("key-up interval: %v, want >=%dms", elapsed, ms)
		}
	}
}

func shortcutTicketTest(t *testing.T, s *Server, h *httptest.Server, token, id string) string {
	t.Helper()
	response := request(t, h, "POST", "/api/shortcuts/ticket", token, nil, http.Header{"X-RMic-Session": {id}, "X-RMic-Shortcuts": {"1"}})
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("ticket HTTP %d", response.StatusCode)
	}
	var result struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result.Ticket
}
func dialShortcutTest(t *testing.T, h *httptest.Server, ticket string) *websocket.Conn {
	t.Helper()
	d := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	conn, _, err := d.Dial(strings.Replace(h.URL, "https:", "wss:", 1)+"/api/shortcuts/control?ticket="+ticket, http.Header{"Origin": {h.URL}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	var response map[string]any
	if err = conn.ReadJSON(&response); err != nil || response["type"] != "ready" {
		t.Fatalf("ready: %v %v", response, err)
	}
	return conn
}
func holdShortcutTest(t *testing.T, conn *websocket.Conn, b *shortcutRecorder) {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{"type": "button.hold_begin", "seq": 1, "button_id": "1", "config_revision": 1, "hold_id": "a"}); err != nil {
		t.Fatal(err)
	}
	var reply map[string]any
	if err := conn.ReadJSON(&reply); err != nil || reply["error"] != nil {
		t.Fatalf("hold: %v %v", reply, err)
	}
	expectShortcutEvent(t, b, "down:AltLeft")
	expectShortcutEvent(t, b, "down:ShiftLeft")
}
func TestShortcutsHiddenAndUnauthorizedByDefault(t *testing.T) {
	s, err := NewServer(&testOutput{}, "mic", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewTLSServer(s.Handler())
	defer h.Close()
	if s.Status().Shortcuts != nil {
		t.Fatal("disabled shortcuts advertised")
	}
	requireStatus(t, request(t, h, "POST", "/api/shortcuts/ticket", "", nil), 401)
	requireStatus(t, request(t, h, "POST", "/api/shortcuts/ticket", s.Token(), nil), 404)
	requireStatus(t, request(t, h, "GET", "/api/shortcuts/control?ticket=invalid", "", nil), 403)
}
func TestShortcutTicketBindingAndLeaseRelease(t *testing.T) {
	s, h, b := shortcutFixture(t)
	audio := createPCMTest(t, h, s.Token())
	requireStatus(t, request(t, h, "POST", "/api/shortcuts/ticket", s.Token(), nil, http.Header{"X-RMic-Session": {"wrong"}, "X-RMic-Shortcuts": {"1"}}), 409)
	ticket := shortcutTicketTest(t, s, h, s.Token(), audio.ID)
	requireStatus(t, request(t, h, "GET", "/api/shortcuts/control?ticket="+ticket, "", nil, http.Header{"Origin": {"https://evil.example"}}), 403)
	conn := dialShortcutTest(t, h, ticket)
	requireStatus(t, request(t, h, "GET", "/api/shortcuts/control?ticket="+ticket, "", nil), 403)
	holdShortcutTest(t, conn, b)
	// No heartbeat: lease expiry must release without waiting for audio timeout.
	expectShortcutEvent(t, b, "up:ShiftLeft")
	expectShortcutEvent(t, b, "up:AltLeft")
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expired control stayed open")
	}
}
func TestShortcutLifecycleReleasesHeldKeys(t *testing.T) {
	for _, scenario := range []string{"disconnect", "disable", "replace", "revoke"} {
		t.Run(scenario, func(t *testing.T) {
			s, h, b := shortcutFixture(t)
			token := s.Token()
			var manager *pairing.Manager
			var deviceID string
			if scenario == "revoke" {
				var err error
				manager, err = pairing.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				result, err := manager.Remember("phone")
				if err != nil {
					t.Fatal(err)
				}
				s.pairing = manager
				token = result.Token
				deviceID = result.DeviceID
			}
			audio := createPCMTest(t, h, token)
			conn := dialShortcutTest(t, h, shortcutTicketTest(t, s, h, token, audio.ID))
			holdShortcutTest(t, conn, b)
			switch scenario {
			case "disconnect":
				s.Disconnect()
			case "disable":
				if err := s.ConfigureShortcuts(shortcuts.Config{}); err != nil {
					t.Fatal(err)
				}
				if s.Status().Shortcuts != nil {
					t.Fatal("disabled capability leaked")
				}
			case "replace":
				requireStatus(t, request(t, h, "POST", "/api/pcm", token, []byte(`{"mode":"playback"}`), http.Header{"X-RMic-Session": {audio.ID}}), 200)
			case "revoke":
				if err := manager.Revoke(deviceID); err != nil {
					t.Fatal(err)
				}
				_ = conn.WriteJSON(map[string]any{"type": "heartbeat", "seq": 2})
			}
			expectShortcutEvent(t, b, "up:ShiftLeft")
			expectShortcutEvent(t, b, "up:AltLeft")
		})
	}
}
