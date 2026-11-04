package ui

import (
	"fmt"
	"strings"

	"cnb.cool/tdh6/RMic/internal/transport"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/skip2/go-qrcode"
)

func (v *View) startSaved() {
	if v.busy || v.closing {
		return
	}
	if !v.configured() {
		v.openSetup()
		return
	}
	s := v.saved
	v.perform("正在准备连接…", func() error { return v.backend.Start(s) }, nil)
}
func (v *View) configured() bool {
	return (!v.saved.Mode.Microphone() || v.saved.Output != "") && (!v.saved.Mode.Playback() || v.saved.Capture != "")
}
func (v *View) focusRoute(send bool) {
	picker := v.form.output
	if send {
		picker = v.form.capture
	}
	v.closeTool()
	v.window.Canvas().Focus(picker)
	picker.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
}
func (v *View) updateHome() {
	if v.connection == nil {
		return
	}
	s := v.snapshot
	v.homeError.SetText(s.Status.Error)
	active := s.Running && s.Status.State != "waiting" && s.Status.State != "idle" && s.Status.State != ""
	switch {
	case !v.configured():
		setLabelText(v.state, "选择音频设备")
		setButtonText(v.start, "设置向导")
	case !s.Running:
		setLabelText(v.state, "准备就绪")
		setButtonText(v.start, "启动服务")
	case active:
		setLabelText(v.state, stateLabel(s.Status.State))
	default:
		setLabelText(v.state, "等待手机")
	}
	showWhen(v.start, !s.Running)
	showWhen(v.stop, s.Running)
	showWhen(v.qr, active)
	showWhen(v.disconnect, active)
	showWhen(v.sessionAction, active)
	if active {
		label := strings.ToUpper(s.Status.Transport)
		if label == "" {
			label = "当前会话"
		}
		if s.Status.Quality.ID != "" {
			label += " · " + qualityLabel(s.Status.Quality.ID)
		}
		setButtonText(v.sessionAction, label)
	}
	enableWhen(v.disconnect, active && !v.busy && !v.closing)
	receive, send := s.Status.Receive, s.Status.Send
	latency := "往返 —"
	if active && s.Status.State != "reconnecting" && s.Status.MetricsAgeMS >= 0 && s.Status.MetricsAgeMS <= 5000 && s.Status.Playback.RTTMS > 0 {
		latency = fmt.Sprintf("往返 %.0f ms", s.Status.Playback.RTTMS)
	}
	v.receiveControl.refresh(receive, active, v.busy, latency)
	v.sendControl.refresh(send, active, v.busy, latency)
	mode := modeValue(v.form.mode.Selected)
	showWhen(v.receiveControl.card, mode.Microphone() || active && receive.Device != "")
	showWhen(v.sendControl.card, mode.Playback() || active && send.Device != "")
	for i, enabled := range []bool{mode.Microphone(), mode.Playback()} {
		control := v.receiveControl
		if i == 1 {
			control = v.sendControl
		}
		device := receive.Device
		if i == 1 {
			device = send.Device
		}
		if active && !enabled && device != "" {
			control.status.SetText("待应用")
			control.status.Show()
			control.running.SetText("运行中：" + device)
			control.running.Show()
		}
	}
	signature := fmt.Sprint(s.Running, active, v.configured(), s.URLs)
	if signature == v.connectionSignature {
		return
	}
	v.connectionSignature = signature
	v.connection.RemoveAll()
	v.connection.Show()
	if !s.Running {
		if !v.configured() {
			v.connection.Add(container.NewBorder(nil, nil, widget.NewLabel("首次使用"), widget.NewButton("准备驱动与设备", v.openSetup)))
		} else {
			v.connection.Hide()
		}
		return
	}
	if active {
		v.connection.Hide()
		return
	}
	if len(s.URLs) == 0 {
		v.connection.Add(wrap("正在获取连接地址…"))
		return
	}
	image := canvas.NewImageFromImage(nil)
	image.FillMode = canvas.ImageFillContain
	image.ScaleMode = canvas.ImageScalePixels
	image.SetMinSize(fyne.NewSize(104, 104))
	selector := widget.NewSelect(s.URLs, func(url string) {
		code, err := qrcode.New(url, qrcode.Medium)
		if err == nil {
			image.Image = code.Image(220)
			image.Refresh()
		}
	})
	selector.SetSelectedIndex(0)
	copyLink := widget.NewButtonWithIcon("复制地址", theme.ContentCopyIcon(), func() { v.window.Clipboard().SetContent(selector.Selected) })
	instructions := container.NewVBox(widget.NewLabelWithStyle("扫码连接", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), container.NewBorder(nil, nil, nil, copyLink, bounded(9999, selector)))
	v.connection.Add(container.NewBorder(nil, nil, image, nil, instructions))
}
func stateLabel(state string) string {
	labels := map[string]string{"waiting": "等待手机", "idle": "待机", "disabled": "未启用", "connecting": "正在连接", "connected": "已连接", "streaming": "正在传音", "waiting_audio": "等待声音", "paused": "已暂停", "reconnecting": "正在重连"}
	if label, ok := labels[state]; ok {
		return label
	}
	return "准备中"
}
func (v *View) showSession() {
	s := v.snapshot.Status
	dialog.ShowInformation("当前会话", fmt.Sprintf("协议：%s\n会话音质：%s\n%s", s.Transport, qualityLabel(s.Quality.ID), v.receiveControl.latency.Text), v.window)
}

type directionControl struct {
	card                              *controlPanel
	status, failure, running, latency *widget.Label
	percent                           *gainEntry
	picker                            *widget.Select
	gain                              *widget.Slider
	mute                              *widget.Check
	reset, retry                      *widget.Button
	meter                             *levelMeter
	signal                            *fyne.Container
	syncing, working, pending         bool
}

func (v *View) newDirectionControl(send bool) *directionControl {
	c := &directionControl{status: widget.NewLabel("未启动"), failure: wrap(""), running: widget.NewLabel(""), latency: widget.NewLabel("往返 —"), picker: v.form.output}
	c.failure.Importance = widget.DangerImportance
	c.running.Truncation = fyne.TextTruncateEllipsis
	c.running.Wrapping = fyne.TextWrapOff
	c.gain = widget.NewSlider(0, 3)
	c.gain.Step = .05
	c.gain.SetValue(1)
	c.mute = widget.NewCheck("静音", nil)
	c.meter = newLevelMeter()
	c.percent = newGainEntry(func(value float64) {
		if !c.syncing && !c.gain.Disabled() {
			c.gain.SetValue(value)
		}
	})
	// Serialize each direction and coalesce slider gestures to their latest value.
	var apply func()
	apply = func() {
		if c.syncing || v.closing || v.busy {
			if v.busy || v.closing {
				c.pending = false
			}
			return
		}
		c.pending = true
		if c.working {
			return
		}
		c.pending, c.working = false, true
		gain, muted := c.gain.Value, c.mute.Checked
		v.commandGeneration.Add(1)
		go func() {
			err := v.backend.SetLevels(send, gain, muted)
			fyne.Do(func() {
				c.working = false
				v.commandGeneration.Add(1)
				if v.closing {
					return
				}
				if err != nil {
					c.failure.SetText(err.Error())
					c.failure.Show()
				}
				if c.pending {
					apply()
				}
			})
		}()
	}
	c.gain.OnChanged = func(value float64) {
		if v.window.Canvas().Focused() != c.percent {
			c.percent.SetText(fmt.Sprintf("%.0f", value*100))
		}
		apply()
	}
	c.mute.OnChanged = func(bool) { apply() }
	c.reset = widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { c.percent.SetText("100"); c.gain.SetValue(1) })
	c.reset.Importance = widget.LowImportance
	c.retry = widget.NewButton("重选", func() { v.focusRoute(send) })
	c.retry.Importance = widget.LowImportance
	title := "手机麦克风 → 电脑"
	if send {
		title = "电脑声音 → 手机"
		c.picker = v.form.capture
	}
	heading := widget.NewLabel(title)
	c.status.Wrapping = fyne.TextWrapOff
	route := container.NewBorder(nil, nil, fixedWidth(140, heading), actionRow(c.status, c.retry), bounded(9999, c.picker))
	meter := container.NewBorder(nil, nil, widget.NewLabel("电平"), nil, fixedWidth(94, c.meter))
	c.signal = actionRow(fixedWidth(140, meter), widget.NewLabel("音量"), fixedWidth(112, c.gain), fixedWidth(62, c.percent), widget.NewLabel("%"), c.mute, c.reset, c.latency)
	c.card = panel(route, c.signal, c.running, c.failure)
	return c
}
func (c *directionControl) refresh(d transport.Direction, active, busy bool, latency string) {
	status := stateLabel(d.State)
	if !active {
		status = "待机"
	}
	if d.Muted {
		status = "已静音"
	}
	if d.DeviceState == "released" && active {
		status = "已释放"
	}
	if d.Error != "" {
		status = "设备故障"
	}
	changedDevice := c.picker.Selected != d.Device && active && d.Device != ""
	if changedDevice {
		status = "待应用"
		if d.Error != "" {
			status = "设备故障 · 待应用"
		}
	}
	setLabelText(c.status, status)
	showWhen(c.status, !(d.State == "streaming" && !d.Muted && d.Error == "" && c.picker.Selected == d.Device && active))
	if changedDevice {
		setLabelText(c.running, "运行中："+d.Device)
	}
	showWhen(c.running, changedDevice)
	showWhen(c.retry, d.Error != "")
	setLabelText(c.latency, latency)
	c.meter.SetValue(float64(d.Audio.Level))
	if !c.working && !c.pending {
		c.syncing = true
		c.gain.SetValue(d.Gain)
		c.mute.SetChecked(d.Muted)
		c.syncing = false
	}
	showWhen(c.signal, active)
	enabled := !busy && active && d.Device != "" && d.Error == "" && d.State != "reconnecting" && d.State != "disabled"
	enableWhen(c.gain, enabled)
	enableWhen(c.mute, enabled)
	enableWhen(c.reset, enabled)
	enableWhen(c.percent, enabled)
	setLabelText(c.failure, d.Error)
	showWhen(c.failure, d.Error != "")
}

func (v *View) preferencesPage() fyne.CanvasObject {
	network := actionRow(widget.NewLabel("监听"), fixedWidth(190, v.form.network), widget.NewLabel("HTTPS"), fixedWidth(74, v.form.https), widget.NewLabel("音频端口"), fixedWidth(74, v.form.udp))
	v.scopeActions("network", func() { v.resetScope("network") })
	appearance, loadErr := loadAppearance(v.dir)
	themeWarning := wrap("")
	themeWarning.Importance = widget.DangerImportance
	themeWarning.Hide()
	if loadErr != nil {
		themeWarning.SetText("无法读取主题设置：" + loadErr.Error())
		themeWarning.Show()
	}
	themeChoice := widget.NewSelect(appearanceNames, nil)
	themeChoice.SetSelected(appearance)
	if current, ok := fyne.CurrentApp().Settings().Theme().(desktopTheme); ok {
		current.variant = appearance
		fyne.CurrentApp().Settings().SetTheme(current)
	}
	themeChoice.OnChanged = func(value string) {
		fyne.CurrentApp().Settings().SetTheme(desktopTheme{font: findThemeFont(), variant: value})
		if err := saveAppearance(v.dir, value); err != nil {
			themeWarning.SetText("主题已切换，但无法保存设置：" + err.Error())
			themeWarning.Show()
		} else {
			themeWarning.Hide()
		}
	}
	themeChoice.PlaceHolder = "跟随系统"
	drivers := widget.NewButtonWithIcon("管理驱动…", theme.SettingsIcon(), v.openDrivers)
	about := widget.NewAccordion(widget.NewAccordionItem("关于与帮助", v.aboutPage()))
	body := container.NewVBox(sectionHeading("窗口"), v.trayControls(), actionRow(widget.NewLabel("主题"), fixedWidth(120, themeChoice)), themeWarning, widget.NewSeparator(), sectionHeading("网络"), network, widget.NewSeparator(), container.NewBorder(nil, nil, widget.NewLabel("虚拟音频驱动"), drivers), about)
	return container.NewVScroll(body)
}
func sectionHeading(text string) *widget.Label {
	return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}
