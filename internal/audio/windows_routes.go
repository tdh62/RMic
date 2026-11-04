package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// LoopbackPrefix explicitly selects a WASAPI render endpoint for capture. It
// never means the default output device and never enables microphone monitoring.
const LoopbackPrefix = "loopback:"

var ErrWindowsEndpointMissing = errors.New("已选 Windows 音频端点失效")

func decodeMicrophoneListening(data []byte) (bool, error) {
	if len(data) < 10 || binary.LittleEndian.Uint16(data) != 11 {
		return false, fmt.Errorf("无法识别 Windows 侦听设置，请检查录音设备属性")
	}
	return binary.LittleEndian.Uint16(data[8:]) != 0, nil
}

// VB-CABLE's render and capture endpoints have opposite Input/Output labels.
// Match the whole endpoint label, not a substring of a physical device name.
// The 16-channel render endpoint shares the original cable and must also be
// recognized when checking feedback, but is not offered for stereo mic output.
func cableEndpoint(name string) (family string, render, stereo bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, family = range []string{"cable", "cable-a", "cable-b", "cable-c", "cable-d"} {
		for _, suffix := range []string{" input", " output", " in 16 ch"} {
			label := family + suffix
			if name == label || strings.HasPrefix(name, label+" (") {
				return family, suffix != " output", suffix != " in 16 ch"
			}
		}
	}
	return "", false, false
}

// Steam Streaming Microphone is a separate virtual device from Steam Audio
// SDK and Steam Streaming Speakers. Render/capture direction comes from WASAPI
// enumeration; localized endpoint prefixes may differ while the model stays.
func steamMicrophone(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "steam streaming microphone" || strings.HasSuffix(name, " (steam streaming microphone)")
}

// WindowsMicrophonePeer resolves the recording endpoint used by other apps.
// Physical speakers cannot act as the virtual microphone's destination.
func WindowsMicrophonePeer(output string, captures []string) (string, error) {
	family, render, stereo := cableEndpoint(output)
	steam := steamMicrophone(output)
	if steam {
		family, render, stereo = "steam-microphone", true, true
	}
	if family == "" || !render || !stereo {
		return "", fmt.Errorf("手机麦克风必须送入 Steam Streaming Microphone 的虚拟播放端或 CABLE Input，不能选择物理扬声器或 CABLE Output；外部软件选择配对录音端")
	}
	var matches []string
	for _, name := range captures {
		if strings.HasPrefix(name, LoopbackPrefix) {
			continue
		}
		f, r, _ := cableEndpoint(name)
		if (steam && steamMicrophone(name)) || (!steam && f == family && !r) {
			matches = append(matches, name)
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("%s 对应的录音端匹配到 %d 个设备；请安装/启用对应虚拟驱动的两端，并保留 Steam 或 VB-CABLE 的原始名称", output, len(matches))
	}
	return matches[0], nil
}

func WindowsMicrophoneDevices(outputs, captures []string) []string {
	var result []string
	for _, name := range outputs {
		if _, err := WindowsMicrophonePeer(name, captures); err == nil {
			result = append(result, name)
		}
	}
	return result
}

func PreferredWindowsMicrophone(outputs, captures []string) string {
	var steam, cable []string
	for _, name := range WindowsMicrophoneDevices(outputs, captures) {
		if steamMicrophone(name) {
			steam = append(steam, name)
		} else if family, _, _ := cableEndpoint(name); family == "cable" {
			cable = append(cable, name)
		}
	}
	if len(steam) == 1 {
		return steam[0]
	}
	if len(steam) > 1 {
		return ""
	}
	if len(cable) == 1 {
		return cable[0]
	}
	return ""
}

// Windows currently supports known Steam Microphone and VB-CABLE families plus an explicitly chosen
// WASAPI render loopback. Third-party mixers and application monitoring are not
// inferred from unrelated friendly names.
func ValidateWindowsDuplex(output, capture string) error {
	outFamily, render, stereo := cableEndpoint(output)
	if steamMicrophone(output) {
		outFamily, render, stereo = "steam-microphone", true, true
	}
	if outFamily == "" || !render || !stereo {
		return fmt.Errorf("双向手机麦克风请选择 Steam Streaming Microphone 或 CABLE Input 虚拟播放端")
	}
	source, loopback := strings.CutPrefix(capture, LoopbackPrefix)
	if !loopback {
		source = capture
	}
	if source == "" {
		return fmt.Errorf("请明确选择电脑声音来源，不会使用默认设备")
	}
	family, captureRender, _ := cableEndpoint(source)
	if steamMicrophone(source) {
		family, captureRender = "steam-microphone", false
	}
	if family == outFamily {
		return fmt.Errorf("手机麦克风和电脑声音来源属于同一条 %s 虚拟通道，会把麦克风送回手机；请选择 loopback:扬声器/耳机、Steam Streaming Speakers，或另一条独立虚拟通道的录音端", strings.ToUpper(family))
	}
	if !loopback && (family == "" || captureRender) {
		return fmt.Errorf("双向电脑声音来源请选择 loopback:播放端，或另一条独立虚拟设备的录音端")
	}
	return nil
}
