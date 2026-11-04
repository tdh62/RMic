package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type pairDone struct {
	err     error
	message string
}

func (m *model) selectPairFirst() {
	m.pairSelected = ""
	if m.pairDevices {
		if devices := m.pairing.Devices(); len(devices) > 0 {
			m.pairSelected = devices[0].ID
		}
	} else if requests := m.pairing.Requests(); len(requests) > 0 {
		m.pairSelected = requests[0].ID
	}
}
func (m model) pairingKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pairBusy {
		return m, nil
	}
	action := key.String()
	if action == "enter" && m.pairConfirm == "" {
		actions := []string{"approve", "o", "n", "p"}
		if m.pairDevices {
			actions = []string{"r", "p"}
		}
		action = actions[m.pairAction]
	}
	if m.pairConfirm != "" {
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
		case "y":
			id := m.pairConfirm
			m.pairConfirm = ""
			m.pairBusy = true
			return m, func() tea.Msg {
				return pairDone{m.pairing.Revoke(id), "已撤销该设备授权，活动连接将在一秒内停止。"}
			}
		case "n", "esc":
			m.pairConfirm = ""
		}
		return m, nil
	}
	switch action {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "p":
		m.showPairing = false
	case "tab", "shift+tab":
		m.pairAction = 0
		m.pairDevices = !m.pairDevices
		m.selectPairFirst()
		m.pairNotice = ""
	case "left", "right", "a", "d":
		count := 4
		if m.pairDevices {
			count = 2
		}
		delta := 1
		if key.String() == "left" || key.String() == "a" {
			delta = -1
		}
		m.pairAction = (m.pairAction + delta + count) % count
	case "up", "down", "j", "k", "w", "s":
		var ids []string
		if m.pairDevices {
			for _, d := range m.pairing.Devices() {
				ids = append(ids, d.ID)
			}
		} else {
			for _, r := range m.pairing.Requests() {
				ids = append(ids, r.ID)
			}
		}
		direction := 1
		if key.String() == "up" || key.String() == "k" || key.String() == "w" {
			direction = -1
		}
		m.pairSelected = cycle(m.pairSelected, ids, direction)
	case "approve", "n", "o":
		if m.pairDevices {
			return m, nil
		}
		valid := false
		for _, r := range m.pairing.Requests() {
			if r.ID == m.pairSelected {
				valid = true
			}
		}
		if !valid {
			m.pairNotice = "所选申请已过期，请重新选择并核对确认码。"
			return m, nil
		}
		id, allow := m.pairSelected, action != "n"
		once := action == "o"
		m.pairBusy = true
		return m, func() tea.Msg {
			err := error(nil)
			if once {
				err = m.pairing.DecideOnce(id)
			} else {
				err = m.pairing.Decide(id, allow)
			}
			return pairDone{err, map[bool]string{true: "已允许设备，请在手机开始传音。", false: "已拒绝申请。"}[allow]}
		}
	case "r":
		if m.pairDevices && m.pairSelected != "" {
			m.pairConfirm = m.pairSelected
			m.confirmYes = false
		}
	}
	return m, nil
}
func (m model) pairingView() string {
	var rows []string
	section := "待许可申请"
	if m.pairDevices {
		section = "已授权设备"
	}
	rows = append(rows, accent.Render(" RMic · 配对 "), "", clickable("key:tab", section+"  · Tab 切换列表"), "")
	if !m.pairDevices {
		rows = append(rows, "核对手机确认码后许可。", "")
	}
	var entries []string
	detail := ""
	selected := 0
	if m.pairDevices {
		for index, d := range m.pairing.Devices() {
			marker := "  "
			if d.ID == m.pairSelected {
				marker = "▶ "
				selected = index
				detail = "授权：" + d.Created.Local().Format("2006-01-02 15:04")
			}
			scope := "记住设备"
			if !d.Remember {
				scope = "仅本次电脑运行"
			}
			entries = append(entries, clickable("pair:"+d.ID, fitText(fmt.Sprintf("%s%s · %s", marker, d.Name, scope), m.width-2)))
		}
	} else {
		for index, r := range m.pairing.Requests() {
			marker := "  "
			if r.ID == m.pairSelected {
				marker = "▶ "
				selected = index
				detail = "来源：" + r.Address + " · " + map[bool]string{true: "记住设备", false: "仅本次"}[r.Remember]
			}
			entries = append(entries, clickable("pair:"+r.ID, fitText(fmt.Sprintf("%s%s · %s", marker, r.Code, r.Name), m.width-2)))
		}
	}
	if len(entries) == 0 {
		if m.pairDevices {
			rows = append(rows, "暂无已授权设备")
		} else {
			rows = append(rows, "暂无申请 · 请在手机点击‘申请配对’")
		}
	} else {
		capacity := max(1, m.height-13)
		start := max(0, min(selected-capacity/2, len(entries)-capacity))
		rows = append(rows, entries[start:min(len(entries), start+capacity)]...)
	}
	if detail != "" {
		rows = append(rows, "", detail)
	}
	if m.pairConfirm != "" {
		rows = append(rows, "", "撤销当前设备的授权并停止其连接？")
	}
	if m.pairBusy {
		rows = append(rows, "正在保存授权……")
	} else if m.pairNotice != "" {
		rows = append(rows, m.pairNotice)
	}
	actions := []string{"允许", "仅本次 o", "拒绝 n", "返回 Esc/p"}
	if m.pairDevices {
		actions = []string{"撤销 r", "返回 Esc/p"}
	}
	footer := actionBar(actions, m.pairAction, m.width) + "\n↑↓ 设备 · ←→ 操作 · Enter 执行 · Esc 返回 · ? 帮助"
	if m.pairConfirm != "" {
		selected := 0
		if m.confirmYes {
			selected = 1
		}
		footer = actionBar([]string{"取消 Esc/n", "确认撤销 y"}, selected, m.width) + "\n方向键/WASD 选择 · Enter 确定 · Esc 取消"
	}
	return page(rows, footer, m.width, m.height, len(rows)-1)
}
