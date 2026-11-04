package audio

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativeMultichannelCapture(t *testing.T) {
	if os.Getenv("RMIC_NATIVE_TEST") != "1" || runtime.GOOS != "linux" {
		t.Skip("requires native PulseAudio test session")
	}
	for _, channels := range []int{6, 8} {
		t.Run(fmt.Sprint(channels), func(t *testing.T) {
			layout := "front-left,front-right,front-center,lfe,side-left,side-right"
			if channels == 8 {
				layout = "front-left,front-right,front-center,lfe,rear-left,rear-right,side-left,side-right"
			}
			name := fmt.Sprintf("rmic_native_%d", channels)
			module, err := exec.Command("pactl", "load-module", "module-null-sink", "sink_name="+name, "sink_properties=device.description="+name, fmt.Sprintf("channels=%d", channels), "channel_map="+layout, "rate=48000").Output()
			if err != nil {
				t.Fatal(err)
			}
			defer exec.Command("pactl", "unload-module", strings.TrimSpace(string(module))).Run()
			engine, err := NewEngine()
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			source := ""
			for _, device := range engine.CaptureDevices() {
				if strings.Contains(device, name) {
					source = device
				}
			}
			if source == "" {
				t.Fatal("native source not found")
			}
			if err = engine.StartCaptureChannels(source, channels); err != nil {
				t.Fatal(err)
			}
			buffer := NewBufferForChannels(20, 200, channels)
			engine.SetCaptureSink(buffer)
			producer := exec.Command("pacat", "--playback", "--raw", "--format=s16le", "--rate=48000", fmt.Sprintf("--channels=%d", channels), "--channel-map="+layout, "--device="+name)
			stdin, err := producer.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = producer.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { stdin.Close(); producer.Process.Kill(); producer.Wait() }()
			data := make([]byte, 48000*channels*2*2)
			for i := 0; i < len(data)/2; i++ {
				binary.LittleEndian.PutUint16(data[i*2:], uint16((i%channels+1)*1000))
			}
			go func() { stdin.Write(data) }()
			deadline := time.Now().Add(4 * time.Second)
			out := make([]byte, 960*channels*2)
			for time.Now().Before(deadline) {
				buffer.Read(out, 1, false)
				if binary.LittleEndian.Uint16(out) == 1000 {
					for channel := 0; channel < channels; channel++ {
						if binary.LittleEndian.Uint16(out[channel*2:]) != uint16((channel+1)*1000) {
							t.Fatal("native channel order changed")
						}
					}
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("native multichannel samples not captured")
		})
	}
}
