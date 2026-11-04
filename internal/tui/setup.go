package tui

import (
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SetupDevices struct {
	Windows                     bool
	MacOS                       bool
	Outputs, Captures, Networks []string
	Warning                     string
}
type SetupOptions struct {
	Running         bool
	Apply           func(launcher.Settings) error
	ApplyScope      func(launcher.Settings, string) error
	RunningSettings *launcher.Settings
	Draft           *launcher.Settings
	Directory       string
	Settings        launcher.Settings
	Notice          string
	Discover        func() (SetupDevices, error)
	Save            func(launcher.Settings) error
	Manager         VirtualManager
	OpenDrivers     bool
}
type setupDiscovery struct {
	devices SetupDevices
	err     error
}
type setupSaved struct {
	err      error
	start    bool
	settings launcher.Settings
	scope    string
}
type Setup struct {
	shortcutEditor      *shortcutEditor
	shortcutDraft       *shortcutEditor
	mouse               *mouseFrame
	picker              *setupPicker
	options             SetupOptions
	Settings            launcher.Settings
	Start               bool
	devices             SetupDevices
	driver              model
	saved               launcher.Settings
	scope               string
	confirmApply        bool
	confirmApplyYes     bool
	guide, guideOffered bool
	focus               int
	busy                bool
	notice              string
	showHelp            bool
	helpScroll          int
	editing             bool
	edit                string
	width, height       int
}

func NewSetup(options SetupOptions) *Setup {
	mouse := &mouseFrame{}
	initial := options.Settings
	if options.Draft != nil {
		initial = *options.Draft
	}
	return &Setup{mouse: mouse, options: options, Settings: initial, saved: options.Settings, notice: options.Notice, busy: true, width: 100, height: 30,
		driver: model{mouse: mouse, windowsDrivers: runtime.GOOS == "windows", virtualSteam: runtime.GOOS == "windows", virtualManager: options.Manager, width: 100, height: 30}}
}
func (m *Setup) discover() tea.Cmd {
	return func() tea.Msg { devices, err := m.options.Discover(); return setupDiscovery{devices, err} }
}
func (m *Setup) MaintenanceRequested() bool { return m.driver.maintenanceRequested }
func (m *Setup) Init() tea.Cmd              { return m.discover() }
func (m *Setup) save(start bool) tea.Cmd    { return m.saveScope(start) }
func (m *Setup) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	defer func() {
		if m.options.Draft != nil {
			*m.options.Draft = m.Settings
		}
	}()
	if m.focus < 9 {
		m.scope = m.currentScope()
	}
	if _, ok := message.(tea.KeyMsg); ok && m.mouse != nil {
		m.mouse.pressedTarget = ""
		if m.picker != nil {
			m.picker.resetClick()
		}
	}
	if tick, ok := message.(shortcutCaptureTick); ok {
		return m.updateShortcutCapture(tick)
	}
	if m.shortcutEditor != nil && m.shortcutEditor.capture != nil {
		if key, ok := message.(tea.KeyMsg); ok {
			return m.shortcutKey(key)
		}
	}
	if key, ok := message.(tea.KeyMsg); ok && key.String() == "f2" && m.mouse != nil {
		return m, m.mouse.toggle()
	}
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		if m.picker != nil {
			m.picker.resetClick()
		}
		if m.mouse != nil {
			m.mouse.cells = nil
			m.mouse.pressedTarget = ""
		}
		m.width = msg.Width
		m.height = msg.Height
		m.driver.width = msg.Width
		m.driver.height = msg.Height
	case setupDiscovery:
		m.busy = false
		m.devices = msg.devices
		if msg.err != nil {
			m.notice = "设备读取失败：" + msg.err.Error()
		}
		// Prefer only the known virtual cable on first use. A lost saved device
		// remains selected and must be explicitly replaced by the user.
		if m.Settings.Output == "" && !m.driver.hasDriverList() {
			m.Settings.Output = uniqueCable(msg.devices.Outputs, "CABLE Input")
			if msg.devices.Windows {
				m.Settings.Output = audio.PreferredWindowsMicrophone(msg.devices.Outputs, msg.devices.Captures)
			}
			if msg.devices.MacOS {
				m.Settings.Output = uniqueCable(msg.devices.Outputs, "BlackHole 2ch")
			}
		}
		if m.Settings.Capture == "" && !m.driver.hasDriverList() {
			m.Settings.Capture = uniqueCable(msg.devices.Captures, "CABLE Output")
			if msg.devices.MacOS {
				m.Settings.Capture = uniqueCable(msg.devices.Captures, "BlackHole 16ch")
			}
		}
		if !m.guideOffered && !m.options.Running && !m.options.OpenDrivers && m.driver.hasDriverList() {
			m.guideOffered = true
			m.guide = (m.Settings.Mode.Microphone() && m.Settings.Output == "") || (m.Settings.Mode.Playback() && m.Settings.Capture == "")
		}
		if m.options.OpenDrivers {
			m.options.OpenDrivers = false
			m.driver.showVirtual = true
			m.driver.virtualBusy = true
			return m, m.driver.virtualCommand("status")
		}
		return m, nil
	case setupSaved:
		m.busy = false
		if msg.err != nil {
			m.notice = "保存配置失败：" + msg.err.Error()
			if m.options.Running && msg.start {
				m.notice = "应用配置失败：" + msg.err.Error()
			}
			return m, nil
		}
		m.saved = msg.settings
		if msg.start {
			m.Start = true
			return m, tea.Quit
		}
		m.notice = "已保存"
		return m, nil
	}
	if m.driver.showVirtual {
		updated, command := m.driver.Update(message)
		m.driver = updated.(model)
		if m.driver.maintenanceRequested {
			return m, tea.Quit
		}
		if !m.driver.showVirtual {
			m.busy = true
			return m, m.discover()
		}
		return m, command
	}
	if m.shortcutEditor != nil {
		if mouse, ok := message.(tea.MouseMsg); ok {
			return m.shortcutMouse(mouse)
		}
		if key, ok := message.(tea.KeyMsg); ok {
			return m.shortcutKey(key)
		}
		return m, nil
	}
	if mouse, ok := message.(tea.MouseMsg); ok {
		return m.mouseUpdate(mouse)
	}
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.busy {
		if key.String() == "ctrl+c" && !m.options.Running {
			return m, tea.Quit
		}
		return m, nil
	}
	if m.confirmApply {
		switch key.String() {
		case "esc", "n":
			m.confirmApply = false
		case "left", "right", "tab":
			m.confirmApplyYes = !m.confirmApplyYes
		case "enter", "y":
			if key.String() == "y" || m.confirmApplyYes {
				m.confirmApply = false
				m.busy = true
				return m, m.save(true)
			}
			m.confirmApply = false
		}
		return m, nil
	}
	if m.guide {
		return m.guideKey(key)
	}
	if m.picker != nil {
		return m.pickerKey(key)
	}
	if key.String() == "?" && !m.editing {
		m.showHelp = !m.showHelp
		return m, nil
	}
	if m.showHelp {
		if key.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if key.String() == "esc" {
			m.showHelp = false
		}
		if key.String() == "down" || key.String() == "s" {
			m.helpScroll++
		}
		if key.String() == "up" || key.String() == "w" {
			m.helpScroll = max(0, m.helpScroll-1)
		}
		return m, nil
	}
	if m.editing {
		switch key.String() {
		case "esc":
			m.editing = false
		case "enter":
			port, err := strconv.Atoi(m.edit)
			if err != nil || port < 1 || port > 65535 {
				m.notice = "端口必须为 1–65535"
				return m, nil
			}
			if m.focus == 5 {
				m.Settings.HTTPSPort = port
			} else {
				m.Settings.UDPPort = port
			}
			m.editing = false
			m.notice = ""
		case "backspace", "ctrl+h":
			if len(m.edit) > 0 {
				m.edit = m.edit[:len(m.edit)-1]
			}
		case "ctrl+u":
			m.edit = ""
		case "ctrl+c":
			return m, tea.Quit
		default:
			for _, digit := range key.Runes {
				if digit >= '0' && digit <= '9' && len(m.edit) < 5 {
					m.edit += string(digit)
				}
			}
		}
		return m, nil
	}
	if key.String() == "enter" && m.focus >= 9 {
		commands := []rune{'g', 'S', 'v', 'r', 'K', 'q'}
		return m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{commands[m.focus-9]}})
	}
	switch key.String() {
	case "f3":
		m.focus = 4
		m.scope = "network"
		return m, nil
	case "f4":
		m.focus = 7
		m.scope = "audio"
		return m, nil
	case "c":
		m.focus = 0
		m.scope = "audio"
		return m, nil
	case "K":
		m.shortcutEditor = m.shortcutDraft
		if m.shortcutEditor == nil {
			m.shortcutEditor = &shortcutEditor{draft: m.Settings.Shortcuts}
		}
		return m, nil
	case "esc":
		if m.options.Running {
			return m, tea.Quit
		}
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab", "down", "j", "s":
		m.focus = (m.focus + 1) % 15
	case "shift+tab", "up", "k", "w":
		m.focus = (m.focus + 14) % 15
	case "left", "h", "a":
		if m.focus >= 9 {
			m.focus = 9 + (m.focus-9+5)%6
		} else {
			m.change(-1)
		}
	case "right", "l", "d", " ":
		if m.focus >= 9 {
			m.focus = 9 + (m.focus-9+1)%6
		} else {
			m.change(1)
		}
	case "enter":
		if m.openPicker() {
			return m, nil
		}
		if m.focus == 5 || m.focus == 6 {
			m.editing = true
			if m.focus == 5 {
				m.edit = strconv.Itoa(m.Settings.HTTPSPort)
			} else {
				m.edit = strconv.Itoa(m.Settings.UDPPort)
			}
		} else {
			m.change(1)
		}
	case "r":
		m.busy = true
		m.notice = ""
		return m, m.discover()
	case "v":
		m.driver.virtualReadOnly = m.options.Running
		m.driver.canMaintain = m.options.Running
		m.driver.showVirtual = true
		m.driver.virtualBusy = true
		return m, m.driver.virtualCommand("status")
	case "S", "ctrl+s":
		if err := m.scopedSettings().Validate(); err != nil {
			m.notice = err.Error()
			return m, nil
		}
		m.busy = true
		return m, m.save(false)
	case "ctrl+r":
		m.resetScope()
		return m, nil
	case "g":
		if m.options.Running {
			candidate := m.scopedSettings()
			if err := candidate.Validate(); err != nil {
				m.notice = err.Error()
				return m, nil
			}
			if m.currentScope() == "audio" {
				if err := candidate.ValidateDevices(m.devices.Outputs, m.devices.Captures); err != nil {
					m.notice = err.Error()
					return m, nil
				}
			}
			if m.currentScope() == "network" && candidate.Network != "" && !slices.Contains(m.devices.Networks, candidate.Network) {
				m.notice = "所选网络地址已失效"
				return m, nil
			}
			current := m.options.Settings
			if m.options.RunningSettings != nil {
				current = *m.options.RunningSettings
			}
			next := MergeScope(current, candidate, m.currentScope())
			if current.AudioChanged(next) || current.NetworkChanged(next) {
				m.confirmApply = true
				m.confirmApplyYes = false
				return m, nil
			}
			m.busy = true
			return m, m.save(true)
		}
		if m.devices.Windows && m.Settings.Mode.Microphone() {
			if _, err := audio.WindowsMicrophonePeer(m.Settings.Output, m.devices.Captures); err != nil {
				m.notice = err.Error()
				return m, nil
			}
		}
		if m.devices.Windows && m.Settings.Mode == audio.ModeDuplex {
			if err := audio.ValidateWindowsDuplex(m.Settings.Output, m.Settings.Capture); err != nil {
				m.notice = err.Error()
				return m, nil
			}
		}
		if m.devices.MacOS && m.Settings.Mode == audio.ModeDuplex {
			if err := audio.ValidateMacDuplex(m.Settings.Output, m.Settings.Capture); err != nil {
				m.notice = err.Error()
				return m, nil
			}
		}
		if err := m.Settings.ValidateDevices(m.devices.Outputs, m.devices.Captures); err != nil {
			m.notice = err.Error()
			return m, nil
		}
		if m.Settings.Network != "" && !slices.Contains(m.devices.Networks, m.Settings.Network) {
			m.notice = "所选网络地址已失效；请刷新后重新选择。"
			return m, nil
		}
		m.busy = true
		return m, m.save(true)
	}
	return m, nil
}
func uniqueCable(devices []string, prefix string) string {
	matches := []string{}
	for _, device := range devices {
		if device == prefix || strings.HasPrefix(device, prefix+" (") {
			matches = append(matches, device)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
func cycle(current string, values []string, delta int) string {
	if len(values) == 0 {
		return current
	}
	index := slices.Index(values, current)
	if index < 0 {
		if delta > 0 {
			return values[0]
		}
		return values[len(values)-1]
	}
	return values[(index+delta+len(values))%len(values)]
}
func (m *Setup) change(delta int) {
	if !m.fieldEnabled(m.focus) {
		return
	}
	switch m.focus {
	case 0:
		next := audio.Mode(cycle(string(m.Settings.Mode), []string{"mic", "playback", "duplex"}, delta))
		candidate, err := m.modeSettings(next)
		if err != nil {
			m.notice = err.Error()
			return
		}
		m.Settings = candidate
	case 1:
		if !m.Settings.Mode.Microphone() {
			return
		}
		m.Settings.Output = cycle(m.Settings.Output, m.devices.Outputs, delta)
	case 2:
		if !m.Settings.Mode.Playback() {
			return
		}
		m.Settings.Capture = cycle(m.Settings.Capture, m.devices.Captures, delta)
	case 3:
		var ids []string
		for _, q := range audio.Qualities() {
			if q.ValidateMode(m.Settings.SessionMode()) == nil {
				ids = append(ids, q.ID)
			}
		}
		m.Settings.Quality = cycle(m.Settings.Quality, ids, delta)
	case 8:
		var modes []string
		quality, _ := audio.FindQuality(m.Settings.Quality)
		for _, mode := range []audio.Mode{audio.ModeMic, audio.ModePlayback, audio.ModeDuplex} {
			if launcher.Allows(m.Settings.Mode, mode) && quality.ValidateMode(mode) == nil {
				modes = append(modes, string(mode))
			}
		}
		m.Settings.DefaultMode = audio.Mode(cycle(string(m.Settings.SessionMode()), modes, delta))
	case 7:
		current := m.Settings.CaptureChannels
		if current == 0 {
			current = 2
		}
		next := cycle(strconv.Itoa(current), []string{"2", "6", "8"}, delta)
		m.Settings.CaptureChannels, _ = strconv.Atoi(next)
	case 4:
		m.Settings.Network = cycle(m.Settings.Network, append([]string{""}, m.devices.Networks...), delta)
	}
	m.notice = ""
}
func (m *Setup) View() string {
	if m.driver.showVirtual {
		return m.driver.View()
	}
	return m.mouse.render(m.view(), m.mousePage())
}

func (m *Setup) view() string {
	if m.confirmApply {
		selected := 0
		if m.confirmApplyYes {
			selected = 1
		}
		return page([]string{accent.Render(" 应用配置 "), "当前连接将重新建立；失败保留草稿并报告恢复结果。"}, actionBar([]string{"取消 Esc/n", "确认 y"}, selected, m.width)+"\n←→ 选择 · Enter 确定", m.width, m.height, 0)
	}
	if m.guide {
		return m.guideView()
	}
	if m.shortcutEditor != nil {
		return m.shortcutView()
	}
	if m.picker != nil {
		return m.pickerView()
	}
	deviceValue := func(value string, devices []string, enabled bool) string {
		if !enabled {
			return "未启用"
		}
		if value == "" {
			return "未选择"
		}
		if !slices.Contains(devices, value) {
			return value + "（不可用）"
		}
		return value
	}
	output := deviceValue(m.Settings.Output, m.devices.Outputs, m.Settings.Mode.Microphone())
	capture := deviceValue(m.Settings.Capture, m.devices.Captures, m.Settings.Mode.Playback())
	quality, _ := audio.FindQuality(m.Settings.Quality)
	network := m.Settings.Network
	if network == "" {
		network = "自动"
	} else if !slices.Contains(m.devices.Networks, network) {
		network += "（已失效）"
	}
	count := m.Settings.CaptureChannels
	if count == 0 {
		count = 2
	}
	rows := []string{m.Settings.Mode.Label(), output, capture, quality.Label, network, strconv.Itoa(m.Settings.HTTPSPort), strconv.Itoa(m.Settings.UDPPort), strconv.Itoa(count), m.Settings.SessionMode().Label()}
	labels := []string{"允许的音频方向", "麦克风送入", "电脑声音来源", "默认音质", "监听地址", "HTTPS 端口", "音频端口", "电脑采集声道", "默认会话模式"}
	var lines []string
	lines = append(lines, accent.Render(" RMic · 启动配置 "))
	focusLine := 1
	for index, value := range rows {
		if index == 0 {
			lines = append(lines, "音频")
		}
		if index == 4 {
			lines = append(lines, "网络")
		}
		if index == 7 {
			lines = append(lines, "音频选项")
		}
		if index == m.focus {
			focusLine = len(lines)
			if m.width >= 100 && !m.editing && (index == 6 || index == 8) {
				focusLine--
			}
		}
		marker := "  "
		if index == m.focus {
			marker = "▶ "
		}
		if index == m.focus && m.editing {
			value = "[ " + m.edit + "_ ]  Enter 确定 / Esc 取消 / Ctrl+U 清空"
		}
		width := max(12, m.width-2)
		if m.width >= 100 && !m.editing && index >= 5 {
			width = (m.width - 4) / 2
		}
		enabled := m.fieldEnabled(index)
		hint := " ›"
		if index == 5 || index == 6 {
			hint = " ✎"
		}
		if !enabled {
			hint = ""
		}
		line := fitText(fmt.Sprintf("%s%s：%s", marker, labels[index], value), width-lipgloss.Width(hint))
		line += hint
		line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
		if !enabled {
			line = dim.Render(line)
		} else if index == m.focus {
			line = accent.Render(line)
		}
		if !m.editing && enabled {
			line = clickable("field:"+strconv.Itoa(index), line)
		}
		if m.width >= 100 && !m.editing && (index == 6 || index == 8) {
			lines[len(lines)-1] += "  " + line
		} else {
			lines = append(lines, line)
		}
	}
	lines = append(lines, "")
	if m.Settings.Mode.Microphone() && m.devices.Windows {
		if peer, err := audio.WindowsMicrophonePeer(m.Settings.Output, m.devices.Captures); err == nil {
			lines = append(lines, "应用麦克风："+peer)
		}
	}
	if m.showHelp {
		lines = []string{accent.Render(" RMic · 配置帮助 "), ""}
		if m.notice != "" {
			lines = append(lines, m.notice)
		}
		lines = append(lines, "", "麦克风送入：选择虚拟声卡播放端；会议/游戏选择其配对录音端。",
			"电脑声音来源：选择独立线缆录音端，或 loopback: 播放端回环。",
			"双向使用不同通道。关闭录音端的‘侦听此设备’以免听到自己。",
			"未选择设备时，进入驱动管理安装后刷新。",
			"监听地址‘自动’包含局域网及 Tailscale。")
		if quality.PlaybackOnly {
			lines = append(lines, "Ultra：384 kbps / 48 kHz 立体声，仅电脑 → 手机。发送与手机播放缓冲在网页分别设置，默认手机自动调节；适合音乐、播客。")
		}
		if m.options.Directory != "" {
			lines = append(lines, "数据目录："+m.options.Directory)
		}
		lines = append(lines, "c 音频 · F3 网络 · F4 音频选项 · Ctrl+R 还原当前分组。")
		lines = append(lines, "配置选项：点击或 Enter 打开列表，输入名称搜索，↑↓/滚轮选择。", "双击项目或 Enter 确认，右键或 Esc 取消，Ctrl+U 清空搜索；PageUp/PageDown 翻页。")
		lines = append(lines, "", "鼠标点击整行打开，按下后拖出可取消，滚轮浏览；F2 关闭/恢复鼠标，方便选取文字。")
		return m.mouse.scrolledPage(lines, "↑↓/WS 滚动 · ?/Esc 返回", m.width, m.height, m.helpScroll)
	}
	if m.options.Running {
		lines = append(lines, "传音保持运行 · Esc 返回并保留草稿", m.runningImpact())
	}
	if m.devices.Warning != "" {
		lines = append(lines, "提示："+m.devices.Warning)
	}
	if m.scopedSettings() != m.saved {
		lines = append(lines, "待保存："+map[string]string{"audio": "音频", "network": "网络", "shortcuts": "快捷按钮"}[m.currentScope()]+" · Ctrl+R 还原分组")
	}
	if m.busy {
		lines = append(lines, "正在读取或保存，请稍候……")
	}
	actions := []string{"开始 g", "保存 S", "驱动 v", "刷新 r", "快捷键 K", "退出 q"}
	if m.options.Running {
		actions = []string{"应用 g", "保存 S", "驱动 v", "刷新 r", "快捷键 K", "返回 q"}
	}
	footer := actionBar(actions, m.focus-9, m.width) + "\n↑↓ 选择 · Enter 编辑 · ? 帮助"
	if m.editing {
		footer = clickable("key:enter", footerControl("确定", true, 6)) + "  " + clickable("key:esc", footerControl("取消", false, 6)) + "\nEnter 确定 · Esc 取消 · Ctrl+U 清空"
	}
	if m.notice != "" {
		footer = "提示：" + m.notice + "\n" + footer
	}
	return page(lines, footer, m.width, m.height, focusLine)
}
