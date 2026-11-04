package tui

import (
	"strings"
	"time"

	"cnb.cool/tdh6/RMic/internal/shortcuts"
	tea "github.com/charmbracelet/bubbletea"
)

type shortcutCapture struct {
	ready    bool
	keys     []string
	deadline time.Time
}
type shortcutCaptureTick struct {
	capture *shortcutCapture
	keys    []string
}

func captureTick(c *shortcutCapture) tea.Cmd {
	return tea.Tick(15*time.Millisecond, func(time.Time) tea.Msg {
		return shortcutCaptureTick{c, shortcuts.PressedKeys()}
	})
}

func (m *Setup) updateShortcutCapture(msg shortcutCaptureTick) (tea.Model, tea.Cmd) {
	e := m.shortcutEditor
	if e == nil || e.capture != msg.capture {
		return m, nil
	}
	c := e.capture
	if time.Now().After(c.deadline) {
		e.capture = nil
		e.notice = "录制超时，原动作未修改。"
		return m, nil
	}
	if !c.ready {
		c.ready = len(msg.keys) == 0
	} else if len(msg.keys) > 0 {
		for _, key := range msg.keys {
			found := false
			for _, existing := range c.keys {
				if existing == key {
					found = true
					break
				}
			}
			if !found {
				c.keys = append(c.keys, key)
			}
		}
	} else if len(c.keys) > 0 {
		e.capture = nil
		action := strings.Join(c.keys, "+")
		if _, err := (shortcuts.Button{Name: "录制", Mode: "hold", Action: action}).Steps(); err != nil {
			e.notice = err.Error()
			return m, nil
		}
		e.value = action
		e.notice = "已录制：" + action + "；Enter 确定，Esc 放弃。"
		return m, nil
	}
	return m, captureTick(c)
}
