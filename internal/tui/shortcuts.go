package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cnb.cool/tdh6/RMic/internal/shortcuts"
	tea "github.com/charmbracelet/bubbletea"
)

type shortcutEditor struct {
	slot           int
	slotOpen, help bool
	steps          *stepEditor
	capture        *shortcutCapture
	draft          shortcuts.Config
	focus          int
	editing        bool
	value, notice  string
}

func (m *Setup) shortcutKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.shortcutEditor
	if e.steps != nil {
		return m.stepsKey(key)
	}
	if !e.editing && e.capture == nil && key.String() == "?" {
		e.help = !e.help
		return m, nil
	}
	if e.help {
		if key.String() == "esc" {
			e.help = false
		}
		return m, nil
	}
	if !e.slotOpen && !e.editing && e.focus == 0 && key.String() != "esc" && key.String() != "q" && key.String() != "ctrl+s" {
		return m.shortcutSlotsKey(key)
	}
	if !e.editing && key.String() == "f4" {
		e.slot = max(0, min(2, (e.focus-1)/3))
		lines := []string{}
		if action := e.draft.Buttons[e.slot].Action; action != "" {
			lines = strings.Split(action, "\n")
		}
		e.steps = &stepEditor{lines: lines}
		return m, nil
	}
	if e.capture != nil {
		if key.String() == "esc" {
			e.capture = nil
			e.notice = "录制已取消，原动作未修改。"
		}
		return m, nil
	}
	if e.editing {
		switch key.String() {
		case "f9":
			if (e.focus-1)%3 == 2 && shortcuts.Supported() {
				e.capture = &shortcutCapture{deadline: time.Now().Add(15 * time.Second)}
				e.notice = "先松开所有按键，再按住组合键并全部松开；Esc 取消。"
				return m, captureTick(e.capture)
			}
		case "esc":
			e.editing = false
		case "enter":
			if e.focus == 10 {
				ms, err := strconv.Atoi(e.value)
				if err != nil || ms < 0 || ms > shortcuts.MaxKeyIntervalMS {
					e.notice = fmt.Sprintf("组合键间隔须为 0–%d 毫秒", shortcuts.MaxKeyIntervalMS)
					return m, nil
				}
				e.draft.KeyIntervalMS = ms
				e.editing = false
				e.notice = ""
				return m, nil
			}
			b := &e.draft.Buttons[(e.focus-1)/3]
			if (e.focus-1)%3 == 0 {
				b.Name = e.value
			} else {
				b.Action = e.value
			}
			if b.Mode == "" {
				b.Mode = "tap"
			}
			e.editing = false
			e.notice = ""
		case "ctrl+n":
			if (e.focus-1)%3 == 2 && strings.Count(e.value, "\n") < 9 {
				e.value += "\n"
			}
		case "backspace", "ctrl+h":
			if len(e.value) > 0 {
				_, n := utf8.DecodeLastRuneInString(e.value)
				e.value = e.value[:len(e.value)-n]
			}
		case "ctrl+u":
			e.value = ""
		default:
			for _, r := range key.Runes {
				if e.focus == 10 && (r < '0' || r > '9' || len(e.value) >= 3) {
					continue
				}
				if r >= 32 && r != 127 && len(e.value) < 8192 {
					e.value += string(r)
				}
			}
		}
		return m, nil
	}
	rows := 1
	if e.draft.Enabled || e.slotOpen {
		rows = 11
	}
	switch key.String() {
	case "esc", "q":
		if e.slotOpen {
			e.slotOpen = false
			e.focus = 0
		} else {
			m.shortcutDraft = e
			m.shortcutEditor = nil
		}
	case "tab", "down":
		if e.slotOpen {
			first := e.slot*3 + 1
			fields := []int{first, first + 1, first + 2, 10}
			for i, field := range fields {
				if e.focus == field {
					e.focus = fields[(i+1)%len(fields)]
					return m, nil
				}
			}
		}
		e.focus = (e.focus + 1) % rows
	case "shift+tab", "up":
		if e.slotOpen {
			first := e.slot*3 + 1
			fields := []int{first, first + 1, first + 2, 10}
			for i, field := range fields {
				if e.focus == field {
					e.focus = fields[(i+len(fields)-1)%len(fields)]
					return m, nil
				}
			}
		}
		e.focus = (e.focus + rows - 1) % rows
	case "ctrl+s":
		if err := e.draft.Validate(); err != nil {
			e.notice = err.Error()
			return m, nil
		}
		if e.draft.Enabled && !shortcuts.Supported() {
			e.notice = "快捷按钮目前仅支持 Windows 电脑"
			return m, nil
		}
		m.Settings.Shortcuts = e.draft
		m.scope = "shortcuts"
		m.focus = 13
		m.shortcutDraft = nil
		m.shortcutEditor = nil
		m.notice = "快捷按钮草稿已更新；请保存或应用配置。"
	case "enter", " ", "left", "right":
		if e.focus == 0 {
			if !shortcuts.Supported() {
				e.notice = "快捷按钮目前仅支持 Windows 电脑"
				return m, nil
			}
			e.draft.Enabled = !e.draft.Enabled
			e.notice = ""
			return m, nil
		}
		if e.focus == 10 {
			e.value = strconv.Itoa(e.draft.KeyIntervalMS)
			e.editing = true
			return m, nil
		}
		b := &e.draft.Buttons[(e.focus-1)/3]
		switch (e.focus - 1) % 3 {
		case 0:
			e.value = b.Name
			e.editing = true
		case 1:
			if b.Mode == "hold" {
				b.Mode = "tap"
			} else {
				b.Mode = "hold"
			}
		case 2:
			e.value = b.Action
			e.editing = true
		}
	}
	return m, nil
}
func (m *Setup) shortcutView() string {
	e := m.shortcutEditor
	if e.steps != nil {
		return m.stepsView()
	}
	if !e.slotOpen && e.focus == 0 && !e.help {
		return m.shortcutSlotsView()
	}
	lines := []string{accent.Render(" RMic · 快捷按钮 "), ""}
	enabled := "关闭"
	if e.draft.Enabled {
		enabled = "开启"
	}
	rows := []string{"允许远端快捷按钮：" + enabled}
	if e.draft.Enabled || e.slotOpen {
		for i, b := range e.draft.Buttons {
			mode := "点击执行"
			if b.Mode == "hold" {
				mode = "按住保持"
			}
			name := b.Name
			if name == "" {
				name = "未配置（留空隐藏）"
			}
			action := strings.ReplaceAll(b.Action, "\n", " → ")
			if action == "" {
				action = "未配置"
			}
			rows = append(rows, fmt.Sprintf("按钮 %d 名称：%s", i+1, name), "  触发方式："+mode, "  动作："+action)
		}
		rows = append(rows, fmt.Sprintf("组合键间隔：%d 毫秒（0 不延时）", e.draft.KeyIntervalMS))
	}
	for i, row := range rows {
		if !e.help && e.focus >= 1 && e.focus <= 9 && i != 0 && i != 10 && (i-1)/3 != (e.focus-1)/3 {
			continue
		}
		marker := "  "
		if i == e.focus {
			marker = "▶ "
		}
		if e.editing && i == e.focus {
			row = "编辑：" + strings.ReplaceAll(e.value, "\n", " ↵ ") + "_"
		}
		line := marker + row
		if i == e.focus {
			line = accent.Render(line)
		}
		lines = append(lines, clickable("shortcut-field:"+strconv.Itoa(i), line))
	}
	if e.help {
		lines = append(lines, "名称留空隐藏按钮；最多 10 步。", "每行一个步骤：ControlLeft+KeyV / text:文字 / wait:100", "动作编辑 Ctrl+N 换行，F9 录制；按住模式只允许一组按键。", "输入发送到电脑当前窗口。Esc 返回。")
	}
	if !shortcuts.Supported() {
		lines = append(lines, "当前平台暂不支持输入注入；仅 Windows 可开启。")
	}
	if e.notice != "" {
		lines = append(lines, "提示："+e.notice)
	}
	footer := clickable("key:ctrl+s", "Ctrl+S 确认草稿") + " · " + clickable("key:esc", "Esc 返回") + " · ↑↓ 选择 · Enter 编辑"
	footer += " · F4 步骤 · ? 帮助"
	if e.editing {
		footer = clickable("key:enter", "Enter 确定") + " · " + clickable("key:esc", "Esc 取消") + " · Ctrl+U 清空 · Ctrl+N 换行"
		if e.focus == 10 {
			footer = clickable("key:enter", "Enter 确定") + " · " + clickable("key:esc", "Esc 取消") + " · Ctrl+U 清空 · 输入 0–100 毫秒"
		}
		if (e.focus-1)%3 == 2 && shortcuts.Supported() {
			footer += " · " + clickable("key:f9", "F9 录制组合键")
		}
	}
	if e.capture != nil {
		footer = clickable("key:esc", "Esc 取消录制") + " · 请按住组合键后全部松开（15 秒内）"
	}
	focusLine := 2 + e.focus
	if e.slotOpen && e.focus > 0 {
		focusLine = 3 + (e.focus-1)%3
		if e.focus == 10 {
			focusLine = 6
		}
	}
	return page(lines, footer, m.width, m.height, focusLine)
}
func (m *Setup) shortcutMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.mouse.enabled() {
		return m, nil
	}
	if m.shortcutEditor.steps != nil {
		if m.mouse.target(msg, m.mousePage()) == "key:esc" {
			return m.stepsKey(mouseKey("esc"))
		}
		return m, nil
	}
	target := m.mouse.target(msg, m.mousePage())
	if strings.HasPrefix(target, "key:") {
		return m.shortcutKey(mouseKey(strings.TrimPrefix(target, "key:")))
	}
	if !m.shortcutEditor.editing && strings.HasPrefix(target, "shortcut-field:") {
		index, err := strconv.Atoi(strings.TrimPrefix(target, "shortcut-field:"))
		limit := 10
		if err == nil && index >= 0 && index <= limit {
			m.shortcutEditor.focus = index
			m.shortcutEditor.slotOpen = index != 0
			if index == 0 {
				return m.shortcutSlotsKey(mouseKey(" "))
			}
			return m.shortcutKey(mouseKey("enter"))
		}
	}
	return m, nil
}
