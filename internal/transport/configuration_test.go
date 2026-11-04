package transport

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/pairing"
)

func TestConfigurationKeepsServiceAndTemporaryAuthorization(t *testing.T) {
	manager, err := pairing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := manager.Create("phone", "127.0.0.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.DecideOnce(ticket.ID); err != nil {
		t.Fatal(err)
	}
	authorization, err := manager.Poll(ticket.ID, ticket.Secret)
	if err != nil {
		t.Fatal(err)
	}
	output, input := &testOutput{}, &testInput{}
	server, err := NewServer(output, "mic", 0, WithInput(input, "source"), WithPairing(manager))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	httpServer := httptest.NewTLSServer(server.Handler())
	defer httpServer.Close()
	initial := server.Status()
	active := createPCMTest(t, httpServer, authorization.Token)
	conn := dialPCMTest(t, httpServer, active.Ticket)
	if err = server.Configure(output, "mic", input, "source", audio.ModePlayback, "music", false); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	same := server.active != nil && server.active.id == active.ID
	server.mu.Unlock()
	if !same || server.Status().ConfigRevision != initial.ConfigRevision {
		t.Fatal("defaults replaced the current session")
	}
	if server.Status().DefaultMode != audio.ModePlayback || server.Status().DefaultQuality.ID != "music" {
		t.Fatal("defaults not published separately from session settings")
	}

	replacement := &multichannelInput{channels: 6}
	if err = server.Configure(nil, "", replacement, "surround", audio.ModePlayback, "balanced", true); err != nil {
		t.Fatal(err)
	}
	status := server.Status()
	if status.InstanceID != initial.InstanceID || status.ConfigRevision != initial.ConfigRevision+1 || status.CaptureChannels != 6 || len(status.Modes) != 1 || status.Modes[0] != audio.ModePlayback {
		t.Fatalf("wrong replacement capabilities: %+v", status)
	}
	if server.PairingManager() != manager || !manager.Valid(authorization.Token) {
		t.Fatal("temporary authorization was lost")
	}
	output.mu.Lock()
	detached := output.buffer == nil
	output.mu.Unlock()
	input.mu.Lock()
	detached = detached && input.sink == nil
	input.mu.Unlock()
	if !detached {
		t.Fatal("old devices retain session buffers")
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	for {
		if _, _, err = conn.ReadMessage(); err != nil {
			break
		}
	}
	requireStatus(t, request(t, httpServer, "GET", "/api/status", authorization.Token, nil), 200)

	headers := http.Header{"X-RMic-Config": {fmt.Sprint(initial.ConfigRevision)}, "X-RMic-Instance": {initial.InstanceID}}
	for _, path := range []string{"/api/session", "/api/pcm"} {
		response := request(t, httpServer, "POST", path, authorization.Token, []byte(`{"type":"offer","mode":"playback"}`), headers)
		if response.Header.Get("X-RMic-Error") != "configuration_changed" {
			t.Fatal("stale capabilities did not request refresh")
		}
		requireStatus(t, response, 409)
	}
	requireStatus(t, request(t, httpServer, "POST", "/api/pcm", authorization.Token, []byte(`{"mode":"mic"}`)), 422)
	next := createPCMTest(t, httpServer, authorization.Token)
	nextConn := dialPCMTest(t, httpServer, next.Ticket, 6)
	defer nextConn.Close()
	if err = manager.Revoke(authorization.DeviceID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(t, httpServer, "GET", "/api/status", authorization.Token, nil), 401)
}

func TestConfigurationValidationPreservesActiveSession(t *testing.T) {
	output, input := &testOutput{}, &testInput{}
	server, err := NewServer(output, "mic", 0, WithInput(input, "source"))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	h := httptest.NewTLSServer(server.Handler())
	defer h.Close()
	active := createPCMTest(t, h, server.Token())
	initial := server.Status()
	for _, bad := range []struct {
		mode    audio.Mode
		quality string
	}{{audio.ModeMic, "ultra"}, {audio.Mode("bad"), "balanced"}, {audio.ModeMic, "missing"}} {
		if server.Configure(output, "mic", input, "source", bad.mode, bad.quality, true) == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	if server.Configure(nil, "", input, "source", audio.ModeDuplex, "balanced", true) == nil {
		t.Fatal("missing direction accepted")
	}
	server.mu.Lock()
	same := server.active != nil && server.active.id == active.ID
	server.mu.Unlock()
	if !same || server.Status().ConfigRevision != initial.ConfigRevision {
		t.Fatal("failed configuration interrupted the session")
	}
}

func TestConcurrentConfigurationAndStatus(t *testing.T) {
	server, err := NewServer(&testOutput{}, "mic", 0, WithInput(&testInput{}, "source"))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	h := httptest.NewTLSServer(server.Handler())
	defer h.Close()
	var group sync.WaitGroup
	group.Go(func() {
		for range 100 {
			_ = server.Status()
		}
	})
	for range 10 {
		ticket := createPCMTest(t, h, server.Token())
		conn := dialPCMTest(t, h, ticket.Ticket)
		if err := server.Configure(&testOutput{}, "new mic", &testInput{}, "new source", audio.ModeMic, "balanced", true); err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
	group.Wait()
}
