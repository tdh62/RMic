package tui

import (
	"fmt"
	"strconv"
	"strings"

	"cnb.cool/tdh6/RMic/internal/shortcuts"
	tea "github.com/charmbracelet/bubbletea"
)

type stepEditor struct {
	lines         []string
	selected      int
	editing       bool
	value, notice string
}

func (m *Setup) stepsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.shortcutEditor
	s := e.steps
	if s.editing {
		switch key.String() {
		case "esc":
			s.editing = false
		case "enter":
			s.lines[s.selected] = s.value
			s.editing = false
		case "ctrl+u":
			s.value = ""
		case "backspace", "ctrl+h":
			r := []rune(s.value)
			if len(r) > 0 {
				s.value = string(r[:len(r)-1])
			}
		default:
			for _, r := range key.Runes {
				if r >= 32 && r != 127 && len(s.value) < 8192 {
					s.value += string(r)
				}
			}
		}
		return m, nil
	}
	switch key.String() {
	case "esc":
		e.steps = nil
	case "ctrl+s":
		b := e.draft.Buttons[e.slot]
		b.Action = strings.Join(s.lines, "\n")
		if b.Name == "" {
			b.Name = "步骤"
		}
		if b.Mode == "" {
			b.Mode = "tap"
		}
		if _, err := b.Steps(); err != nil {
			s.notice = err.Error()
			return m, nil
		}
		e.draft.Buttons[e.slot].Action = b.Action
		e.steps = nil
	case "up":
		s.selected = max(0, s.selected-1)
	case "down":
		s.selected = min(max(0, len(s.lines)-1), s.selected+1)
	case "enter":
		if len(s.lines) > 0 {
			s.value = s.lines[s.selected]
			s.editing = true
		}
	case "a", "t", "w":
		if len(s.lines) >= 10 {
			s.notice = "最多 10 个步骤"
			return m, nil
		}
		value := "ControlLeft+KeyC"
		if key.String() == "t" {
			value = "text:"
		}
		if key.String() == "w" {
			value = "wait:100"
		}
		s.lines = append(s.lines, value)
		s.selected = len(s.lines) - 1
	case "delete":
		if len(s.lines) > 0 {
			s.lines = append(s.lines[:s.selected], s.lines[s.selected+1:]...)
			s.selected = min(s.selected, max(0, len(s.lines)-1))
		}
	case "[":
		if s.selected > 0 {
			s.lines[s.selected], s.lines[s.selected-1] = s.lines[s.selected-1], s.lines[s.selected]
			s.selected--
		}
	case "]":
		if s.selected+1 < len(s.lines) {
			s.lines[s.selected], s.lines[s.selected+1] = s.lines[s.selected+1], s.lines[s.selected]
			s.selected++
		}
	}
	return m, nil
}
func (m *Setup) stepsView() string {
	s := m.shortcutEditor.steps
	rows := []string{accent.Render(fmt.Sprintf(" RMic · 按钮 %d 步骤 ", m.shortcutEditor.slot+1))}
	for i, line := range s.lines {
		marker := "  "
		if i == s.selected {
			marker = "> "
		}
		if s.editing && i == s.selected {
			line = s.value + "_"
		}
		rows = append(rows, marker+fmt.Sprintf("%d  %s", i+1, line))
	}
	if len(s.lines) == 0 {
		rows = append(rows, "暂无步骤，a 按键 / t 文本 / w 等待")
	}
	if s.notice != "" {
		rows = append(rows, s.notice)
	}
	footer := "↑↓ 选择 · Enter 编辑 · [ / ] 排序 · Delete 删除\na 按键 · t 文本 · w 等待 · Ctrl+S 更新草稿 · Esc 返回"
	if s.editing {
		footer = "Enter 确定 · Esc 放弃 · Ctrl+U 清空"
	}
	return page(rows, footer, m.width, m.height, s.selected+1)
}
func (m *Setup) shortcutSlotsView() string {
	e := m.shortcutEditor
	state := "关闭"
	if e.draft.Enabled {
		state = "开启"
	}
	rows := []string{accent.Render(" RMic · 快捷按钮 "), clickable("shortcut-field:0", "允许远端控制："+state)}
	for i, b := range e.draft.Buttons {
		marker := "  "
		if i == e.slot {
			marker = "> "
		}
		name := b.Name
		if name == "" {
			name = "未配置"
		}
		mode := "点击"
		if b.Mode == "hold" {
			mode = "按住"
		}
		rows = append(rows, clickable(fmt.Sprintf("key:%d", i+1), fitText(fmt.Sprintf("%s%d %s · %s", marker, i+1, name, mode), m.width-2)))
	}
	rows = append(rows, clickable("key:enter", "Enter 编辑当前按钮"), clickable("shortcut-field:10", fmt.Sprintf("按键间隔：%d ms", e.draft.KeyIntervalMS)))
	if e.notice != "" {
		rows = append(rows, e.notice)
	}
	if !shortcuts.Supported() {
		rows = append(rows, "当前平台仅可编辑，输入注入需要 Windows。")
	}
	return page(rows, "↑↓ 选择按钮 · 1/2/3 编辑 · Space 启用 · i 间隔\nCtrl+S 更新配置草稿 · Esc 返回 · ? 帮助", m.width, m.height, e.slot+2)
}
func (m *Setup) shortcutSlotsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.shortcutEditor
	switch key.String() {
	case "up", "shift+tab":
		e.slot = (e.slot + 2) % 3
	case "down", "tab":
		e.slot = (e.slot + 1) % 3
	case "1", "2", "3":
		e.slot = int(key.Runes[0] - '1')
		e.slotOpen = true
		e.focus = e.slot*3 + 1
	case "enter":
		e.slotOpen = true
		e.focus = e.slot*3 + 1
	case "i":
		e.slotOpen = true
		e.focus = 10
		e.value = strconv.Itoa(e.draft.KeyIntervalMS)
		e.editing = true
	case " ":
		if shortcuts.Supported() {
			e.draft.Enabled = !e.draft.Enabled
		} else {
			e.notice = "输入注入仅支持 Windows"
		}
	}
	return m, nil
}
