//go:build windows

package virtual

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
)

var mediaClass = windows.GUID{Data1: 0x4d36e96c, Data2: 0xe325, Data3: 0x11ce, Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18}}

type steamNode struct {
	index int
	info  *windows.DevInfoData
}

func steamNodes(set windows.DevInfo) ([]steamNode, error) {
	var nodes []steamNode
	for index := 0; ; index++ {
		info, err := set.EnumDeviceInfo(index)
		if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			return nodes, nil
		}
		if err != nil {
			return nil, err
		}
		value, err := set.DeviceRegistryProperty(info, windows.SPDRP_HARDWAREID)
		if errors.Is(err, windows.ERROR_INVALID_DATA) {
			continue
		}
		if err != nil {
			return nil, err
		}
		ids, ok := value.([]string)
		if !ok {
			continue
		}
		if target := steamHardwareIndex(ids); target >= 0 {
			nodes = append(nodes, steamNode{target, info})
		}
	}
}
func steamDeviceSet() (windows.DevInfo, error) {
	return windows.SetupDiGetClassDevsEx(&mediaClass, "", 0, 0, 0, "")
}
func windowsSteamStatus() (string, error) {
	set, err := steamDeviceSet()
	if err != nil {
		return "", err
	}
	defer set.Close()
	nodes, err := steamNodes(set)
	if err != nil {
		return "", err
	}
	counts := [2]int{}
	for _, node := range nodes {
		counts[node.index]++
	}
	return fmt.Sprintf("Steam Streaming Microphone：%d 个设备；Steam Streaming Speakers：%d 个设备。\n设备存在不代表已启用；请在配置页刷新并核实播放、录音两端。", counts[0], counts[1]), nil
}

// ApplySteamDriver rejects the legacy elevated helper entry point.
func ApplySteamDriver(action string) (string, error) {
	return "", fmt.Errorf("公开分发版不安装或卸载 Steam 驱动，请通过 Steam 官方客户端管理")
}

func manageWindowsSteam(action, dir string) (string, error) {
	if action != "status" {
		return ApplySteamDriver(action)
	}
	status, err := windowsSteamStatus()
	return status + "\n公开分发版不附带 Steam 驱动；请通过 Steam 官方客户端管理。", err
}
