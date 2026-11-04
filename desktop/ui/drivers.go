package ui

import (
	"fmt"
	"strings"

	"cnb.cool/tdh6/RMic/internal/virtual"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type driverItem struct {
	info               virtual.DriverInfo
	checked            bool
	report             string
	selectInstall      *widget.Check
	name               *widget.Button
	status             *widget.Label
	description        *widget.Label
	install, uninstall *widget.Button
}

func (v *View) driverPage() fyne.CanvasObject {
	if v.driverContent != nil {
		return v.driverContent
	}
	if v.driverRows == nil {
		v.driverRows = container.NewVBox()
		v.driverProgress = wrap("")
		for _, provider := range virtual.DriverProviders() {
			item := &driverItem{info: provider, status: widget.NewLabel("未检测")}
			item.selectInstall = widget.NewCheck("", func(bool) { v.updateDriverActions() })
			item.name = widget.NewButton(provider.Name, func() {
				detail := strings.TrimSpace(item.info.Detail + "\n" + item.info.Error + "\n" + item.report)
				if detail == "" {
					detail = "尚未检测"
				}
				dialog.ShowInformation(item.info.Name, detail, v.window)
			})
			item.name.Importance = widget.LowImportance
			item.name.Alignment = widget.ButtonAlignLeading
			item.install = widget.NewButton("安装", func() { v.confirmDriver("install", []*driverItem{item}) })
			item.uninstall = widget.NewButton("卸载…", func() { v.confirmDriver("uninstall", []*driverItem{item}) })
			item.uninstall.Importance = widget.LowImportance
			description := widget.NewLabel(provider.Description)
			description.Truncation = fyne.TextTruncateEllipsis
			description.Wrapping = fyne.TextWrapOff
			item.description = description
			name := container.NewBorder(nil, nil, fixedWidth(156, item.name), nil, description)
			v.driverRows.Add(container.NewBorder(nil, nil, item.selectInstall, actionRow(fixedWidth(86, item.status), fixedWidth(58, item.install), fixedWidth(66, item.uninstall)), bounded(9999, name)))
			v.driverItems = append(v.driverItems, item)
		}
	}
	v.driverCheck = widget.NewButton("检测全部", v.inspectDrivers)
	v.driverInstall = widget.NewButton("安装所选", func() {
		var items []*driverItem
		for _, item := range v.driverItems {
			if item.selectInstall.Checked {
				items = append(items, item)
			}
		}
		v.confirmDriver("install", items)
	})
	header := actionRow(layout.NewSpacer(), v.driverCheck, v.driverInstall)
	v.updateDriverActions()
	v.driverContent = container.NewBorder(header, v.driverProgress, nil, nil, container.NewVScroll(v.driverRows))
	return v.driverContent
}
func (v *View) openDrivers() {
	footer := actionRow(layout.NewSpacer(), widget.NewButton("返回设置", v.openPreferences), widget.NewButton("关闭", v.closeTool))
	v.showTool("虚拟音频驱动", "", v.driverPage(), footer)
	if !v.driverChecked {
		v.inspectDrivers()
	}
}
func (v *View) inspectDrivers() {
	if v.busy || v.closing || v.backend == nil {
		return
	}
	v.driverScanning = true
	v.driverProgress.SetText("正在检测全部驱动…")
	var items []virtual.DriverInfo
	v.perform("正在检测驱动…", func() error { items = v.backend.InspectDrivers(); return nil }, func() {
		v.driverScanning = false
		v.acceptDriverInfo(items)
		v.driverProgress.SetText("检测完成；点击驱动名称查看详情。")
	})
}
func (v *View) acceptDriverInfo(infos []virtual.DriverInfo) {
	v.driverChecked = true
	for _, item := range v.driverItems {
		found := false
		for _, info := range infos {
			if item.info.Name == info.Name {
				item.info = info
				found = true
				break
			}
		}
		item.checked = found
		if !found {
			item.info.Error = "未返回此驱动的检测结果"
		}
		if item.info.Complete {
			item.selectInstall.SetChecked(false)
		}
	}
	v.updateDriverActions()
	v.updateSetupActions()
}
func (v *View) updateDriverActions() {
	if v.driverCheck == nil {
		return
	}
	busy := v.busy || v.closing
	selected := 0
	for _, item := range v.driverItems {
		label := "未检测"
		switch {
		case item.info.Error != "":
			label = "检测失败"
		case item.checked && item.info.Complete:
			label = "已安装"
		case item.checked && item.info.Installed:
			label = "不完整"
		case item.checked && item.info.ReadOnly:
			label = "需配置"
		case item.checked:
			label = "未安装"
		}
		if busy && v.driverScanning {
			label = "检测中…"
		}
		if busy && v.driverActive == item.info.Name {
			label = "安装中…"
			if v.driverOperation == "uninstall" {
				label = "卸载中…"
			}
		}
		item.status.SetText(label)
		description := item.info.Description
		if v.tool != nil && v.tool.name == "首次使用" && item.info.Prefix == "" && !item.info.Complete {
			description = "基础通道 · " + description
		}
		item.description.SetText(description)
		ready := !busy && item.checked && item.info.Error == "" && !item.info.ReadOnly
		enableWhen(item.install, ready && !item.info.Complete)
		enableWhen(item.uninstall, ready && item.info.Installed)
		enableWhen(item.selectInstall, ready && !item.info.Complete)
		item.uninstall.Show()
		item.install.Show()
		item.selectInstall.Show()
		if item.info.ReadOnly {
			item.install.Hide()
			item.uninstall.Hide()
			item.selectInstall.Hide()
		}
		if v.tool != nil && v.tool.name == "首次使用" {
			item.uninstall.Hide()
		}
		if item.selectInstall.Checked && !item.selectInstall.Disabled() {
			selected++
		}
	}
	enableWhen(v.driverCheck, !busy)
	enableWhen(v.driverInstall, !busy && selected > 0)
	canManage := false
	for _, item := range v.driverItems {
		canManage = canManage || !item.info.ReadOnly
	}
	if canManage {
		v.driverInstall.Show()
	} else {
		v.driverInstall.Hide()
	}
}
func (v *View) confirmDriver(operation string, items []*driverItem) {
	if v.busy || v.closing {
		return
	}
	var selected []*driverItem
	var names []string
	for _, item := range items {
		if !item.checked || item.info.Error != "" || item.info.ReadOnly {
			continue
		}
		if operation == "install" && item.info.Complete || operation == "uninstall" && !item.info.Installed {
			continue
		}
		selected = append(selected, item)
		names = append(names, item.info.Name)
	}
	if len(selected) == 0 {
		return
	}
	verb := "安装"
	if operation == "uninstall" {
		verb = "卸载"
	}
	message := verb + "以下驱动：\n" + strings.Join(names, "\n")
	if v.snapshot.Running {
		message = "将先停止服务并断开手机，再" + message
	}
	if operation == "uninstall" {
		message += "\n其他应用也可能受影响。"
	}
	if strings.Contains(selected[0].info.Name, "CABLE") || selected[0].info.Prefix == "steam-" {
		message += "\n请确认系统 UAC；原版安装器可能需要重启电脑。"
	}
	dialog.ShowConfirm(verb+"驱动", message, func(ok bool) {
		if ok {
			v.runDriverQueue(operation, selected)
		}
	}, v.window)
}

type driverQueueResult struct {
	infos   []virtual.DriverInfo
	reports map[string]string
	failed  error
}

func executeDriverQueue(backend Backend, operation string, items []virtual.DriverInfo, progress func(string, int)) (driverQueueResult, error) {
	result := driverQueueResult{reports: make(map[string]string)}
	if err := backend.Stop(); err != nil {
		return result, err
	}
	for i, item := range items {
		progress(item.Name, i+1)
		message, err := backend.Driver(item.Prefix + operation)
		result.reports[item.Name] = message
		if err != nil {
			result.reports[item.Name] += "\n" + err.Error()
			result.failed = err
			break
		}
	}
	result.infos = backend.InspectDrivers()
	return result, nil
}
func (v *View) runDriverQueue(operation string, items []*driverItem) {
	if v.busy || v.closing {
		return
	}
	providers := make([]virtual.DriverInfo, len(items))
	for i, item := range items {
		providers[i] = item.info
	}
	var result driverQueueResult
	v.perform("正在维护驱动，请留意安装器与 UAC…", func() error {
		var err error
		result, err = executeDriverQueue(v.backend, operation, providers, func(name string, index int) {
			fyne.Do(func() {
				if !v.closing {
					v.driverProgress.SetText(fmt.Sprintf("%s · %d/%d · 等待安装器完成", name, index, len(providers)))
					v.driverActive = name
					v.driverOperation = operation
					v.updateDriverActions()
				}
			})
		})
		return err
	}, func() {
		v.driverActive = ""
		for _, item := range v.driverItems {
			if report, ok := result.reports[item.info.Name]; ok {
				item.report = report
			}
		}
		v.acceptDriverInfo(result.infos)
		v.driverProgress.SetText("操作结束，请核实检测状态；重启提示可点击驱动名称查看。")
		if result.failed != nil {
			v.driverProgress.SetText("操作未完成；后续项目未执行，点击驱动名称查看原因。")
		}
		v.refreshDevices()
	})
}
