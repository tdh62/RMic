//go:build linux

package audio

import (
	"testing"

	"github.com/gen2brain/malgo"
)

// Exercise the same native-device lifecycle as WASAPI without physical hardware.
func TestNativeDeviceRecovery(t *testing.T) {
	// malgo v0.11.26 omits ma_backend_custom from its Go enum; its bundled
	// miniaudio.h places the null backend one slot after BackendNull.
	ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendNull + 1}, malgo.ContextConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{context: ctx, captureChannels: 2, stop: make(chan struct{}), done: make(chan struct{})}
	close(e.done) // Recovery is driven explicitly instead of starting watch.
	defer e.Close()
	e.SetGain(1.5)
	e.SetCaptureGain(.75)
	e.ToggleMute()
	source, sink := NewBuffer(), NewBuffer()
	e.SetSource(source)
	e.SetCaptureSink(sink)

	for _, item := range []struct {
		kind malgo.DeviceType
		ep   *endpoint
	}{{malgo.Playback, &e.playback}, {malgo.Capture, &e.capture}} {
		devices, err := e.list(item.kind)
		if err != nil || len(devices) == 0 {
			t.Fatalf("enumerate null endpoints: %v", err)
		}
		if err := e.startSelected(item.kind, devices[0], false, item.ep); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		kind malgo.DeviceType
		ep   *endpoint
	}{{malgo.Playback, &e.playback}, {malgo.Capture, &e.capture}} {
		id, name := item.ep.id, item.ep.name
		if err := item.ep.device.Stop(); err != nil {
			t.Fatal(err)
		}
		if item.ep.problem() == "" || !item.ep.recovering() {
			t.Fatal("device stop did not request recovery")
		}
		// A different endpoint (even with the same name) cannot satisfy recovery.
		item.ep.id[0] ^= 0xff
		e.recoverDevice(item.kind, item.ep)
		if item.ep.device.IsStarted() || item.ep.problem() == "" {
			t.Fatal("recovery accepted a different device identity")
		}
		item.ep.id = id
		e.recoverDevice(item.kind, item.ep)
		if item.ep.problem() != "" || item.ep.recovering() || !item.ep.device.IsStarted() {
			t.Fatalf("device did not resume: %s", item.ep.problem())
		}
		if item.ep.id != id || item.ep.name != name {
			t.Fatal("recovery changed selected endpoint")
		}
	}
	if e.source != source || e.captureSink != sink || e.Gain() != 1.5 || e.CaptureGain() != .75 || !e.Muted() {
		t.Fatal("recovery changed session buffers or volume controls")
	}

	// A safety pause must survive a subsequent native Stop notification.
	e.capture.interrupt("temporary loss")
	e.PauseCapture("unsafe route")
	if err := e.capture.device.Stop(); err != nil {
		t.Fatal(err)
	}
	e.recoverDevice(malgo.Capture, &e.capture)
	if e.CaptureError() != "unsafe route" || e.capture.recovering() || e.capture.device.IsStarted() {
		t.Fatal("automatic recovery cleared an explicit safety pause")
	}
}
