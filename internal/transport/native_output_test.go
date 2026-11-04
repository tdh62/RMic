package transport

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/virtual"
)

func TestNativeOutputLossLeavesCaptureRunning(t *testing.T) {
	if os.Getenv("RMIC_NATIVE_TEST") != "1" {
		t.Skip("set RMIC_NATIVE_TEST=1 for native device loss")
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
	buffer := audio.NewBuffer()
	e.SetCaptureSink(buffer)
	output, capture := e.PulseRoutes()
	modules, err := exec.Command("pactl", "list", "short", "modules").Output()
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	for _, line := range strings.Split(string(modules), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && fields[1] == "module-null-sink" && strings.Contains(line, "sink_name=rmic_sink ") {
			if err = exec.Command("pactl", "unload-module", fields[0]).Run(); err != nil {
				t.Fatal(err)
			}
			removed = true
			break
		}
	}
	if !removed {
		t.Fatal("microphone output module not found")
	}
	deadline := time.After(6 * time.Second)
	for e.OutputError() == "" {
		select {
		case <-deadline:
			t.Fatal("output device loss was not detected")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err = virtual.CheckRoutes(output, capture); !errors.Is(err, virtual.ErrEndpointMissing) {
		t.Fatalf("missing endpoint must not be treated as a feedback loop: %v", err)
	}
	before := buffer.Stats().Samples
	for buffer.Stats().Samples <= before {
		select {
		case <-deadline:
			t.Fatal("capture stopped when the other device disappeared")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if e.CaptureError() != "" {
		t.Fatal("unrelated capture direction was paused")
	}
}
