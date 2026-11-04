package ui

import (
	"fmt"
	"time"

	core "cnb.cool/tdh6/RMic/internal/app"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (v *View) pairingPage() fyne.CanvasObject {
	return container.NewPadded(container.NewVScroll(container.NewVBox(v.requests, widget.NewSeparator(), v.devices)))
}

func (v *View) refreshPairing(s core.Snapshot) {
	if v.deviceAction != nil {
		title := "设备"
		if len(s.Requests) > 0 {
			title = fmt.Sprintf("设备 (%d)", len(s.Requests))
		}
		v.deviceAction.SetText(title)
	}
	if v.pairDialog != nil {
		valid := false
		for _, r := range s.Requests {
			if r.ID == v.pairRequestID && time.Now().Before(r.Expires) {
				valid = true
			}
		}
		if !valid {
			for _, button := range v.pairApprove {
				button.Disable()
			}
		}
	}
	if len(s.Requests) > 0 {
		v.pairAction.SetText(fmt.Sprintf("核对 %d 个配对请求", len(s.Requests)))
		v.homeRequests.Show()
	} else {
		v.homeRequests.Hide()
	}
	signature := fmt.Sprint(s.Requests, s.Devices)
	if signature == v.pairSignature {
		return
	}
	v.pairSignature = signature
	v.requests.RemoveAll()
	v.devices.RemoveAll()
	if len(s.Requests) == 0 {
		v.requests.Add(widget.NewLabel("暂无配对申请"))
	}
	for _, request := range s.Requests {
		r := request
		v.requests.Add(widget.NewButton(compactName(r.Name, 16)+" · "+compactName(r.Address, 24)+" · 核对申请", func() {
			if v.busy {
				return
			}
			var d dialog.Dialog
			decide := func(allow, once bool) func() {
				return func() {
					d.Hide()
					if !time.Now().Before(r.Expires) {
						v.notice.SetText("所选申请已过期，请重新申请并核对确认码。")
						return
					}
					v.perform("正在处理配对申请…", func() error { return v.backend.Decide(r.ID, allow, once) }, nil)
				}
			}
			text := r.Code
			if len(text) == 6 {
				text = text[:3] + "  " + text[3:]
			}
			code := canvas.NewText(text, theme.ForegroundColor())
			code.Alignment = fyne.TextAlignCenter
			code.TextSize = 32
			code.TextStyle = fyne.TextStyle{Bold: true}
			once := widget.NewButton("仅本次允许", decide(true, true))
			remember := widget.NewButton("允许并记住", decide(true, false))
			if !time.Now().Before(r.Expires) {
				once.Disable()
				remember.Disable()
			}
			v.pairApprove = []*widget.Button{once, remember}
			v.pairRequestID = r.ID
			d = dialog.NewCustom("核对手机上的确认码", "取消", container.NewVBox(
				code, wrap(fmt.Sprintf("设备：%s\n来源：%s\n有效至：%s", r.Name, r.Address, r.Expires.Format("15:04:05"))),
				wrap("两端确认码一致后再允许。记住授权还需手机选择记住。"),
				container.NewHBox(widget.NewButton("拒绝", decide(false, false)), once, remember)), v.window)
			v.pairDialog = d
			d.SetOnClosed(func() { v.pairDialog = nil; v.pairApprove = nil })
			d.Show()
		}))
	}
	if len(s.Devices) == 0 {
		v.devices.Add(actionRow(widget.NewLabel("暂无已授权设备"), widget.NewButton("连接手机", func() { v.closeTool() })))
	}
	for _, device := range s.Devices {
		d := device
		kind := "仅本次"
		if d.Remember {
			kind = "已记住"
		}
		revoke := widget.NewButton("撤销授权…", func() {
			dialog.ShowConfirm("撤销授权", "撤销 "+d.Name+" 的授权并停止对应活动连接？", func(ok bool) {
				if ok {
					v.perform("正在撤销授权…", func() error { return v.backend.Revoke(d.ID) }, nil)
				}
			}, v.window)
		})
		name := widget.NewButton(compactName(d.Name, 24), func() { dialog.ShowInformation("授权设备", d.Name+"\n授权类型："+kind, v.window) })
		name.Importance = widget.LowImportance
		name.Alignment = widget.ButtonAlignLeading
		v.devices.Add(container.NewBorder(nil, nil, actionRow(fixedWidth(240, name), widget.NewLabel(kind)), revoke))
	}
}
