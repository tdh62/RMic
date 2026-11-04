package tui

import (
	"fmt"
	"strings"

	"cnb.cool/tdh6/RMic/internal/virtual"
	tea "github.com/charmbracelet/bubbletea"
)

type driverInspector interface{ Inspect() []virtual.DriverInfo }
type driverList struct {
	items          []virtual.DriverInfo
	selected       int
	marked         map[string]bool
	operation      string
	targets        []virtual.DriverInfo
	queue          []virtual.DriverInfo
	queueIndex     int
	queueOperation string
	reports        []string
}
type driverListResult struct {
	items   []virtual.DriverInfo
	message string
}

func (m model) hasDriverList() bool { _, ok := m.virtualManager.(driverInspector); return ok }
func (m model) inspectAll() tea.Cmd {
	manager := m.virtualManager.(driverInspector)
	return func() tea.Msg { return driverListResult{items: manager.Inspect()} }
}
func (m model) driverListKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.virtualBusy {
		return m, nil
	}
	d := &m.drivers
	if d.operation != "" {
		switch key.String() {
		case "esc", "n":
			d.operation = ""
			d.targets = nil
		case "left", "right", "tab":
			m.confirmYes = !m.confirmYes
		case "enter":
			if !m.confirmYes {
				d.operation = ""
				d.targets = nil
				return m, nil
			}
			return m.runDriverList()
		case "y":
			return m.runDriverList()
		}
		return m, nil
	}
	switch key.String() {
	case "esc", "v":
		if m.managerOnly {
			return m, tea.Quit
		}
		m.showVirtual = false
	case "q", "ctrl+c":
		return m, tea.Quit
	case "r":
		m.virtualBusy = true
		return m, m.inspectAll()
	case "up", "w", "k":
		d.selected = max(0, d.selected-1)
	case "down", "s", "j", "t":
		d.selected = min(max(0, len(d.items)-1), d.selected+1)
	case "left", "a", "shift+tab":
		m.virtualFocus = (m.virtualFocus + 3) % 4
	case "right", "d", "tab":
		m.virtualFocus = (m.virtualFocus + 1) % 4
	case "enter":
		return m.driverListKey(mouseKey([]string{"r", "i", "u", "esc"}[m.virtualFocus]))
	case " ":
		if len(d.items) == 0 {
			return m, nil
		}
		item := d.items[d.selected]
		if item.ReadOnly || item.Error != "" || item.Complete {
			return m, nil
		}
		if d.marked == nil {
			d.marked = make(map[string]bool)
		}
		if d.marked[item.Name] {
			delete(d.marked, item.Name)
		} else {
			d.marked[item.Name] = true
		}
	case "i", "u":
		manageable := false
		for _, item := range d.items {
			manageable = manageable || !item.ReadOnly
		}
		if !manageable {
			m.virtualResult = "请按驱动说明手动安装，完成后按 r 刷新检测。"
			return m, nil
		}
		if m.server != nil || m.virtualReadOnly {
			if m.canMaintain {
				d.operation = "stop"
				m.confirmYes = false
			} else {
				m.virtualResult = "请退出当前服务后运行 --setup 管理驱动；检测不影响传音。"
			}
			return m, nil
		}
		d.targets = nil
		operation := "install"
		if key.String() == "u" {
			operation = "uninstall"
		}
		for index, item := range d.items {
			selected := index == d.selected
			if operation == "install" && len(d.marked) > 0 {
				selected = d.marked[item.Name]
			}
			if selected && !item.ReadOnly && item.Error == "" && ((operation == "install" && !item.Complete) || (operation == "uninstall" && item.Installed)) {
				d.targets = append(d.targets, item)
			}
		}
		if len(d.targets) > 0 {
			d.operation = operation
			m.confirmYes = false
		}
	}
	return m, nil
}
func (m model) runDriverList() (tea.Model, tea.Cmd) {
	if m.drivers.operation == "stop" {
		m.maintenanceRequested = true
		return m, tea.Quit
	}
	m.drivers.queue = append([]virtual.DriverInfo(nil), m.drivers.targets...)
	m.drivers.queueIndex = 0
	m.drivers.queueOperation = m.drivers.operation
	m.drivers.operation = ""
	m.drivers.reports = nil
	m.virtualBusy = true
	return m, m.driverStep()
}

type driverStepResult struct {
	message string
	err     error
}

func (m model) driverStep() tea.Cmd {
	item := m.drivers.queue[m.drivers.queueIndex]
	operation := m.drivers.queueOperation
	manager := m.virtualManager
	return func() tea.Msg {
		message, err := manager.Run(item.Prefix + operation)
		return driverStepResult{item.Name + "：" + message, err}
	}
}
func (m model) driverStepDone(result driverStepResult) (tea.Model, tea.Cmd) {
	m.drivers.reports = append(m.drivers.reports, result.message)
	if result.err != nil {
		m.drivers.reports = append(m.drivers.reports, "操作失败："+result.err.Error()+"；后续项目未执行。")
	}
	m.drivers.queueIndex++
	if result.err == nil && m.drivers.queueIndex < len(m.drivers.queue) {
		return m, m.driverStep()
	}
	message := strings.Join(m.drivers.reports, "\n")
	manager := m.virtualManager.(driverInspector)
	return m, func() tea.Msg { return driverListResult{items: manager.Inspect(), message: message} }
}
func (m model) driverListView() string {
	rows := []string{accent.Render(" RMic · 虚拟声卡管理 ")}
	d := m.drivers
	for i, item := range d.items {
		marker := "  "
		if i == d.selected {
			marker = "> "
		}
		state := "未安装"
		if item.Installed {
			state = "不完整"
		}
		if item.Complete {
			state = "已安装"
		}
		if item.Error != "" {
			state = "检测失败"
		}
		if item.ReadOnly {
			state = "安装指引"
		}
		check := "[ ]"
		if d.marked[item.Name] {
			check = "[x]"
		}
		rows = append(rows, clickable(fmt.Sprintf("driver:%d", i), fitText(marker+check+" "+item.Name+" · "+state, max(1, m.width-2))))
	}
	if len(d.items) > 0 {
		item := d.items[d.selected]
		rows = append(rows, "", item.Description)
		if item.Error != "" {
			rows = append(rows, item.Error)
		}
	}
	if m.virtualBusy {
		if m.drivers.queueIndex < len(m.drivers.queue) {
			rows = append(rows, fmt.Sprintf("%d/%d · %s", m.drivers.queueIndex+1, len(m.drivers.queue), m.drivers.queue[m.drivers.queueIndex].Name))
		}
		rows = append(rows, "正在处理，请留意系统 UAC 或安装器；完成后重新检测。")
	}
	if m.virtualResult != "" {
		lines := strings.Split(m.virtualResult, "\n")
		summary := lines[0]
		for _, line := range lines {
			if strings.Contains(line, "操作失败") {
				summary = line
				break
			}
		}
		rows = append(rows, fitText(summary, max(1, m.width-2)), "? 查看完整结果与重启提示")
	}
	actions := []string{"检测全部 r", "安装所选 i", "卸载当前 u", "返回 Esc"}
	manageable := false
	for _, item := range d.items {
		manageable = manageable || !item.ReadOnly
	}
	if !manageable {
		actions[1], actions[2] = "手动安装指引", "手动卸载指引"
	}
	footer := actionBar(actions, m.virtualFocus, m.width) + "\n↑↓ 选择 · Space 勾选 · ←→ 操作 · ? 详情"
	if d.operation != "" {
		verb := "安装"
		if d.operation == "uninstall" {
			verb = "卸载"
		}
		rows = append(rows, "确认"+verb+"：")
		for _, item := range d.targets {
			rows = append(rows, item.Name)
		}
		if d.operation == "stop" {
			rows = append(rows, "停止服务并断开手机，进入驱动管理？", "进入后需重新选择安装或卸载；不会自动执行。")
		}
		rows = append(rows, "可能请求 UAC 或重启；卸载会影响其他使用此驱动的应用。")
		selected := 0
		if m.confirmYes {
			selected = 1
		}
		footer = actionBar([]string{"取消 Esc/n", "确认 y"}, selected, m.width) + "\n←→ 选择 · Enter 确定"
	}
	focus := 1 + d.selected
	if d.operation != "" {
		focus = len(rows) - 1
	}
	return page(rows, footer, m.width, m.height, focus)
}
