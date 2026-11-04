package audio

import (
	"reflect"
	"testing"
)

func TestSteamMicrophonePairsUseEnumeratedEndpointRoles(t *testing.T) {
	render := "Speakers (Steam Streaming Microphone)"
	capture := "Microphone (Steam Streaming Microphone)"
	outputs := []string{"Speakers (Realtek)", render, "Speakers (Steam Streaming Speakers)", "CABLE Input"}
	captures := []string{capture, "CABLE Output", LoopbackPrefix + render}
	peer, err := WindowsMicrophonePeer(render, captures)
	if err != nil || peer != capture {
		t.Fatalf("Steam microphone did not resolve its recording peer: %s %v", peer, err)
	}
	if got := WindowsMicrophoneDevices(outputs, captures); !reflect.DeepEqual(got, []string{render, "CABLE Input"}) {
		t.Fatalf("wrong virtual microphone destinations: %v", got)
	}
	if _, err = WindowsMicrophonePeer("Speakers (Steam Streaming Speakers)", captures); err == nil {
		t.Fatal("Steam Speakers incorrectly treated as virtual mic")
	}
	if _, err = WindowsMicrophonePeer(render, []string{LoopbackPrefix + render}); err == nil {
		t.Fatal("render loopback incorrectly used as microphone's recording peer")
	}
	if _, err = WindowsMicrophonePeer(render, []string{capture, capture}); err == nil {
		t.Fatal("ambiguous Steam recording endpoints accepted")
	}
	peer, err = WindowsMicrophonePeer("扬声器 (Steam Streaming Microphone)", []string{"麦克风 (Steam Streaming Microphone)"})
	if err != nil || peer != "麦克风 (Steam Streaming Microphone)" {
		t.Fatalf("localized prefix rejected: %q %v", peer, err)
	}
	if _, err = WindowsMicrophonePeer("Speakers (Steam Streaming Microphone) Fake", captures); err == nil {
		t.Fatal("partial model name accepted")
	}
	if got := PreferredWindowsMicrophone(outputs, captures); got != render {
		t.Fatalf("fresh setup did not prefer installed Steam: %q", got)
	}
	if got := PreferredWindowsMicrophone(append(outputs, render), captures); got != "" {
		t.Fatal("ambiguous Steam render silently replaced")
	}
}

func TestWindowsListenPreferenceDecoding(t *testing.T) {
	disabled := []byte{11, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	enabled := append([]byte(nil), disabled...)
	enabled[8] = 255
	enabled[9] = 255
	for _, sample := range []struct {
		data []byte
		want bool
	}{{disabled, false}, {enabled, true}, {enabled[:10], true}} {
		got, err := decodeMicrophoneListening(sample.data)
		if err != nil || got != sample.want {
			t.Fatalf("listen preference %x: %t %v", sample.data, got, err)
		}
	}
	for _, data := range [][]byte{nil, {11, 0}, {8, 0, 0, 0, 0, 0, 0, 0, 0, 0}} {
		if _, err := decodeMicrophoneListening(data); err == nil {
			t.Fatal("unsupported registry data treated as monitoring disabled")
		}
	}
}

func TestWindowsMicrophoneIsPairedVirtualCable(t *testing.T) {
	captures := []string{"Microphone (Realtek)", "CABLE Output (VB-Audio Virtual Cable)", "CABLE-A Output (VB-Audio Cable A)"}
	outputs := []string{"Speakers (Realtek)", "CABLE Input (VB-Audio Virtual Cable)", "Headphones", "CABLE-A Input (VB-Audio Cable A)", "CABLE In 16 Ch (VB-Audio Virtual Cable)", "Fake CABLE Input"}
	got := WindowsMicrophoneDevices(outputs, captures)
	if want := []string{outputs[1], outputs[3]}; !reflect.DeepEqual(got, want) {
		t.Fatalf("physical/ambiguous destinations offered: %v", got)
	}
	for _, output := range []string{outputs[0], outputs[2], outputs[4], outputs[5], "CABLE Output"} {
		if _, err := WindowsMicrophonePeer(output, captures); err == nil {
			t.Fatalf("non-stereo-cable microphone destination accepted: %s", output)
		}
	}
	peer, err := WindowsMicrophonePeer(outputs[1], captures)
	if err != nil || peer != captures[1] {
		t.Fatalf("wrong external app recording endpoint: %s %v", peer, err)
	}
	if _, err = WindowsMicrophonePeer(outputs[1], nil); err == nil {
		t.Fatal("missing driver recording endpoint accepted")
	}
	if _, err = WindowsMicrophonePeer(outputs[1], append(captures, captures[1])); err == nil {
		t.Fatal("ambiguous recording endpoint accepted")
	}
}

func TestWindowsDuplexKeepsMicrophoneOutOfComputerCapture(t *testing.T) {
	for _, test := range []struct {
		output, capture string
		allowed         bool
	}{
		{"CABLE Input", "loopback:Speakers (Realtek)", true},
		{"CABLE Input", "loopback:Headphones (USB DAC)", true},
		{"CABLE Input", "CABLE-A Output (VB-Audio Cable A)", true},
		{"CABLE-A Input", "CABLE-B Output", true},
		{"CABLE Input", "loopback:CABLE-A Input", true},
		{"Speakers (Steam Streaming Microphone)", "loopback:Speakers (Realtek)", true},
		{"Speakers (Steam Streaming Microphone)", "loopback:Speakers (Steam Streaming Speakers)", true},
		{"Speakers (Steam Streaming Microphone)", "CABLE Output", true},
		{"CABLE Input", "Microphone (Steam Streaming Microphone)", true},
		{"Speakers (Steam Streaming Microphone)", "Microphone (Steam Streaming Microphone)", false},
		{"Speakers (Steam Streaming Microphone)", "loopback:Speakers (Steam Streaming Microphone)", false},
		{"扬声器 (Steam Streaming Microphone)", "麦克风 (Steam Streaming Microphone)", false},
		{"CABLE Input", "CABLE Output", false},
		{"CABLE Input", "loopback:CABLE Input", false},
		{"CABLE Input", "loopback:CABLE In 16 Ch (VB-Audio Virtual Cable)", false},
		{"CABLE-A Input", "CABLE-A Output", false},
		{"CABLE Input", "Microphone (Realtek)", false},
		{"CABLE Input", "loopback:", false},
		{"Speakers", "CABLE Output", false},
		{"CABLE Input", "CABLE-A Input", false},
	} {
		err := ValidateWindowsDuplex(test.output, test.capture)
		if (err == nil) != test.allowed {
			t.Errorf("%s -> %s: allowed=%t err=%v", test.output, test.capture, test.allowed, err)
		}
	}
}
