package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Click markers are temporary OSC 8 sequences understood as zero-width by the
// layout engine. They are removed before output. Mark each grapheme separately
// so wrapping and viewport clipping cannot separate text from its target.
const clickPrefix = "\x1b]8;;rmic-click:"
const clickEnd = "\x1b]8;;\x07"

func clickable(id, text string) string {
	var out strings.Builder
	var state byte
	for len(text) > 0 {
		seq, width, n, next := ansi.DecodeSequence(text, state, nil)
		state, text = next, text[n:]
		if width > 0 {
			out.WriteString(clickPrefix + id + "\x07")
			out.WriteString(seq)
			out.WriteString(clickEnd)
		} else {
			out.WriteString(seq)
		}
	}
	return out.String()
}

// Only annotate explicit controls, before wrapping the footer.
func mouseHints(text string) string {
	return strings.NewReplacer(
		"?/Esc 返回", clickable("key:esc", "?/Esc 返回"),
		"? 帮助", clickable("key:?", "? 帮助"),
		"? 详情", clickable("key:?", "? 详情"),
		"Esc 返回", clickable("key:esc", "Esc 返回"),
		"Esc 取消", clickable("key:esc", "Esc 取消"),
	).Replace(text)
}

type mouseCell struct{ x, y int }

type mouseFrame struct {
	cells                       map[mouseCell]string
	page                        string
	disabled                    bool
	scrollPosition, scrollLimit int
	pressedTarget, pressedPage  string
	pressedAt                   mouseCell
	pressedButton               tea.MouseButton
}

func (f *mouseFrame) toggle() tea.Cmd {
	f.disabled = !f.disabled
	f.pressedTarget = ""
	if f.disabled {
		return tea.DisableMouse
	}
	return tea.EnableMouseCellMotion
}

func (f *mouseFrame) enabled() bool { return f != nil && !f.disabled }

func (f *mouseFrame) scroll(delta int) int {
	f.scrollPosition = max(0, min(f.scrollPosition+delta*3, f.scrollLimit))
	return f.scrollPosition
}

// render records only the targets actually visible in this frame. In
// particular a disappearing pairing request never turns into a different ID.
func (f *mouseFrame) render(text, page string) string {
	cells := make(map[mouseCell]string)
	var out strings.Builder
	var state byte
	var target string
	x, y := 0, 0
	for len(text) > 0 {
		seq, width, n, next := ansi.DecodeSequence(text, state, nil)
		state, text = next, text[n:]
		switch {
		case strings.HasPrefix(seq, clickPrefix):
			target = strings.TrimSuffix(strings.TrimPrefix(seq, clickPrefix), "\x07")
			continue
		case seq == clickEnd:
			target = ""
			continue
		case seq == "\n":
			x, y = 0, y+1
		default:
			for column := 0; column < width; column++ {
				if target != "" {
					cells[mouseCell{x + column, y}] = target
				}
			}
			x += width
		}
		out.WriteString(seq)
	}
	if f != nil {
		if f.page != page || (f.pressedButton != tea.MouseButtonRight && cells[f.pressedAt] != f.pressedTarget) {
			f.pressedTarget = ""
		}
		f.cells, f.page = cells, page
	}
	return out.String()
}

// A click commits on release only if the same control stayed under the pointer.
// Moving away, scrolling, resizing or changing pages cancels the gesture.
func (f *mouseFrame) target(msg tea.MouseMsg, page string) string {
	if f == nil {
		return ""
	}
	cell := mouseCell{msg.X, msg.Y}
	current := ""
	if f.page == page {
		current = f.cells[cell]
	}
	if msg.Action == tea.MouseActionPress {
		f.pressedTarget = ""
		if msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonRight {
			if msg.Button == tea.MouseButtonRight && f.page == page {
				current = "key:esc"
			}
			f.pressedTarget, f.pressedPage, f.pressedAt = current, page, cell
			f.pressedButton = msg.Button
		}
		return ""
	}
	if msg.Action == tea.MouseActionMotion {
		if f.pressedButton == tea.MouseButtonRight {
			if cell != f.pressedAt {
				f.pressedTarget = ""
			}
		} else if current != f.pressedTarget {
			f.pressedTarget = ""
		}
		return ""
	}
	if msg.Action != tea.MouseActionRelease {
		return ""
	}
	pressed := f.pressedTarget
	f.pressedTarget = ""
	if msg.Button != f.pressedButton && msg.Button != tea.MouseButtonNone {
		return ""
	}
	if f.pressedPage != page || f.page != page {
		return ""
	}
	if f.pressedButton == tea.MouseButtonRight {
		if cell == f.pressedAt {
			return pressed
		}
		return ""
	}
	if current != pressed || f.cells[f.pressedAt] != pressed {
		return ""
	}
	return pressed
}

func mouseKey(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func wheelDelta(msg tea.MouseMsg) int {
	if msg.Button == tea.MouseButtonWheelUp {
		return -1
	}
	if msg.Button == tea.MouseButtonWheelDown {
		return 1
	}
	return 0
}

func actionIndex(target string) (int, bool) {
	if !strings.HasPrefix(target, "action:") {
		return 0, false
	}
	index, err := strconv.Atoi(strings.TrimPrefix(target, "action:"))
	return index, err == nil && index >= 0
}

func (m model) mousePage() string {
	switch {
	case m.editGain:
		return "gain-edit"
	case m.showHelp:
		return "help"
	case m.showQR:
		return "qr"
	case m.showPairing:
		if m.pairConfirm != "" {
			return "pair-confirm:" + m.pairConfirm
		}
		if m.pairDevices {
			return "devices:" + m.pairSelected
		}
		return "requests:" + m.pairSelected
	case m.showVirtual:
		if m.hasDriverList() && m.drivers.operation != "" {
			return "driver-confirm:" + m.drivers.operation
		}
		if m.confirmUninstall {
			return "uninstall"
		}
		return "virtual"
	default:
		return "main"
	}
}

func (m model) mouseUpdate(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.mouse.enabled() || m.pairBusy || m.virtualBusy {
		return m, nil
	}
	if m.editGain {
		if m.mouse.target(msg, m.mousePage()) == "key:esc" {
			return m.gainKey(mouseKey("esc"))
		}
		return m, nil
	}
	if m.showVirtual && m.hasDriverList() && m.drivers.operation != "" {
		target := m.mouse.target(msg, m.mousePage())
		if index, ok := actionIndex(target); ok {
			m.confirmYes = index == 1
			return m.driverListKey(mouseKey("enter"))
		}
		if target == "key:esc" {
			return m.driverListKey(mouseKey("esc"))
		}
		return m, nil
	}
	if delta := wheelDelta(msg); delta != 0 {
		m.mouse.pressedTarget = ""
		switch {
		case m.showVirtual && m.hasDriverList() && !m.showHelp:
			name := "down"
			if delta < 0 {
				name = "up"
			}
			return m.driverListKey(mouseKey(name))
		case m.showHelp, m.showVirtual && !m.confirmUninstall:
			m.virtualScroll = m.mouse.scroll(delta)
		case m.showPairing && m.pairConfirm == "":
			key := "down"
			if delta < 0 {
				key = "up"
			}
			return m.Update(mouseKey(key))
		case !m.showQR && !m.showPairing && !m.showVirtual:
			m.mainScroll = m.mouse.scroll(delta)
		}
		return m, nil
	}
	target := m.mouse.target(msg, m.mousePage())
	if target == "" {
		return m, nil
	}
	if m.showVirtual && m.hasDriverList() && strings.HasPrefix(target, "driver:") {
		index, err := strconv.Atoi(strings.TrimPrefix(target, "driver:"))
		if err == nil && index >= 0 && index < len(m.drivers.items) {
			m.drivers.selected = index
		}
		return m, nil
	}
	if m.pairConfirm != "" || m.confirmUninstall {
		if _, ok := actionIndex(target); !ok && target != "key:esc" && target != "key:enter" {
			return m, nil
		}
	}
	if strings.HasPrefix(target, "key:") {
		return m.Update(mouseKey(strings.TrimPrefix(target, "key:")))
	}
	if index, ok := actionIndex(target); ok {
		switch {
		case m.pairConfirm != "" || m.confirmUninstall:
			m.confirmYes = index == 1
		case m.showPairing:
			m.pairAction = index
		case m.showVirtual:
			m.virtualFocus = index
		default:
			m.actionFocus = index
		}
		return m.Update(mouseKey("enter"))
	}
	if strings.HasPrefix(target, "pair:") && m.showPairing && m.pairConfirm == "" {
		id := strings.TrimPrefix(target, "pair:")
		if m.pairDevices {
			for _, d := range m.pairing.Devices() {
				if d.ID == id {
					m.pairSelected = id
				}
			}
		} else {
			for _, r := range m.pairing.Requests() {
				if r.ID == id {
					m.pairSelected = id
				}
			}
		}
	}
	if target == "receive" {
		m.selectedSend = false
	}
	if target == "send" {
		m.selectedSend = true
	}
	return m, nil
}

func (m *Setup) mousePage() string {
	if m.confirmApply {
		return "setup-apply-confirm"
	}
	if m.guide {
		return "setup-guide"
	}
	if m.shortcutEditor != nil {
		if e := m.shortcutEditor; e.steps != nil {
			return "shortcut-steps:" + strconv.Itoa(e.slot) + ":" + strconv.Itoa(e.steps.selected) + ":" + strconv.FormatBool(e.steps.editing)
		}
		return "setup-shortcuts:" + strconv.Itoa(m.shortcutEditor.focus) + ":" + strconv.FormatBool(m.shortcutEditor.editing)
	}
	if m.picker != nil {
		return "setup-picker:" + strconv.Itoa(m.picker.field) + ":" + m.picker.query
	}
	if m.showHelp {
		return "setup-help"
	}
	if m.editing {
		return "setup-edit"
	}
	return "setup"
}

func (m *Setup) mouseUpdate(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.mouse.enabled() || m.busy {
		return m, nil
	}
	if m.confirmApply {
		target := m.mouse.target(msg, m.mousePage())
		if index, ok := actionIndex(target); ok {
			m.confirmApplyYes = index == 1
			return m.Update(mouseKey("enter"))
		}
		if target == "key:esc" {
			return m.Update(mouseKey("esc"))
		}
		return m, nil
	}
	if m.guide {
		target := m.mouse.target(msg, m.mousePage())
		if strings.HasPrefix(target, "key:") {
			return m.guideKey(mouseKey(strings.TrimPrefix(target, "key:")))
		}
		return m, nil
	}
	if m.picker != nil {
		return m.pickerMouse(msg)
	}
	if delta := wheelDelta(msg); delta != 0 {
		m.mouse.pressedTarget = ""
		if m.showHelp {
			m.helpScroll = m.mouse.scroll(delta)
		} else if !m.editing {
			m.focus = max(0, min(14, m.focus+delta))
		}
		return m, nil
	}
	target := m.mouse.target(msg, m.mousePage())
	if strings.HasPrefix(target, "key:") {
		return m.Update(mouseKey(strings.TrimPrefix(target, "key:")))
	}
	if m.showHelp || m.editing {
		return m, nil
	}
	if index, ok := actionIndex(target); ok && index < 6 {
		m.focus = 9 + index
		return m.Update(mouseKey("enter"))
	}
	if strings.HasPrefix(target, "field:") {
		index, err := strconv.Atoi(strings.TrimPrefix(target, "field:"))
		if err != nil || index < 0 || index > 8 || !m.fieldEnabled(index) {
			return m, nil
		}
		m.focus = index
		return m.Update(mouseKey("enter"))
	}
	return m, nil
}
