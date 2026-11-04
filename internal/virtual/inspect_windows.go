//go:build windows

package virtual

import (
	"fmt"

	windowsdriver "cnb.cool/tdh6/RMic/drivers/windows"
)

func inspectDriver(info *DriverInfo, dir string) {
	if info.Prefix == "steam-" {
		set, err := steamDeviceSet()
		if err != nil {
			info.Error = err.Error()
			return
		}
		defer set.Close()
		nodes, err := steamNodes(set)
		if err != nil {
			info.Error = err.Error()
			return
		}
		counts := [2]int{}
		for _, node := range nodes {
			counts[node.index]++
		}
		info.Installed = counts[0] > 0 || counts[1] > 0
		info.Complete = counts[0] > 0 && counts[1] > 0
		info.Detail = fmt.Sprintf("Steam Streaming Microphone：%d；Steam Streaming Speakers：%d。设备存在不代表已启用，请刷新音频设备核实。", counts[0], counts[1])
		return
	}
	if info.Prefix == "" {
		if _, err := windowsdriver.LocalArchive(dir); err == nil {
			info.ReadOnly = false
			info.Description = "已启用本地原版安装包"
		} else {
			info.ReadOnly = true
			info.Detail = err.Error() + "；请解压随包 drivers/windows/VBCABLE_Driver_Pack45.zip，按 readme 手动安装；也可从 https://vb-audio.com/Cable/ 下载。\n"
		}
	}
	driver := map[string]string{"": "vbcable", "cable-a-": "vbcable-a", "cable-b-": "vbcable-b"}[info.Prefix]
	status, err := windowsCableStatusFor(driver)
	if err != nil {
		info.Error = err.Error()
		return
	}
	info.Installed, info.Complete = status != "", status != ""
	info.Detail += status
	if status == "" {
		info.Detail += "未检测到设备；安装后可能需要重启电脑。"
	}
}
