package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *Setup) guideKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "enter", "v":
		m.guide = false
		m.driver.showVirtual = true
		m.driver.virtualBusy = true
		return m, m.driver.virtualCommand("status")
	case "e", "esc":
		m.guide = false
		m.notice = "选择实际设备后按 g 保存并启动；v 可随时返回驱动管理。"
	}
	return m, nil
}
func (m *Setup) guideView() string {
	rows := []string{accent.Render(" RMic · 首次使用 "), "1 准备驱动 → 2 选择设备 → 3 保存并启动", "", "先检测已有驱动；Windows 默认解压随包 VB-CABLE ZIP 手动安装。", "已准备好设备可跳过安装；不会自动启动或开麦。"}
	if m.devices.Warning != "" {
		rows = append(rows, m.devices.Warning)
	}
	footer := clickable("key:enter", "Enter 检测驱动") + " · " + clickable("key:e", "e 使用现有设备") + " · " + clickable("key:q", "q 退出")
	return page(rows, footer, m.width, m.height, 0)
}
