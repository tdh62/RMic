package ui

import (
	"slices"
	"strings"

	core "cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/audio"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func (v *View) openSetup() {
	v.setupOffered = true
	v.setupStep = 0
	v.renderSetup()
	if !v.driverChecked {
		v.inspectDrivers()
	}
}
func (v *View) renderSetup() {
	progress := sectionHeading("1  准备驱动     →     2  选择设备")
	later := widget.NewButton("稍后设置", v.closeTool)
	var content fyne.CanvasObject
	if v.setupStep == 0 {
		content = v.driverPage()
		v.setupNext = widget.NewButton("下一步", func() {
			if !v.busy {
				v.setupStep = 1
				v.renderSetup()
			}
		})
		existing := widget.NewButton("使用现有设备", func() {
			if !v.busy {
				v.setupStep = 1
				v.renderSetup()
			}
		})
		v.setupFooter = actionRow(layout.NewSpacer(), later, existing, v.setupNext)
	} else {
		progress.SetText("✓  准备驱动     →     2  选择设备")
		if v.setupRoutes == nil {
			v.initSetupRoutes()
		}
		v.syncSetupFields()
		content = container.NewVScroll(v.setupRoutes)
		back := widget.NewButton("上一步", func() {
			if !v.busy {
				v.setupStep = 0
				v.renderSetup()
			}
		})
		v.setupNext = widget.NewButton("保存并启动", func() { v.submitScopeAfter("audio", v.closeTool) })
		v.setupFooter = actionRow(layout.NewSpacer(), back, later, v.setupNext)
	}
	v.setupNext.Importance = widget.HighImportance
	v.setupBody = container.NewBorder(progress, nil, nil, nil, content)
	v.showTool("首次使用", "", v.setupBody, v.setupFooter)
	v.updateDriverActions()
	v.updateSetupActions()
}
func (v *View) initSetupRoutes() {
	v.setupMode = widget.NewSelect(modes, func(value string) {
		if !v.setupSync {
			v.form.mode.SetSelected(value)
		}
	})
	v.setupOutput = widget.NewSelect(nil, func(value string) {
		if !v.setupSync {
			v.form.output.SetSelected(value)
		}
	})
	v.setupCapture = widget.NewSelect(nil, func(value string) {
		if !v.setupSync {
			v.form.capture.SetSelected(value)
		}
	})
	v.setupOutput.PlaceHolder = "选择设备…"
	v.setupCapture.PlaceHolder = "选择设备…"
	v.setupWarning = wrap("")
	v.setupWarning.Importance = widget.DangerImportance
	use := widget.NewButton("使用检测到的独立通道", func() {
		output, capture := suggestedRoutes(v.deviceList)
		mode := modeValue(v.form.mode.Selected)
		if mode.Microphone() && output != "" {
			selectExact(v.form.output, output)
		}
		if mode.Playback() && capture != "" {
			selectExact(v.form.capture, capture)
		}
	})
	refresh := widget.NewButton("刷新设备", v.refreshDevices)
	v.setupReceiveRow = field("手机送入", bounded(9999, v.setupOutput))
	v.setupSendRow = field("电脑来源", bounded(9999, v.setupCapture))
	v.setupSuggest = use
	v.setupRoutes = container.NewVBox(actionRow(widget.NewLabel("用途"), fixedWidth(168, v.setupMode), layout.NewSpacer(), refresh), v.setupReceiveRow, v.setupSendRow, use, v.setupWarning)
}
func (v *View) syncSetupFields() {
	if v.setupMode == nil || v.setupSync {
		return
	}
	v.setupSync = true
	defer func() { v.setupSync = false }()
	v.setupMode.SetSelected(v.form.mode.Selected)
	v.setupOutput.SetOptions(v.form.output.Options)
	selectExact(v.setupOutput, v.form.output.Selected)
	v.setupCapture.SetOptions(v.form.capture.Options)
	selectExact(v.setupCapture, v.form.capture.Selected)
	mode := modeValue(v.form.mode.Selected)
	showWhen(v.setupReceiveRow, mode.Microphone())
	showWhen(v.setupSendRow, mode.Playback())
	output, capture := suggestedRoutes(v.deviceList)
	showWhen(v.setupSuggest, (!mode.Microphone() || output != "") && (!mode.Playback() || capture != ""))
}
func (v *View) updateSetupActions() {
	if v.tool == nil || v.tool.name != "首次使用" || v.setupNext == nil {
		return
	}
	v.setupNext.Disable()
	if v.busy || v.closing {
		return
	}
	if v.setupStep == 0 {
		for _, item := range v.driverItems {
			if item.checked && item.info.Complete && item.info.Error == "" {
				v.setupNext.Enable()
				break
			}
		}
		return
	}
	mode := modeValue(v.form.mode.Selected)
	ready := (!mode.Microphone() || slices.Contains(v.deviceList.Outputs, v.form.output.Selected)) && (!mode.Playback() || slices.Contains(v.deviceList.Captures, v.form.capture.Selected))
	if ready {
		v.setupNext.Enable()
	}
	message := v.deviceList.Warning
	if bar := v.scopeBars["audio"]; bar != nil && bar.errorText != "" {
		message = bar.errorText
	}
	if mode.Microphone() && len(v.deviceList.Outputs) == 0 {
		message = "未检测到可用的麦克风送入设备。请返回安装驱动，或启用现有设备后刷新。"
	}
	if mode.Playback() && len(v.deviceList.Captures) == 0 {
		message = "未检测到可用的电脑声音来源。请启用设备或准备独立通道后刷新。"
	}
	v.setupWarning.SetText(message)
	showWhen(v.setupWarning, message != "")
}

// Suggestions use only identities actually returned by discovery. Applying them
// requires an explicit click; no physical default or guessed name is substituted.
func suggestedRoutes(devices core.Devices) (output, capture string) {
	find := func(names []string, match func(string) bool) string {
		var matches []string
		for _, name := range names {
			if match(name) {
				matches = append(matches, name)
			}
		}
		if len(matches) == 1 {
			return matches[0]
		}
		return ""
	}
	if devices.Windows {
		output = find(devices.Outputs, func(name string) bool { return strings.Contains(strings.ToLower(name), "steam streaming microphone") })
		capture = find(devices.Captures, func(name string) bool {
			return strings.HasPrefix(name, audio.LoopbackPrefix) && strings.Contains(strings.ToLower(name), "steam streaming speakers")
		})
		if output != "" && capture != "" && audio.ValidateWindowsDuplex(output, capture) != nil {
			capture = ""
		}
	} else if devices.MacOS {
		output = find(devices.Outputs, func(name string) bool { return name == "BlackHole 2ch" })
		capture = find(devices.Captures, func(name string) bool { return name == "BlackHole 16ch" })
	} else {
		output = find(devices.Outputs, func(name string) bool { return name == "RMic_Virtual_Output" })
		capture = find(devices.Captures, func(name string) bool { return name == "RMic_Computer_Capture" })
	}
	return
}
