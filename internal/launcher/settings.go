// Package launcher stores desktop startup preferences separately from TLS and
// driver ownership. It never stores pairing tokens or substitutes lost devices.
package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/shortcuts"
)

type Settings struct {
	Shortcuts        shortcuts.Config `json:"shortcuts"`
	AudioIdleSeconds int              `json:"audio_idle_seconds,omitempty"` // zero preserves the 30-second default
	CaptureChannels  int              `json:"capture_channels,omitempty"`
	// Mode describes the directions permitted by the computer, independently of
	// the initial mode selected on the phone. Empty DefaultMode preserves old files.
	Mode        audio.Mode `json:"mode"`
	DefaultMode audio.Mode `json:"default_mode,omitempty"`
	Output      string     `json:"output"`
	Capture     string     `json:"capture"`
	Quality     string     `json:"quality"`
	Network     string     `json:"network"`
	HTTPSPort   int        `json:"https_port"`
	UDPPort     int        `json:"udp_port"`
}

func Default() Settings {
	return Settings{Shortcuts: shortcuts.Config{KeyIntervalMS: 20}, Mode: audio.ModeMic, Quality: "balanced", HTTPSPort: 8443, UDPPort: 50000}
}
func (s Settings) SessionMode() audio.Mode {
	if s.DefaultMode != "" {
		return s.DefaultMode
	}
	return s.Mode
}
func Allows(allowed, mode audio.Mode) bool {
	_, err := audio.ParseMode(string(mode))
	return err == nil && (!mode.Microphone() || allowed.Microphone()) && (!mode.Playback() || allowed.Playback())
}
func (s Settings) Validate() error {
	if s.AudioIdleSeconds < 0 || s.AudioIdleSeconds > 3600 {
		return fmt.Errorf("音频空闲释放时间必须为 1–3600 秒（0 使用默认 30 秒）")
	}
	if err := s.Shortcuts.Validate(); err != nil {
		return err
	}
	if s.Shortcuts.Enabled && !shortcuts.Supported() {
		return fmt.Errorf("快捷按钮目前仅支持 Windows 电脑")
	}
	if s.CaptureChannels != 0 {
		if err := audio.ValidateChannels(s.CaptureChannels); err != nil {
			return err
		}
	}
	if _, err := audio.ParseMode(string(s.Mode)); err != nil {
		return fmt.Errorf("请选择允许的音频方向")
	}
	if !Allows(s.Mode, s.SessionMode()) {
		return fmt.Errorf("默认会话模式必须在电脑允许的音频方向内")
	}
	quality, err := audio.FindQuality(s.Quality)
	if err != nil {
		return err
	}
	if err = quality.ValidateMode(s.SessionMode()); err != nil {
		return err
	}
	if s.HTTPSPort < 1 || s.HTTPSPort > 65535 || s.UDPPort < 1 || s.UDPPort > 65535 {
		return fmt.Errorf("HTTPS 和 UDP 端口必须为 1–65535")
	}
	if s.Network != "" {
		ip := net.ParseIP(s.Network)
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("请选择有效的本机网络地址")
		}
	}
	return nil
}
func (s Settings) ValidateDevices(outputs, captures []string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.Mode.Microphone() && !slices.Contains(outputs, s.Output) {
		return fmt.Errorf("所选播放设备不存在；请安装驱动或刷新后重新选择设备")
	}
	if s.Mode.Playback() && !slices.Contains(captures, s.Capture) {
		return fmt.Errorf("所选采集设备不存在；请安装驱动或刷新后重新选择设备")
	}
	return nil
}
func Load(dir string) (Settings, error) {
	settings := Default()
	data, err := os.ReadFile(filepath.Join(dir, "desktop.json"))
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if err = json.Unmarshal(data, &settings); err != nil {
		return Default(), fmt.Errorf("读取桌面配置：%w", err)
	}
	if err = settings.Validate(); err != nil {
		return Default(), fmt.Errorf("桌面配置无效：%w", err)
	}
	return settings, nil
}
func Save(dir string, settings Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".desktop-*")
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
	return os.Rename(file.Name(), filepath.Join(dir, "desktop.json"))
}
func (s Settings) Args(dir string) []string {
	args := []string{"--mode", string(s.Mode), "--virtual", "off", "--quality", s.Quality, "--listen", ":" + strconv.Itoa(s.HTTPSPort), "--udp-port", strconv.Itoa(s.UDPPort), "--config-dir", dir}
	args = append(args, "--audio-idle-timeout", s.AudioIdleTimeout().String())
	if s.DefaultMode != "" || s.Mode == audio.ModeDuplex {
		args = append(args, "--default-mode", string(s.SessionMode()))
	}
	if s.CaptureChannels > 0 {
		args = append(args, "--capture-channels", strconv.Itoa(s.CaptureChannels))
	}
	if s.Mode.Microphone() {
		args = append(args, "--device", s.Output)
	}
	if s.Mode.Playback() {
		args = append(args, "--capture", s.Capture)
	}
	if s.Network != "" {
		args = append(args, "--bind-ips", s.Network)
	}
	return args
}

// NetworkChanged requires listener replacement; audio and defaults are applied
// within the existing service otherwise.
func (s Settings) NetworkChanged(next Settings) bool {
	return s.Network != next.Network || s.HTTPSPort != next.HTTPSPort || s.UDPPort != next.UDPPort
}
func (s Settings) AudioChanged(next Settings) bool {
	return s.AudioIdleTimeout() != next.AudioIdleTimeout() || s.Mode != next.Mode || s.Output != next.Output || s.Capture != next.Capture || s.Channels() != next.Channels()
}
func (s Settings) AudioIdleTimeout() time.Duration {
	if s.AudioIdleSeconds == 0 {
		return 30 * time.Second
	}
	return time.Duration(s.AudioIdleSeconds) * time.Second
}
func (s Settings) Channels() int {
	if s.CaptureChannels == 0 {
		return 2
	}
	return s.CaptureChannels
}
func (s Settings) Impact(next Settings) string {
	if s.NetworkChanged(next) {
		return "应用将重启网络监听；地址或端口变化后，请使用新地址和二维码连接。"
	}
	if s.AudioChanged(next) {
		return "应用将短暂重建音频会话；HTTPS 与设备授权保持，手机自动重新协商。"
	}
	if s.Shortcuts != next.Shortcuts {
		return "应用将更新快捷按钮并停止当前按键操作；当前传音保持运行。"
	}
	return "应用只更新默认设置，当前传音保持运行。"
}
