package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The script remains the canonical representation. Structured edits preserve
// unknown key expressions and the advanced editor permits exact round trips.
func shortcutSteps(script *widget.Entry, window fyne.Window) fyne.CanvasObject {
	rows := container.NewVBox()
	updating := false
	var rebuild func()
	split := func() []string {
		if script.Text == "" {
			return nil
		}
		return strings.Split(script.Text, "\n")
	}
	set := func(lines []string) { updating = true; script.SetText(strings.Join(lines, "\n")); updating = false }
	rebuild = func() {
		rows.RemoveAll()
		for index, line := range split() {
			i := index
			kind := widget.NewSelect([]string{"按键", "文本", "等待 ms"}, nil)
			value := widget.NewEntry()
			kind.SetSelected("按键")
			value.SetText(line)
			if strings.HasPrefix(line, "text:") {
				kind.SetSelected("文本")
				value.SetText(strings.TrimPrefix(line, "text:"))
			}
			if strings.HasPrefix(line, "wait:") {
				kind.SetSelected("等待 ms")
				value.SetText(strings.TrimPrefix(line, "wait:"))
			}
			value.SetPlaceHolder("例如 ControlLeft+KeyC")
			update := func() {
				lines := split()
				if i >= len(lines) {
					return
				}
				prefix := ""
				switch kind.Selected {
				case "文本":
					prefix = "text:"
				case "等待 ms":
					prefix = "wait:"
				}
				lines[i] = prefix + value.Text
				set(lines)
			}
			chord := widget.NewButton("组合键", func() { editChord(value, window) })
			showWhen(chord, kind.Selected == "按键")
			kind.OnChanged = func(string) { update(); showWhen(chord, kind.Selected == "按键") }
			value.OnChanged = func(string) { update() }
			up := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
				lines := split()
				if i > 0 && i < len(lines) {
					lines[i-1], lines[i] = lines[i], lines[i-1]
					set(lines)
					rebuild()
				}
			})
			if i == 0 {
				up.Disable()
			}
			remove := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				lines := split()
				if i < len(lines) {
					set(append(lines[:i], lines[i+1:]...))
					rebuild()
				}
			})
			down := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() {
				lines := split()
				if i+1 < len(lines) {
					lines[i], lines[i+1] = lines[i+1], lines[i]
					set(lines)
					rebuild()
				}
			})
			if i == len(split())-1 {
				down.Disable()
			}
			rows.Add(container.NewBorder(nil, nil, container.NewHBox(widget.NewLabel(fmt.Sprint(i+1)), kind), container.NewHBox(chord, up, down, remove), value))
		}
	}
	script.OnChanged = func(string) {
		if !updating {
			rebuild()
		}
	}
	rebuild()
	add := widget.NewSelect([]string{"按键", "文本", "等待 ms"}, nil)
	add.PlaceHolder = "添加步骤…"
	add.OnChanged = func(kind string) {
		if kind == "" {
			return
		}
		value := "ControlLeft+KeyC"
		if kind == "文本" {
			value = "text:"
		}
		if kind == "等待 ms" {
			value = "wait:100"
		}
		set(append(split(), value))
		rebuild()
		add.ClearSelected()
	}
	text := widget.NewButton("编辑文本", func() {
		draft := widget.NewMultiLineEntry()
		draft.SetText(script.Text)
		draft.SetMinRowsVisible(6)
		dialog.ShowCustomConfirm("步骤文本", "确定", "取消", bounded(460, draft), func(ok bool) {
			if ok {
				script.SetText(draft.Text)
			}
		}, window)
	})
	return container.NewBorder(actionRow(add, text), nil, nil, nil, container.NewVScroll(rows))
}
