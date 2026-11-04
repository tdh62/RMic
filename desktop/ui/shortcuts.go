package ui

import (
	"fmt"
	"strconv"

	"cnb.cool/tdh6/RMic/internal/shortcuts"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type shortcutEditor struct {
	enabled  *widget.Check
	interval *widget.Entry
	names    [3]*widget.Entry
	modes    [3]*widget.Select
	scripts  [3]*widget.Entry
	panels   [3]fyne.CanvasObject
	slots    [3]*widget.Button
}

func (v *View) initShortcutEditor() {
	e := &shortcutEditor{enabled: widget.NewCheck("启用", nil), interval: widget.NewEntry()}
	v.shortcut = e
	changed := func() {
		if bar := v.scopeBars["shortcuts"]; bar != nil {
			bar.errorText = ""
		}
		v.updateScopeBars()
		for i, b := range e.slots {
			if b != nil {
				name := e.names[i].Text
				if name == "" {
					name = "未配置"
				}
				b.SetText(fmt.Sprintf("%d  %s", i+1, compactName(name, 12)))
			}
		}
	}
	for i := range e.names {
		index := i
		e.names[i] = widget.NewEntry()
		e.modes[i] = widget.NewSelect([]string{"点击执行", "按住执行"}, nil)
		e.scripts[i] = widget.NewMultiLineEntry()
		steps := shortcutSteps(e.scripts[i], v.window)
		parameters := actionRow(widget.NewLabel("名称"), fixedWidth(220, e.names[i]), widget.NewLabel("触发"), fixedWidth(120, e.modes[i]))
		e.panels[i] = container.NewBorder(parameters, nil, nil, nil, steps)
		e.names[i].OnChanged = func(string) { changed() }
		e.modes[i].OnChanged = func(string) { changed() }
		old := e.scripts[index].OnChanged
		e.scripts[index].OnChanged = func(value string) {
			if old != nil {
				old(value)
			}
			changed()
		}
	}
	v.form.resetShortcuts = func(c shortcuts.Config) {
		e.enabled.SetChecked(c.Enabled)
		e.interval.SetText(strconv.Itoa(c.KeyIntervalMS))
		for i, b := range c.Buttons {
			e.names[i].SetText(b.Name)
			label := "点击执行"
			if b.Mode == "hold" {
				label = "按住执行"
			}
			e.modes[i].SetSelected(label)
			e.scripts[i].SetText(b.Action)
		}
	}
	v.form.shortcuts = func() (shortcuts.Config, error) {
		c := shortcuts.Config{Enabled: e.enabled.Checked}
		var err error
		c.KeyIntervalMS, err = strconv.Atoi(e.interval.Text)
		for i := range e.names {
			mode := "tap"
			if e.modes[i].Selected == "按住执行" {
				mode = "hold"
			}
			c.Buttons[i] = shortcuts.Button{Name: e.names[i].Text, Mode: mode, Action: e.scripts[i].Text}
		}
		if err != nil {
			return c, fmt.Errorf("按键间隔须为整数")
		}
		return c, c.Validate()
	}
	v.form.resetShortcuts(v.saved.Shortcuts)
	e.enabled.OnChanged = func(bool) { changed() }
	e.interval.OnChanged = func(string) { changed() }
	if !shortcuts.Supported() {
		e.enabled.Disable()
	}
}
func (v *View) quickControls() fyne.CanvasObject {
	e := v.shortcut
	release := widget.NewButton("释放按键", func() { v.perform("正在释放按键…", v.backend.StopShortcuts, nil) })
	if !shortcuts.Supported() {
		release.Disable()
	}
	title := "手机端快捷按钮"
	if !shortcuts.Supported() {
		title += " · 仅 Windows"
	}
	header := container.NewBorder(nil, nil, actionRow(widget.NewLabel(title), e.enabled), release)
	buttons := container.NewGridWithColumns(3)
	for i := range e.slots {
		index := i
		name := e.names[i].Text
		if name == "" {
			name = "未配置"
		}
		e.slots[i] = widget.NewButton(fmt.Sprintf("%d  %s", i+1, compactName(name, 12)), func() { v.openShortcut(index) })
		if !shortcuts.Supported() {
			e.slots[i].Disable()
		}
		buttons.Add(e.slots[i])
	}
	return container.NewVBox(widget.NewSeparator(), header, buttons, v.scopeActions("shortcuts", func() { v.resetScope("shortcuts") }))
}
func (v *View) openShortcut(index int) {
	controls := actionRow(widget.NewLabel("按键间隔"), fixedWidth(76, v.shortcut.interval), widget.NewLabel("ms"), layout.NewSpacer(), widget.NewButton("释放按键", func() { v.perform("正在释放按键…", v.backend.StopShortcuts, nil) }))
	v.showTool(fmt.Sprintf("快捷按钮 %d", index+1), "shortcuts", container.NewBorder(controls, nil, nil, nil, v.shortcut.panels[index]), nil)
}
