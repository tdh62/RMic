package audio

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/gen2brain/malgo"
)

var ErrMacEndpointMissing = errors.New("所选 CoreAudio 设备已失效；不会改用默认设备")

// Only independent BlackHole devices have a known direct input/output pairing.
// Aggregate and Multi-Output devices can contain the microphone cable, so they
// cannot be accepted as a verified independent return path.
func IsMacCable(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "blackhole 2ch", "blackhole 16ch", "blackhole 64ch":
		return true
	}
	return false
}

func MacMicrophoneDevices(outputs []string) []string {
	var names []string
	for _, name := range outputs {
		if IsMacCable(name) {
			names = append(names, name)
		}
	}
	return names
}

func ValidateMacDuplex(output, capture string) error {
	if !IsMacCable(output) || !IsMacCable(capture) {
		return fmt.Errorf("macOS 双向模式需要两条独立 BlackHole 设备，例如麦克风送入 BlackHole 2ch、电脑声音来源 BlackHole 16ch；暂不接受聚集或多输出设备")
	}
	if strings.EqualFold(strings.TrimSpace(output), strings.TrimSpace(capture)) {
		return fmt.Errorf("不能采集手机麦克风写入的同一 BlackHole 设备；请使用两条独立通道")
	}
	return nil
}

// CoreAudio IDs are NUL-terminated UIDs inside a larger backend union.
// Ignore unrelated trailing bytes and never equate empty default-device IDs.
func sameCoreAudioID(a, b malgo.DeviceID) bool {
	left, _, _ := strings.Cut(string(a[:]), "\x00")
	right, _, _ := strings.Cut(string(b[:]), "\x00")
	return left != "" && left == right
}

// CheckMacRoutes validates the actual pinned IDs on each check, rather than
// accepting newly selected system defaults or a replacement with the same name.
func (e *Engine) CheckMacRoutes() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("CoreAudio route checks require macOS")
	}
	e.mu.RLock()
	outputID, captureID := e.playback.id, e.capture.id
	e.mu.RUnlock()
	names := [2]string{}
	for i, kind := range []malgo.DeviceType{malgo.Playback, malgo.Capture} {
		devices, err := e.list(kind)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrMacEndpointMissing, err)
		}
		target := outputID
		if i == 1 {
			target = captureID
		}
		for _, device := range devices {
			if sameDeviceID(device.ID, target) {
				names[i] = device.Name()
				break
			}
		}
	}
	if names[0] == "" || names[1] == "" {
		return ErrMacEndpointMissing
	}
	if sameDeviceID(outputID, captureID) {
		return fmt.Errorf("不能读写同一个 CoreAudio 音频设备")
	}
	return ValidateMacDuplex(names[0], names[1])
}
