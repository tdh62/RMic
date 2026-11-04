package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The picker edits a draft selection. Only explicit confirmation changes settings.
// Entries retain their original index while filtering, so a click cannot select
// a different device merely because its visible row moved.
type setupPicker struct {
	field                         int
	title, current, query, notice string
	values                        []string
	selected                      int
	lastClick                     time.Time
	lastIndex                     int
}

// A second completed click on the same entry confirms within 400 ms.
func (p *setupPicker) doubleClick(index int, now time.Time) bool {
	double := !p.lastClick.IsZero() && p.lastIndex == index && now.Sub(p.lastClick) >= 0 && now.Sub(p.lastClick) <= 400*time.Millisecond
	p.lastClick, p.lastIndex = now, index
	if double {
		p.lastClick = time.Time{}
	}
	return double
}
func (p *setupPicker) resetClick() { p.lastClick = time.Time{} }
func (m *Setup) fieldEnabled(index int) bool {
	if index == 1 {
		return m.Settings.Mode.Microphone()
	}
	if index == 2 || index == 7 {
		return m.Settings.Mode.Playback()
	}
	return true
}

func (m *Setup) modeSettings(mode audio.Mode) (launcher.Settings, error) {
	candidate := m.Settings
	candidate.Mode = mode
	if !launcher.Allows(mode, candidate.SessionMode()) {
		candidate.DefaultMode = ""
	}
	quality, _ := audio.FindQuality(candidate.Quality)
	return candidate, quality.ValidateMode(candidate.SessionMode())
}
func (m *Setup) openPicker() bool {
	if !m.fieldEnabled(m.focus) {
		return false
	}
	p := &setupPicker{field: m.focus}
	switch m.focus {
	case 0:
		p.title, p.current = "允许的音频方向", string(m.Settings.Mode)
		for _, mode := range []audio.Mode{audio.ModeMic, audio.ModePlayback, audio.ModeDuplex} {
			if _, err := m.modeSettings(mode); err == nil {
				p.values = append(p.values, string(mode))
			}
		}
	case 3:
		p.title, p.current = "默认音质", m.Settings.Quality
		for _, quality := range audio.Qualities() {
			if quality.ValidateMode(m.Settings.SessionMode()) == nil {
				p.values = append(p.values, quality.ID)
			}
		}
	case 7:
		channels := m.Settings.CaptureChannels
		if channels == 0 {
			channels = 2
		}
		p.title, p.current, p.values = "电脑采集声道", strconv.Itoa(channels), []string{"2", "6", "8"}
	case 8:
		p.title, p.current = "默认会话模式", string(m.Settings.SessionMode())
		quality, _ := audio.FindQuality(m.Settings.Quality)
		for _, mode := range []audio.Mode{audio.ModeMic, audio.ModePlayback, audio.ModeDuplex} {
			if launcher.Allows(m.Settings.Mode, mode) && quality.ValidateMode(mode) == nil {
				p.values = append(p.values, string(mode))
			}
		}
	case 1:
		if !m.Settings.Mode.Microphone() {
			return false
		}
		p.title, p.current, p.values = "麦克风送入", m.Settings.Output, slices.Clone(m.devices.Outputs)
	case 2:
		if !m.Settings.Mode.Playback() {
			return false
		}
		p.title, p.current, p.values = "电脑声音来源", m.Settings.Capture, slices.Clone(m.devices.Captures)
	case 4:
		p.title, p.current, p.values = "监听地址", m.Settings.Network, append([]string{""}, m.devices.Networks...)
	default:
		return false
	}
	p.selected = max(0, slices.Index(p.values, p.current))
	m.picker = p
	m.mouse.cells = nil
	return true
}

func (p *setupPicker) label(index int) string {
	if p.field == 4 && p.values[index] == "" {
		return "自动（局域网及覆盖网络）"
	}
	switch p.field {
	case 0, 8:
		return audio.Mode(p.values[index]).Label()
	case 3:
		quality, _ := audio.FindQuality(p.values[index])
		return quality.Label
	case 7:
		return p.values[index] + " 声道"
	}
	return p.values[index]
}

func (p *setupPicker) matches() []int {
	var matches []int
	query := strings.ToLower(strings.TrimSpace(p.query))
	for i := range p.values {
		if strings.Contains(strings.ToLower(p.label(i)), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

func (p *setupPicker) move(delta int) {
	matches := p.matches()
	if len(matches) == 0 {
		return
	}
	position := max(0, slices.Index(matches, p.selected))
	p.selected = matches[max(0, min(len(matches)-1, position+delta))]
}

func (m *Setup) confirmPicker() {
	p := m.picker
	if !slices.Contains(p.matches(), p.selected) {
		return
	}
	value := p.values[p.selected]
	valid := false
	switch p.field {
	case 0:
		candidate, err := m.modeSettings(audio.Mode(value))
		valid = err == nil
		if valid {
			m.Settings = candidate
		}
	case 3:
		quality, err := audio.FindQuality(value)
		valid = err == nil && quality.ValidateMode(m.Settings.SessionMode()) == nil
		if valid {
			m.Settings.Quality = value
		}
	case 7:
		valid = m.fieldEnabled(7)
		if valid {
			m.Settings.CaptureChannels, _ = strconv.Atoi(value)
		}
	case 8:
		quality, _ := audio.FindQuality(m.Settings.Quality)
		valid = launcher.Allows(m.Settings.Mode, audio.Mode(value)) && quality.ValidateMode(audio.Mode(value)) == nil
		if valid {
			m.Settings.DefaultMode = audio.Mode(value)
		}
	case 1:
		valid = m.Settings.Mode.Microphone() && slices.Contains(m.devices.Outputs, value)
		if valid {
			m.Settings.Output = value
		}
	case 2:
		valid = m.Settings.Mode.Playback() && slices.Contains(m.devices.Captures, value)
		if valid {
			m.Settings.Capture = value
		}
	case 4:
		valid = value == "" || slices.Contains(m.devices.Networks, value)
		if valid {
			m.Settings.Network = value
		}
	}
	if !valid {
		p.notice = "所选项目已失效，请取消后刷新。"
		return
	}
	m.picker, m.notice = nil, ""
	m.mouse.cells = nil
}

func (m *Setup) pickerKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	p.lastClick = time.Time{}
	previousSelection := p.selected
	defer func() {
		if m.picker != nil && p.selected != previousSelection {
			m.mouse.cells = nil
		}
	}()
	switch key.String() {
	case "esc":
		m.picker = nil
		m.mouse.cells = nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		m.confirmPicker()
	case "up", "shift+tab":
		p.move(-1)
	case "down", "tab":
		p.move(1)
	case "pgup":
		p.move(-m.pickerCapacity())
	case "pgdown":
		p.move(m.pickerCapacity())
	case "home":
		p.move(-len(p.values))
	case "end":
		p.move(len(p.values))
	default:
		previous := p.query
		switch key.String() {
		case "backspace", "ctrl+h":
			runes := []rune(p.query)
			if len(runes) > 0 {
				p.query = string(runes[:len(runes)-1])
			}
		case "ctrl+u":
			p.query = ""
		default:
			if key.Type == tea.KeyRunes && !key.Alt {
				for _, r := range key.Runes {
					if r >= ' ' && r != '\x7f' && len([]rune(p.query)) < 128 {
						p.query += string(r)
					}
				}
			} else if key.Type == tea.KeySpace && len([]rune(p.query)) < 128 {
				p.query += " "
			}
		}
		if p.query != previous {
			p.notice = ""
			matches := p.matches()
			if len(matches) > 0 && !slices.Contains(matches, p.selected) {
				p.selected = matches[0]
			}
			m.mouse.cells = nil
		}
	}
	return m, nil
}

func (m *Setup) pickerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action == tea.MouseActionMotion || wheelDelta(msg) != 0 || (msg.Action == tea.MouseActionPress && msg.Button != tea.MouseButtonLeft) {
		m.picker.lastClick = time.Time{}
	}
	if delta := wheelDelta(msg); delta != 0 {
		m.mouse.pressedTarget = ""
		m.picker.move(delta * 3)
		m.mouse.cells = nil
		return m, nil
	}
	target := m.mouse.target(msg, m.mousePage())
	if !strings.HasPrefix(target, "choice:") && msg.Action == tea.MouseActionRelease {
		m.picker.lastClick = time.Time{}
	}
	switch target {
	case "key:esc":
		return m.pickerKey(mouseKey("esc"))
	case "picker:confirm":
		m.confirmPicker()
	case "picker:cancel":
		m.picker = nil
		m.mouse.cells = nil
	case "picker:clear":
		return m.pickerKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	case "picker:prev":
		m.picker.move(-m.pickerCapacity())
		m.mouse.cells = nil
	case "picker:next":
		m.picker.move(m.pickerCapacity())
		m.mouse.cells = nil
	default:
		if strings.HasPrefix(target, "choice:") {
			index, err := strconv.Atoi(strings.TrimPrefix(target, "choice:"))
			if err == nil && slices.Contains(m.picker.matches(), index) {
				double := m.picker.doubleClick(index, time.Now())
				m.picker.selected = index
				m.picker.notice = ""
				m.mouse.cells = nil
				if double {
					m.confirmPicker()
				}
			}
		}
	}
	return m, nil
}

func (m *Setup) pickerFooter() string {
	p := m.picker
	matches := p.matches()
	position := max(0, slices.Index(matches, p.selected))
	width := max(12, m.width-2)
	controls := clickable("picker:confirm", footerControl("确定", true, 6)) + " " + clickable("picker:cancel", footerControl("取消", false, 6)) + " " + clickable("picker:clear", footerControl("清空", false, 6))
	navigation := clickable("picker:prev", "‹") + fmt.Sprintf(" %d/%d ", min(len(matches), position+1), len(matches)) + clickable("picker:next", "›")
	if width >= 38 {
		navigation += " 双击确认 · 右键取消"
	}
	return lipgloss.NewStyle().Width(width).Render(controls + "\n" + navigation)
}

func (m *Setup) pickerCapacity() int {
	detailLines := max(1, min(4, m.height/5))
	return max(1, m.height-strings.Count(m.pickerFooter(), "\n")-6-detailLines)
}

func (m *Setup) pickerView() string {
	p := m.picker
	width := max(12, m.width-2)
	matches := p.matches()
	position := max(0, slices.Index(matches, p.selected))
	capacity := m.pickerCapacity()
	start := max(0, min(position-capacity/2, len(matches)-capacity))
	end := min(len(matches), start+capacity)
	current := p.current
	if index := slices.Index(p.values, p.current); index >= 0 {
		current = p.label(index)
	}
	if current == "" {
		current = "未选择"
		if p.field == 4 {
			current = "自动"
		}
	} else if !slices.Contains(p.values, current) {
		current += "（不可用）"
	}
	query := p.query + "_"
	if p.query == "" {
		query = "输入名称筛选"
	}
	if ansi.StringWidth(query) > width-8 {
		query = ansi.TruncateLeft(query, ansi.StringWidth(query)-(width-8), "…")
	}
	rows := []string{
		accent.Render(fitText("选择 · "+p.title, width)),
		fitText("当前："+current, width),
		fitText("搜索："+query, width),
	}
	if len(matches) == 0 {
		message := "没有匹配项，清空搜索重试。"
		if len(p.values) == 0 {
			message = "暂无可用设备，取消后刷新或安装驱动。"
		}
		rows = append(rows, fitText(message, width))
	} else {
		for _, index := range matches[start:end] {
			marker := "  "
			if index == p.selected {
				marker = "▶ "
			}
			label := p.label(index)
			if p.values[index] == p.current {
				label = "✓ " + label
			}
			row := fitText(marker+label, width)
			if index == p.selected {
				row = accent.Render(row)
			}
			rows = append(rows, clickable("choice:"+strconv.Itoa(index), row))
		}
	}
	footer := m.pickerFooter()
	// Reserve remaining space for the full selected name, wrapping long names.
	room := max(0, m.height-len(rows)-strings.Count(footer, "\n")-2)
	detail := p.notice
	if detail == "" && len(matches) > 0 {
		detail = p.label(p.selected)
	}
	if room > 0 && detail != "" {
		lines := strings.Split(ansi.Wrap(detail, width, ""), "\n")
		rows = append(rows, lines[:min(room, len(lines))]...)
	}
	return page(rows, footer, m.width, m.height, 0)
}
