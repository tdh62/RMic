package virtual

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// Manager serializes TUI operations with application cleanup. It may retain
// this process's temporary routes without moving or interrupting audio streams.
type Manager struct {
	mu                   sync.Mutex
	dir                  string
	microphone, playback bool
	temporary            *Device
	closed               bool
}

func NewManager(dir string, microphone, playback bool, temporary *Device) *Manager {
	return &Manager{dir: dir, microphone: microphone, playback: playback, temporary: temporary}
}
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.temporary != nil {
		return m.temporary.Close()
	}
	return nil
}
func (m *Manager) Run(action string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return "", fmt.Errorf("virtual device manager is closed")
	}
	if runtime.GOOS == "linux" && m.temporary != nil && len(m.temporary.modules) > 0 {
		switch action {
		case "install":
			return m.retain()
		case "uninstall":
			if err := m.temporary.Close(); err != nil {
				return "", err
			}
			message, err := Manage(action, m.dir, m.microphone, m.playback)
			return "已移除当前运行创建的临时设备。\n" + message, err
		}
	}
	return Manage(action, m.dir, m.microphone, m.playback)
}
func (m *Manager) retain() (string, error) {
	unlock, err := lockInstallation(m.dir)
	if err != nil {
		return "", err
	}
	defer unlock()
	previous, err := readInstallation(m.dir)
	if err != nil {
		return "", err
	}
	if previous != nil {
		return "", fmt.Errorf("已有其他安装记录；请先在管理页卸载，再安装所需通道，随后重启 RMic")
	}
	device := m.temporary
	_, owner, _ := strings.Cut(device.modules[0].Argument, "rmic.owner=")
	owner = strings.TrimSuffix(owner, "\"")
	state := &installation{owner, device.Output, device.Input, device.Playback, device.Capture, append([]ownedModule(nil), device.modules...)}
	count, err := matchingModules(state)
	if err != nil {
		return "", err
	}
	if count != len(state.Modules) {
		return "", fmt.Errorf("当前临时设备已失效；请先卸载，再安装并重启 RMic")
	}
	if err = saveInstallation(m.dir, state); err != nil {
		return "", err
	}
	device.modules = nil
	return "已将当前临时通道原地保留，音频不中断。退出 RMic 后设备保留至音频服务重启。", nil
}
