package transport

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
)

type demandOutput struct {
	testOutput
	opens atomic.Int32
}

func (o *demandOutput) EnsureOutput() error { o.opens.Add(1); return nil }

type demandInput struct {
	testInput
	opens atomic.Int32
	fail  bool
}

func (i *demandInput) EnsureCapture() error {
	i.opens.Add(1)
	if i.fail {
		return errors.New("selected device unavailable")
	}
	return nil
}

func TestAudioAcquiredOnlyAfterAuthorizedConnection(t *testing.T) {
	out, in := &demandOutput{}, &demandInput{}
	s, err := NewServer(out, "mic", 0, WithInput(in, "computer"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewTLSServer(s.Handler())
	defer h.Close()
	requireStatus(t, request(t, h, "POST", "/api/pcm", "", []byte(`{"mode":"playback"}`)), 401)
	ticket := createPCMTest(t, h, s.Token())
	if in.opens.Load() != 0 || out.opens.Load() != 0 {
		t.Fatal("ticket request opened audio")
	}
	conn := dialPCMTest(t, h, ticket.Ticket)
	if in.opens.Load() != 1 || out.opens.Load() != 0 {
		t.Fatal("playback connection acquired wrong direction")
	}
	conn.Close()
	s.Disconnect()
	for _, mode := range []audio.Mode{audio.ModeMic, audio.ModePlayback, audio.ModeDuplex} {
		beforeOut, beforeIn := out.opens.Load(), in.opens.Load()
		p := connectMode(t, s, h, mode)
		deadline := time.Now().Add(3 * time.Second)
		for {
			s.mu.Lock()
			ready := s.active != nil && s.active.state == "connected"
			s.mu.Unlock()
			if ready {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("server did not activate connection")
			}
			time.Sleep(time.Millisecond)
		}
		wantOut, wantIn := beforeOut, beforeIn
		if mode.Microphone() {
			wantOut++
		}
		if mode.Playback() {
			wantIn++
		}
		if out.opens.Load() != wantOut || in.opens.Load() != wantIn {
			t.Fatalf("%s acquired wrong direction: output=%d input=%d", mode, out.opens.Load(), in.opens.Load())
		}
		_ = p.pc.Close()
		s.Disconnect()
	}
}

func TestDeviceOpenFailureLeavesServerAvailable(t *testing.T) {
	out, in := &demandOutput{}, &demandInput{fail: true}
	s, err := NewServer(out, "mic", 0, WithInput(in, "computer"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := httptest.NewTLSServer(s.Handler())
	defer h.Close()
	_ = connectMode(t, s, h, audio.ModeDuplex)
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(s.Status().Error, "selected device unavailable") {
		if time.Now().After(deadline) {
			t.Fatalf("device failure not reported: %+v", s.Status())
		}
		time.Sleep(time.Millisecond)
	}
	out.mu.Lock()
	attached := out.buffer != nil
	out.mu.Unlock()
	if attached {
		t.Fatal("partial failure left microphone buffer attached")
	}
	requireStatus(t, request(t, h, "GET", "/api/status", s.Token(), nil), 200)
	// The independent microphone direction remains usable despite capture failure.
	_ = connectMode(t, s, h, audio.ModeMic)
}
