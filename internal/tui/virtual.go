package tui

import (
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type virtualResultMsg struct {
	action, result, status string
	err, statusErr         error
}

func NewVirtual(manager VirtualManager, options ...Option) tea.Model {
	m := model{mouse: &mouseFrame{}, windowsDrivers: runtime.GOOS == "windows", virtualSteam: runtime.GOOS == "windows", virtualManager: manager, showVirtual: true, managerOnly: true, virtualBusy: true, width: 80, height: 24}
	for _, option := range options {
		option(&m)
	}
	return m
}
func (m model) virtualCommand(action string) tea.Cmd {
	if action == "status" && m.hasDriverList() {
		return m.inspectAll()
	}
	return func() tea.Msg {
		prefix := ""
		if m.virtualSteam {
			prefix = "steam-"
		} else if m.virtualCable == 1 {
			prefix = "cable-a-"
		} else if m.virtualCable == 2 {
			prefix = "cable-b-"
		}
		result, err := m.virtualManager.Run(prefix + action)
		message := virtualResultMsg{action: action, result: result, err: err}
		if action == "status" {
			message.status = result
			message.result = ""
		} else {
			message.status, message.statusErr = m.virtualManager.Run(prefix + "status")
		}
		return message
	}
}
func (m model) virtualCompleted(message virtualResultMsg) (tea.Model, tea.Cmd) {
	m.virtualBusy = false
	m.confirmUninstall = false
	m.virtualScroll = 0
	m.virtualBody = message.status
	if message.action != "status" {
		m.virtualResult = message.result
	}
	if message.err != nil {
		m.virtualResult = "操作失败：" + message.err.Error()
	}
	if message.statusErr != nil {
		m.virtualBody = "状态读取失败：" + message.statusErr.Error()
	}
	if message.action == "uninstall" && message.err == nil && !m.managerOnly && runtime.GOOS == "linux" {
		m.virtualResult += "\n受影响通道重新安装后，请退出并重启 RMic 恢复传音。"
	}
	return m, nil
}
func (m model) updateVirtual(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.hasDriverList() {
		return m.driverListKey(key)
	}
	if m.virtualBusy {
		return m, nil
	}
	if key.String() == "enter" && !m.confirmUninstall {
		keys := []rune{'i', 'u', 'r', 'v'}
		if m.windowsDrivers {
			keys = append([]rune{'t'}, keys...)
		}
		return m.updateVirtual(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{keys[m.virtualFocus]}})
	}
	if m.confirmUninstall {
		if key.String() == "enter" {
			if m.confirmYes {
				key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
			} else {
				key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}
			}
		}
		switch key.String() {
		case "left", "right", "up", "down", "a", "d", "w", "s", "tab":
			m.confirmYes = !m.confirmYes
		case "q", "ctrl+c":
			return m, tea.Quit
		case "y", "Y":
			m.confirmUninstall = false
			m.virtualBusy = true
			if m.server != nil {
				m.server.Disconnect()
			}
			return m, m.virtualCommand("uninstall")
		case "n", "N", "esc":
			m.confirmUninstall = false
		}
		return m, nil
	}
	switch key.String() {
	case "up", "left", "w", "a", "shift+tab":
		count := 4
		if m.windowsDrivers {
			count++
		}
		m.virtualFocus = (m.virtualFocus + count - 1) % count
	case "down", "right", "s", "d", "tab":
		count := 4
		if m.windowsDrivers {
			count++
		}
		m.virtualFocus = (m.virtualFocus + 1) % count
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "v":
		if m.managerOnly {
			return m, tea.Quit
		}
		m.showVirtual = false
	case "t":
		if m.windowsDrivers {
			if m.virtualSteam {
				m.virtualSteam = false
				m.virtualCable = 0
			} else if m.virtualCable < 2 {
				m.virtualCable++
			} else {
				m.virtualSteam = true
				m.virtualCable = 0
			}
			m.virtualBody = ""
			m.virtualResult = ""
			m.virtualBusy = true
			return m, m.virtualCommand("status")
		}
	case "i":
		m.virtualBusy = true
		m.virtualResult = ""
		return m, m.virtualCommand("install")
	case "u":
		if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
			m.virtualBusy = true
			return m, m.virtualCommand("uninstall")
		}
		m.confirmUninstall = true
		m.confirmYes = false
	case "r":
		m.virtualBusy = true
		return m, m.virtualCommand("status")
	case "pgdown", "j":
		m.virtualScroll++
	case "pgup", "k":
		m.virtualScroll = max(0, m.virtualScroll-1)
	}
	return m, nil
}
func (m model) virtualView() string {
	if m.hasDriverList() {
		return m.driverListView()
	}
	var body strings.Builder
	provider := []string{"VB-CABLE", "VB-CABLE A", "VB-CABLE B"}[m.virtualCable]
	if m.virtualSteam {
		provider = "Steam Streaming"
	}
	if m.windowsDrivers {
		body.WriteString("当前驱动：" + provider + "\n")
	}
	body.WriteString("\n")
	if m.virtualBusy {
		body.WriteString("正在处理，请稍候……\n\n")
	}
	if m.confirmUninstall {
		if m.windowsDrivers && m.virtualSteam {
			body.WriteString("确认移除 Steam 虚拟音频设备？会影响 Steam Remote Play，驱动缓存保留。\n")
		} else {
			body.WriteString("确认卸载所选虚拟声卡？当前连接会断开。\n")
		}
	}
	if m.virtualResult != "" {
		body.WriteString(m.virtualResult + "\n\n")
	}
	// The full platform guide stays available in help; keep live results intact.
	body.WriteString("设备状态\n" + strings.SplitN(m.virtualBody, "\n", 2)[0])
	actions := []string{"安装 i", "卸载 u", "刷新 r", "返回 Esc/v"}
	if m.windowsDrivers {
		actions = append([]string{"切换驱动 t"}, actions...)
	}
	footer := actionBar(actions, m.virtualFocus, m.width) + "\n←→ 选择 · Enter 执行 · Esc 返回 · ? 详情"
	if m.confirmUninstall {
		selected := 0
		if m.confirmYes {
			selected = 1
		}
		footer = actionBar([]string{"取消 Esc/n", "确认卸载 y"}, selected, m.width) + "\n方向键/WASD 选择 · Enter 确定 · Esc 取消"
	}
	return m.mouse.scrolledPage([]string{accent.Render(" RMic · 虚拟声卡管理 "), "", body.String()}, footer, m.width, m.height, m.virtualScroll)
}
