// Package app contains desktop operations shared by the terminal and graphical frontends.
package app

import (
	"fmt"
	"runtime"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/transport"
	"cnb.cool/tdh6/RMic/internal/virtual"
)

type Devices struct {
	Windows, MacOS              bool
	Outputs, Captures, Networks []string
	Warning                     string
}

func Discover() (Devices, error) {
	devices := Devices{Windows: runtime.GOOS == "windows", MacOS: runtime.GOOS == "darwin"}
	if devices.MacOS {
		devices.Warning = "macOS：请使用两条独立 BlackHole 设备，并允许程序使用麦克风。"
	}
	addresses, err := transport.Interfaces()
	if err != nil {
		devices.Warning += " 无法读取网卡：" + err.Error()
	} else {
		for _, address := range addresses {
			if selected, err := transport.SelectAddresses([]transport.InterfaceAddress{address}, "", address.IP.String()); err == nil && len(selected) > 0 {
				devices.Networks = append(devices.Networks, address.IP.String())
			}
		}
	}
	engine, err := audio.NewEngine()
	if err != nil {
		devices.Warning += " 音频设备读取失败：" + err.Error()
		return devices, nil
	}
	defer engine.Close()
	devices.Outputs = engine.MicrophoneDevices()
	devices.Captures = engine.CaptureDevices()
	return devices, nil
}

func CheckAudioRoutes(engine *audio.Engine, mode audio.Mode) error {
	if mode != audio.ModeDuplex {
		return nil
	}
	switch runtime.GOOS {
	case "windows":
		return engine.CheckWindowsRoutes()
	case "darwin":
		return engine.CheckMacRoutes()
	default:
		output, capture := engine.PulseRoutes()
		return virtual.CheckRoutes(output, capture)
	}
}

func PrepareAudio(settings launcher.Settings) (*audio.Engine, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	engine, err := audio.NewEngine()
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			engine.Close()
		}
	}()
	output, capture := "", ""
	if settings.Mode.Microphone() {
		if settings.Output == "" {
			return nil, fmt.Errorf("请选择麦克风送入设备")
		}
		output = settings.Output
	}
	if settings.Mode.Playback() {
		if settings.Capture == "" {
			return nil, fmt.Errorf("请选择电脑声音来源")
		}
		capture = settings.Capture
	}
	if err = engine.Prepare(output, capture, settings.Channels(), settings.AudioIdleTimeout()); err != nil {
		return nil, err
	}
	if err = CheckAudioRoutes(engine, settings.Mode); err != nil {
		return nil, err
	}
	success = true
	return engine, nil
}
