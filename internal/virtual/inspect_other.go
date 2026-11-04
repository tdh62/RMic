//go:build !windows

package virtual

import "runtime"

func inspectDriver(info *DriverInfo, dir string) {
	if runtime.GOOS != "linux" {
		info.Detail = PlatformGuide()
		return
	}
	device, err := Installed(dir)
	if err != nil {
		info.Error = err.Error()
		return
	}
	info.Installed = device != nil
	info.Complete = device != nil && device.Output != "" && device.Capture != ""
	if device != nil {
		info.Detail = "手机送入：" + device.Output + "\n电脑来源：" + device.Capture
	} else {
		info.Detail = "当前配置目录没有可保留通道。其他实例的通道不会被卸载。"
	}
}
