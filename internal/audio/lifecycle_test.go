package audio

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativeOnDemandLifecycle(t *testing.T) {
	if os.Getenv("RMIC_NATIVE_TEST") != "1" || runtime.GOOS != "linux" {
		t.Skip("requires private PulseAudio session")
	}
	name := "rmic_demand_lifecycle"
	load := func() string {
		t.Helper()
		out, err := exec.Command("pactl", "load-module", "module-null-sink", "sink_name="+name, "sink_properties=device.description="+name, "rate=48000", "channels=2").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	module := load()
	defer func() { _ = exec.Command("pactl", "unload-module", module).Run() }()
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	capture := ""
	for _, device := range e.CaptureDevices() {
		if strings.Contains(device, name) {
			capture = device
		}
	}
	if capture == "" {
		t.Fatal("monitor source not found")
	}
	streams := func(kind string) int {
		t.Helper()
		out, err := exec.Command("pactl", "list", "short", kind).Output()
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(out)) == "" {
			return 0
		}
		return len(strings.Split(strings.TrimSpace(string(out)), "\n"))
	}
	assertStreams := func(playback, recording int) {
		t.Helper()
		if got := streams("sink-inputs"); got != playback {
			t.Fatalf("playback streams=%d, want %d", got, playback)
		}
		if got := streams("source-outputs"); got != recording {
			t.Fatalf("record streams=%d, want %d", got, recording)
		}
	}
	basePlay, baseRecord := streams("sink-inputs"), streams("source-outputs")
	if err := e.Prepare(name, capture, 2, time.Second); err != nil {
		t.Fatal(err)
	}
	assertStreams(basePlay, baseRecord)
	if e.OutputDeviceState() != "released" || e.CaptureDeviceState() != "released" {
		t.Fatal("preparation opened devices")
	}
	for i := 0; i < 3; i++ {
		if err := e.EnsureOutput(); err != nil {
			t.Fatal(err)
		}
		if err := e.EnsureCapture(); err != nil {
			t.Fatal(err)
		}
		e.SetSource(NewBuffer())
		e.SetCaptureSink(NewBuffer())
		assertStreams(basePlay+1, baseRecord+1)
		e.SetCaptureSink(nil)
		e.lifecycle.Lock()
		e.releaseIdle(&e.capture, time.Now().Add(2*time.Second))
		e.lifecycle.Unlock()
		assertStreams(basePlay+1, baseRecord)
		if e.OutputDeviceState() != "open" {
			t.Fatal("capture release interrupted microphone direction")
		}
		e.SetCaptureSink(NewBuffer())
		e.SetCaptureSink(nil)
		e.SetCaptureSink(NewBuffer()) // reconnect cancels expiration
		e.lifecycle.Lock()
		released := e.releaseIdle(&e.capture, time.Now().Add(time.Hour))
		e.lifecycle.Unlock()
		if released {
			t.Fatal("stale deadline released reconnected capture")
		}
		e.ToggleCaptureMute()
		e.lifecycle.Lock()
		released = e.releaseIdle(&e.capture, time.Now().Add(time.Hour))
		e.lifecycle.Unlock()
		if released {
			t.Fatal("mute released an active device")
		}
		e.ToggleCaptureMute()
		e.SetSource(nil)
		e.SetCaptureSink(nil)
		deadline := time.Now().Add(4 * time.Second)
		for e.OutputDeviceState() != "released" || e.CaptureDeviceState() != "released" {
			if time.Now().After(deadline) {
				t.Fatalf("watcher failed to release idle devices: output=%s capture=%s", e.OutputDeviceState(), e.CaptureDeviceState())
			}
			time.Sleep(10 * time.Millisecond)
		}
		assertStreams(basePlay, baseRecord)
	}
	// A new device with the same name must never replace the selected identity.
	if err := exec.Command("pactl", "unload-module", module).Run(); err != nil {
		t.Fatal(err)
	}
	module = load()
	if err := e.EnsureCapture(); err == nil {
		t.Fatal("same-name replacement was silently opened")
	}
	if e.CaptureError() == "" {
		t.Fatal("missing device error was not exposed")
	}
	assertStreams(basePlay, baseRecord)
	e.Close()
	if err := e.EnsureOutput(); err == nil {
		t.Fatal("closed engine reopened")
	}
	assertStreams(basePlay, baseRecord)
}

func TestIdleReleaseRequiresNoDemand(t *testing.T) {
	// Exercise the deadline invariant without any native device dependency.
	e := &Engine{onDemand: true, idleTimeout: 30 * time.Second}
	e.playback.enabled = true
	e.playback.idleSince = time.Now()
	if e.releaseIdle(&e.playback, e.playback.idleSince.Add(29*time.Second)) {
		t.Fatal("released before timeout")
	}
	e.playback.idleSince = time.Time{}
	if e.releaseIdle(&e.playback, time.Now().Add(time.Hour)) {
		t.Fatal("released active direction")
	}
	for _, d := range []time.Duration{0, -time.Second} {
		if err := e.Prepare("", "", 2, d); err == nil {
			t.Fatal(fmt.Sprintf("accepted idle duration %s", d))
		}
	}
}
