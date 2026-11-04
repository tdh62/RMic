package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type toolWindow struct {
	name, scope       string
	popup             *widget.PopUp
	status            *widget.Label
	feedback          *messageLabel
	apply, ok, cancel *widget.Button
	returnFocus       fyne.Focusable
	content           fyne.CanvasObject
}

func (v *View) showTool(name, scope string, content fyne.CanvasObject, footer fyne.CanvasObject) {
	returnFocus := v.window.Canvas().Focused()
	if v.tool != nil {
		v.rememberToolFocus(v.tool)
		returnFocus = v.tool.returnFocus
		v.tool.popup.Hide()
	}
	t := &toolWindow{name: name, scope: scope, returnFocus: returnFocus, content: content}
	v.tool = t
	closeButton := widget.NewButtonWithIcon("", theme.CancelIcon(), v.closeTool)
	closeButton.Importance = widget.LowImportance
	header := container.NewBorder(nil, nil, nil, closeButton, sectionHeading(name))
	if footer == nil {
		if scope == "" {
			footer = actionRow(layout.NewSpacer(), widget.NewButton("关闭", v.closeTool))
		} else {
			t.status = widget.NewLabel("")
			t.status.Truncation = fyne.TextTruncateEllipsis
			t.status.Wrapping = fyne.TextWrapOff
			t.cancel = widget.NewButton("取消", func() {
				if !v.busy {
					v.resetScope(scope)
					v.closeTool()
				}
			})
			t.apply = widget.NewButton("应用", func() { v.submitScope(scope) })
			t.ok = widget.NewButton("确定", func() {
				if v.busy {
					return
				}
				if v.scopeDraft(scope) == v.scopeBars[scope].baseline {
					v.closeTool()
					return
				}
				v.submitScopeAfter(scope, v.closeTool)
			})
			t.ok.Importance = widget.HighImportance
			footer = container.NewBorder(nil, nil, nil, actionRow(t.cancel, t.apply, t.ok), t.status)
		}
	}
	t.feedback = &messageLabel{Label: wrap("")}
	t.feedback.Wrapping = fyne.TextWrapOff
	t.feedback.Truncation = fyne.TextTruncateEllipsis
	t.feedback.Importance = widget.DangerImportance
	t.feedback.details = widget.NewButton("详情", func() { dialog.ShowInformation("操作结果", t.feedback.Text, v.window) })
	t.feedback.area = container.NewBorder(nil, nil, nil, t.feedback.details, t.feedback)
	t.feedback.SetText("")
	t.popup = widget.NewModalPopUp(container.NewBorder(header, container.NewVBox(t.feedback.area, footer), nil, nil, content), v.window.Canvas())
	size := v.window.Canvas().Size()
	t.popup.Resize(fyne.NewSize(min(660, max(300, size.Width-32)), min(420, max(240, size.Height-32))))
	t.popup.Show()
	focus := fyne.Focusable(closeButton)
	if previous := v.toolFocus[name]; previous != nil && containsFocus(content, previous) {
		focus = previous
	}
	v.window.Canvas().Focus(focus)
	v.window.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyEscape}, func(fyne.Shortcut) { v.handleToolEscape() })
	v.updateToolActions()
}
func containsFocus(root fyne.CanvasObject, target fyne.Focusable) bool {
	if target == nil || !root.Visible() {
		return false
	}
	if object, ok := root.(fyne.Focusable); ok && object == target {
		return true
	}
	switch object := root.(type) {
	case *fyne.Container:
		for _, child := range object.Objects {
			if containsFocus(child, target) {
				return true
			}
		}
	case *container.Scroll:
		return containsFocus(object.Content, target)
	}
	return false
}
func (v *View) rememberToolFocus(t *toolWindow) {
	focus := v.window.Canvas().Focused()
	if !containsFocus(t.content, focus) {
		return
	}
	if v.toolFocus == nil {
		v.toolFocus = make(map[string]fyne.Focusable)
	}
	v.toolFocus[t.name] = focus
}
func (v *View) handleToolEscape() {
	overlays := v.window.Canvas().Overlays().List()
	if v.tool != nil && len(overlays) > 0 && overlays[len(overlays)-1] == v.tool.popup {
		v.closeTool()
		return
	}
	if focused := v.window.Canvas().Focused(); focused != nil {
		focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	}
}
func (v *View) closeTool() {
	if v.tool == nil {
		return
	}
	t := v.tool
	v.rememberToolFocus(t)
	v.tool = nil
	t.popup.Hide()
	v.window.Canvas().RemoveShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyEscape})
	v.window.Canvas().Focus(t.returnFocus)
}
func (v *View) openPreferences() { v.showTool("程序设置", "network", v.preferences, nil) }
func (v *View) openAudioOptions() {
	if v.advanced == nil {
		v.advanced = container.NewVScroll(container.NewVBox(field("默认用途", fixedWidth(200, v.form.defaultMode)), actionRow(widget.NewLabel("采集声道"), fixedWidth(72, v.form.channels), widget.NewLabel("空闲释放"), fixedWidth(76, v.form.idle), widget.NewLabel("秒"))))
	}
	v.showTool("音频选项", "audio", v.advanced, nil)
}
func (v *View) updateToolActions() {
	t := v.tool
	if t == nil {
		return
	}
	message := ""
	if !v.busy {
		message = v.notice.Text
	}
	t.feedback.SetText(message)
	if t.scope == "" {
		return
	}
	bar := v.scopeBars[t.scope]
	dirty := v.scopeDraft(t.scope) != bar.baseline
	t.status.SetText("")
	if dirty {
		t.status.SetText("有未应用更改")
	}
	if bar.errorText != "" {
		t.status.SetText("设置未保存")
		if message == "" {
			t.feedback.SetText(bar.errorText)
		}
		t.status.Importance = widget.DangerImportance
	} else {
		t.status.Importance = widget.MediumImportance
	}
	enabled := !v.busy && !v.closing
	enableWhen(t.ok, enabled)
	enableWhen(t.cancel, enabled)
	enableWhen(t.apply, enabled && dirty)
}
func (v *View) resetScope(scope string) {
	switch scope {
	case "audio":
		v.form.loadAudio(v.saved)
	case "network":
		selectExact(v.form.network, v.saved.Network)
		v.form.https.SetText(fmt.Sprint(v.saved.HTTPSPort))
		v.form.udp.SetText(fmt.Sprint(v.saved.UDPPort))
	case "shortcuts":
		v.form.resetShortcuts(v.saved.Shortcuts)
	}
	if bar := v.scopeBars[scope]; bar != nil {
		bar.errorText = ""
	}
	v.updateActions()
}
