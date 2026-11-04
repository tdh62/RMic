package virtual

import "runtime"

// DriverInfo separates a device's presence from an installer's exit status.
// Complete means that all devices supplied by this provider were found; audio
// endpoint availability is still checked independently by the audio engine.
type DriverInfo struct {
	Name, Prefix, Description string
	Installed, Complete       bool
	ReadOnly                  bool
	Detail, Error             string
}

func DriverProviders() []DriverInfo {
	switch runtime.GOOS {
	case "windows":
		return []DriverInfo{
			{Name: "Steam Streaming", Prefix: "steam-", Description: "通过 Steam 官方客户端管理", ReadOnly: true},
			{Name: "VB-CABLE", Description: "解压随包 ZIP 按 readme 手动安装；vb-audio.com/Cable/", ReadOnly: true},
		}
	case "linux":
		return []DriverInfo{{Name: "RMic 虚拟通道", Description: "两条独立 PulseAudio / PipeWire 通道"}}
	default:
		return []DriverInfo{{Name: "BlackHole", Description: "2ch 与 16ch 两条独立设备", ReadOnly: true}}
	}
}

func (m *Manager) Inspect() []DriverInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := DriverProviders()
	for i := range items {
		if m.closed {
			items[i].Error = "虚拟设备管理器已关闭"
			continue
		}
		inspectDriver(&items[i], m.dir)
	}
	return items
}
