package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type scopeBar struct {
	content      *fyne.Container
	status       *widget.Label
	apply, reset *widget.Button
	details      *widget.Button
	baseline     string
	errorText    string
}

func (v *View) scopeActions(scope string, reset func()) fyne.CanvasObject {
	if v.scopeBars == nil {
		v.scopeBars = make(map[string]*scopeBar)
	}
	bar := &scopeBar{status: wrap("")}
	bar.status.Wrapping = fyne.TextWrapOff
	bar.status.Truncation = fyne.TextTruncateEllipsis
	bar.details = widget.NewButtonWithIcon("", theme.InfoIcon(), func() { dialog.ShowInformation("设置未保存", bar.errorText, v.window) })
	bar.details.Hide()
	bar.apply = widget.NewButton("保存设置", func() { v.submitScope(scope) })
	bar.apply.Importance = widget.HighImportance
	bar.reset = widget.NewButton("还原", func() { reset(); bar.errorText = ""; v.updateScopeBars() })
	v.scopeBars[scope] = bar
	bar.content = container.NewBorder(nil, nil, nil, actionRow(bar.details, bar.reset, bar.apply), bar.status)
	bar.content.Hide()
	return bar.content
}

func (v *View) scopeDraft(scope string) string {
	f := v.form
	switch scope {
	case "audio":
		return fmt.Sprintf("%q", []string{f.mode.Selected, f.output.Selected, f.capture.Selected, f.quality.Selected, f.defaultMode.Selected, f.channels.Selected, f.idle.Text})
	case "network":
		return fmt.Sprintf("%q", []string{f.network.Selected, f.https.Text, f.udp.Text})
	case "shortcuts":
		if f.shortcuts != nil {
			c, err := f.shortcuts()
			return fmt.Sprintf("%#v/%v", c, err)
		}
	}
	return ""
}

func (v *View) updateScopeBars() {
	if v.form == nil {
		return
	}
	for scope, bar := range v.scopeBars {
		dirty := v.scopeDraft(scope) != bar.baseline
		enabled := dirty && !v.busy && !v.closing
		enableWhen(bar.apply, enabled)
		enableWhen(bar.reset, enabled)
		label := "保存"
		if v.snapshot.Running {
			label = "保存并应用"
		} else if scope == "audio" && !v.configured() {
			label = "保存并启动"
		}
		setButtonText(bar.apply, label)
		text := ""
		if dirty {
			text = "有未保存更改"
		}
		if v.busy {
			text = "正在处理，请稍候…"
		}
		if bar.errorText != "" {
			text = bar.errorText
		}
		if bar.errorText != "" {
			bar.details.Show()
			bar.status.Importance = widget.DangerImportance
		} else {
			bar.details.Hide()
			bar.status.Importance = widget.MediumImportance
		}
		bar.status.SetText(text)
		if dirty || bar.errorText != "" {
			bar.content.Show()
		} else {
			bar.content.Hide()
		}
	}
	v.updateToolActions()
	v.updateSetupActions()
}

// Preserve existing callbacks (including dependent field updates). Editing a
// field clears only that page's error, never another page's draft.
func (v *View) watchDrafts() {
	for scope, bar := range v.scopeBars {
		bar.baseline = v.scopeDraft(scope)
	}
	changed := func(scope string) {
		if b := v.scopeBars[scope]; b != nil {
			b.errorText = ""
		}
		v.updateScopeBars()
		v.updateHome()
		v.syncSetupFields()
	}
	f := v.form
	for _, entry := range []*widget.Entry{f.idle, f.https, f.udp} {
		e := entry
		scope := "audio"
		if e != f.idle {
			scope = "network"
		}
		old := e.OnChanged
		e.OnChanged = func(s string) {
			if old != nil {
				old(s)
			}
			changed(scope)
		}
	}
	for _, selectBox := range []*widget.Select{f.mode, f.output, f.capture, f.quality, f.defaultMode, f.channels, f.network} {
		s := selectBox
		scope := "audio"
		if s == f.network {
			scope = "network"
		}
		old := s.OnChanged
		s.OnChanged = func(value string) {
			if old != nil {
				old(value)
			}
			changed(scope)
		}
	}
	v.updateScopeBars()
}
