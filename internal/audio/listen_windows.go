//go:build windows

package audio

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Windows stores the recording endpoint's Listen-to-this-device preference as
// a serialized VT_BOOL. Read it without changing the user's routing settings.
func microphoneListening(endpointID string) (bool, error) {
	index := strings.LastIndex(endpointID, "{")
	if index < 0 || !strings.HasSuffix(endpointID, "}") {
		return false, fmt.Errorf("无法识别 WASAPI 录音端点 ID")
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\MMDevices\Audio\Capture\`+endpointID[index:]+`\Properties`, registry.QUERY_VALUE)
	if err != nil {
		return false, fmt.Errorf("读取 Windows 侦听设置：%w", err)
	}
	defer key.Close()
	const property = `{24dbb0fc-9311-4b3d-9cf0-18ff155639d4},1`
	data, _, err := key.GetBinaryValue(property)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取 Windows 侦听设置：%w", err)
	}
	return decodeMicrophoneListening(data)
}
