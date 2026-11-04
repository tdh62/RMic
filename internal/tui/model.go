package tui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/internal/transport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/skip2/go-qrcode"
)

type Controller interface {
	Gain() float64
	SetGain(float64)
	ToggleMute()
	CaptureGain() float64
	SetCaptureGain(float64)
	ToggleCaptureMute()
}

type VirtualManager interface{ Run(string) (string, error) }
type Option func(*model)

func WithWindowsDriver(driver string) Option {
	return func(m *model) {
		m.virtualSteam = driver == "steam"
		m.virtualCable = 0
		if driver == "vbcable-a" {
			m.virtualCable = 1
		}
		if driver == "vbcable-b" {
			m.virtualCable = 2
		}
	}
}
func WithNotice(notice string) Option        { return func(m *model) { m.notice = notice } }
func WithConfiguration() Option              { return func(m *model) { m.canConfigure = true } }
func (m model) ConfigurationRequested() bool { return m.returnToSetup }
func (m model) MaintenanceRequested() bool   { return m.maintenanceRequested }
func WithMaintenance() Option                { return func(m *model) { m.canMaintain = true } }

func WithVirtualManager(manager VirtualManager) Option {
	return func(m *model) { m.virtualManager = manager }
}

type model struct {
	mouse                                                   *mouseFrame
	mainScroll                                              int
	notice                                                  string
	pairing                                                 *pairing.Manager
	showPairing, pairDevices, pairBusy                      bool
	pairSelected, pairConfirm, pairNotice                   string
	virtualCable                                            int
	windowsDrivers, virtualSteam                            bool
	canConfigure, returnToSetup                             bool
	virtualManager                                          VirtualManager
	showVirtual, managerOnly, virtualBusy, confirmUninstall bool
	virtualBody, virtualResult                              string
	virtualScroll                                           int
	drivers                                                 driverList
	virtualReadOnly                                         bool
	canMaintain, maintenanceRequested                       bool
	server                                                  *transport.Server
	control                                                 Controller
	urls                                                    []string
	input                                                   string
	qr                                                      string
	showQR                                                  bool
	actionFocus, virtualFocus, pairAction                   int
	confirmYes                                              bool
	showHelp                                                bool
	selectedSend                                            bool
	urlIndex                                                int
	editGain                                                bool
	gainText, gainError                                     string
	width, height                                           int
}
type tick time.Time

func refresh() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return tick(t) })
}
func New(server *transport.Server, control Controller, urls []string, input string, options ...Option) tea.Model {
	qr, _ := qrcode.New(urls[0], qrcode.Low)
	showSend := server.Status().Receive.Device == ""
	m := model{mouse: &mouseFrame{}, pairing: server.PairingManager(), windowsDrivers: runtime.GOOS == "windows", virtualSteam: runtime.GOOS == "windows", server: server, control: control, urls: urls, input: input, qr: qr.ToSmallString(false), selectedSend: showSend, width: 80, height: 24}
	for _, option := range options {
		option(&m)
	}
	return m
}
func (m model) Init() tea.Cmd {
	if m.managerOnly {
		return m.virtualCommand("status")
	}
	return refresh()
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if result, ok := msg.(driverStepResult); ok {
		return m.driverStepDone(result)
	}
	if result, ok := msg.(driverListResult); ok {
		m.virtualBusy = false
		m.drivers.items = result.items
		m.drivers.selected = min(m.drivers.selected, max(0, len(result.items)-1))
		m.drivers.marked = nil
		m.virtualResult = result.message
		m.drivers.queue = nil
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && m.virtualBusy {
		allowed := key.String() == "R" || key.String() == "f2" || key.String() == "?"
		if m.showHelp {
			switch key.String() {
			case "esc", "up", "down", "pgup", "pgdown", "w", "s":
				allowed = true
			}
		}
		if !allowed {
			return m, nil
		}
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "R" && m.server != nil && m.server.Status().Shortcuts != nil {
		m.server.StopShortcuts()
		m.notice = "已停止快捷按钮控制；请检查按键状态。"
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && m.editGain {
		return m.gainKey(key)
	}
	if _, ok := msg.(tea.KeyMsg); ok && m.mouse != nil {
		m.mouse.pressedTarget = ""
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "f2" && m.mouse != nil {
		return m, m.mouse.toggle()
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		return m.mouseUpdate(mouse)
	}
	if result, ok := msg.(pairDone); ok {
		m.pairBusy = false
		m.pairNotice = result.message
		if result.err != nil {
			m.pairNotice = "操作失败：" + result.err.Error()
		}
		m.selectPairFirst()
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "?" {
		m.showHelp = !m.showHelp
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && m.showHelp {
		switch key.String() {
		case "esc":
			m.showHelp = false
		case "q", "ctrl+c":
			return m, tea.Quit
		case "down", "s", "pgdown":
			m.virtualScroll++
		case "up", "w", "pgup":
			m.virtualScroll = max(0, m.virtualScroll-1)
		}
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && m.showQR {
		switch key.String() {
		case "esc", "x", "enter":
			m.showQR = false
		case "n", "right", "down", "d", "s", "tab":
			m.nextURL(1)
		case "left", "up", "a", "w", "shift+tab":
			m.nextURL(-1)
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && m.showPairing {
		return m.pairingKey(key)
	}
	if result, ok := msg.(virtualResultMsg); ok {
		return m.virtualCompleted(result)
	}
	if key, ok := msg.(tea.KeyMsg); ok && m.showVirtual {
		return m.updateVirtual(key)
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if m.mouse != nil {
			m.mouse.cells = nil
			m.mouse.pressedTarget = ""
		}
		m.width = msg.Width
		m.height = msg.Height
	case tick:
		return m, refresh()
	case tea.KeyMsg:
		if msg.String() == "enter" {
			return m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{m.mainActions()[m.actionFocus].key}})
		}
		switch msg.String() {
		case "left", "a":
			m.actionFocus = (m.actionFocus + len(m.mainActions()) - 1) % len(m.mainActions())
		case "right", "d":
			m.actionFocus = (m.actionFocus + 1) % len(m.mainActions())
		case "ctrl+c", "q":
			return m, tea.Quit
		case "p":
			if m.pairing != nil {
				m.showPairing = true
				m.pairAction = 0
				m.pairConfirm = ""
				m.pairDevices = false
				m.selectPairFirst()
			}
		case "c":
			if m.canConfigure {
				m.returnToSetup = true
				return m, tea.Quit
			}
		case "tab", "shift+tab", "up", "down", "w", "s":
			status := m.server.Status()
			if status.Receive.Device != "" && status.Send.Device != "" {
				m.selectedSend = !m.selectedSend
			}
		case "g":
			gain := m.control.Gain()
			if m.selectedSend {
				gain = m.control.CaptureGain()
			}
			m.editGain = true
			m.gainText = fmt.Sprintf("%.0f", gain*100)
			m.gainError = ""
		case "m":
			if m.selectedSend {
				m.control.ToggleCaptureMute()
			} else {
				m.control.ToggleMute()
			}
		case "-":
			if m.selectedSend {
				m.control.SetCaptureGain(m.control.CaptureGain() - .1)
			} else {
				m.control.SetGain(m.control.Gain() - .1)
			}
		case "+", "=":
			if m.selectedSend {
				m.control.SetCaptureGain(m.control.CaptureGain() + .1)
			} else {
				m.control.SetGain(m.control.Gain() + .1)
			}
		case "D":
			m.server.Disconnect()
		case "n":
			m.nextURL(1)
		case "v":
			if m.virtualManager != nil {
				m.showVirtual = true
				m.virtualBusy = true
				return m, m.virtualCommand("status")
			}
		case "x":
			m.showQR = !m.showQR
		case "esc":
			m.showQR = false
		}
	}
	return m, nil
}

var accent = lipgloss.NewStyle().Foreground(lipgloss.Color("115")).Bold(true)
var dim = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

func (m model) View() string {
	return m.mouse.render(m.view(), m.mousePage())
}

func (m model) view() string {
	if m.showHelp {
		return m.helpView()
	}
	if m.showQR {
		return m.qrView()
	}
	if m.showPairing {
		return m.pairingView()
	}
	if m.showVirtual {
		return m.virtualView()
	}
	status := m.server.Status()
	labels := map[string]string{"waiting": "等待设备", "idle": "待机", "disabled": "未启用", "connecting": "连接中", "connected": "已连接", "streaming": "传音中", "waiting_audio": "等待音频", "paused": "已暂停", "reconnecting": "重连中"}
	rows := m.workspaceRows(status, labels)
	var actions []string
	for _, action := range m.mainActions() {
		actions = append(actions, fmt.Sprintf("%s %c", action.label, action.key))
	}
	if m.editGain {
		return page(rows, "Enter 确定 · Esc 取消 · Ctrl+U 清空", m.width, m.height, len(rows)-1)
	}
	return m.mouse.scrolledPage(rows, actionBar(actions, m.actionFocus, m.width)+"\n↑↓ 方向 · ←→ 操作 · Enter 执行 · "+clickable("key:-", "[−]")+" 增益 "+clickable("key:+", "[+]")+" · g 数值 · ? 帮助", m.width, m.height, m.mainScroll)
}

type mainAction struct {
	label string
	key   rune
}

func (m model) mainActions() []mainAction {
	actions := []mainAction{{"二维码", 'x'}}
	if m.pairing != nil {
		actions = append(actions, mainAction{"配对", 'p'})
	}
	actions = append(actions, mainAction{"静音", 'm'}, mainAction{"断开", 'D'})
	if m.virtualManager != nil {
		actions = append(actions, mainAction{"驱动", 'v'})
	}
	if m.canConfigure {
		actions = append(actions, mainAction{"配置", 'c'})
	}
	return append(actions, mainAction{"退出", 'q'})
}
func (m *model) nextURL(delta int) {
	m.urlIndex = (m.urlIndex + delta + len(m.urls)) % len(m.urls)
	qr, _ := qrcode.New(m.urls[m.urlIndex], qrcode.Low)
	m.qr = qr.ToSmallString(false)
}
func (m model) qrView() string {
	footer := fmt.Sprintf("扫码连接 · %d/%d · %s · %s · %s", m.urlIndex+1, len(m.urls), clickable("key:left", "‹ 上一个"), clickable("key:right", "下一个 ›"), clickable("key:esc", "返回 Esc"))
	qr := strings.TrimRight(m.qr, "\n")
	link := ansi.Wrap(m.urls[m.urlIndex], max(1, m.width-2), "")
	bottom := lipgloss.NewStyle().Width(max(12, m.width-2)).Render(dim.Render(footer))
	footerLines := strings.Count(bottom, "\n") + 1
	content := lipgloss.JoinVertical(lipgloss.Center, qr, link)
	if lipgloss.Width(content) > m.width || lipgloss.Height(content) > m.height-footerLines-1 {
		return m.mouse.scrolledPage([]string{"终端空间不足，请放大窗口显示二维码。", "", link}, footer, m.width, m.height, 0)
	}
	available := m.height - footerLines - 1
	body := lipgloss.Place(m.width, available, lipgloss.Center, lipgloss.Center, content)
	return body + "\n" + bottom
}
func (m model) helpView() string {
	rows := []string{accent.Render(" RMic · 帮助 "), ""}
	switch {
	case m.showPairing:
		rows = append(rows, "许可前核对手机上的确认码、设备名与来源 IP。", "记住的设备可在电脑重启后继续使用；仅本次授权随服务结束失效。", "撤销设备后会停止其活动连接。")
	case m.showVirtual:
		rows = append(rows, m.virtualBody, "", m.virtualResult)
		if m.hasDriverList() && len(m.drivers.items) > 0 {
			item := m.drivers.items[m.drivers.selected]
			rows = append(rows, item.Name, item.Detail, item.Error)
		}
	default:
		rows = append(rows, "手机扫码打开网页，申请配对；电脑按 p 核对确认码并许可。", "首次访问时需在手机浏览器信任本机证书。", "目标软件的麦克风："+m.input, "↑↓/WS 切换音频方向，←→/AD 选择操作，Enter 执行。", "+/- 调整增益，m 静音，D 断开，x 二维码，n 下一个地址。")
		status := m.server.Status()
		rows = append(rows, "手机送入："+status.Receive.Device, "电脑来源："+status.Send.Device, fmt.Sprintf("电脑缓冲：接收 %.0f ms / 发送 %.0f ms", status.Receive.Audio.BufferedMS, status.Send.Audio.BufferedMS), "g 输入音量百分比；Enter 确定，Esc 放弃。")
		if runtime.GOOS == "windows" {
			rows = append(rows, "电脑声音可选择指定播放端回环，或独立虚拟线缆的录音端。")
		}
	}
	rows = append(rows, "", "鼠标点击按钮，滚轮浏览，右键等同 Esc 取消/返回；F2 关闭/恢复鼠标，方便选取文字。")
	return m.mouse.scrolledPage(rows, "↑↓/WS 滚动 · ?/Esc 返回", m.width, m.height, m.virtualScroll)
}
