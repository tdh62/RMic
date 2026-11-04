package ui

import (
	"fmt"
	"image/color"

	"cnb.cool/tdh6/RMic/internal/buildinfo"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/skip2/go-qrcode"
)

func (v *View) audioPage() fyne.CanvasObject {
	v.start = widget.NewButtonWithIcon("启动服务", theme.MediaPlayIcon(), v.startSaved)
	v.start.Importance = widget.HighImportance
	v.stop = widget.NewButton("停止", func() {
		dialog.ShowConfirm("停止服务？", "手机将断开，连接地址暂时不可用。", func(ok bool) {
			if ok {
				v.perform("正在停止服务…", v.backend.Stop, nil)
			}
		}, v.window)
	})
	v.stop.Importance = widget.LowImportance
	v.qr = widget.NewButton("连接地址", v.showQR)
	v.disconnect = widget.NewButton("断开", func() { v.perform("正在断开手机…", v.backend.Disconnect, nil) })
	v.sessionAction = widget.NewButton("", v.showSession)
	v.sessionAction.Importance = widget.LowImportance
	v.deviceAction = widget.NewButtonWithIcon("设备", theme.ComputerIcon(), func() { v.showTool("设备与授权", "", v.pairingPage(), nil) })
	settings := widget.NewButtonWithIcon("", theme.SettingsIcon(), v.openPreferences)
	v.connection = container.NewVBox()
	v.homeError = &messageLabel{Label: wrap("")}
	v.homeError.Importance = widget.DangerImportance
	v.receiveControl = v.newDirectionControl(false)
	v.sendControl = v.newDirectionControl(true)
	v.pairAction = widget.NewButton("配对请求", func() {
		if len(v.snapshot.Requests) == 1 && len(v.requests.Objects) == 1 {
			v.requests.Objects[0].(*widget.Button).OnTapped()
			return
		}
		list := container.NewVScroll(v.requests)
		list.SetMinSize(fyne.NewSize(420, 200))
		dialog.ShowCustom("手机配对请求", "关闭", list, v.window)
	})
	v.homeRequests = actionRow(v.pairAction)
	toolbar := container.NewBorder(nil, nil, fixedWidth(104, v.state), actionRow(v.start, v.stop, v.qr, v.disconnect, v.deviceAction, settings), bounded(200, v.sessionAction))
	more := widget.NewButtonWithIcon("音频选项", theme.SettingsIcon(), v.openAudioOptions)
	more.Importance = widget.LowImportance
	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), v.refreshDevices)
	parameters := container.NewBorder(nil, nil, actionRow(widget.NewLabel("用途"), fixedWidth(168, v.form.mode), widget.NewLabel("默认音质"), fixedWidth(184, v.form.quality)), actionRow(refresh, more))
	content := container.NewVBox(v.connection, parameters, v.receiveControl.card, v.sendControl.card, v.form.warning, v.quickControls())
	return container.NewPadded(container.NewBorder(container.NewVBox(toolbar, v.homeError), v.scopeActions("audio", func() { v.resetScope("audio") }), nil, nil, container.NewVScroll(content)))
}

func (v *View) showQR() {
	if !v.snapshot.Running || len(v.snapshot.URLs) == 0 {
		return
	}
	image := canvas.NewImageFromImage(nil)
	image.FillMode = canvas.ImageFillContain
	image.ScaleMode = canvas.ImageScalePixels
	image.SetMinSize(fyne.NewSize(220, 220))
	background := canvas.NewRectangle(color.White)
	selectURL := widget.NewSelect(v.snapshot.URLs, func(value string) {
		qr, err := qrcode.New(value, qrcode.Medium)
		if err != nil {
			v.notice.SetText(err.Error())
			return
		}
		image.Image = qr.Image(220)
		image.Refresh()
	})
	selectURL.SetSelectedIndex(0)
	copyURL := widget.NewButton("复制连接地址", func() { v.window.Clipboard().SetContent(selectURL.Selected) })
	dialog.ShowCustom("连接手机", "关闭", container.NewVBox(container.NewBorder(nil, nil, nil, copyURL, selectURL), container.NewStack(background, image), wrap("首次访问可能需信任证书；配对请核对两端确认码。")), v.window)
}

func (v *View) aboutPage() fyne.CanvasObject {
	copyStatus := widget.NewButtonWithIcon("复制状态", theme.ContentCopyIcon(), func() {
		s := v.snapshot
		v.window.Clipboard().SetContent(fmt.Sprintf("RMic GUI %s\n运行：%t\n会话状态：%s\n协议：%s\n采集声道：%d\n", buildinfo.Version, s.Running, s.Status.State, s.Status.Transport, s.Status.CaptureChannels))
	})
	help := widget.NewButton("使用帮助", func() {
		dialog.ShowInformation("使用帮助",
			"选择音频设备并保存，启动服务后用手机扫码。在电脑核对确认码后允许连接，再在手机开始传音。\n\n手机麦克风：电脑应用选择对应的虚拟录音端。电脑声音：选择要采集的播放端或独立通道。\n\n托盘默认关闭；启用后关闭窗口继续传音，可从托盘恢复。退出会停止服务。\n\nLinux 中文需要 fonts-noto-cjk 或 FYNE_FONT。", v.window)
	})
	license := widget.NewButton("许可", func() {
		dialog.ShowInformation("第三方许可", "GUI 使用 Fyne（BSD-3-Clause）。完整许可文件随发行包提供。", v.window)
	})
	return container.NewVScroll(container.NewVBox(
		actionRow(widget.NewLabelWithStyle("RMic GUI "+buildinfo.Version, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), copyStatus),
		widget.NewLabel("Commit: "+buildinfo.Commit+"   构建："+buildinfo.Date),
		field("数据目录", wrap(v.dir)), actionRow(help, license)))
}
