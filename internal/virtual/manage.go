package virtual

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type installation struct {
	Owner    string        `json:"owner"`
	Output   string        `json:"output,omitempty"`
	Input    string        `json:"input,omitempty"`
	Playback string        `json:"playback,omitempty"`
	Capture  string        `json:"capture,omitempty"`
	Modules  []ownedModule `json:"modules"`
}

func installationPath(dir string) string { return filepath.Join(dir, "virtual-devices.json") }
func readInstallation(dir string) (*installation, error) {
	data, err := os.ReadFile(installationPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state installation
	if err = json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if token, err := hex.DecodeString(state.Owner); err != nil || len(token) != 16 || len(state.Modules) == 0 {
		return nil, fmt.Errorf("invalid virtual device ownership record")
	}
	for _, module := range state.Modules {
		if _, err := strconv.ParseUint(module.ID, 10, 32); err != nil {
			return nil, fmt.Errorf("invalid virtual module ID")
		}
		if (module.Name != "module-null-sink" && module.Name != "module-remap-source") || !strings.Contains(module.Argument, "rmic.owner="+state.Owner+"\"") {
			return nil, fmt.Errorf("invalid virtual module ownership tag")
		}
	}
	return &state, nil
}
func (state *installation) device() *Device {
	return &Device{Output: state.Output, Input: state.Input, Playback: state.Playback, Capture: state.Capture, modules: append([]ownedModule(nil), state.Modules...)}
}
func matchingModules(state *installation) (int, error) {
	data, err := run("list", "short", "modules")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, module := range state.Modules {
		for _, line := range strings.Split(data, "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) >= 3 && fields[0] == module.ID && fields[1] == module.Name && fields[2] == module.Argument {
				count++
				break
			}
		}
	}
	return count, nil
}

// Installed returns reusable devices without transferring their lifetime to the
// application. Installed modules remain after RMic exits.
func Installed(dir string) (*Device, error) {
	state, err := readInstallation(dir)
	if err != nil || state == nil {
		return nil, err
	}
	count, err := matchingModules(state)
	if err != nil {
		return nil, err
	}
	if count != len(state.Modules) {
		return nil, fmt.Errorf("retained virtual devices are incomplete (%d/%d); run 'rmic virtual uninstall' then 'rmic virtual install' with the same --config-dir", count, len(state.Modules))
	}
	device := state.device()
	device.modules = nil
	return device, nil
}
func saveInstallation(dir string, state *installation) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".virtual-devices-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), installationPath(dir))
}
func PlatformGuide() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows：解压随包 drivers/windows/VBCABLE_Driver_Pack45.zip，按原版 readme 手动安装（也可从 https://vb-audio.com/Cable/ 下载），重启后刷新设备。配置目录中的 vbcable-installer.json 可指定本地原版 Pack45 ZIP 以启用安装/卸载，仍需确认系统 UAC。公开版仅原样附带基础 ZIP，不提供 CABLE-A/B 安装；Steam 驱动通过 Steam 官方客户端管理。双向需独立音频通道，并关闭“侦听此设备”。"
	case "darwin":
		return "macOS：brew install --cask blackhole-2ch blackhole-16ch，按系统提示授权或重启。麦克风送入 BlackHole 2ch，会议软件选它作为输入；电脑声音来源选独立的 BlackHole 16ch，并在目标应用中选它作为输出。双向不可使用同一设备，不接受聚集或多输出设备。系统设置 → 隐私与安全性 → 麦克风允许启动 RMic 的终端。卸载：brew uninstall --cask blackhole-2ch blackhole-16ch。官方说明：https://github.com/ExistentialAudio/BlackHole 。"
	default:
		return "Linux：需要用户会话中的 PulseAudio 或 PipeWire PulseAudio 兼容服务及 pactl（通常由 pulseaudio-utils 提供）。无需安装内核声卡驱动。"
	}
}
func lockInstallation(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock := installationPath(dir) + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return nil, fmt.Errorf("virtual device manager is busy; if a previous command crashed, remove %s after checking no manager is running: %w", lock, err)
	}
	return func() { _ = os.Remove(lock) }, nil
}
func Manage(action, dir string, microphone, playback bool) (string, error) {
	for _, variant := range []string{"a", "b"} {
		if base, ok := strings.CutPrefix(action, "cable-"+variant+"-"); ok {
			if base != "install" && base != "uninstall" && base != "status" {
				return "", fmt.Errorf("invalid cable action")
			}
			return manageWindowsCable(base, dir, "vbcable-"+variant)
		}
	}
	if base, steam := strings.CutPrefix(action, "steam-"); steam {
		if base != "install" && base != "uninstall" && base != "status" {
			return "", fmt.Errorf("invalid Steam driver action")
		}
		return manageWindowsSteam(base, dir)
	}
	if action != "install" && action != "uninstall" && action != "status" {
		return "", fmt.Errorf("virtual command must be install, uninstall or status")
	}
	if runtime.GOOS == "windows" {
		return manageWindows(action, dir)
	}
	if runtime.GOOS != "linux" {
		return PlatformGuide() + "\n本命令仅显示配置指引，没有安装或卸载驱动。", nil
	}
	if action != "status" {
		unlock, err := lockInstallation(dir)
		if err != nil {
			return "", err
		}
		defer unlock()
	}
	state, err := readInstallation(dir)
	if err != nil {
		return "", err
	}
	switch action {
	case "status":
		sinks, err := run("list", "short", "sinks")
		if err != nil {
			return "", fmt.Errorf("%s: %w", PlatformGuide(), err)
		}
		sources, err := run("list", "short", "sources")
		if err != nil {
			return "", err
		}
		var lines []string
		for _, entry := range strings.Split(sinks+"\n"+sources, "\n") {
			fields := strings.Fields(entry)
			if len(fields) > 1 && strings.HasPrefix(fields[1], "rmic_") {
				lines = append(lines, entry)
			}
		}
		summary := "未记录可保留设备；临时设备或其他实例的设备不会被管理器卸载。"
		if state != nil {
			count, err := matchingModules(state)
			if err != nil {
				return "", err
			}
			summary = fmt.Sprintf("可保留设备：%d/%d 个所有权匹配模块。", count, len(state.Modules))
		}
		return summary + "\n" + strings.Join(lines, "\n"), nil
	case "uninstall":
		if state == nil {
			return "没有本配置目录安装的设备；未修改音频设备。", nil
		}
		if err := state.device().Close(); err != nil {
			return "", err
		}
		if err := os.Remove(installationPath(dir)); err != nil {
			return "", err
		}
		return "已卸载本配置目录记录且所有权匹配的 RMic 设备；其他设备未修改。", nil
	case "install":
		if state != nil {
			if _, err := Installed(dir); err != nil {
				return "", err
			}
			if (!microphone || state.Output != "") && (!playback || state.Capture != "") {
				return "所需 RMic 设备已安装；正常启动会自动复用。", nil
			}
			return "", fmt.Errorf("installed device modes differ; uninstall before installing the new --mode")
		}
		owner, err := moduleOwner()
		if err != nil {
			return "", err
		}
		device, err := createFor(microphone, playback, owner)
		if err != nil {
			return "", fmt.Errorf("%s: %w", PlatformGuide(), err)
		}
		state = &installation{owner, device.Output, device.Input, device.Playback, device.Capture, device.modules}
		if err := saveInstallation(dir, state); err != nil {
			cleanupErr := device.Close()
			return "", errors.Join(err, cleanupErr)
		}
		return "已创建 RMic 虚拟声卡。正常启动会自动复用，退出 RMic 后保留。\n设备保留至音频服务重启；跨系统重启自动加载尚未启用。卸载：rmic virtual uninstall", nil
	}
	return "", nil
}
